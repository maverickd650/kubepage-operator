package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
	"github.com/maverickd650/kubepage-operator/internal/render"
)

const instanceFinalizer = "page.kubepage.dev/finalizer"

const instanceContainerName = "instance"

const (
	// configVolumeName/configMountPath mount the rendered config ConfigMap
	// into the homepage container at the path it reads config files from.
	configVolumeName = "config"
	configMountPath  = "/app/config"

	// configHashAnnotation records a hash of the rendered config on the pod
	// template, so a Configuration change (which only touches the ConfigMap)
	// still triggers a rollout even though homepage also hot-reloads files
	// on its own.
	configHashAnnotation = "page.kubepage.dev/config-hash"
)

// Definitions to manage status conditions
const (
	// typeAvailableInstance represents the status of the Deployment reconciliation
	typeAvailableInstance = "Available"
	// typeDegradedInstance represents the status used when the custom resource is deleted and the finalizer operations are yet to occur.
	typeDegradedInstance = "Degraded"
)

// InstanceReconciler reconciles a Instance object
type InstanceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html

// +kubebuilder:rbac:groups=page.kubepage.dev,resources=instances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=instances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=instances/finalizers,verbs=update
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=configurations,verbs=get;list;watch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=serviceentries,verbs=get;list;watch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=bookmarks,verbs=get;list;watch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=infowidgets,verbs=get;list;watch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// It is essential for the controller's reconciliation loop to be idempotent. By following the Operator
// pattern you will create Controllers which provide a reconcile function
// responsible for synchronizing resources until the desired state is reached on the cluster.
// Breaking this recommendation goes against the design principles of controller-runtime.
// and may lead to unforeseen consequences such as resources becoming stuck and requiring manual intervention.
// For further info:
// - About Operator Pattern: https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
// - About Controllers: https://kubernetes.io/docs/concepts/architecture/controller/
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/reconcile
func (r *InstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the Instance instance
	// The purpose is check if the Custom Resource for the Kind Instance
	// is applied on the cluster if not we return nil to stop the reconciliation
	instance := &pagev1alpha1.Instance{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			log.Info("Instance resource not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get instance")
		return ctrl.Result{}, err
	}

	if len(instance.Status.Conditions) == 0 {
		meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance, Status: metav1.ConditionUnknown, Reason: reasonReconciling, Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, instance); err != nil {
			log.Error(err, "Failed to update Instance status")
			return ctrl.Result{}, err
		}

		// Let's re-fetch the instance Custom Resource after updating the status
		// so that we have the latest state of the resource on the cluster and we will avoid
		// raising the error "the object has been modified, please apply
		// your changes to the latest version and try again" which would re-trigger the reconciliation
		// if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
			log.Error(err, "Failed to re-fetch instance")
			return ctrl.Result{}, err
		}
	}

	// Let's add a finalizer. Then, we can define some operations which should
	// occur before the custom resource is deleted.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(instance, instanceFinalizer) {
		log.Info("Adding finalizer for Instance")
		controllerutil.AddFinalizer(instance, instanceFinalizer)
		if err = r.Update(ctx, instance); err != nil {
			log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the Instance instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isInstanceMarkedToBeDeleted := instance.GetDeletionTimestamp() != nil
	if isInstanceMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(instance, instanceFinalizer) {
			log.Info("Performing finalizer operations for Instance before deleting CR")

			// Let's add here a status "Downgrade" to reflect that this resource began its process to be terminated.
			meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeDegradedInstance,
				Status: metav1.ConditionUnknown, Reason: reasonFinalizing,
				Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s ", instance.Name)})

			if err := r.Status().Update(ctx, instance); err != nil {
				log.Error(err, "Failed to update Instance status")
				return ctrl.Result{}, err
			}

			// Perform all operations required before removing the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			r.doFinalizerOperationsForInstance(instance)

			// TODO(user): If you add operations to the doFinalizerOperationsForInstance method
			// then you need to ensure that all worked fine before deleting and updating the Downgrade status
			// otherwise, you should requeue here.

			// Re-fetch the instance Custom Resource before updating the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raising the error "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
				log.Error(err, "Failed to re-fetch instance")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeDegradedInstance,
				Status: metav1.ConditionTrue, Reason: reasonFinalizing,
				Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", instance.Name)})

			if err := r.Status().Update(ctx, instance); err != nil {
				log.Error(err, "Failed to update Instance status")
				return ctrl.Result{}, err
			}

			log.Info("Removing finalizer for Instance after successfully performing the operations")
			if ok := controllerutil.RemoveFinalizer(instance, instanceFinalizer); !ok {
				err = fmt.Errorf("finalizer for Instance was not removed")
				log.Error(err, "Failed to remove finalizer for Instance")
				return ctrl.Result{}, err
			}

			if err := r.Update(ctx, instance); err != nil {
				log.Error(err, "Failed to remove finalizer for Instance")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Render the bound Configuration (if any) into homepage's config files and
	// ensure the owned ConfigMap matches. This happens before the Deployment
	// so its pod template's config-hash annotation always reflects the
	// ConfigMap content it's about to be (or already is) mounting.
	rc, configHash, err := r.reconcileConfigMap(ctx, instance)
	if err != nil {
		log.Error(err, "Failed to reconcile ConfigMap for Instance")

		meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance,
			Status: metav1.ConditionFalse, Reason: reasonReconciling,
			Message: fmt.Sprintf("Failed to render configuration for the custom resource (%s): (%s)", instance.Name, err)})

		if serr := r.Status().Update(ctx, instance); serr != nil {
			log.Error(serr, "Failed to update Instance status")
			return ctrl.Result{}, serr
		}

		return ctrl.Result{}, err
	}

	// Define the Deployment we want on the cluster, create it if it doesn't
	// exist yet, or reconcile drift (replica count or pod template, including
	// the config-hash annotation) if it does.
	result, handled, err := r.reconcileDeployment(ctx, instance, configHash, rc.secretSources, rc.secretEnv)
	if handled {
		return result, err
	}

	// Ensure the homepage Service (always) and Ingress (only if the user
	// opted in via spec.ingress.enabled) match the desired state.
	if err := r.reconcileService(ctx, instance); err != nil {
		return r.failAvailable(ctx, instance, "Service", err)
	}
	if err := r.reconcileIngress(ctx, instance); err != nil {
		return r.failAvailable(ctx, instance, "Ingress", err)
	}

	// If the size is not defined in the Custom Resource then we will set the desired replicas to 0
	var desiredReplicas int32 = 0
	if instance.Spec.Size != nil {
		desiredReplicas = *instance.Spec.Size
	}

	instance.Status.ObservedGeneration = instance.Generation
	instance.Status.BoundConfigurations = rc.boundCounts.configurations
	instance.Status.BoundServiceEntries = rc.boundCounts.serviceEntries
	instance.Status.BoundBookmarks = rc.boundCounts.bookmarks
	instance.Status.BoundInfoWidgets = rc.boundCounts.infoWidgets
	instance.Status.RenderHash = configHash

	// The following implementation will update the status
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance,
		Status: metav1.ConditionTrue, Reason: reasonReconciling,
		Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", instance.Name, desiredReplicas)})

	if err := r.Status().Update(ctx, instance); err != nil {
		log.Error(err, "Failed to update Instance status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// failAvailable sets the Available condition to False with err's message
// (identifying which resource kind failed) and updates status, returning the
// error so the caller can return it from Reconcile unchanged.
func (r *InstanceReconciler) failAvailable(ctx context.Context, instance *pagev1alpha1.Instance, resource string, err error) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Error(err, fmt.Sprintf("Failed to reconcile %s for Instance", resource))

	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance,
		Status: metav1.ConditionFalse, Reason: reasonReconciling,
		Message: fmt.Sprintf("Failed to reconcile %s for the custom resource (%s): (%s)", resource, instance.Name, err)})

	if serr := r.Status().Update(ctx, instance); serr != nil {
		log.Error(serr, "Failed to update Instance status")
		return ctrl.Result{}, serr
	}

	return ctrl.Result{}, err
}

// finalizeInstance will perform the required operations before delete the CR.
func (r *InstanceReconciler) doFinalizerOperationsForInstance(cr *pagev1alpha1.Instance) {
	// TODO(user): Add the cleanup steps that the operator
	// needs to do before the CR can be deleted. Examples
	// of finalizers include performing backups and deleting
	// resources that are not owned by this CR, like a PVC.

	// Note: It is not recommended to use finalizers with the purpose of deleting resources which are
	// created and managed in the reconciliation. These ones, such as the Deployment created on this reconcile,
	// are defined as dependent of the custom resource. See that we use the method ctrl.SetControllerReference.
	// to set the ownerRef which means that the Deployment will be deleted by the Kubernetes API.
	// More info: https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/

	// The following implementation will raise an event
	r.Recorder.Eventf(cr, nil, corev1.EventTypeWarning, "Deleting", "DeleteCR",
		"Custom Resource %s is being deleted from the namespace %s",
		cr.Name,
		cr.Namespace)
}

// renderConfigForInstance renders every homepage config file from the config
// CRDs bound to instance: settings.yaml (Configuration), services.yaml
// (ServiceEntry), bookmarks.yaml (Bookmark), widgets.yaml (InfoWidget), and
// kubernetes.yaml (disabled unless an InfoWidget of type "kubernetes" is
// bound, per the operator's CRD-only discovery posture, D5). It returns the
// ConfigMap data ready to write as-is.
// renderedConfig is the result of rendering all of an Instance's bound
// config CRDs: the ConfigMap data, plus whatever the Deployment needs to
// back any secret references those CRDs made (volume sources + env vars).
type renderedConfig struct {
	data          map[string]string
	secretSources []corev1.VolumeProjection
	secretEnv     []corev1.EnvVar
	boundCounts   boundCounts
}

// boundCounts is how many of each config CRD kind are currently bound to an
// Instance, surfaced in its status (see pagev1alpha1.InstanceStatus).
type boundCounts struct {
	configurations int32
	serviceEntries int32
	bookmarks      int32
	infoWidgets    int32
}

func (r *InstanceReconciler) renderConfigForInstance(ctx context.Context, instance *pagev1alpha1.Instance) (renderedConfig, error) {
	log := logf.FromContext(ctx)
	data := map[string]string{}
	projection := newSecretProjection()

	var configs pagev1alpha1.ConfigurationList
	if err := r.List(ctx, &configs, client.InNamespace(instance.Namespace)); err != nil {
		return renderedConfig{}, fmt.Errorf("listing Configurations: %w", err)
	}

	var bound []pagev1alpha1.Configuration
	for _, c := range configs.Items {
		if c.Spec.InstanceRef.Name == instance.Name {
			bound = append(bound, c)
		}
	}
	if len(bound) > 0 {
		// Exactly one Configuration is expected per Instance. If more than one
		// references the same Instance, pick deterministically (lexicographically
		// first by name) rather than erroring the whole reconcile, and surface
		// the ambiguity via an event.
		slices.SortFunc(bound, func(a, b pagev1alpha1.Configuration) int { return strings.Compare(a.Name, b.Name) })
		if len(bound) > 1 {
			log.Info("Multiple Configurations reference this Instance; using the lexicographically first by name",
				"Instance", instance.Name, "chosen", bound[0].Name)
			r.Recorder.Eventf(instance, nil, corev1.EventTypeWarning, "AmbiguousConfiguration", "Reconcile",
				"Multiple Configuration objects reference Instance %s; using %s and ignoring the rest",
				instance.Name, bound[0].Name)
		}

		settingsYAML, err := render.Settings(&bound[0].Spec)
		if err != nil {
			return renderedConfig{}, fmt.Errorf("rendering settings.yaml: %w", err)
		}
		data["settings.yaml"] = string(settingsYAML)
	}

	var serviceEntries pagev1alpha1.ServiceEntryList
	if err := r.List(ctx, &serviceEntries, client.InNamespace(instance.Namespace)); err != nil {
		return renderedConfig{}, fmt.Errorf("listing ServiceEntries: %w", err)
	}

	var boundServices []pagev1alpha1.ServiceEntry
	for _, s := range serviceEntries.Items {
		if s.Spec.InstanceRef.Name == instance.Name {
			boundServices = append(boundServices, s)
		}
	}

	if len(boundServices) > 0 {
		// Deterministic rendering order: ServiceEntry names, not list order.
		slices.SortFunc(boundServices, func(a, b pagev1alpha1.ServiceEntry) int { return strings.Compare(a.Name, b.Name) })

		inputs, err := r.buildServiceInputs(ctx, instance.Namespace, boundServices, projection)
		if err != nil {
			return renderedConfig{}, fmt.Errorf("resolving ServiceEntries: %w", err)
		}

		servicesYAML, err := render.Services(inputs)
		if err != nil {
			return renderedConfig{}, fmt.Errorf("rendering services.yaml: %w", err)
		}
		data["services.yaml"] = string(servicesYAML)
	}

	var bookmarks pagev1alpha1.BookmarkList
	if err := r.List(ctx, &bookmarks, client.InNamespace(instance.Namespace)); err != nil {
		return renderedConfig{}, fmt.Errorf("listing Bookmarks: %w", err)
	}

	var boundBookmarks []pagev1alpha1.Bookmark
	for _, b := range bookmarks.Items {
		if b.Spec.InstanceRef.Name == instance.Name {
			boundBookmarks = append(boundBookmarks, b)
		}
	}
	if len(boundBookmarks) > 0 {
		// Deterministic rendering order: Bookmark names, not list order.
		slices.SortFunc(boundBookmarks, func(a, b pagev1alpha1.Bookmark) int { return strings.Compare(a.Name, b.Name) })

		inputs := make([]render.BookmarkInput, 0, len(boundBookmarks))
		for _, b := range boundBookmarks {
			inputs = append(inputs, render.BookmarkInput{
				Group:       b.Spec.Group,
				Name:        b.Spec.Name,
				Order:       b.Spec.Order,
				Href:        b.Spec.Href,
				Abbr:        b.Spec.Abbr,
				Icon:        b.Spec.Icon,
				Description: b.Spec.Description,
			})
		}

		bookmarksYAML, err := render.Bookmarks(inputs)
		if err != nil {
			return renderedConfig{}, fmt.Errorf("rendering bookmarks.yaml: %w", err)
		}
		data["bookmarks.yaml"] = string(bookmarksYAML)
	}

	var infoWidgets pagev1alpha1.InfoWidgetList
	if err := r.List(ctx, &infoWidgets, client.InNamespace(instance.Namespace)); err != nil {
		return renderedConfig{}, fmt.Errorf("listing InfoWidgets: %w", err)
	}

	var boundWidgets []pagev1alpha1.InfoWidget
	for _, w := range infoWidgets.Items {
		if w.Spec.InstanceRef.Name == instance.Name {
			boundWidgets = append(boundWidgets, w)
		}
	}

	// kubernetes.yaml stays disabled (D5: CRD-only discovery) unless an
	// InfoWidget of type "kubernetes" is bound, in which case homepage needs
	// its own cluster connection to fetch the stats that widget displays.
	// "cluster" mode (the in-cluster service account) is the only sensible
	// choice here since homepage always runs in-cluster in this operator.
	// Provisioning the RBAC the homepage pod needs for that connection
	// (nodes/pods/metrics.k8s.io get;list) is deliberately left to the
	// deployer rather than auto-granted by this controller — see
	// IMPLEMENTATION_PLAN.md's Risks section.
	kubeMode := "disabled"
	if slices.ContainsFunc(boundWidgets, func(w pagev1alpha1.InfoWidget) bool { return w.Spec.Type == "kubernetes" }) {
		kubeMode = "cluster"
	}
	kubeYAML, err := render.Kubernetes(kubeMode)
	if err != nil {
		return renderedConfig{}, fmt.Errorf("rendering kubernetes.yaml: %w", err)
	}
	data["kubernetes.yaml"] = string(kubeYAML)

	if len(boundWidgets) > 0 {
		// Deterministic rendering order: InfoWidget names, not list order.
		slices.SortFunc(boundWidgets, func(a, b pagev1alpha1.InfoWidget) int { return strings.Compare(a.Name, b.Name) })

		inputs, err := r.buildWidgetInputs(ctx, instance.Namespace, boundWidgets, projection)
		if err != nil {
			return renderedConfig{}, fmt.Errorf("resolving InfoWidgets: %w", err)
		}

		widgetsYAML, err := render.Widgets(inputs)
		if err != nil {
			return renderedConfig{}, fmt.Errorf("rendering widgets.yaml: %w", err)
		}
		data["widgets.yaml"] = string(widgetsYAML)
	}

	secretSources, secretEnv := projection.finalize()
	counts := boundCounts{
		configurations: int32(len(bound)),
		serviceEntries: int32(len(boundServices)),
		bookmarks:      int32(len(boundBookmarks)),
		infoWidgets:    int32(len(boundWidgets)),
	}
	return renderedConfig{data: data, secretSources: secretSources, secretEnv: secretEnv, boundCounts: counts}, nil
}

// configMapForInstance returns the ConfigMap object holding data, owned by instance.
func (r *InstanceReconciler) configMapForInstance(instance *pagev1alpha1.Instance, data map[string]string) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
		Data: data,
	}
	if err := ctrl.SetControllerReference(instance, cm, r.Scheme); err != nil {
		return nil, err
	}
	return cm, nil
}

// reconcileConfigMap renders instance's config and ensures the owned
// ConfigMap matches, creating or updating it as needed. It returns a hash of
// the rendered data for use as the Deployment's config-hash annotation.
func (r *InstanceReconciler) reconcileConfigMap(ctx context.Context, instance *pagev1alpha1.Instance) (renderedConfig, string, error) {
	log := logf.FromContext(ctx)

	rc, err := r.renderConfigForInstance(ctx, instance)
	if err != nil {
		return renderedConfig{}, "", err
	}

	cm, err := r.configMapForInstance(instance, rc.data)
	if err != nil {
		return renderedConfig{}, "", err
	}

	foundCM := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, foundCM)
	switch {
	case apierrors.IsNotFound(err):
		log.Info("Creating a new ConfigMap", "ConfigMap.Namespace", cm.Namespace, "ConfigMap.Name", cm.Name)
		if err := r.Create(ctx, cm); err != nil {
			return renderedConfig{}, "", fmt.Errorf("creating ConfigMap %s/%s: %w", cm.Namespace, cm.Name, err)
		}
	case err != nil:
		return renderedConfig{}, "", fmt.Errorf("getting ConfigMap %s/%s: %w", cm.Namespace, cm.Name, err)
	case !maps.Equal(foundCM.Data, cm.Data):
		foundCM.Data = cm.Data
		if err := r.Update(ctx, foundCM); err != nil {
			return renderedConfig{}, "", fmt.Errorf("updating ConfigMap %s/%s: %w", foundCM.Namespace, foundCM.Name, err)
		}
	}

	return rc, hashConfigData(rc.data), nil
}

// hashConfigData returns a deterministic short hash of data, used as the
// pod template's config-hash annotation value.
func hashConfigData(data map[string]string) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(data[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// reconcileDeployment ensures the Deployment for instance exists and matches
// the desired state (replicas, pod template incl. configHash annotation).
// handled is true when the caller should return (result, err) immediately;
// when false, the Deployment was already up to date and the caller should
// continue with its own logic (e.g. updating the Instance's status).
func (r *InstanceReconciler) reconcileDeployment(
	ctx context.Context,
	instance *pagev1alpha1.Instance,
	configHash string,
	secretSources []corev1.VolumeProjection,
	secretEnv []corev1.EnvVar,
) (result ctrl.Result, handled bool, err error) {
	log := logf.FromContext(ctx)

	desiredDep, err := r.deploymentForInstance(instance, configHash, secretSources, secretEnv)
	if err != nil {
		log.Error(err, "Failed to define new Deployment resource for Instance")

		meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance,
			Status: metav1.ConditionFalse, Reason: reasonReconciling,
			Message: fmt.Sprintf("Failed to create Deployment for the custom resource (%s): (%s)", instance.Name, err)})

		if serr := r.Status().Update(ctx, instance); serr != nil {
			log.Error(serr, "Failed to update Instance status")
			return ctrl.Result{}, true, serr
		}

		return ctrl.Result{}, true, err
	}

	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		log.Info("Creating a new Deployment",
			"Deployment.Namespace", desiredDep.Namespace, "Deployment.Name", desiredDep.Name)
		if err = r.Create(ctx, desiredDep); err != nil {
			log.Error(err, "Failed to create new Deployment",
				"Deployment.Namespace", desiredDep.Namespace, "Deployment.Name", desiredDep.Name)
			return ctrl.Result{}, true, err
		}

		// Deployment created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, true, nil
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-triggered again
		return ctrl.Result{}, true, err
	}

	// Reconcile drift: the Deployment exists, but its replica count or
	// config-hash annotation (set after a Configuration change) no longer
	// matches the desired state. We deliberately don't DeepEqual the whole
	// pod template against a freshly-built one: the API server fills in
	// defaults (RestartPolicy, DNSPolicy, etc.) on the stored object that a
	// bare struct literal never has, which would make every comparison show
	// spurious drift. The config-hash annotation is the one signal we fully
	// control and need: it only changes when rendered config actually does.
	replicasChanged := found.Spec.Replicas == nil || desiredDep.Spec.Replicas == nil || *found.Spec.Replicas != *desiredDep.Spec.Replicas
	hashChanged := found.Spec.Template.Annotations[configHashAnnotation] != configHash
	if !replicasChanged && !hashChanged {
		return ctrl.Result{}, false, nil
	}

	found.Spec.Replicas = desiredDep.Spec.Replicas
	found.Spec.Template = desiredDep.Spec.Template
	if err = r.Update(ctx, found); err != nil {
		log.Error(err, "Failed to update Deployment",
			"Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)

		// Re-fetch the instance Custom Resource before updating the status
		// so that we have the latest state of the resource on the cluster and we will avoid
		// raising the error "the object has been modified, please apply
		// your changes to the latest version and try again" which would re-trigger the reconciliation
		if gerr := r.Get(ctx, client.ObjectKeyFromObject(instance), instance); gerr != nil {
			log.Error(gerr, "Failed to re-fetch instance")
			return ctrl.Result{}, true, gerr
		}

		meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance,
			Status: metav1.ConditionFalse, Reason: "Resizing",
			Message: fmt.Sprintf("Failed to update the size for the custom resource (%s): (%s)", instance.Name, err)})

		if serr := r.Status().Update(ctx, instance); serr != nil {
			log.Error(serr, "Failed to update Instance status")
			return ctrl.Result{}, true, serr
		}

		return ctrl.Result{}, true, err
	}

	// Now that we've updated the Deployment we want to requeue the
	// reconciliation so that we can ensure that we have the latest state of
	// the resource before update. Also, it will help ensure the desired
	// state on the cluster
	return ctrl.Result{Requeue: true}, true, nil
}

// deploymentForInstance returns a Instance Deployment object
func (r *InstanceReconciler) deploymentForInstance(
	instance *pagev1alpha1.Instance,
	configHash string,
	secretSources []corev1.VolumeProjection,
	secretEnv []corev1.EnvVar,
) (*appsv1.Deployment, error) {
	ls := labelsForInstance()

	// Get the Operand image
	image, err := imageForInstance()
	if err != nil {
		return nil, err
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: instance.Spec.Size,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					// ls is layered on top of user labels so the selector
					// (which matches on ls) can never be broken by a
					// colliding user-supplied label.
					Labels: mergeStringMaps(instance.Spec.Labels, ls),
					// configHashAnnotation is layered on top of user
					// annotations so it always reflects the mounted
					// ConfigMap's actual content, triggering a rollout on
					// every Configuration change.
					Annotations: mergeStringMaps(instance.Spec.Annotations, map[string]string{
						configHashAnnotation: configHash,
					}),
				},
				Spec: corev1.PodSpec{
					HostUsers: instance.Spec.HostUsers,
					// TODO(user): Uncomment the following code to configure the nodeAffinity expression
					// according to the platforms which are supported by your solution. It is considered
					// best practice to support multiple architectures. build your manager image using the
					// makefile target docker-buildx. Also, you can use docker manifest inspect <image>
					// to check what are the platforms supported.
					// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#node-affinity
					// Affinity: &corev1.Affinity{
					//	 NodeAffinity: &corev1.NodeAffinity{
					//		 RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					//			 NodeSelectorTerms: []corev1.NodeSelectorTerm{
					//				 {
					//					 MatchExpressions: []corev1.NodeSelectorRequirement{
					//						 {
					//							 Key:      "kubernetes.io/arch",
					//							 Operator: "In",
					//							 Values:   []string{"amd64", "arm64", "ppc64le", "s390x"},
					//						 },
					//						 {
					//							 Key:      "kubernetes.io/os",
					//							 Operator: "In",
					//							 Values:   []string{"linux"},
					//						 },
					//					 },
					//				 },
					//		 	 },
					//		 },
					//	 },
					// },
					SecurityContext: mergeOverride(corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						// IMPORTANT: seccomProfile was introduced with Kubernetes 1.19
						// If you are looking for to produce solutions to be supported
						// on lower versions you must remove this option.
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					}, instance.Spec.PodSecurityContext),
					Containers: []corev1.Container{{
						Image:           image,
						Name:            instanceContainerName,
						ImagePullPolicy: corev1.PullIfNotPresent,
						// Ensure restrictive context for the container
						// More info: https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted
						SecurityContext: mergeOverride(corev1.SecurityContext{
							RunAsNonRoot:             ptr.To(true),
							RunAsUser:                ptr.To(int64(568)),
							AllowPrivilegeEscalation: ptr.To(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{
									"ALL",
								},
							},
						}, instance.Spec.ContainerSecurityContext),
						Ports: []corev1.ContainerPort{{
							ContainerPort: instance.Spec.ContainerPort,
							Name:          instanceContainerName,
						}},
						Env:            append(envForInstance(instance), secretEnv...),
						ReadinessProbe: instance.Spec.ReadinessProbe,
						LivenessProbe:  instance.Spec.LivenessProbe,
						Resources:      instance.Spec.Resources,
						VolumeMounts:   volumeMountsForInstance(secretSources),
					}},
					Volumes: volumesForInstance(instance, secretSources),
				},
			},
		},
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(instance, dep, r.Scheme); err != nil {
		return nil, err
	}
	return dep, nil
}

// envForInstance returns the homepage container env: AppURL/AllowedHosts as
// the homepage-specific env vars they map to, followed by any user-supplied
// Env entries (which take precedence if names collide, since they're appended last).
func envForInstance(instance *pagev1alpha1.Instance) []corev1.EnvVar {
	var env []corev1.EnvVar
	if instance.Spec.AppURL != "" {
		env = append(env, corev1.EnvVar{Name: "APP_URL", Value: instance.Spec.AppURL})
	}
	if instance.Spec.AllowedHosts != "" {
		env = append(env, corev1.EnvVar{Name: "HOMEPAGE_ALLOWED_HOSTS", Value: instance.Spec.AllowedHosts})
	}
	return append(env, instance.Spec.Env...)
}

// volumesForInstance returns the config ConfigMap volume, plus the
// aggregated secrets projected volume if any ServiceEntry widget referenced
// a Secret.
func volumesForInstance(instance *pagev1alpha1.Instance, secretSources []corev1.VolumeProjection) []corev1.Volume {
	volumes := []corev1.Volume{{
		Name: configVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: instance.Name},
			},
		},
	}}
	if len(secretSources) > 0 {
		volumes = append(volumes, corev1.Volume{
			Name: secretsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{Sources: secretSources},
			},
		})
	}
	return volumes
}

// volumeMountsForInstance mirrors volumesForInstance for the container side.
func volumeMountsForInstance(secretSources []corev1.VolumeProjection) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{{
		Name:      configVolumeName,
		MountPath: configMountPath,
	}}
	if len(secretSources) > 0 {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      secretsVolumeName,
			MountPath: secretsMountPath,
			ReadOnly:  true,
		})
	}
	return mounts
}

// mergeOverride layers override onto a copy of base, field by field: any
// pointer, slice, or map field set (non-nil) in override replaces the
// corresponding field in base, while every field left nil in override keeps
// base's value. This lets operator-enforced defaults (e.g. the security
// hardening below) survive a partial user-supplied PodSecurityContext or
// SecurityContext instead of being silently dropped by a wholesale
// replacement.
func mergeOverride[T any](base T, override *T) *T {
	result := base
	if override != nil {
		rv := reflect.ValueOf(&result).Elem()
		ov := reflect.ValueOf(*override)
		for i := 0; i < rv.NumField(); i++ {
			f := ov.Field(i)
			switch f.Kind() { //nolint:exhaustive // only pointer/slice/map fields are ever overridden
			case reflect.Pointer, reflect.Slice, reflect.Map:
				if !f.IsNil() {
					rv.Field(i).Set(f)
				}
			}
		}
	}
	return &result
}

// mergeStringMaps returns a new map containing userValues overlaid with
// builtinValues, so builtin values (e.g. the Deployment selector labels, or
// the config-hash annotation) always win on key collisions.
func mergeStringMaps(userValues, builtinValues map[string]string) map[string]string {
	merged := make(map[string]string, len(userValues)+len(builtinValues))
	maps.Copy(merged, userValues)
	maps.Copy(merged, builtinValues)
	return merged
}

// labelsForInstance returns the labels for selecting the resources
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForInstance() map[string]string {
	var imageTag string
	image, err := imageForInstance()
	if err == nil {
		imageTag = strings.Split(image, ":")[1]
	}
	return map[string]string{
		"app.kubernetes.io/name":       "kubepage-operator",
		"app.kubernetes.io/version":    imageTag,
		"app.kubernetes.io/managed-by": "InstanceController",
	}
}

// imageForInstance gets the Operand image which is managed by this controller
// from the INSTANCE_IMAGE environment variable defined in the config/manager/manager.yaml
func imageForInstance() (string, error) {
	var imageEnvVar = "INSTANCE_IMAGE"
	image, found := os.LookupEnv(imageEnvVar)
	if !found {
		return "", fmt.Errorf("unable to find %s environment variable with the image", imageEnvVar)
	}
	return image, nil
}

// SetupWithManager sets up the controller with the Manager.
// The whole idea is to be watching the resources that matter for the controller.
// When a resource that the controller is interested in changes, the Watch triggers
// the controller’s reconciliation loop, ensuring that the actual state of the resource
// matches the desired state as defined in the controller’s logic.
//
// Notice how we configured the Manager to monitor events such as the creation, update,
// or deletion of a Custom Resource (CR) of the Instance kind, as well as any changes
// to the Deployment that the controller manages and owns.
func (r *InstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Watch the Instance CR(s) and trigger reconciliation whenever it
		// is created, updated, or deleted
		For(&pagev1alpha1.Instance{}).
		Named("instance").
		// Watch the Deployment and ConfigMap managed by the InstanceReconciler. If any changes occur to
		// either, owned and managed by this controller, it will trigger reconciliation, ensuring that the
		// cluster state aligns with the desired state. See that the ownerRef was set when each was created.
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		// Watch the config CRDs and reconcile the Instance each one's
		// instanceRef names, so a change to either re-renders that Instance's
		// config without waiting for the Instance itself to change.
		Watches(
			&pagev1alpha1.Configuration{},
			handler.EnqueueRequestsFromMapFunc(mapToInstance(func(c *pagev1alpha1.Configuration) string { return c.Spec.InstanceRef.Name })),
		).
		Watches(
			&pagev1alpha1.ServiceEntry{},
			handler.EnqueueRequestsFromMapFunc(mapToInstance(func(s *pagev1alpha1.ServiceEntry) string { return s.Spec.InstanceRef.Name })),
		).
		Watches(
			&pagev1alpha1.Bookmark{},
			handler.EnqueueRequestsFromMapFunc(mapToInstance(func(b *pagev1alpha1.Bookmark) string { return b.Spec.InstanceRef.Name })),
		).
		Watches(
			&pagev1alpha1.InfoWidget{},
			handler.EnqueueRequestsFromMapFunc(mapToInstance(func(w *pagev1alpha1.InfoWidget) string { return w.Spec.InstanceRef.Name })),
		).
		Complete(r)
}

// mapToInstance builds a handler.MapFunc that enqueues the Instance named by
// extract(obj) (in obj's own namespace), for watching a config CRD that
// carries an InstanceRef. Returns no requests if obj isn't a T or its
// instanceRef name is empty.
func mapToInstance[T client.Object](extract func(T) string) handler.MapFunc {
	return func(_ context.Context, obj client.Object) []reconcile.Request {
		t, ok := obj.(T)
		if !ok {
			return nil
		}
		name := extract(t)
		if name == "" {
			return nil
		}
		return []reconcile.Request{{NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: t.GetNamespace(),
		}}}
	}
}

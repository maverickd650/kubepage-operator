package controller

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const instanceFinalizer = "page.kubepage.dev/finalizer"

const instanceContainerName = "instance"

// dashboardMetricsPort is the fixed port the dashboard's /metrics endpoint
// listens on, deliberately separate from instance.Spec.ContainerPort: the
// Service exposes both, but only ContainerPort is ever wired into an Ingress
// or HTTPRoute (see instance_network.go), so Prometheus metrics stay
// unreachable through whatever public URL the Instance's ingress/gateway
// exposes. Fixed rather than a spec field since it's an implementation
// detail of the dashboard binary, not something users need to tune per
// Instance.
const dashboardMetricsPort int32 = 9090

const dashboardMetricsPortName = "metrics"

// Definitions to manage status conditions
const (
	// typeAvailableInstance represents the status of the Deployment reconciliation
	typeAvailableInstance = "Available"
	// typeDegradedInstance represents the status used when the custom resource is deleted and the finalizer operations are yet to occur.
	typeDegradedInstance = "Degraded"
)

// Reason constants for the Available condition on Instance. Each reconcile
// step that can fail gets its own reason so `kubectl describe` and any
// alerting built on Reason (rather than the free-form Message) can tell a
// failed RBAC provision apart from a failed Ingress reconcile without
// string-matching the message.
const (
	// reasonReconcileSucceeded marks Available=True once every reconcile
	// step (RBAC, Deployment, Service, Ingress, HTTPRoute, bound counts)
	// completed without error.
	reasonReconcileSucceeded = "ReconcileSucceeded"
	// reasonRBACFailed marks Available=False when provisioning the
	// per-Instance ServiceAccount/Role/RoleBinding, or the cluster-scoped
	// kubemetrics RBAC, failed.
	reasonRBACFailed = "RBACReconcileFailed"
	// reasonDeploymentDefinitionFailed marks Available=False when building
	// the desired Deployment object itself failed (e.g. SetControllerReference).
	reasonDeploymentDefinitionFailed = "DeploymentDefinitionFailed"
	// reasonDeploymentUpdateFailed marks Available=False when an existing
	// Deployment was found to have drifted from the desired state but the
	// Update call to correct it failed.
	reasonDeploymentUpdateFailed = "DeploymentUpdateFailed"
	// reasonServiceFailed marks Available=False when reconciling the
	// dashboard Service failed.
	reasonServiceFailed = "ServiceReconcileFailed"
	// reasonIngressFailed marks Available=False when reconciling the
	// optional Ingress failed.
	reasonIngressFailed = "IngressReconcileFailed"
	// reasonHTTPRouteFailed marks Available=False when reconciling the
	// optional HTTPRoute failed.
	reasonHTTPRouteFailed = "HTTPRouteReconcileFailed"
	// reasonNetworkPolicyFailed marks Available=False when reconciling the
	// optional NetworkPolicy failed.
	reasonNetworkPolicyFailed = "NetworkPolicyReconcileFailed"
	// reasonBoundCountsFailed marks Available=False when listing the config
	// CRDs (Configuration/ServiceEntry/Bookmark/InfoWidget) bound to this
	// Instance failed.
	reasonBoundCountsFailed = "BoundCountsListFailed"
	// reasonDeploymentNotReady marks Available=False when the Deployment
	// object matches the desired spec but doesn't yet have as many ready
	// replicas as requested — e.g. an unpullable image, a crash-looping
	// container, or insufficient cluster resources. Without this, Available
	// would report True the instant the Deployment object exists/matches
	// spec, even though no dashboard pod is actually serving traffic.
	reasonDeploymentNotReady = "DeploymentNotReady"
)

// deploymentNotReadyRequeueInterval is how soon Reconcile re-checks Deployment
// readiness while it's not yet ready. Deployment status changes (pod
// transitions, crash-loop backoff) already trigger a reconcile via Owns(&appsv1.Deployment{}),
// but this acts as a fallback so a stalled pull-backoff timer or slow
// container start doesn't leave Available stuck on stale information.
const deploymentNotReadyRequeueInterval = 15 * time.Second

// InstanceReconciler reconciles a Instance object
type InstanceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder

	// DashboardImage is the image the dashboard Deployment runs (D11 / Phase
	// 6.4): the manager's own image, running the same binary's `dashboard`
	// subcommand rather than a separately-pinned operand image. Resolved
	// once at manager startup (see cmd/main.go) since there is no
	// environment variable or kustomize image-transformer hook that
	// surfaces a running pod's own image to itself.
	DashboardImage string

	// GatewayAPIEnabled records whether the cluster has Gateway API CRDs
	// installed, checked once via discovery at manager startup (see
	// cmd/main.go). Gateway API is an optional, separately-installed CRD
	// set; without this check, registering a watch for HTTPRoute on a
	// cluster that doesn't have it would crash the manager at startup
	// rather than degrade gracefully for the (likely common) case of a user
	// who only wants Ingress.
	GatewayAPIEnabled bool

	// DirectReader is an uncached client used the same way cmd/main.go's
	// ownDashboardImage uses one: to Get individual objects (here, Secrets,
	// in filterLabeledSecrets) without starting a cluster-wide informer
	// cache for that type on the manager. See filterLabeledSecrets' doc
	// comment for why that matters specifically for Secrets.
	DirectReader client.Reader
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
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=metrics.k8s.io,resources=nodes,verbs=get;list;watch
// Pods get/list/watch, like the secrets rule below, is needed only so the
// manager can delegate it: it provisions a per-Instance Role granting the
// dashboard pod the same access, to evaluate a ServiceEntry's PodSelector
// (internal/controller/instance_rbac.go, internal/dashboard/poller.go's
// monitor). The manager itself never lists Pods.
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// Secrets get is needed only so the manager can delegate it: it provisions a
// per-Instance Role granting the dashboard pod get on the specific Secrets its
// widgets reference (internal/controller/instance_rbac.go), and the API
// server's privilege-escalation check requires the manager to hold a verb to
// grant it. The manager itself never reads Secret contents.
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete

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
			if err := r.doFinalizerOperationsForInstance(ctx, instance); err != nil {
				log.Error(err, "Failed to run finalizer operations for Instance")
				return ctrl.Result{}, err
			}

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

	// Ensure the per-Instance ServiceAccount/Role/RoleBinding the dashboard
	// pod authenticates as, before the Deployment that references it.
	if err := r.reconcileDashboardRBAC(ctx, instance); err != nil {
		return r.failAvailable(ctx, instance, "RBAC", reasonRBACFailed, err)
	}

	// Cluster-scoped RBAC for the kubemetrics InfoWidget, created only while
	// one is bound and removed otherwise (see reconcileClusterMetricsRBAC).
	if err := r.reconcileClusterMetricsRBAC(ctx, instance); err != nil {
		return r.failAvailable(ctx, instance, "RBAC", reasonRBACFailed, err)
	}

	// Define the Deployment we want on the cluster, create it if it doesn't
	// exist yet, or reconcile drift (replica count or image) if it does.
	result, handled, err := r.reconcileDeployment(ctx, instance)
	if handled {
		return result, err
	}

	// Ensure the dashboard Service (always), Ingress (only if the user opted
	// in via spec.ingress.enabled), and HTTPRoute (only if opted in via
	// spec.gateway.enabled, and only if Gateway API CRDs are installed)
	// match the desired state.
	if err := r.reconcileService(ctx, instance); err != nil {
		return r.failAvailable(ctx, instance, "Service", reasonServiceFailed, err)
	}
	if err := r.reconcileIngress(ctx, instance); err != nil {
		return r.failAvailable(ctx, instance, "Ingress", reasonIngressFailed, err)
	}
	if err := r.reconcileHTTPRoute(ctx, instance); err != nil {
		return r.failAvailable(ctx, instance, "HTTPRoute", reasonHTTPRouteFailed, err)
	}
	if err := r.reconcileNetworkPolicy(ctx, instance); err != nil {
		return r.failAvailable(ctx, instance, "NetworkPolicy", reasonNetworkPolicyFailed, err)
	}

	counts, err := r.boundCountsForInstance(ctx, instance)
	if err != nil {
		return r.failAvailable(ctx, instance, "bound config CRDs", reasonBoundCountsFailed, err)
	}

	// If the size is not defined in the Custom Resource then we will set the desired replicas to 0
	var desiredReplicas int32 = 0
	if instance.Spec.Size != nil {
		desiredReplicas = *instance.Spec.Size
	}

	instance.Status.ObservedGeneration = instance.Generation
	instance.Status.BoundConfigurations = counts.configurations
	instance.Status.BoundServiceEntries = counts.serviceEntries
	instance.Status.BoundBookmarks = counts.bookmarks
	instance.Status.BoundInfoWidgets = counts.infoWidgets

	ready, notReadyMessage, err := r.deploymentReady(ctx, instance, desiredReplicas)
	if err != nil {
		return r.failAvailable(ctx, instance, "Deployment", reasonDeploymentNotReady, err)
	}
	if !ready {
		meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance,
			Status: metav1.ConditionFalse, Reason: reasonDeploymentNotReady,
			Message: notReadyMessage})

		if err := r.Status().Update(ctx, instance); err != nil {
			log.Error(err, "Failed to update Instance status")
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: deploymentNotReadyRequeueInterval}, nil
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance,
		Status: metav1.ConditionTrue, Reason: reasonReconcileSucceeded,
		Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", instance.Name, desiredReplicas)})

	if err := r.Status().Update(ctx, instance); err != nil {
		log.Error(err, "Failed to update Instance status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// deploymentReady reports whether the dashboard Deployment for instance has
// at least as many ready replicas as desiredReplicas, so Available reflects
// that pods are actually serving rather than merely that a Deployment object
// matching spec exists. Without this check, an unpullable image or a
// crash-looping container would leave Available=True the instant the
// Deployment is created/updated, even though no pod is actually up.
func (r *InstanceReconciler) deploymentReady(ctx context.Context, instance *pagev1alpha1.Instance, desiredReplicas int32) (ready bool, message string, err error) {
	found := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found); err != nil {
		return false, "", err
	}
	if found.Status.ReadyReplicas >= desiredReplicas {
		return true, "", nil
	}
	return false, fmt.Sprintf("Deployment %s has %d/%d ready replicas", found.Name, found.Status.ReadyReplicas, desiredReplicas), nil
}

// failAvailable sets the Available condition to False with err's message
// (identifying which resource kind failed) and updates status, returning the
// error so the caller can return it from Reconcile unchanged.
func (r *InstanceReconciler) failAvailable(ctx context.Context, instance *pagev1alpha1.Instance, resource, reason string, err error) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Error(err, fmt.Sprintf("Failed to reconcile %s for Instance", resource))

	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance,
		Status: metav1.ConditionFalse, Reason: reason,
		Message: fmt.Sprintf("Failed to reconcile %s for the custom resource (%s): (%s)", resource, instance.Name, err)})

	if serr := r.Status().Update(ctx, instance); serr != nil {
		log.Error(serr, "Failed to update Instance status")
		return ctrl.Result{}, serr
	}

	return ctrl.Result{}, err
}

// doFinalizerOperationsForInstance performs cleanup that owner-reference
// garbage collection can't: the cluster-scoped RBAC for the kubemetrics
// InfoWidget (reconcileClusterMetricsRBAC) carries no owner reference, since a
// namespaced Instance can't own cluster-scoped objects, so it must be deleted
// explicitly here. Everything else the Instance creates is namespace-scoped
// and owned via ctrl.SetControllerReference, so the API server cascades it.
func (r *InstanceReconciler) doFinalizerOperationsForInstance(ctx context.Context, cr *pagev1alpha1.Instance) error {
	// The following implementation will raise an event
	r.Recorder.Eventf(cr, nil, corev1.EventTypeWarning, "Deleting", "DeleteCR",
		"Custom Resource %s is being deleted from the namespace %s",
		cr.Name,
		cr.Namespace)

	return r.deleteClusterMetricsRBAC(ctx, cr)
}

// boundCounts is how many of each config CRD kind are currently bound to an
// Instance, surfaced in its status (see pagev1alpha1.InstanceStatus). Unlike
// the pre-Phase-6.4 homepage-wrapper path, this is purely informational now:
// the dashboard pod reads these same CRDs live through its own
// controller-runtime cache (internal/dashboard), so a Configuration/
// ServiceEntry/Bookmark/InfoWidget change needs no Instance-mediated
// re-render or rollout to take effect.
type boundCounts struct {
	configurations int32
	serviceEntries int32
	bookmarks      int32
	infoWidgets    int32
}

// boundCountsForInstance counts the config CRDs bound to instance, for
// status visibility (kubectl get/describe) without having to cross-reference
// every config CRD's own instanceRef by hand.
func (r *InstanceReconciler) boundCountsForInstance(ctx context.Context, instance *pagev1alpha1.Instance) (boundCounts, error) {
	var configs pagev1alpha1.ConfigurationList
	if err := r.List(ctx, &configs, client.InNamespace(instance.Namespace)); err != nil {
		return boundCounts{}, fmt.Errorf("listing Configurations: %w", err)
	}
	var serviceEntries pagev1alpha1.ServiceEntryList
	if err := r.List(ctx, &serviceEntries, client.InNamespace(instance.Namespace)); err != nil {
		return boundCounts{}, fmt.Errorf("listing ServiceEntries: %w", err)
	}
	var bookmarks pagev1alpha1.BookmarkList
	if err := r.List(ctx, &bookmarks, client.InNamespace(instance.Namespace)); err != nil {
		return boundCounts{}, fmt.Errorf("listing Bookmarks: %w", err)
	}
	var infoWidgets pagev1alpha1.InfoWidgetList
	if err := r.List(ctx, &infoWidgets, client.InNamespace(instance.Namespace)); err != nil {
		return boundCounts{}, fmt.Errorf("listing InfoWidgets: %w", err)
	}

	counts := boundCounts{}
	for _, c := range configs.Items {
		if c.Spec.InstanceRef.Name == instance.Name {
			counts.configurations++
		}
	}
	for _, s := range serviceEntries.Items {
		if s.Spec.InstanceRef.Name == instance.Name {
			counts.serviceEntries++
		}
	}
	for _, b := range bookmarks.Items {
		if b.Spec.InstanceRef.Name == instance.Name {
			counts.bookmarks++
		}
	}
	for _, w := range infoWidgets.Items {
		if w.Spec.InstanceRef.Name == instance.Name {
			counts.infoWidgets++
		}
	}
	return counts, nil
}

// reconcileDeployment ensures the dashboard Deployment for instance exists
// and matches the desired state (replicas, image, and every other
// spec-driven field deploymentForInstance derives from the Instance). handled
// is true when the caller should return (result, err) immediately; when
// false, the Deployment was already up to date and the caller should
// continue with its own logic (e.g. updating the Instance's status).
func (r *InstanceReconciler) reconcileDeployment(ctx context.Context, instance *pagev1alpha1.Instance) (result ctrl.Result, handled bool, err error) {
	log := logf.FromContext(ctx)

	desiredDep, err := r.deploymentForInstance(instance)
	if err != nil {
		log.Error(err, "Failed to define new Deployment resource for Instance")

		meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance,
			Status: metav1.ConditionFalse, Reason: reasonDeploymentDefinitionFailed,
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

	// Reconcile drift: the Deployment exists, but one or more spec-driven
	// fields no longer match the desired state. We deliberately don't
	// DeepEqual the whole pod template against a freshly-built one: the API
	// server fills in defaults (RestartPolicy, DNSPolicy, etc.) on the
	// stored object that a bare struct literal never has, which would make
	// every comparison show spurious drift. Instead we compare exactly the
	// fields deploymentForInstance derives from the Instance spec (or from
	// DashboardImage), so an edit to any of them — not just replicas or
	// image — is detected without false positives from API-server
	// defaulting.
	replicasChanged := found.Spec.Replicas == nil || desiredDep.Spec.Replicas == nil || *found.Spec.Replicas != *desiredDep.Spec.Replicas

	desiredContainer := desiredDep.Spec.Template.Spec.Containers[0]
	foundContainers := found.Spec.Template.Spec.Containers
	templateChanged := len(foundContainers) == 0 ||
		foundContainers[0].Image != desiredContainer.Image ||
		!reflect.DeepEqual(foundContainers[0].Args, desiredContainer.Args) ||
		!reflect.DeepEqual(foundContainers[0].Ports, desiredContainer.Ports) ||
		!reflect.DeepEqual(foundContainers[0].Env, desiredContainer.Env) ||
		!reflect.DeepEqual(foundContainers[0].ReadinessProbe, desiredContainer.ReadinessProbe) ||
		!reflect.DeepEqual(foundContainers[0].LivenessProbe, desiredContainer.LivenessProbe) ||
		!reflect.DeepEqual(foundContainers[0].Resources, desiredContainer.Resources) ||
		!reflect.DeepEqual(foundContainers[0].SecurityContext, desiredContainer.SecurityContext) ||
		!reflect.DeepEqual(found.Spec.Template.Labels, desiredDep.Spec.Template.Labels) ||
		!reflect.DeepEqual(found.Spec.Template.Annotations, desiredDep.Spec.Template.Annotations) ||
		!reflect.DeepEqual(found.Spec.Template.Spec.HostUsers, desiredDep.Spec.Template.Spec.HostUsers) ||
		!reflect.DeepEqual(found.Spec.Template.Spec.SecurityContext, desiredDep.Spec.Template.Spec.SecurityContext)

	if !replicasChanged && !templateChanged {
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
			Status: metav1.ConditionFalse, Reason: reasonDeploymentUpdateFailed,
			Message: fmt.Sprintf("Failed to update the Deployment for the custom resource (%s): (%s)", instance.Name, err)})

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

// defaultPollIntervalSeconds mirrors the dashboard subcommand's own
// --poll-interval default (cmd/main.go), used when
// instance.Spec.PollIntervalSeconds is unset. Kept as an explicit fallback
// here (rather than always passing the flag with a Go-side default) so an
// Instance created before this field existed keeps behaving exactly as
// before.
const defaultPollIntervalSeconds = 15

// dashboardArgs returns the dashboard subcommand's CLI flags for instance:
// which Instance to serve (namespace/name, so the dashboard's own
// controller-runtime cache can be scoped to just that namespace), which
// address to listen on (instance.Spec.ContainerPort, the same port the
// Service and Ingress target), and how often to poll (instance.Spec.
// PollIntervalSeconds, or defaultPollIntervalSeconds if unset).
func dashboardArgs(instance *pagev1alpha1.Instance) []string {
	pollIntervalSeconds := int32(defaultPollIntervalSeconds)
	if instance.Spec.PollIntervalSeconds != nil {
		pollIntervalSeconds = *instance.Spec.PollIntervalSeconds
	}
	return []string{
		"dashboard",
		"--namespace=" + instance.Namespace,
		"--instance-name=" + instance.Name,
		fmt.Sprintf("--addr=:%d", instance.Spec.ContainerPort),
		fmt.Sprintf("--metrics-addr=:%d", dashboardMetricsPort),
		fmt.Sprintf("--poll-interval=%ds", pollIntervalSeconds),
	}
}

// deploymentForInstance returns a Instance Deployment object
func (r *InstanceReconciler) deploymentForInstance(instance *pagev1alpha1.Instance) (*appsv1.Deployment, error) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: instance.Spec.Size,
			// Selector must stay stable across reconciles — Deployment
			// selectors are immutable once created — so it deliberately
			// excludes app.kubernetes.io/version, which changes whenever
			// DashboardImage does (an operator upgrade). selectorLabelsForInstance()
			// is the fixed subset; labelsForInstance() (used below for the
			// pod template) is selectorLabelsForInstance() plus version.
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabelsForInstance(),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					// labelsForInstance(...) is layered on top of user labels
					// so the selector (a subset of it) can never be broken by
					// a colliding user-supplied label.
					Labels:      mergeStringMaps(instance.Spec.Labels, labelsForInstance(r.DashboardImage)),
					Annotations: instance.Spec.Annotations,
				},
				Spec: corev1.PodSpec{
					HostUsers:          hostUsersBool(instance.Spec.HostUsers),
					ServiceAccountName: instance.Name,
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
						Image:           r.DashboardImage,
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
						Args: dashboardArgs(instance),
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: instance.Spec.ContainerPort,
								Name:          instanceContainerName,
								// Protocol is set explicitly (rather than left to
								// the API server's defaulting) so the drift check
								// in reconcileDeployment, which compares the
								// stored Deployment's Ports against this struct
								// literal, isn't fooled into seeing permanent
								// drift by a field the server fills in on its own.
								Protocol: corev1.ProtocolTCP,
							},
							{
								ContainerPort: dashboardMetricsPort,
								Name:          dashboardMetricsPortName,
								Protocol:      corev1.ProtocolTCP,
							},
						},
						Env:            instance.Spec.Env,
						ReadinessProbe: instance.Spec.ReadinessProbe,
						LivenessProbe:  instance.Spec.LivenessProbe,
						Resources:      instance.Spec.Resources,
					}},
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

// hostUsersBool converts InstanceSpec.HostUsers's Enabled/Disabled enum into
// the *bool corev1.PodSpec.HostUsers expects.
func hostUsersBool(s *string) *bool {
	if s == nil {
		return nil
	}
	b := *s == pagev1alpha1.Enabled
	return &b
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
		for i := range rv.NumField() {
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

// selectorLabelsForInstance returns the fixed label subset used as both the
// Deployment's (immutable) selector and the Service's selector. Deliberately
// excludes app.kubernetes.io/version: that label changes whenever
// DashboardImage does (an operator upgrade), and a selector that included it
// would make the Deployment's selector update-incompatible with itself
// across upgrades — Kubernetes rejects changing spec.selector after
// creation.
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func selectorLabelsForInstance() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "kubepage-operator",
		"app.kubernetes.io/managed-by": "InstanceController",
	}
}

// labelsForInstance returns selectorLabelsForInstance() plus
// app.kubernetes.io/version (image's tag), for use on the pod template
// itself (never on a Selector). image is the dashboard image currently in
// use (DashboardImage).
func labelsForInstance(image string) map[string]string {
	var imageTag string
	if parts := strings.SplitN(image, ":", 2); len(parts) == 2 {
		imageTag = parts[1]
	}
	labels := selectorLabelsForInstance()
	labels["app.kubernetes.io/version"] = imageTag
	return labels
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
	bldr := ctrl.NewControllerManagedBy(mgr).
		// Watch the Instance CR(s) and trigger reconciliation whenever it
		// is created, updated, or deleted
		For(&pagev1alpha1.Instance{}).
		Named("instance").
		// Watch the resources owned and managed by the InstanceReconciler. If
		// any changes occur to one of these, it will trigger reconciliation,
		// ensuring that the cluster state aligns with the desired state. See
		// that the ownerRef was set when each was created.
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{})

	// Only watch HTTPRoute if the cluster actually has Gateway API CRDs
	// installed (checked once at startup, see cmd/main.go) — registering a
	// watch for a Kind with no RESTMapping would crash the manager on
	// start, and Gateway API is optional/separately installed, unlike
	// Ingress which is built into every cluster.
	if r.GatewayAPIEnabled {
		bldr = bldr.Owns(&gatewayv1.HTTPRoute{})
	}

	return bldr.
		// Watch the config CRDs and reconcile the Instance each one's
		// instanceRef names, so a change to either keeps the Instance's
		// bound-count status fresh. The dashboard pod itself reads these
		// CRDs live through its own cache (internal/dashboard), so this no
		// longer drives any render or rollout — only status visibility.
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

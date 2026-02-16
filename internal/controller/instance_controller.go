/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const instanceFinalizer = "page.kubepage.dev/finalizer"

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
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

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
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.1/pkg/reconcile
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
			log.Info("instance resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get instance")
		return ctrl.Result{}, err
	}

	if len(instance.Status.Conditions) == 0 {
		meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
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
		log.Info("Adding Finalizer for Instance")
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
			log.Info("Performing Finalizer Operations for Instance before delete CR")

			// Let's add here a status "Downgrade" to reflect that this resource began its process to be terminated.
			meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeDegradedInstance,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
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
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", instance.Name)})

			if err := r.Status().Update(ctx, instance); err != nil {
				log.Error(err, "Failed to update Instance status")
				return ctrl.Result{}, err
			}

			log.Info("Removing Finalizer for Instance after successfully perform the operations")
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

	// Check if the deployment already exists, if not create a new one
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new deployment
		dep, err := r.deploymentForInstance(instance)
		if err != nil {
			log.Error(err, "Failed to define new Deployment resource for Instance")

			// The following implementation will update the status
			meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Deployment for the custom resource (%s): (%s)", instance.Name, err)})

			if err := r.Status().Update(ctx, instance); err != nil {
				log.Error(err, "Failed to update Instance status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new Deployment",
			"Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		if err = r.Create(ctx, dep); err != nil {
			log.Error(err, "Failed to create new Deployment",
				"Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return ctrl.Result{}, err
		}

		// Deployment created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-triggered again
		return ctrl.Result{}, err
	}

	// If the size is not defined in the Custom Resource then we will set the desired replicas to 0
	var desiredReplicas int32 = 0
	if instance.Spec.Size != nil {
		desiredReplicas = *instance.Spec.Size
	}

	// The CRD API defines that the Instance type have a InstanceSpec.Size field
	// to set the quantity of Deployment instances to the desired state on the cluster.
	// Therefore, the following code will ensure the Deployment size is the same as defined
	// via the Size spec of the Custom Resource which we are reconciling.
	if found.Spec.Replicas == nil || *found.Spec.Replicas != desiredReplicas {
		found.Spec.Replicas = ptr.To(desiredReplicas)
		if err = r.Update(ctx, found); err != nil {
			log.Error(err, "Failed to update Deployment",
				"Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)

			// Re-fetch the instance Custom Resource before updating the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raising the error "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
				log.Error(err, "Failed to re-fetch instance")
				return ctrl.Result{}, err
			}

			// The following implementation will update the status
			meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance,
				Status: metav1.ConditionFalse, Reason: "Resizing",
				Message: fmt.Sprintf("Failed to update the size for the custom resource (%s): (%s)", instance.Name, err)})

			if err := r.Status().Update(ctx, instance); err != nil {
				log.Error(err, "Failed to update Instance status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		// Now, that we update the size we want to requeue the reconciliation
		// so that we can ensure that we have the latest state of the resource before
		// update. Also, it will help ensure the desired state on the cluster
		return ctrl.Result{Requeue: true}, nil
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{Type: typeAvailableInstance,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", instance.Name, desiredReplicas)})

	if err := r.Status().Update(ctx, instance); err != nil {
		log.Error(err, "Failed to update Instance status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
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

// deploymentForInstance returns a Instance Deployment object
func (r *InstanceReconciler) deploymentForInstance(
	instance *pagev1alpha1.Instance) (*appsv1.Deployment, error) {
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
					Labels: ls,
				},
				Spec: corev1.PodSpec{
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
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						// IMPORTANT: seccomProfile was introduced with Kubernetes 1.19
						// If you are looking for to produce solutions to be supported
						// on lower versions you must remove this option.
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{{
						Image:           image,
						Name:            "instance",
						ImagePullPolicy: corev1.PullIfNotPresent,
						// Ensure restrictive context for the container
						// More info: https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             ptr.To(true),
							RunAsUser:                ptr.To(int64(568)),
							AllowPrivilegeEscalation: ptr.To(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{
									"ALL",
								},
							},
						},
						Ports: []corev1.ContainerPort{{
							ContainerPort: instance.Spec.ContainerPort,
							Name:          "instance",
						}},
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
		// Watch the Deployment managed by the InstanceReconciler. If any changes occur to the Deployment
		// owned and managed by this controller, it will trigger reconciliation, ensuring that the cluster
		// state aligns with the desired state. See that the ownerRef was set when the Deployment was created.
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

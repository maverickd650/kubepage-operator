package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// typeAvailableConfiguration represents whether a Configuration's
// instanceRef resolves to an existing Instance.
const typeAvailableConfiguration = "Available"

// ConfigurationReconciler reconciles a Configuration object.
//
// This controller is intentionally thin: it only validates that instanceRef
// resolves to an existing Instance and reflects that in status. It does not
// render config or touch the ConfigMap — that's the InstanceReconciler's job
// (see instance_controller.go), which already watches Configuration objects
// and re-renders the Instance they reference on every change.
type ConfigurationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=page.kubepage.dev,resources=configurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=configurations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=configurations/finalizers,verbs=update
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=instances,verbs=get;list;watch

// Reconcile validates that the Configuration's instanceRef resolves to an
// existing Instance in the same namespace and sets the Available status
// condition accordingly.
func (r *ConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cfg := &pagev1alpha1.Configuration{}
	if err := r.Get(ctx, req.NamespacedName, cfg); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Configuration")
		return ctrl.Result{}, err
	}

	instance := &pagev1alpha1.Instance{}
	err := r.Get(ctx, types.NamespacedName{Name: cfg.Spec.InstanceRef.Name, Namespace: cfg.Namespace}, instance)
	switch {
	case apierrors.IsNotFound(err):
		meta.SetStatusCondition(&cfg.Status.Conditions, metav1.Condition{
			Type: typeAvailableConfiguration, Status: metav1.ConditionFalse, Reason: reasonInstanceNotFound,
			Message: fmt.Sprintf("Instance %q referenced by instanceRef does not exist in namespace %q", cfg.Spec.InstanceRef.Name, cfg.Namespace),
		})
	case err != nil:
		log.Error(err, "Failed to get referenced Instance")
		return ctrl.Result{}, err
	default:
		meta.SetStatusCondition(&cfg.Status.Conditions, metav1.Condition{
			Type: typeAvailableConfiguration, Status: metav1.ConditionTrue, Reason: reasonReconciling,
			Message: fmt.Sprintf("Bound to Instance %q", cfg.Spec.InstanceRef.Name),
		})
	}

	if err := r.Status().Update(ctx, cfg); err != nil {
		log.Error(err, "Failed to update Configuration status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pagev1alpha1.Configuration{}).
		Named("configuration").
		// Watch Instance objects too: if a Configuration is applied before
		// its referenced Instance exists (order isn't guaranteed by e.g.
		// `kubectl apply -k`), the Instance showing up later should flip
		// this Configuration's status from InstanceNotFound to Available
		// without waiting for the Configuration itself to be touched again.
		Watches(
			&pagev1alpha1.Instance{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				instance, ok := obj.(*pagev1alpha1.Instance)
				if !ok {
					return nil
				}

				var configs pagev1alpha1.ConfigurationList
				if err := mgr.GetClient().List(ctx, &configs, client.InNamespace(instance.Namespace)); err != nil {
					return nil
				}

				var reqs []reconcile.Request
				for _, c := range configs.Items {
					if c.Spec.InstanceRef.Name == instance.Name {
						reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{
							Name:      c.Name,
							Namespace: c.Namespace,
						}})
					}
				}
				return reqs
			}),
		).
		Complete(r)
}

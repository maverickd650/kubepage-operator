package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// ServiceEntryReconciler reconciles a ServiceEntry object.
//
// Thin, like ConfigurationReconciler: it only validates that instanceRef
// resolves to an existing Instance and reflects that in status. Rendering
// services.yaml (including secret resolution) and watching ServiceEntry
// changes is the InstanceReconciler's job (see instance_controller.go and
// serviceentry_render.go).
type ServiceEntryReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=page.kubepage.dev,resources=serviceentries,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=serviceentries/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=serviceentries/finalizers,verbs=update
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=instances,verbs=get;list;watch

// Reconcile validates that the ServiceEntry's instanceRef resolves to an
// existing Instance in the same namespace and sets the Available status
// condition accordingly.
func (r *ServiceEntryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	entry := &pagev1alpha1.ServiceEntry{}
	if err := r.Get(ctx, req.NamespacedName, entry); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get ServiceEntry")
		return ctrl.Result{}, err
	}

	cond, err := boundInstanceCondition(ctx, r.Client, entry.Namespace, entry.Spec.InstanceRef.Name)
	if err != nil {
		log.Error(err, "Failed to get referenced Instance")
		return ctrl.Result{}, err
	}
	meta.SetStatusCondition(&entry.Status.Conditions, cond)

	if err := r.Status().Update(ctx, entry); err != nil {
		log.Error(err, "Failed to update ServiceEntry status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceEntryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pagev1alpha1.ServiceEntry{}).
		Named("serviceentry").
		// Watch Instance objects too: see ConfigurationReconciler.SetupWithManager
		// for why (out-of-order apply self-heals without waiting for the
		// ServiceEntry itself to be touched again).
		Watches(
			&pagev1alpha1.Instance{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				instance, ok := obj.(*pagev1alpha1.Instance)
				if !ok {
					return nil
				}

				var entries pagev1alpha1.ServiceEntryList
				if err := mgr.GetClient().List(ctx, &entries, client.InNamespace(instance.Namespace)); err != nil {
					return nil
				}

				var reqs []reconcile.Request
				for _, e := range entries.Items {
					if e.Spec.InstanceRef.Name == instance.Name {
						reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{
							Name:      e.Name,
							Namespace: e.Namespace,
						}})
					}
				}
				return reqs
			}),
		).
		Complete(r)
}

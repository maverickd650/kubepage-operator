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

// BookmarkReconciler reconciles a Bookmark object.
//
// Thin, like ConfigurationReconciler and ServiceEntryReconciler: it only
// validates that instanceRef resolves to an existing Instance and reflects
// that in status. Rendering bookmarks.yaml and watching Bookmark changes is
// the InstanceReconciler's job (see instance_controller.go).
type BookmarkReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=page.kubepage.dev,resources=bookmarks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=bookmarks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=bookmarks/finalizers,verbs=update
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=instances,verbs=get;list;watch

// Reconcile validates that the Bookmark's instanceRef resolves to an existing
// Instance in the same namespace and sets the Available status condition
// accordingly.
func (r *BookmarkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	bookmark := &pagev1alpha1.Bookmark{}
	if err := r.Get(ctx, req.NamespacedName, bookmark); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Bookmark")
		return ctrl.Result{}, err
	}

	cond, err := boundInstanceCondition(ctx, r.Client, bookmark.Namespace, bookmark.Spec.InstanceRef.Name)
	if err != nil {
		log.Error(err, "Failed to get referenced Instance")
		return ctrl.Result{}, err
	}
	meta.SetStatusCondition(&bookmark.Status.Conditions, cond)

	if err := r.Status().Update(ctx, bookmark); err != nil {
		log.Error(err, "Failed to update Bookmark status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BookmarkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pagev1alpha1.Bookmark{}).
		Named("bookmark").
		// Watch Instance objects too: see ConfigurationReconciler.SetupWithManager
		// for why (out-of-order apply self-heals without waiting for the
		// Bookmark itself to be touched again).
		Watches(
			&pagev1alpha1.Instance{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				instance, ok := obj.(*pagev1alpha1.Instance)
				if !ok {
					return nil
				}

				var bookmarks pagev1alpha1.BookmarkList
				if err := mgr.GetClient().List(ctx, &bookmarks, client.InNamespace(instance.Namespace)); err != nil {
					return nil
				}

				var reqs []reconcile.Request
				for _, b := range bookmarks.Items {
					if b.Spec.InstanceRef.Name == instance.Name {
						reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{
							Name:      b.Name,
							Namespace: b.Namespace,
						}})
					}
				}
				return reqs
			}),
		).
		Complete(r)
}

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

// InfoWidgetReconciler reconciles a InfoWidget object.
//
// Thin, like ConfigurationReconciler, ServiceEntryReconciler, and
// BookmarkReconciler: it only validates that instanceRef resolves to an
// existing Instance and reflects that in status. Rendering widgets.yaml
// (including secret resolution and the kubernetes.yaml mode toggle) and
// watching InfoWidget changes is the InstanceReconciler's job (see
// instance_controller.go and infowidget_render.go).
type InfoWidgetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=page.kubepage.dev,resources=infowidgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=infowidgets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=infowidgets/finalizers,verbs=update
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=instances,verbs=get;list;watch

// Reconcile validates that the InfoWidget's instanceRef resolves to an
// existing Instance in the same namespace and sets the Available status
// condition accordingly.
func (r *InfoWidgetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	widget := &pagev1alpha1.InfoWidget{}
	if err := r.Get(ctx, req.NamespacedName, widget); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get InfoWidget")
		return ctrl.Result{}, err
	}

	cond, err := boundInstanceCondition(ctx, r.Client, widget.Namespace, widget.Spec.InstanceRef.Name)
	if err != nil {
		log.Error(err, "Failed to get referenced Instance")
		return ctrl.Result{}, err
	}
	meta.SetStatusCondition(&widget.Status.Conditions, cond)

	if err := r.Status().Update(ctx, widget); err != nil {
		log.Error(err, "Failed to update InfoWidget status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *InfoWidgetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pagev1alpha1.InfoWidget{}).
		Named("infowidget").
		// Watch Instance objects too: see ConfigurationReconciler.SetupWithManager
		// for why (out-of-order apply self-heals without waiting for the
		// InfoWidget itself to be touched again).
		Watches(
			&pagev1alpha1.Instance{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				instance, ok := obj.(*pagev1alpha1.Instance)
				if !ok {
					return nil
				}

				var widgets pagev1alpha1.InfoWidgetList
				if err := mgr.GetClient().List(ctx, &widgets, client.InNamespace(instance.Namespace)); err != nil {
					return nil
				}

				var reqs []reconcile.Request
				for _, w := range widgets.Items {
					if w.Spec.InstanceRef.Name == instance.Name {
						reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{
							Name:      w.Name,
							Namespace: w.Namespace,
						}})
					}
				}
				return reqs
			}),
		).
		Complete(r)
}

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

// DashboardStyleReconciler reconciles a DashboardStyle object.
//
// This controller is intentionally thin: it only validates that dashboardRef
// resolves to an existing Dashboard and reflects that in status. It does not
// render config or touch the ConfigMap — that's the DashboardReconciler's job
// (see instance_controller.go), which already watches DashboardStyle objects
// and re-renders the Dashboard they reference on every change.
type DashboardStyleReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=page.kubepage.dev,resources=dashboardstyles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=dashboardstyles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=dashboardstyles/finalizers,verbs=update
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=dashboards,verbs=get;list;watch

// Reconcile validates that the DashboardStyle's dashboardRef resolves to an
// existing Dashboard in the same namespace and sets the Available status
// condition accordingly.
func (r *DashboardStyleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cfg := &pagev1alpha1.DashboardStyle{}
	if err := r.Get(ctx, req.NamespacedName, cfg); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get DashboardStyle")
		return ctrl.Result{}, err
	}

	cond, err := boundDashboardCondition(ctx, r.Client, cfg.Namespace, cfg.Spec.DashboardRef.Name, cfg.Generation)
	if err != nil {
		log.Error(err, "Failed to get referenced Dashboard")
		return ctrl.Result{}, err
	}
	meta.SetStatusCondition(&cfg.Status.Conditions, cond)

	if err := r.Status().Update(ctx, cfg); err != nil {
		log.Error(err, "Failed to update DashboardStyle status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DashboardStyleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pagev1alpha1.DashboardStyle{}).
		Named("dashboardstyle").
		// Watch Dashboard objects too: if a DashboardStyle is applied before
		// its referenced Dashboard exists (order isn't guaranteed by e.g.
		// `kubectl apply -k`), the Dashboard showing up later should flip
		// this DashboardStyle's status from DashboardNotFound to Available
		// without waiting for the DashboardStyle itself to be touched again.
		Watches(
			&pagev1alpha1.Dashboard{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				instance, ok := obj.(*pagev1alpha1.Dashboard)
				if !ok {
					return nil
				}

				var configs pagev1alpha1.DashboardStyleList
				if err := mgr.GetClient().List(ctx, &configs, client.InNamespace(instance.Namespace)); err != nil {
					return nil
				}

				var reqs []reconcile.Request
				for _, c := range configs.Items {
					if c.Spec.DashboardRef.Name == instance.Name {
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

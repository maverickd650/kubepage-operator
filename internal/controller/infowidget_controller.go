package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
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

// InfoWidgetReconciler reconciles a InfoWidget object.
//
// Thin, like DashboardStyleReconciler, ServiceCardReconciler, and
// BookmarkReconciler: it only validates that dashboardRef resolves to an
// existing Dashboard and reflects that in status. Actually polling and
// rendering an InfoWidget (datetime, greeting, openmeteo, kubemetrics)
// happens in the native dashboard's poller (internal/dashboard/poller.go),
// which reads InfoWidgets directly through its own namespace-scoped cache.
type InfoWidgetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=page.kubepage.dev,resources=infowidgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=infowidgets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=infowidgets/finalizers,verbs=update
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=dashboards,verbs=get;list;watch

// Reconcile validates that the InfoWidget's dashboardRef resolves to an
// existing Dashboard in the same namespace and sets the Available status
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

	cond, err := boundDashboardCondition(ctx, r.Client, widget.Namespace, widget.Spec.DashboardRef.Name, widget.Generation)
	if err != nil {
		log.Error(err, "Failed to get referenced Dashboard")
		return ctrl.Result{}, err
	}

	entries := widget.Spec.Entries()
	availableOverride, configValid := validateWidgetConfigs(infoWidgetConfigInstances(entries), widget.Generation)
	if cond.Status == metav1.ConditionTrue && availableOverride != nil {
		cond = *availableOverride
	}
	meta.SetStatusCondition(&widget.Status.Conditions, cond)
	meta.SetStatusCondition(&widget.Status.Conditions, configValid)
	widget.Status.Entries = int32(len(entries))

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
		// Watch Dashboard objects too: see DashboardStyleReconciler.SetupWithManager
		// for why (out-of-order apply self-heals without waiting for the
		// InfoWidget itself to be touched again).
		Watches(
			&pagev1alpha1.Dashboard{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				instance, ok := obj.(*pagev1alpha1.Dashboard)
				if !ok {
					return nil
				}

				var widgets pagev1alpha1.InfoWidgetList
				if err := mgr.GetClient().List(ctx, &widgets, client.InNamespace(instance.Namespace)); err != nil {
					return nil
				}

				var reqs []reconcile.Request
				for _, w := range widgets.Items {
					if w.Spec.DashboardRef.Name == instance.Name {
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

// infoWidgetConfigInstances flattens entries into the widgetConfigInstance
// form validateWidgetConfigs expects. URLSet is derived from each entry's
// typed URL field, which satisfies a schema's "url" key (glances, longhorn)
// the same way setting it via Options would.
func infoWidgetConfigInstances(entries []pagev1alpha1.InfoWidgetEntry) []widgetConfigInstance {
	instances := make([]widgetConfigInstance, 0, len(entries))
	for widgetIdx, e := range entries {
		instances = append(instances, widgetConfigInstance{
			Location:   fmt.Sprintf("widget[%d] (type %q)", widgetIdx, e.Type),
			WidgetType: e.Type,
			Raw:        e.Options,
			URLSet:     e.URL != nil,
		})
	}
	return instances
}

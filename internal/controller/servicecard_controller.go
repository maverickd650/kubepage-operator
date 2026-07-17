package controller

import (
	"cmp"
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

// ServiceCardReconciler reconciles a ServiceCard object.
//
// Thin, like ServiceCardReconciler: it only validates that dashboardRef
// resolves to an existing Dashboard and reflects that in status. The
// dashboard pod (internal/dashboard) reads and polls ServiceCards directly
// through its own cache; this controller never renders or resolves secrets.
type ServiceCardReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=page.kubepage.dev,resources=servicecards,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=servicecards/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=servicecards/finalizers,verbs=update
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=dashboards,verbs=get;list;watch

// Reconcile validates that the ServiceCard's dashboardRef resolves to an
// existing Dashboard in the same namespace and sets the Available status
// condition accordingly.
func (r *ServiceCardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	entry := &pagev1alpha1.ServiceCard{}
	if err := r.Get(ctx, req.NamespacedName, entry); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get ServiceCard")
		return ctrl.Result{}, err
	}

	cond, err := boundDashboardCondition(ctx, r.Client, entry.Namespace, entry.Spec.DashboardRef.Name, entry.Generation)
	if err != nil {
		log.Error(err, "Failed to get referenced Dashboard")
		return ctrl.Result{}, err
	}

	entries := entry.Spec.Entries()
	availableOverride, configValid := validateWidgetConfigs(serviceCardWidgetConfigInstances(entries), entry.Generation)
	if cond.Status == metav1.ConditionTrue && availableOverride != nil {
		cond = *availableOverride
	}
	meta.SetStatusCondition(&entry.Status.Conditions, cond)
	meta.SetStatusCondition(&entry.Status.Conditions, configValid)
	entry.Status.Entries = int32(len(entries))

	if err := r.Status().Update(ctx, entry); err != nil {
		log.Error(err, "Failed to update ServiceCard status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceCardReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pagev1alpha1.ServiceCard{}).
		Named("servicecard").
		// Watch Dashboard objects too: see ServiceCardReconciler.SetupWithManager
		// for why (out-of-order apply self-heals without waiting for the
		// ServiceCard itself to be touched again).
		Watches(
			&pagev1alpha1.Dashboard{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				instance, ok := obj.(*pagev1alpha1.Dashboard)
				if !ok {
					return nil
				}

				var entries pagev1alpha1.ServiceCardList
				if err := mgr.GetClient().List(ctx, &entries, client.InNamespace(instance.Namespace)); err != nil {
					return nil
				}

				var reqs []reconcile.Request
				for _, e := range entries.Items {
					if e.Spec.DashboardRef.Name == instance.Name {
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

// serviceCardWidgetConfigInstances flattens entries' widgets into the
// widgetConfigInstance form validateWidgetConfigs expects, labeling each by
// its entry name (or index, for an unnamed entry) and widget position.
func serviceCardWidgetConfigInstances(entries []pagev1alpha1.ServiceEntry) []widgetConfigInstance {
	var instances []widgetConfigInstance
	for entryIdx, e := range entries {
		label := cmp.Or(e.Name, fmt.Sprintf("entries[%d]", entryIdx))
		for widgetIdx, w := range e.Widgets {
			instances = append(instances, widgetConfigInstance{
				Location:   fmt.Sprintf("entry %q widget[%d] (type %q)", label, widgetIdx, w.Type),
				WidgetType: w.Type,
				Raw:        w.Config,
			})
		}
	}
	return instances
}

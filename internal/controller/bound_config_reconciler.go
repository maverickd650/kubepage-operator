package controller

import (
	"context"

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

// toPointerSlice converts a []T list-item slice (as embedded in a
// *pagev1alpha1.BookmarkList etc.'s Items field) into []*T, for the
// per-kind listItems adapters boundConfigReconciler needs.
func toPointerSlice[T any](items []T) []*T {
	ptrs := make([]*T, len(items))
	for i := range items {
		ptrs[i] = &items[i]
	}
	return ptrs
}

// boundConfigReconciler is the shared reconcile/watch logic for every "thin"
// config CRD (ServiceCard, Bookmark, InfoWidget, and any future kind with the
// same shape): validate that spec.dashboardRef resolves to an existing
// Dashboard in the same namespace, reflect that in the Available status
// condition (and ConfigValid, for kinds that carry widgets), and re-reconcile
// on a Dashboard change that could affect that answer. Rendering — polling
// widgets, building bookmarks.yaml — is the dashboard pod's job
// (internal/dashboard), which reads these CRDs live through its own cache;
// this reconciler never renders.
//
// T is a pointer receiver type implementing client.Object, e.g.
// *pagev1alpha1.Bookmark. Each config CRD kind wires up its own instance via
// a small adapter (see ServiceCardReconciler, BookmarkReconciler,
// InfoWidgetReconciler) rather than embedding this type directly, so the
// kind's own name is preserved for `+kubebuilder:rbac` scanning, `kubectl`
// controller naming, and the existing test call sites that construct
// `&ServiceCardReconciler{...}` and call Reconcile directly.
type boundConfigReconciler[T client.Object] struct {
	client.Client
	Scheme *runtime.Scheme

	// displayName names the kind in log messages, e.g. "ServiceCard".
	displayName string
	// controllerName is the lowercase name passed to Named(), e.g.
	// "servicecard".
	controllerName string
	// newObj returns a new zero-value T for Get.
	newObj func() T
	// newList returns a new list object of T's kind, for the Dashboard
	// watch's indexed lookup.
	newList func() client.ObjectList
	// listItems extracts []T from a list built by newList.
	listItems func(client.ObjectList) []T
	// refName returns obj's dashboardRef.name (via pagev1alpha1.RefName),
	// "" if unset.
	refName func(obj T) string
	// conditions returns a pointer to obj's status conditions slice, for
	// meta.SetStatusCondition.
	conditions func(obj T) *[]metav1.Condition
	// applyEntries stamps obj's status.entries count from its spec.
	applyEntries func(obj T)
	// widgetConfigs optionally extracts the widget config instances to
	// validate against widgetschema.ConfigSchemas (ServiceCard,
	// InfoWidget); nil for kinds without widgets (Bookmark), which then
	// never get a ConfigValid condition — matching the pre-refactor
	// behavior.
	widgetConfigs func(obj T) []widgetConfigInstance
}

// Reconcile validates that obj's dashboardRef resolves to an existing
// Dashboard in the same namespace and sets the Available (and, for kinds
// with widgets, ConfigValid) status condition accordingly.
func (r *boundConfigReconciler[T]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	obj := r.newObj()
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get "+r.displayName)
		return ctrl.Result{}, err
	}

	cond, err := boundDashboardCondition(ctx, r.Client, obj.GetNamespace(), r.refName(obj), obj.GetGeneration())
	if err != nil {
		log.Error(err, "Failed to get referenced Dashboard")
		return ctrl.Result{}, err
	}

	if r.widgetConfigs != nil {
		availableOverride, configValid := validateWidgetConfigs(r.widgetConfigs(obj), obj.GetGeneration())
		if cond.Status == metav1.ConditionTrue && availableOverride != nil {
			cond = *availableOverride
		}
		meta.SetStatusCondition(r.conditions(obj), cond)
		meta.SetStatusCondition(r.conditions(obj), configValid)
	} else {
		meta.SetStatusCondition(r.conditions(obj), cond)
	}
	r.applyEntries(obj)

	if err := r.Status().Update(ctx, obj); err != nil {
		log.Error(err, "Failed to update "+r.displayName+" status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager, watching both T
// and Dashboard: an out-of-order apply (a config object applied before its
// Dashboard) self-heals once the Dashboard shows up, without waiting for the
// config object itself to be touched again.
func (r *boundConfigReconciler[T]) SetupWithManager(mgr ctrl.Manager, forObj T) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(forObj).
		Named(r.controllerName).
		Watches(
			&pagev1alpha1.Dashboard{},
			handler.EnqueueRequestsFromMapFunc(r.mapDashboardToRequests(mgr.GetClient())),
		).
		Complete(r)
}

// mapDashboardToRequests builds the Dashboard-watch map func: on a Dashboard
// event, enqueue every T in the same namespace that could be affected —
// those with an explicit dashboardRef naming it, plus (since an unset ref
// could newly bind to it, or stop being able to) those with dashboardRef
// unset. Both cases are answered with a single indexed lookup each against c
// (the manager's cache-backed client, via dashboardRefIndexKey) rather than
// listing every T in the namespace and filtering in Go.
func (r *boundConfigReconciler[T]) mapDashboardToRequests(c client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		instance, ok := obj.(*pagev1alpha1.Dashboard)
		if !ok {
			return nil
		}

		var reqs []reconcile.Request
		for _, refValue := range [2]string{instance.Name, ""} {
			list := r.newList()
			if err := c.List(ctx, list, client.InNamespace(instance.Namespace), client.MatchingFields{dashboardRefIndexKey: refValue}); err != nil {
				continue
			}
			for _, t := range r.listItems(list) {
				reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      t.GetName(),
					Namespace: t.GetNamespace(),
				}})
			}
		}
		return reqs
	}
}

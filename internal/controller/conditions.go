package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const (
	// reasonReconciling marks a condition as Unknown while a reconcile is in
	// flight and the outcome isn't settled yet.
	reasonReconciling = "Reconciling"
	// reasonFinalizing marks the Degraded condition while finalizer cleanup
	// is in progress (Unknown) or has completed (True).
	reasonFinalizing = "Finalizing"
	// reasonDashboardNotFound marks Available=False on a config CRD whose
	// dashboardRef does not resolve to an existing Dashboard in its
	// namespace — either an explicit ref names one that doesn't exist, or
	// dashboardRef is unset and the namespace has no Dashboard at all to
	// default to.
	reasonDashboardNotFound = "DashboardNotFound"
	// reasonAmbiguousDashboardRef marks Available=False on a config CRD
	// whose dashboardRef is unset in a namespace with more than one
	// Dashboard — there's no sole Dashboard to default to.
	reasonAmbiguousDashboardRef = "AmbiguousDashboardRef"
	// reasonBound marks Available=True on a config CRD whose dashboardRef
	// resolves to an existing Dashboard.
	reasonBound = "Bound"
	// reasonInvalidWidgetConfig marks Available=False on a ServiceCard/
	// InfoWidget with a widget whose config/options block is missing a
	// required key (per widgetschema.ConfigSchemas) or isn't a JSON object
	// at all; also used as ConfigValid=False's reason in the same case.
	reasonInvalidWidgetConfig = "InvalidWidgetConfig"
	// reasonUnknownConfigKeys marks ConfigValid=False on a ServiceCard/
	// InfoWidget with a widget whose config/options block has a key not in
	// widgetschema.ConfigSchemas' Required or Optional lists — not fatal
	// (forward compatibility), so it never flips Available.
	reasonUnknownConfigKeys = "UnknownConfigKeys"
	// reasonConfigValid marks ConfigValid=True: every widget's config/
	// options block has every required key and no unrecognized ones.
	reasonConfigValid = "ConfigValid"
)

// typeAvailableBound represents whether a config CRD's dashboardRef resolves
// to an existing Dashboard. Shared by every thin config-CRD controller
// (ServiceCard, Bookmark, InfoWidget, and future ones with the same shape).
const typeAvailableBound = "Available"

// typeConfigValid represents whether every widget on a ServiceCard/InfoWidget
// has a config/options block whose keys match widgetschema.ConfigSchemas for
// its type: no missing required keys, no unrecognized ones. Set
// unconditionally (independent of typeAvailableBound) so a config typo is
// visible even when the object is otherwise Available.
const typeConfigValid = "ConfigValid"

// boundDashboardCondition returns the Available condition for a config CRD
// instance whose dashboardRef.name is instanceRefName, as returned by
// pagev1alpha1.RefName ("" means dashboardRef is unset). An explicit ref
// resolves to True/Bound if that Dashboard exists in namespace, else
// False/DashboardNotFound. An unset ref defaults to the namespace's sole
// Dashboard: True/Bound if there's exactly one, False/DashboardNotFound if
// there are none, False/AmbiguousDashboardRef (naming every candidate) if
// there's more than one — mirroring pagev1alpha1.BoundTo, which the
// dashboard pod uses to make the same call. generation is the config CRD
// object's own metadata.generation, stamped onto the returned condition's
// ObservedGeneration so `kubectl wait --for=condition=Available` can't be
// satisfied by a stale condition left over from before the object's most
// recent spec change.
func boundDashboardCondition(ctx context.Context, c client.Client, namespace, instanceRefName string, generation int64) (metav1.Condition, error) {
	if instanceRefName != "" {
		instance := &pagev1alpha1.Dashboard{}
		err := c.Get(ctx, types.NamespacedName{Name: instanceRefName, Namespace: namespace}, instance)
		switch {
		case apierrors.IsNotFound(err):
			return metav1.Condition{
				Type: typeAvailableBound, Status: metav1.ConditionFalse, Reason: reasonDashboardNotFound,
				Message:            fmt.Sprintf("Dashboard %q referenced by dashboardRef does not exist in namespace %q", instanceRefName, namespace),
				ObservedGeneration: generation,
			}, nil
		case err != nil:
			return metav1.Condition{}, err
		default:
			return metav1.Condition{
				Type: typeAvailableBound, Status: metav1.ConditionTrue, Reason: reasonBound,
				Message:            fmt.Sprintf("Bound to Dashboard %q", instanceRefName),
				ObservedGeneration: generation,
			}, nil
		}
	}

	var dashboards pagev1alpha1.DashboardList
	if err := c.List(ctx, &dashboards, client.InNamespace(namespace)); err != nil {
		return metav1.Condition{}, err
	}
	switch len(dashboards.Items) {
	case 0:
		return metav1.Condition{
			Type: typeAvailableBound, Status: metav1.ConditionFalse, Reason: reasonDashboardNotFound,
			Message:            fmt.Sprintf("dashboardRef is not set and namespace %q has no Dashboard to default to", namespace),
			ObservedGeneration: generation,
		}, nil
	case 1:
		name := dashboards.Items[0].Name
		return metav1.Condition{
			Type: typeAvailableBound, Status: metav1.ConditionTrue, Reason: reasonBound,
			Message:            fmt.Sprintf("dashboardRef is not set; defaulted to namespace %q's sole Dashboard %q", namespace, name),
			ObservedGeneration: generation,
		}, nil
	default:
		names := make([]string, len(dashboards.Items))
		for i, d := range dashboards.Items {
			names[i] = d.Name
		}
		slices.Sort(names)
		return metav1.Condition{
			Type: typeAvailableBound, Status: metav1.ConditionFalse, Reason: reasonAmbiguousDashboardRef,
			Message:            fmt.Sprintf("dashboardRef is not set and namespace %q has multiple Dashboards (%s); set dashboardRef to disambiguate", namespace, strings.Join(names, ", ")),
			ObservedGeneration: generation,
		}, nil
	}
}

// namespaceDashboardCount returns how many Dashboards exist in namespace, for
// pagev1alpha1.BoundTo callers that need to decide whether an unset
// dashboardRef resolves unambiguously to a sole Dashboard.
func namespaceDashboardCount(ctx context.Context, c client.Client, namespace string) (int, error) {
	var dashboards pagev1alpha1.DashboardList
	if err := c.List(ctx, &dashboards, client.InNamespace(namespace)); err != nil {
		return 0, err
	}
	return len(dashboards.Items), nil
}

// dashboardWatchMayAffect reports whether a Dashboard-triggered watch event
// on the Dashboard named instanceName could change a config object's own
// bound status, given that object's dashboardRef.name refName (as returned
// by pagev1alpha1.RefName; "" means unset). Used by each config CRD
// controller's own Watches(&Dashboard{}) (see ServiceCardReconciler,
// BookmarkReconciler, InfoWidgetReconciler SetupWithManager) to decide which
// of its objects to re-reconcile on that event.
//
// An explicit ref only ever cares about the Dashboard it names. An unset ref
// always needs a fresh look: unlike pagev1alpha1.BoundTo, which decides
// whether an object binds to one specific Dashboard under the *current*
// namespace Dashboard count, this predicate must fire on the event that
// *changes* that count — a Dashboard newly created or deleted — even when
// the object doesn't (yet, or any longer) bind to instanceName. Filtering by
// BoundTo's post-event answer here would miss exactly the case where a
// second Dashboard's creation should flip a previously-defaulted object from
// Bound to AmbiguousDashboardRef: BoundTo(unset ref, new Dashboard's name,
// count=2) is false, so the object would never get re-enqueued and its
// stale Bound condition would never be corrected.
func dashboardWatchMayAffect(refName, instanceName string) bool {
	return refName == instanceName || refName == ""
}

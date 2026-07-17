package controller

import (
	"context"
	"fmt"

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
	// dashboardRef does not resolve to an existing Dashboard in its namespace.
	reasonDashboardNotFound = "DashboardNotFound"
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
// instance that carries an dashboardRef naming instanceRefName: True if that
// Dashboard exists in namespace, False/DashboardNotFound otherwise.
// generation is the config CRD object's own metadata.generation, stamped
// onto the returned condition's ObservedGeneration so `kubectl wait
// --for=condition=Available` can't be satisfied by a stale condition left
// over from before the object's most recent spec change.
func boundDashboardCondition(ctx context.Context, c client.Client, namespace, instanceRefName string, generation int64) (metav1.Condition, error) {
	if instanceRefName == "" {
		// CRD "required" only checks key-presence, not a non-empty value, so
		// dashboardRef.name: "" passes admission; Get-ing an empty name
		// returns a client-side error rather than NotFound, so handle it
		// explicitly rather than letting it fall through as a real error.
		return metav1.Condition{
			Type: typeAvailableBound, Status: metav1.ConditionFalse, Reason: reasonDashboardNotFound,
			Message: "dashboardRef.name is not set", ObservedGeneration: generation,
		}, nil
	}

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

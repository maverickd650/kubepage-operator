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
)

// typeAvailableBound represents whether a config CRD's dashboardRef resolves
// to an existing Dashboard. Shared by every thin config-CRD controller
// (DashboardStyle, ServiceCard, and future ones with the same shape).
const typeAvailableBound = "Available"

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

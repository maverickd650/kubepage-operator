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
	reasonReconciling      = "Reconciling"
	reasonFinalizing       = "Finalizing"
	reasonInstanceNotFound = "InstanceNotFound"
)

// typeAvailableBound represents whether a config CRD's instanceRef resolves
// to an existing Instance. Shared by every thin config-CRD controller
// (Configuration, ServiceEntry, and future ones with the same shape).
const typeAvailableBound = "Available"

// boundInstanceCondition returns the Available condition for a config CRD
// instance that carries an instanceRef naming instanceRefName: True if that
// Instance exists in namespace, False/InstanceNotFound otherwise.
func boundInstanceCondition(ctx context.Context, c client.Client, namespace, instanceRefName string) (metav1.Condition, error) {
	if instanceRefName == "" {
		// CRD "required" only checks key-presence, not a non-empty value, so
		// instanceRef.name: "" passes admission; Get-ing an empty name
		// returns a client-side error rather than NotFound, so handle it
		// explicitly rather than letting it fall through as a real error.
		return metav1.Condition{
			Type: typeAvailableBound, Status: metav1.ConditionFalse, Reason: reasonInstanceNotFound,
			Message: "instanceRef.name is not set",
		}, nil
	}

	instance := &pagev1alpha1.Instance{}
	err := c.Get(ctx, types.NamespacedName{Name: instanceRefName, Namespace: namespace}, instance)
	switch {
	case apierrors.IsNotFound(err):
		return metav1.Condition{
			Type: typeAvailableBound, Status: metav1.ConditionFalse, Reason: reasonInstanceNotFound,
			Message: fmt.Sprintf("Instance %q referenced by instanceRef does not exist in namespace %q", instanceRefName, namespace),
		}, nil
	case err != nil:
		return metav1.Condition{}, err
	default:
		return metav1.Condition{
			Type: typeAvailableBound, Status: metav1.ConditionTrue, Reason: reasonReconciling,
			Message: fmt.Sprintf("Bound to Instance %q", instanceRefName),
		}, nil
	}
}

package controller

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// dashboardRefIndexKey is the field index registered (via
// SetupDashboardRefIndexers) for every config CRD kind's
// spec.dashboardRef.name, so a Dashboard-triggered watch can look up
// matching objects with an in-cache indexed client.List(client.MatchingFields{...})
// instead of listing every object in the namespace and filtering in Go. The
// indexed value is pagev1alpha1.RefName(obj)'s dashboardRef — "" for an
// unset ref, so client.MatchingFields{dashboardRefIndexKey: ""} finds every
// object that could default to the namespace's sole Dashboard.
const dashboardRefIndexKey = "spec.dashboardRef.name"

// SetupDashboardRefIndexers registers the dashboardRefIndexKey field index
// for every config CRD kind that carries a dashboardRef. Must be called
// before mgr.Start, and before any controller's SetupWithManager that relies
// on the index (see boundConfigReconciler.mapDashboardToRequests).
func SetupDashboardRefIndexers(ctx context.Context, mgr ctrl.Manager) error {
	indexers := []struct {
		obj     client.Object
		extract client.IndexerFunc
	}{
		{
			obj: &pagev1alpha1.ServiceCard{},
			extract: func(o client.Object) []string {
				return []string{pagev1alpha1.RefName(o.(*pagev1alpha1.ServiceCard).Spec.DashboardRef)}
			},
		},
		{
			obj: &pagev1alpha1.Bookmark{},
			extract: func(o client.Object) []string {
				return []string{pagev1alpha1.RefName(o.(*pagev1alpha1.Bookmark).Spec.DashboardRef)}
			},
		},
		{
			obj: &pagev1alpha1.InfoWidget{},
			extract: func(o client.Object) []string {
				return []string{pagev1alpha1.RefName(o.(*pagev1alpha1.InfoWidget).Spec.DashboardRef)}
			},
		},
	}
	for _, idx := range indexers {
		if err := mgr.GetFieldIndexer().IndexField(ctx, idx.obj, dashboardRefIndexKey, idx.extract); err != nil {
			return err
		}
	}
	return nil
}

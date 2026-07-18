package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// bookmarkAdapter returns the same *boundConfigReconciler[*Bookmark] the real
// BookmarkReconciler wires up, for exercising mapDashboardToRequests against
// an indexed fake client (the envtest suite's k8sClient is an uncached
// client, so it can't exercise client.MatchingFields — see indexers.go).
func bookmarkAdapter(c client.Client) *boundConfigReconciler[*pagev1alpha1.Bookmark] {
	return (&BookmarkReconciler{Client: c}).adapter()
}

func TestBoundConfigReconcilerMapDashboardToRequests(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Name: testRefDashboardName, Namespace: "ns"}}
	bound := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "bound", Namespace: "ns"},
		Spec:       pagev1alpha1.BookmarkSpec{DashboardRef: &pagev1alpha1.DashboardRef{Name: testRefDashboardName}},
	}
	unset := &pagev1alpha1.Bookmark{ObjectMeta: metav1.ObjectMeta{Name: "unset", Namespace: "ns"}}
	otherRef := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "other-ref", Namespace: "ns"},
		Spec:       pagev1alpha1.BookmarkSpec{DashboardRef: &pagev1alpha1.DashboardRef{Name: testOtherDashboardName}},
	}
	otherNamespace := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "other-namespace-bookmark", Namespace: "elsewhere"},
		Spec:       pagev1alpha1.BookmarkSpec{DashboardRef: &pagev1alpha1.DashboardRef{Name: testRefDashboardName}},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance, bound, unset, otherRef, otherNamespace).
		WithIndex(&pagev1alpha1.Bookmark{}, dashboardRefIndexKey, func(o client.Object) []string {
			return []string{pagev1alpha1.RefName(o.(*pagev1alpha1.Bookmark).Spec.DashboardRef)}
		}).
		Build()

	mapFn := bookmarkAdapter(cl).mapDashboardToRequests(cl)

	t.Run("enqueues the explicitly-bound and unset-ref Bookmarks in the Dashboard's namespace", func(t *testing.T) {
		reqs := mapFn(t.Context(), instance)
		got := map[string]bool{}
		for _, r := range reqs {
			if r.Namespace != "ns" {
				t.Errorf("mapFn() enqueued %+v outside namespace %q", r, "ns")
			}
			got[r.Name] = true
		}
		if len(reqs) != 2 || !got["bound"] || !got["unset"] {
			t.Errorf("mapFn() = %+v, want exactly {bound, unset}", reqs)
		}
	})

	t.Run("returns nil for an object of the wrong type", func(t *testing.T) {
		if reqs := mapFn(t.Context(), &pagev1alpha1.ServiceCard{}); reqs != nil {
			t.Errorf("mapFn() = %+v, want nil for a non-Dashboard object", reqs)
		}
	})
}

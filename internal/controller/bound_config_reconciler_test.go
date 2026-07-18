package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// runMapDashboardToRequestsTest exercises one kind's
// boundConfigReconciler.mapDashboardToRequests against an indexed fake
// client (the envtest suite's k8sClient is an uncached client, so it can't
// exercise client.MatchingFields — see indexers.go). newObj builds a T named
// name in namespace ns with dashboardRef.name ref ("" for unset).
func runMapDashboardToRequestsTest[T client.Object](
	t *testing.T,
	newEmpty T,
	adapter *boundConfigReconciler[T],
	indexExtract client.IndexerFunc,
	newObj func(name, ns, ref string) T,
) {
	t.Helper()

	instance := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Name: testRefDashboardName, Namespace: "ns"}}
	bound := newObj("bound", "ns", testRefDashboardName)
	unset := newObj("unset", "ns", "")
	otherRef := newObj("other-ref", "ns", testOtherDashboardName)
	otherNamespace := newObj("other-namespace", "elsewhere", testRefDashboardName)

	scheme := networkTestScheme(t)
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance, bound, unset, otherRef, otherNamespace).
		WithIndex(newEmpty, dashboardRefIndexKey, indexExtract).
		Build()

	mapFn := adapter.mapDashboardToRequests(cl)

	t.Run("enqueues the explicitly-bound and unset-ref objects in the Dashboard's namespace", func(t *testing.T) {
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
		// mapDashboardToRequests type-asserts its input against *Dashboard,
		// not T, so any non-Dashboard object exercises the branch.
		if reqs := mapFn(t.Context(), &pagev1alpha1.Bookmark{}); reqs != nil {
			t.Errorf("mapFn() = %+v, want nil for a non-Dashboard object", reqs)
		}
	})
}

func TestBoundConfigReconcilerMapDashboardToRequestsBookmark(t *testing.T) {
	adapter := (&BookmarkReconciler{}).adapter()
	runMapDashboardToRequestsTest(t, &pagev1alpha1.Bookmark{}, adapter,
		func(o client.Object) []string {
			return []string{pagev1alpha1.RefName(o.(*pagev1alpha1.Bookmark).Spec.DashboardRef)}
		},
		func(name, ns, ref string) *pagev1alpha1.Bookmark {
			bm := &pagev1alpha1.Bookmark{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
			if ref != "" {
				bm.Spec.DashboardRef = &pagev1alpha1.DashboardRef{Name: ref}
			}
			return bm
		},
	)
}

func TestBoundConfigReconcilerMapDashboardToRequestsServiceCard(t *testing.T) {
	adapter := (&ServiceCardReconciler{}).adapter()
	runMapDashboardToRequestsTest(t, &pagev1alpha1.ServiceCard{}, adapter,
		func(o client.Object) []string {
			return []string{pagev1alpha1.RefName(o.(*pagev1alpha1.ServiceCard).Spec.DashboardRef)}
		},
		func(name, ns, ref string) *pagev1alpha1.ServiceCard {
			sc := &pagev1alpha1.ServiceCard{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
			if ref != "" {
				sc.Spec.DashboardRef = &pagev1alpha1.DashboardRef{Name: ref}
			}
			return sc
		},
	)
}

func TestBoundConfigReconcilerMapDashboardToRequestsInfoWidget(t *testing.T) {
	adapter := (&InfoWidgetReconciler{}).adapter()
	runMapDashboardToRequestsTest(t, &pagev1alpha1.InfoWidget{}, adapter,
		func(o client.Object) []string {
			return []string{pagev1alpha1.RefName(o.(*pagev1alpha1.InfoWidget).Spec.DashboardRef)}
		},
		func(name, ns, ref string) *pagev1alpha1.InfoWidget {
			iw := &pagev1alpha1.InfoWidget{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
			if ref != "" {
				iw.Spec.DashboardRef = &pagev1alpha1.DashboardRef{Name: ref}
			}
			return iw
		},
	)
}

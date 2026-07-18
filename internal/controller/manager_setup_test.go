package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// This spec exercises the wiring cmd/main.go does at startup — registering
// the dashboardRef field indexers, then each thin config CRD controller's
// SetupWithManager — against a real Manager backed by the envtest apiserver,
// since neither is otherwise built in this package's tests (every other spec
// talks to k8sClient directly, without a running Manager/cache).
var _ = Describe("Manager wiring", func() {
	It("registers the dashboardRef field indexers and every thin config CRD controller without error", func() {
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(SetupDashboardRefIndexers(ctx, mgr)).To(Succeed())

		Expect((&BookmarkReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr)).To(Succeed())
		Expect((&ServiceCardReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr)).To(Succeed())
		Expect((&InfoWidgetReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr)).To(Succeed())
	})

	It("indexes every config CRD kind's dashboardRef so a cache-backed MatchingFields lookup finds it", func() {
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(SetupDashboardRefIndexers(ctx, mgr)).To(Succeed())

		mgrCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() { _ = mgr.Start(mgrCtx) }()
		Expect(mgr.GetCache().WaitForCacheSync(mgrCtx)).To(BeTrue())

		const ns = policyTestNamespace
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "index-bm", Namespace: ns},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: testRefDashboardName},
				Group:        "G",
				Bookmarks:    []pagev1alpha1.BookmarkEntry{{Name: "N", Href: "https://example.com"}},
			},
		}
		Expect(k8sClient.Create(ctx, bm)).To(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, bm)).To(Succeed()) }()

		sc := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "index-sc", Namespace: ns},
			Spec: pagev1alpha1.ServiceCardSpec{
				Group:    "G",
				Services: []pagev1alpha1.ServiceEntry{{Name: "N"}},
			},
		}
		Expect(k8sClient.Create(ctx, sc)).To(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, sc)).To(Succeed()) }()

		iw := &pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: "index-iw", Namespace: ns},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: testOtherDashboardName},
				Widgets:      []pagev1alpha1.InfoWidgetEntry{{Type: "datetime"}},
			},
		}
		Expect(k8sClient.Create(ctx, iw)).To(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, iw)).To(Succeed()) }()

		Eventually(func(g Gomega) {
			var bmList pagev1alpha1.BookmarkList
			g.Expect(mgr.GetClient().List(ctx, &bmList, client.InNamespace(ns), client.MatchingFields{dashboardRefIndexKey: testRefDashboardName})).To(Succeed())
			g.Expect(bmList.Items).To(HaveLen(1))

			var scList pagev1alpha1.ServiceCardList
			g.Expect(mgr.GetClient().List(ctx, &scList, client.InNamespace(ns), client.MatchingFields{dashboardRefIndexKey: ""})).To(Succeed())
			g.Expect(scList.Items).To(HaveLen(1))

			var iwList pagev1alpha1.InfoWidgetList
			g.Expect(mgr.GetClient().List(ctx, &iwList, client.InNamespace(ns), client.MatchingFields{dashboardRefIndexKey: testOtherDashboardName})).To(Succeed())
			g.Expect(iwList.Items).To(HaveLen(1))
		}).Should(Succeed())
	})
})

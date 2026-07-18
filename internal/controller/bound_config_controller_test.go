package controller

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// fetchStatus re-Gets o (by concrete type) via k8sClient and returns its
// status conditions, for asserting what a thin config CRD controller's
// Reconcile wrote. o must be one of the three thin config CRD kinds sharing
// boundConfigReconciler.
func fetchStatus(ctx context.Context, o client.Object) []metav1.Condition {
	key := client.ObjectKeyFromObject(o)
	switch o.(type) {
	case *pagev1alpha1.Bookmark:
		fresh := &pagev1alpha1.Bookmark{}
		Expect(k8sClient.Get(ctx, key, fresh)).To(Succeed())
		return fresh.Status.Conditions
	case *pagev1alpha1.ServiceCard:
		fresh := &pagev1alpha1.ServiceCard{}
		Expect(k8sClient.Get(ctx, key, fresh)).To(Succeed())
		return fresh.Status.Conditions
	case *pagev1alpha1.InfoWidget:
		fresh := &pagev1alpha1.InfoWidget{}
		Expect(k8sClient.Get(ctx, key, fresh)).To(Succeed())
		return fresh.Status.Conditions
	default:
		Fail("fetchStatus: unsupported object type")
		return nil
	}
}

// This spec covers the reconcile behavior shared by every thin config CRD
// controller (ServiceCard, Bookmark, InfoWidget) — see boundConfigReconciler
// — against a real envtest apiserver, once per kind via
// reconcilerErrorPathCases' newObject/newReconciler table (also used by
// reconcile_error_paths_test.go's fake-client error-path cases). The one
// real behavioral difference between kinds — ServiceCard/InfoWidget also get
// a ConfigValid condition (they carry widgets), Bookmark doesn't — is
// asserted per kind rather than folded away.
var _ = Describe("Thin config CRD controllers", func() {
	ctx := context.Background()

	for _, tc := range reconcilerErrorPathCases() {
		Describe(tc.name, func() {
			It("sets Available=True/Bound when dashboardRef resolves to an existing Dashboard", func() {
				dash := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Name: "dash-bound-" + strings.ToLower(tc.name), Namespace: policyTestNamespace}}
				Expect(k8sClient.Create(ctx, dash)).To(Succeed())
				defer func() { Expect(k8sClient.Delete(ctx, dash)).To(Succeed()) }()

				obj := tc.newObject(policyTestNamespace, "bound-"+strings.ToLower(tc.name), dash.Name)
				Expect(k8sClient.Create(ctx, obj)).To(Succeed())
				defer func() { Expect(k8sClient.Delete(ctx, obj)).To(Succeed()) }()

				_, err := tc.newReconciler(k8sClient).Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: obj.GetName(), Namespace: policyTestNamespace},
				})
				Expect(err).NotTo(HaveOccurred())

				cond := meta.FindStatusCondition(fetchStatus(ctx, obj), typeAvailableBound)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				Expect(cond.Reason).To(Equal(reasonBound))
			})

			It("sets Available=False/DashboardNotFound when dashboardRef does not resolve", func() {
				obj := tc.newObject(policyTestNamespace, "notfound-"+strings.ToLower(tc.name), testDoesNotExistDashboardName)
				Expect(k8sClient.Create(ctx, obj)).To(Succeed())
				defer func() { Expect(k8sClient.Delete(ctx, obj)).To(Succeed()) }()

				_, err := tc.newReconciler(k8sClient).Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: obj.GetName(), Namespace: policyTestNamespace},
				})
				Expect(err).NotTo(HaveOccurred())

				cond := meta.FindStatusCondition(fetchStatus(ctx, obj), typeAvailableBound)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				Expect(cond.Reason).To(Equal(reasonDashboardNotFound))
			})

			It("sets ConfigValid only for kinds that carry widgets (ServiceCard, InfoWidget)", func() {
				obj := tc.newObject(policyTestNamespace, "configvalid-"+strings.ToLower(tc.name), testDoesNotExistDashboardName)
				Expect(k8sClient.Create(ctx, obj)).To(Succeed())
				defer func() { Expect(k8sClient.Delete(ctx, obj)).To(Succeed()) }()

				_, err := tc.newReconciler(k8sClient).Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: obj.GetName(), Namespace: policyTestNamespace},
				})
				Expect(err).NotTo(HaveOccurred())

				cond := meta.FindStatusCondition(fetchStatus(ctx, obj), typeConfigValid)
				switch obj.(type) {
				case *pagev1alpha1.Bookmark:
					Expect(cond).To(BeNil(), "Bookmark has no widgets, so no ConfigValid condition")
				default:
					Expect(cond).NotTo(BeNil())
					Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					Expect(cond.Reason).To(Equal(reasonConfigValid))
				}
			})
		})
	}
})

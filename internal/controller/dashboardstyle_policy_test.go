package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify the DashboardStyle CRD schema CEL rules
// (api/v1alpha1/dashboardstyle_types.go's XValidation markers) actually
// reject the shapes they forbid, following the same envtest-backed pattern
// as dashboard_policy_test.go/secret_source_validation_test.go.
var _ = Describe("DashboardStyle CRD schema validation", func() {
	Describe("metadata.name == spec.dashboardRef.name", func() {
		It("rejects a DashboardStyle whose metadata.name differs from spec.dashboardRef.name", func() {
			ds := dashboardStyleNamed("style-mismatch", "some-other-dashboard")
			err := k8sClient.Create(ctx, ds)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("admits a DashboardStyle whose metadata.name equals spec.dashboardRef.name", func() {
			ds := dashboardStyleNamed(policyDashboardRef, policyDashboardRef)
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ds)).To(Succeed())
		})
	})

	Describe("SearchSpec (spec.search)", func() {
		It("rejects provider \"custom\" with no url", func() {
			ds := dashboardStyleWithSearch("style-search-nourl", &pagev1alpha1.SearchSpec{
				Provider: new("custom"),
			})
			err := k8sClient.Create(ctx, ds)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("admits provider \"custom\" with a url set", func() {
			ds := dashboardStyleWithSearch("style-search-custom", &pagev1alpha1.SearchSpec{
				Provider: new("custom"),
				URL:      new("https://example.invalid/search"),
			})
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ds)).To(Succeed())
		})

		It("admits a non-custom provider with no url", func() {
			ds := dashboardStyleWithSearch("style-search-duckduckgo", &pagev1alpha1.SearchSpec{
				Provider: new("duckduckgo"),
			})
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ds)).To(Succeed())
		})
	})
})

// dashboardStyleNamed builds a minimally-valid DashboardStyle with the given
// metadata.name and spec.dashboardRef.name (may differ, to exercise the
// name-must-match-ref CEL rule).
func dashboardStyleNamed(name, dashboardRefName string) *pagev1alpha1.DashboardStyle {
	return &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: dashboardRefName},
		},
	}
}

// dashboardStyleWithSearch builds a minimally-valid DashboardStyle (named
// after its own dashboardRef, satisfying the name-must-match-ref rule) with
// the given spec.search.
func dashboardStyleWithSearch(name string, search *pagev1alpha1.SearchSpec) *pagev1alpha1.DashboardStyle {
	return &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: name},
			Search:       search,
		},
	}
}

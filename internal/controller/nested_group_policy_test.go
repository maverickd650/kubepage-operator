package controller

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const (
	nestedGroupTab1        = "Tab1"
	nestedGroupTab2        = "Tab2"
	nestedGroupMediaMovies = "Media/Movies"
)

// These specs verify the nested-group CRD schema changes
// (docs/design/nested-groups.md): ServiceEntry.Group/ServiceCardSpec.Group's
// path pattern (api/v1alpha1/servicecard_types.go), LayoutGroupSpec.Name's
// matching pattern, and LayoutTabSpec's per-tab CEL rule keeping a nested
// group entry's root listed in the same tab (api/v1alpha1/dashboardstyle_types.go),
// following the same envtest-backed pattern as monitor_source_policy_test.go/
// dashboardstyle_policy_test.go.
var _ = Describe("Nested service-card group CRD schema validation", func() {
	Describe("ServiceEntry.Group path pattern", func() {
		DescribeTable("group value acceptance",
			func(group string, wantErr bool) {
				sc := serviceCardWithGroup("sc-group-"+sanitizeName(group), group)
				err := k8sClient.Create(ctx, sc)
				if wantErr {
					Expect(err).To(HaveOccurred())
					Expect(apierrors.IsInvalid(err)).To(BeTrue())
					return
				}
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Delete(ctx, sc)).To(Succeed())
			},
			Entry("admits a single-segment group", "a", false),
			Entry("admits a 3-segment nested path", "a/b/c", false),
			Entry("rejects a leading slash", "/a", true),
			Entry("rejects an empty segment", "a//b", true),
			Entry("rejects a 4-segment path (over the depth-3 limit)", "a/b/c/d", true),
		)
	})

	Describe("LayoutGroupSpec.Name path pattern", func() {
		DescribeTable("name value acceptance",
			func(name string, wantErr bool) {
				// An admitted path entry must also list its root in the same
				// tab (LayoutTabSpec's parent-listed CEL rule), so prepend it
				// for the acceptance cases — this table exercises only the
				// Name pattern, the tab rule has its own specs below.
				groups := []pagev1alpha1.LayoutGroupSpec{{Name: name}}
				if i := strings.Index(name, "/"); i > 0 && !wantErr {
					groups = append([]pagev1alpha1.LayoutGroupSpec{{Name: name[:i]}}, groups...)
				}
				ds := dashboardStyleWithLayout("style-name-"+sanitizeName(name), []pagev1alpha1.LayoutTabSpec{
					{Name: nestedGroupTab1, Groups: groups},
				})
				err := k8sClient.Create(ctx, ds)
				if wantErr {
					Expect(err).To(HaveOccurred())
					Expect(apierrors.IsInvalid(err)).To(BeTrue())
					return
				}
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Delete(ctx, ds)).To(Succeed())
			},
			Entry("admits a single-segment name", "a", false),
			Entry("admits a 3-segment nested path", "a/b/c", false),
			Entry("rejects a leading slash", "/a", true),
			Entry("rejects an empty segment", "a//b", true),
			Entry("rejects a 4-segment path (over the depth-3 limit)", "a/b/c/d", true),
		)
	})

	Describe("LayoutTabSpec root-listed rule", func() {
		It("rejects a path entry whose root isn't listed in the same tab", func() {
			ds := dashboardStyleWithLayout("style-orphan-path", []pagev1alpha1.LayoutTabSpec{
				{Name: nestedGroupTab1, Groups: []pagev1alpha1.LayoutGroupSpec{{Name: nestedGroupMediaMovies}}},
			})
			err := k8sClient.Create(ctx, ds)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("admits a path entry whose root is also listed in the same tab", func() {
			ds := dashboardStyleWithLayout("style-rooted-path", []pagev1alpha1.LayoutTabSpec{
				{Name: nestedGroupTab1, Groups: []pagev1alpha1.LayoutGroupSpec{{Name: testMultiFormGroupMedia}, {Name: nestedGroupMediaMovies}}},
			})
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ds)).To(Succeed())
		})

		It("admits a depth-3 path entry whose root alone is listed (ancestor prefix suffices)", func() {
			ds := dashboardStyleWithLayout("style-deep-rooted-path", []pagev1alpha1.LayoutTabSpec{
				{Name: nestedGroupTab1, Groups: []pagev1alpha1.LayoutGroupSpec{{Name: testMultiFormGroupMedia}, {Name: nestedGroupMediaMovies + "/4K"}}},
			})
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ds)).To(Succeed())
		})

		It("rejects a path entry whose root is only listed in a different tab", func() {
			ds := dashboardStyleWithLayout("style-cross-tab-orphan", []pagev1alpha1.LayoutTabSpec{
				{Name: nestedGroupTab1, Groups: []pagev1alpha1.LayoutGroupSpec{{Name: testMultiFormGroupMedia}}},
				{Name: nestedGroupTab2, Groups: []pagev1alpha1.LayoutGroupSpec{{Name: nestedGroupMediaMovies}}},
			})
			err := k8sClient.Create(ctx, ds)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})
	})
})

// serviceCardWithGroup builds a minimally-valid ServiceCard whose single
// services entry sets Group to the given value (exercising the CRD schema's
// Pattern marker rather than any controller logic).
func serviceCardWithGroup(name, group string) *pagev1alpha1.ServiceCard {
	return &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
			Services: []pagev1alpha1.ServiceEntry{
				{Name: "svc", Group: group},
			},
		},
	}
}

// dashboardStyleWithLayout builds a minimally-valid DashboardStyle (named
// after its own dashboardRef, satisfying the name-must-match-ref rule) with
// the given spec.layout.
func dashboardStyleWithLayout(name string, layout []pagev1alpha1.LayoutTabSpec) *pagev1alpha1.DashboardStyle {
	return &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: name},
			Layout:       layout,
		},
	}
}

// sanitizeName maps a test group/name value (which may itself contain "/",
// invalid in a Kubernetes object name) to a DNS-1123-safe object name
// fragment, distinct enough per case for envtest's shared namespace.
func sanitizeName(s string) string {
	out := make([]byte, 0, len(s))
	for i := range len(s) {
		if s[i] == '/' {
			out = append(out, 's')
			continue
		}
		out = append(out, s[i])
	}
	if len(out) == 0 {
		out = append(out, 'x')
	}
	return string(out)
}

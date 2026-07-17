package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// This spec locks in that ServiceEntry's per-entry override fields with a
// site-wide DashboardStyle fallback (ErrorDisplay, StatusStyle) carry no
// schema default. A `+default=true` on ErrorDisplay previously caused the
// apiserver to stamp every entry with errorDisplay: true at admission,
// which made internal/dashboard/poller.go's "entry value if non-nil, else
// site default" resolution never fall back to DashboardStyle.spec.errorDisplay.
var _ = Describe("ServiceEntry schema defaults", func() {
	ctx := context.Background()

	It("leaves ErrorDisplay and StatusStyle nil when omitted, so the DashboardStyle default can apply", func() {
		name := "sc-defaults-fallback"
		sc := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDoesNotExistDashboardName},
				Group:        policyTestGroup,
				Services: []pagev1alpha1.ServiceEntry{
					{Name: "svc"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, sc)).To(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, sc)).To(Succeed()) }()

		got := &pagev1alpha1.ServiceCard{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: policyTestNamespace}, got)).To(Succeed())
		Expect(got.Spec.Services).To(HaveLen(1))
		Expect(got.Spec.Services[0].ErrorDisplay).To(BeNil())
		Expect(got.Spec.Services[0].StatusStyle).To(BeNil())
	})
})

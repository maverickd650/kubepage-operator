package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify the ServiceCardSpec CRD schema CEL rule
// (api/v1alpha1/servicecard_types.go's XValidation marker on that struct)
// rejects a ServiceCard that sets more than one of ping/siteMonitor/
// podSelector, while admitting zero or exactly one.
var _ = Describe("ServiceCard monitor-source CRD schema validation", func() {
	podSelector := func() *metav1.LabelSelector {
		return &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}
	}

	It("rejects ping + siteMonitor both set", func() {
		se := serviceEntryWithMonitors("se-ping-and-site", new("http://example.invalid/"), new("http://example.invalid/"), nil)
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects ping + podSelector both set", func() {
		se := serviceEntryWithMonitors("se-ping-and-pod", new("http://example.invalid/"), nil, podSelector())
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects siteMonitor + podSelector both set", func() {
		se := serviceEntryWithMonitors("se-site-and-pod", nil, new("http://example.invalid/"), podSelector())
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("admits podSelector alone", func() {
		se := serviceEntryWithMonitors("se-pod-only", nil, nil, podSelector())
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})

	It("admits no monitor source at all", func() {
		se := serviceEntryWithMonitors("se-no-monitor", nil, nil, nil)
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})
})

// serviceEntryWithMonitors builds a minimally-valid ServiceCard with the
// given combination of ping/siteMonitor/podSelector set (any may be nil).
func serviceEntryWithMonitors(name string, ping, siteMonitor *string, sel *metav1.LabelSelector) *pagev1alpha1.ServiceCard {
	return &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
			Group:        policyTestGroup,
			Name:         name,
			Ping:         ping,
			SiteMonitor:  siteMonitor,
			PodSelector:  sel,
		},
	}
}

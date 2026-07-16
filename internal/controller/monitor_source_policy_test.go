package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify the ServiceEntry CRD schema CEL rules
// (api/v1alpha1/servicecard_types.go's XValidation markers on that struct):
// ping/siteMonitor stay mutually exclusive with each other, the pod monitor
// (app/podSelector) is freely combinable with either of them, and namespace
// requires a pod monitor to be configured at all — see
// docs/design/combined-monitor.md.
var _ = Describe("ServiceCard monitor-source CRD schema validation", func() {
	podSelector := func() *metav1.LabelSelector {
		return &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}
	}

	It("rejects ping + siteMonitor both set on a services entry", func() {
		se := serviceEntryWithMonitors("se-ping-and-site", monitorSources{ping: new("http://example.invalid/"), siteMonitor: new("http://example.invalid/")})
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("admits siteMonitor + podSelector both set on a services entry", func() {
		se := serviceEntryWithMonitors("se-site-and-pod", monitorSources{siteMonitor: new("http://example.invalid/"), podSelector: podSelector()})
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})

	It("admits ping + app both set on a services entry", func() {
		se := serviceEntryWithMonitors("se-ping-and-app", monitorSources{ping: new("http://example.invalid/"), app: new("demo")})
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})

	It("admits app + podSelector both set on a services entry (podSelector wins)", func() {
		se := serviceEntryWithMonitors("se-app-and-pod", monitorSources{app: new("demo"), podSelector: podSelector()})
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})

	It("admits podSelector alone on a services entry", func() {
		se := serviceEntryWithMonitors("se-pod-only", monitorSources{podSelector: podSelector()})
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})

	It("admits no monitor source at all on a services entry", func() {
		se := serviceEntryWithMonitors("se-no-monitor", monitorSources{})
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})

	It("rejects namespace set without app or podSelector", func() {
		se := serviceEntryWithMonitors("se-namespace-without-pod-monitor", monitorSources{namespace: new("other-ns")})
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("admits namespace + app together on a services entry", func() {
		se := serviceEntryWithMonitors("se-namespace-and-app", monitorSources{namespace: new("other-ns"), app: new("demo")})
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})
})

// monitorSources bundles every monitor-related field serviceEntryWithMonitors
// can set on a single services entry; any field may be left nil.
type monitorSources struct {
	ping        *string
	siteMonitor *string
	app         *string
	namespace   *string
	podSelector *metav1.LabelSelector
}

// serviceEntryWithMonitors builds a minimally-valid ServiceCard whose single
// services entry carries the given combination of monitor fields.
func serviceEntryWithMonitors(name string, m monitorSources) *pagev1alpha1.ServiceCard {
	return &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
			Group:        policyTestGroup,
			Services: []pagev1alpha1.ServiceEntry{
				{
					Name:        name,
					Ping:        m.ping,
					SiteMonitor: m.siteMonitor,
					App:         m.app,
					Namespace:   m.namespace,
					PodSelector: m.podSelector,
				},
			},
		},
	}
}

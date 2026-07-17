package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify the ServiceEntry CRD schema rules
// (api/v1alpha1/servicecard_types.go's markers on that struct): monitor is a
// URL or the "self" sentinel ("self" requiring internalUrl or href to
// resolve against), the pod monitor (app/podSelector) is freely combinable
// with it, and namespace requires a pod monitor to be configured at all —
// see docs/design/combined-monitor.md.
var _ = Describe("ServiceCard monitor-source CRD schema validation", func() {
	podSelector := func() *metav1.LabelSelector {
		return &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}
	}

	It("rejects monitor: self without internalUrl or href", func() {
		se := serviceEntryWithMonitors("se-self-without-base", monitorSources{monitor: new(pagev1alpha1.MonitorSelf)})
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("admits monitor: self with href set", func() {
		se := serviceEntryWithMonitors("se-self-with-href", monitorSources{monitor: new(pagev1alpha1.MonitorSelf), href: new("http://example.invalid/")})
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})

	It("admits monitor: self with internalUrl set", func() {
		se := serviceEntryWithMonitors("se-self-with-internal", monitorSources{monitor: new(pagev1alpha1.MonitorSelf), internalURL: new("http://svc.ns.svc:8080")})
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})

	It("rejects a monitor value that is neither self nor an http(s) URL", func() {
		se := serviceEntryWithMonitors("se-bad-monitor", monitorSources{monitor: new("selfish")})
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("admits monitor + podSelector both set on a services entry", func() {
		se := serviceEntryWithMonitors("se-monitor-and-pod", monitorSources{monitor: new("http://example.invalid/"), podSelector: podSelector()})
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})

	It("admits monitor + app both set on a services entry", func() {
		se := serviceEntryWithMonitors("se-monitor-and-app", monitorSources{monitor: new("http://example.invalid/"), app: new("demo")})
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
	monitor     *string
	href        *string
	internalURL *string
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
					Monitor:     m.monitor,
					Href:        m.href,
					InternalURL: m.internalURL,
					App:         m.app,
					Namespace:   m.namespace,
					PodSelector: m.podSelector,
				},
			},
		},
	}
}

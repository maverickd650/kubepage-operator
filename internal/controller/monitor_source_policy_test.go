package controller

import (
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify the shipped ValidatingAdmissionPolicy
// (config/admission/serviceentry_monitor_source_policy.yaml) rejects a
// ServiceEntry that sets more than one of ping/siteMonitor/podSelector,
// while admitting zero or exactly one. Loading the real manifest (rather
// than reconstructing the CEL in Go) means a typo in the policy fails the
// build the same way the secret-source policy's test does.
var _ = Describe("ServiceEntry monitor-source ValidatingAdmissionPolicy", Ordered, func() {
	podSelector := func() *metav1.LabelSelector {
		return &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}
	}

	BeforeAll(func() {
		applyManifest(filepath.Join("..", "..", "config", "admission", "serviceentry_monitor_source_policy.yaml"))

		// Policy enforcement isn't instantaneous after the policy/binding are
		// created; poll a known-invalid object until it's rejected before
		// asserting individual cases, so the specs don't race the apiserver.
		Eventually(func() bool {
			se := serviceEntryWithMonitors("warmup", new("http://example.invalid/"), new("http://example.invalid/"), nil)
			err := k8sClient.Create(ctx, se)
			if err == nil {
				_ = k8sClient.Delete(ctx, se)
				return false
			}
			return apierrors.IsInvalid(err) || apierrors.IsForbidden(err)
		}, 30*time.Second, time.Second).Should(BeTrue(), "policy should begin rejecting ServiceEntries with multiple monitor sources")
	})

	It("rejects ping + siteMonitor both set", func() {
		se := serviceEntryWithMonitors("se-ping-and-site", new("http://example.invalid/"), new("http://example.invalid/"), nil)
		Expect(k8sClient.Create(ctx, se)).NotTo(Succeed())
	})

	It("rejects ping + podSelector both set", func() {
		se := serviceEntryWithMonitors("se-ping-and-pod", new("http://example.invalid/"), nil, podSelector())
		Expect(k8sClient.Create(ctx, se)).NotTo(Succeed())
	})

	It("rejects siteMonitor + podSelector both set", func() {
		se := serviceEntryWithMonitors("se-site-and-pod", nil, new("http://example.invalid/"), podSelector())
		Expect(k8sClient.Create(ctx, se)).NotTo(Succeed())
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

// serviceEntryWithMonitors builds a minimally-valid ServiceEntry with the
// given combination of ping/siteMonitor/podSelector set (any may be nil).
func serviceEntryWithMonitors(name string, ping, siteMonitor *string, sel *metav1.LabelSelector) *pagev1alpha1.ServiceEntry {
	return &pagev1alpha1.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.ServiceEntrySpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: policyInstanceRef},
			Group:       policyTestGroup,
			Name:        name,
			Ping:        ping,
			SiteMonitor: siteMonitor,
			PodSelector: sel,
		},
	}
}

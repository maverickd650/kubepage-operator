package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify the Dashboard CRD schema CEL rules
// (api/v1alpha1/dashboard_types.go's XValidation markers) actually reject
// the shapes they forbid, following the same envtest-backed pattern as
// secret_source_validation_test.go/monitor_source_policy_test.go: schema
// validation is baked into the CRD itself and enforced synchronously by the
// apiserver on every Create/Update, so there's no propagation delay to poll
// past (unlike the ValidatingAdmissionPolicy specs in
// credential_shaped_value_policy_test.go).
var _ = Describe("Dashboard CRD schema validation", func() {
	Describe("AuthSpec (spec.auth)", func() {
		It("rejects spec.auth set with no basicAuthSecretRef", func() {
			d := dashboardWithAuth("dash-auth-empty", &pagev1alpha1.AuthSpec{})
			err := k8sClient.Create(ctx, d)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("admits spec.auth with basicAuthSecretRef set", func() {
			d := dashboardWithAuth("dash-auth-ref", &pagev1alpha1.AuthSpec{
				BasicAuthSecretRef: &corev1.LocalObjectReference{Name: testSecretRefName},
			})
			Expect(k8sClient.Create(ctx, d)).To(Succeed())
			Expect(k8sClient.Delete(ctx, d)).To(Succeed())
		})

		It("admits a Dashboard with spec.auth unset entirely", func() {
			d := dashboardWithAuth("dash-auth-unset", nil)
			Expect(k8sClient.Create(ctx, d)).To(Succeed())
			Expect(k8sClient.Delete(ctx, d)).To(Succeed())
		})
	})

	Describe("NetworkPolicySpec.egressCIDRs (spec.networkPolicy.egressCIDRs)", func() {
		It("rejects a malformed CIDR entry", func() {
			d := dashboardWithEgressCIDRs("dash-cidr-bad", []string{"not-a-cidr"})
			err := k8sClient.Create(ctx, d)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("rejects an IP address with no mask", func() {
			d := dashboardWithEgressCIDRs("dash-cidr-nomask", []string{"10.0.0.1"})
			err := k8sClient.Create(ctx, d)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("admits a valid IPv4 CIDR", func() {
			d := dashboardWithEgressCIDRs("dash-cidr-v4", []string{"10.0.0.0/8"})
			Expect(k8sClient.Create(ctx, d)).To(Succeed())
			Expect(k8sClient.Delete(ctx, d)).To(Succeed())
		})

		It("admits a valid IPv6 CIDR", func() {
			d := dashboardWithEgressCIDRs("dash-cidr-v6", []string{"2001:db8::/32"})
			Expect(k8sClient.Create(ctx, d)).To(Succeed())
			Expect(k8sClient.Delete(ctx, d)).To(Succeed())
		})
	})

	Describe("WidgetDefaultsEntry (spec.widgetDefaults[type])", func() {
		It("rejects a widgetDefaults entry with neither secrets nor caCert", func() {
			d := dashboardWithWidgetDefaultsEntry("dash-wd-empty", pagev1alpha1.WidgetDefaultsEntry{})
			err := k8sClient.Create(ctx, d)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("admits a widgetDefaults entry with only secrets set", func() {
			entry := pagev1alpha1.WidgetDefaultsEntry{
				Secrets: map[string]pagev1alpha1.SecretValueSource{
					secretField: {
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: testSecretRefName},
							Key:                  secretField,
						},
					},
				},
			}
			d := dashboardWithWidgetDefaultsEntry("dash-wd-secrets", entry)
			Expect(k8sClient.Create(ctx, d)).To(Succeed())
			Expect(k8sClient.Delete(ctx, d)).To(Succeed())
		})

		It("admits a widgetDefaults entry with only caCert set", func() {
			entry := pagev1alpha1.WidgetDefaultsEntry{
				CACert: &pagev1alpha1.SecretValueSource{Value: new("-----BEGIN CERTIFICATE-----")},
			}
			d := dashboardWithWidgetDefaultsEntry("dash-wd-cacert", entry)
			Expect(k8sClient.Create(ctx, d)).To(Succeed())
			Expect(k8sClient.Delete(ctx, d)).To(Succeed())
		})
	})

	// SecretValueSource's exactly-one-of-value/secretKeyRef rule
	// (api/v1alpha1/common_types.go) is exercised elsewhere (ServiceCard/
	// InfoWidget) via secret_source_validation_test.go; these two specs
	// additionally confirm it's enforced when reached through a Dashboard's
	// own widgetDefaults[type].secrets, a third, independent embedding of
	// SecretValueSource.
	Describe("SecretValueSource via spec.widgetDefaults[type].secrets", func() {
		It("rejects a widgetDefaults secret that sets neither value nor secretKeyRef", func() {
			entry := pagev1alpha1.WidgetDefaultsEntry{
				Secrets: map[string]pagev1alpha1.SecretValueSource{secretField: {}},
			}
			d := dashboardWithWidgetDefaultsEntry("dash-wd-secret-neither", entry)
			err := k8sClient.Create(ctx, d)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("rejects a widgetDefaults secret that sets both value and secretKeyRef", func() {
			entry := pagev1alpha1.WidgetDefaultsEntry{
				Secrets: map[string]pagev1alpha1.SecretValueSource{
					secretField: {
						Value: new("inline"),
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: testSecretRefName},
							Key:                  secretField,
						},
					},
				},
			}
			d := dashboardWithWidgetDefaultsEntry("dash-wd-secret-both", entry)
			err := k8sClient.Create(ctx, d)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})
	})
})

// dashboardWithAuth builds a minimally-valid Dashboard carrying the given
// spec.auth (nil leaves it unset entirely).
func dashboardWithAuth(name string, auth *pagev1alpha1.AuthSpec) *pagev1alpha1.Dashboard {
	return &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			ContainerPort: 8080,
			Auth:          auth,
		},
	}
}

// dashboardWithEgressCIDRs builds a minimally-valid Dashboard with
// networkPolicy enabled and the given egressCIDRs.
func dashboardWithEgressCIDRs(name string, cidrs []string) *pagev1alpha1.Dashboard {
	return &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			ContainerPort: 8080,
			NetworkPolicy: &pagev1alpha1.NetworkPolicySpec{
				Enabled:     pagev1alpha1.Enabled,
				EgressCIDRs: cidrs,
			},
		},
	}
}

// dashboardWithWidgetDefaultsEntry builds a minimally-valid Dashboard whose
// spec.widgetDefaults has a single entry (keyed by testWidgetTypePrometheus)
// set to entry.
func dashboardWithWidgetDefaultsEntry(name string, entry pagev1alpha1.WidgetDefaultsEntry) *pagev1alpha1.Dashboard {
	return &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			ContainerPort:  8080,
			WidgetDefaults: map[string]pagev1alpha1.WidgetDefaultsEntry{testWidgetTypePrometheus: entry},
		},
	}
}

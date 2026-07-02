package controller

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// warningCollector implements rest.WarningHandler, capturing every warning
// header message the apiserver sends back on a request, so these specs can
// assert the credential-shaped-value policy actually fired (rather than
// only that Create still succeeded, which a silently-broken CEL expression
// would also produce under a Warn-action policy).
type warningCollector struct {
	mu       sync.Mutex
	messages []string
}

func (w *warningCollector) HandleWarningHeader(code int, agent, message string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messages = append(w.messages, message)
}

func (w *warningCollector) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messages = nil
}

// containsCredentialShapedWarning reports whether any collected warning
// mentions the credential-shaped-value policy's message. Only ever checked
// against that one substring in this file, so it takes no parameter (an
// unused substr parameter here would just be unparam-flagged dead
// flexibility).
func (w *warningCollector) containsCredentialShapedWarning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, m := range w.messages {
		if strings.Contains(m, "credential-shaped") {
			return true
		}
	}
	return false
}

var _ = Describe("Credential-shaped-value Warn ValidatingAdmissionPolicies", Ordered, func() {
	var (
		collector     *warningCollector
		warningClient client.Client
	)

	BeforeAll(func() {
		applyManifest(filepath.Join("..", "..", "config", "admission", "credential_shaped_value_policy.yaml"))

		warnCfg := *cfg
		collector = &warningCollector{}
		warnCfg.WarningHandler = collector
		var err error
		warningClient, err = client.New(&warnCfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		// Same warm-up pattern as admission_policy_test.go: poll until the
		// policy is actually being enforced before asserting individual
		// cases, since policy/binding creation isn't instantaneous.
		Eventually(func() bool {
			collector.reset()
			se := serviceEntryWithSecret("warn-warmup", &pagev1alpha1.SecretValueSource{Value: ptrString("x")})
			se.Spec.Widgets[0].Secrets = map[string]pagev1alpha1.SecretValueSource{
				secretField: {Value: ptrString("plaintext-value")},
			}
			if err := warningClient.Create(ctx, se); err != nil {
				return false
			}
			_ = warningClient.Delete(ctx, se)
			return collector.containsCredentialShapedWarning()
		}, 30*time.Second, time.Second).Should(BeTrue(), "policy should begin warning on credential-shaped inline values")
	})

	BeforeEach(func() {
		collector.reset()
	})

	Describe("ServiceCard widget secrets", func() {
		It("warns when a credential-shaped field name uses an inline value", func() {
			se := serviceEntryWithSecret("se-cred-shaped", nil)
			se.Spec.Widgets[0].Secrets = map[string]pagev1alpha1.SecretValueSource{
				"apiKey": {Value: ptrString("plaintext-value")},
			}
			Expect(warningClient.Create(ctx, se)).To(Succeed(), "Warn actions must not block the request")
			defer func() { _ = warningClient.Delete(ctx, se) }()
			Expect(collector.containsCredentialShapedWarning()).To(BeTrue())
		})

		It("does not warn for a non-credential-shaped field name using an inline value", func() {
			se := serviceEntryWithSecret("se-not-cred-shaped", nil)
			se.Spec.Widgets[0].Secrets = map[string]pagev1alpha1.SecretValueSource{
				"latitude": {Value: ptrString("51.5")},
			}
			Expect(warningClient.Create(ctx, se)).To(Succeed())
			defer func() { _ = warningClient.Delete(ctx, se) }()
			Expect(collector.containsCredentialShapedWarning()).To(BeFalse())
		})

		It("does not warn when the credential-shaped field uses secretKeyRef", func() {
			se := serviceEntryWithSecret("se-cred-shaped-ref", nil)
			se.Spec.Widgets[0].Secrets = map[string]pagev1alpha1.SecretValueSource{
				"apiKey": {SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: testSecretRefName},
					Key:                  secretField,
				}},
			}
			Expect(warningClient.Create(ctx, se)).To(Succeed())
			defer func() { _ = warningClient.Delete(ctx, se) }()
			Expect(collector.containsCredentialShapedWarning()).To(BeFalse())
		})
	})

	Describe("InfoWidget secrets", func() {
		It("warns when a credential-shaped field name uses an inline value", func() {
			iw := &pagev1alpha1.InfoWidget{
				ObjectMeta: metav1.ObjectMeta{Name: "iw-cred-shaped", Namespace: policyTestNamespace},
				Spec: pagev1alpha1.InfoWidgetSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
					Type:         "openmeteo",
					Secrets: map[string]pagev1alpha1.SecretValueSource{
						"password": {Value: ptrString("plaintext-value")},
					},
				},
			}
			Expect(warningClient.Create(ctx, iw)).To(Succeed())
			defer func() { _ = warningClient.Delete(ctx, iw) }()
			Expect(collector.containsCredentialShapedWarning()).To(BeTrue())
		})
	})
})

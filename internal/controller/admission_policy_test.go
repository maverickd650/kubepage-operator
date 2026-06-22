package controller

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const (
	policyTestNamespace = "default"
	// secretField is reused as both the widget's secret field name and the
	// referenced key within the Secret; the policy doesn't care which.
	secretField = "token"
)

// These specs verify the shipped ValidatingAdmissionPolicies
// (config/admission/secret_source_policy.yaml) actually reject the
// SecretValueSource shapes their CEL forbids. Loading the real manifest (rather
// than reconstructing the policy in Go) means a CEL typo or a drift between the
// policy and the CRD schema fails the build: a broken policy with
// failurePolicy: Fail would reject the *valid* fixtures too.
var _ = Describe("Secret-source ValidatingAdmissionPolicies", Ordered, func() {
	secretKeyRef := func() *pagev1alpha1.SecretValueSource {
		return &pagev1alpha1.SecretValueSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "api-secret"},
				Key:                  secretField,
			},
		}
	}

	BeforeAll(func() {
		applyManifest(filepath.Join("..", "..", "config", "admission", "secret_source_policy.yaml"))

		// Policy enforcement is not instantaneous after the policy/binding are
		// created — the apiserver has to observe and compile them. Poll a
		// known-invalid object until it starts being rejected before asserting
		// individual cases, so the specs don't race the controller.
		Eventually(func() bool {
			se := serviceEntryWithSecret("warmup", &pagev1alpha1.SecretValueSource{})
			err := k8sClient.Create(ctx, se)
			if err == nil {
				_ = k8sClient.Delete(ctx, se)
				return false
			}
			return apierrors.IsInvalid(err) || apierrors.IsForbidden(err)
		}, 30*time.Second, time.Second).Should(BeTrue(), "policy should begin rejecting invalid ServiceEntries")
	})

	Describe("ServiceEntry widget secrets", func() {
		It("rejects a secret that sets neither value nor secretKeyRef", func() {
			se := serviceEntryWithSecret("se-neither", &pagev1alpha1.SecretValueSource{})
			Expect(k8sClient.Create(ctx, se)).NotTo(Succeed())
		})

		It("rejects a secret that sets both value and secretKeyRef", func() {
			both := &pagev1alpha1.SecretValueSource{
				Value: ptrString("inline"),
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "api-secret"},
					Key:                  secretField,
				},
			}
			se := serviceEntryWithSecret("se-both", both)
			Expect(k8sClient.Create(ctx, se)).NotTo(Succeed())
		})

		It("admits a secret that sets only secretKeyRef", func() {
			se := serviceEntryWithSecret("se-ref-only", secretKeyRef())
			Expect(k8sClient.Create(ctx, se)).To(Succeed())
			Expect(k8sClient.Delete(ctx, se)).To(Succeed())
		})

		It("admits a widget with no secrets at all", func() {
			se := serviceEntryWithSecret("se-no-secret", nil)
			Expect(k8sClient.Create(ctx, se)).To(Succeed())
			Expect(k8sClient.Delete(ctx, se)).To(Succeed())
		})
	})

	Describe("InfoWidget secrets", func() {
		It("rejects a secret that sets neither value nor secretKeyRef", func() {
			iw := infoWidgetWithSecret("iw-neither", &pagev1alpha1.SecretValueSource{})
			Expect(k8sClient.Create(ctx, iw)).NotTo(Succeed())
		})

		It("admits a secret that sets only secretKeyRef", func() {
			iw := infoWidgetWithSecret("iw-ref-only", secretKeyRef())
			Expect(k8sClient.Create(ctx, iw)).To(Succeed())
			Expect(k8sClient.Delete(ctx, iw)).To(Succeed())
		})
	})
})

// serviceEntryWithSecret builds a minimally-valid ServiceEntry whose single
// widget carries one secret keyed secretField set to src (nil src => no
// secrets).
func serviceEntryWithSecret(name string, src *pagev1alpha1.SecretValueSource) *pagev1alpha1.ServiceEntry {
	widget := pagev1alpha1.ServiceWidget{Type: "prometheus"}
	if src != nil {
		widget.Secrets = map[string]pagev1alpha1.SecretValueSource{secretField: *src}
	}
	return &pagev1alpha1.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.ServiceEntrySpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: "demo"},
			Group:       "media",
			Name:        name,
			Widgets:     []pagev1alpha1.ServiceWidget{widget},
		},
	}
}

// infoWidgetWithSecret builds a minimally-valid InfoWidget carrying one secret
// keyed secretField set to src.
func infoWidgetWithSecret(name string, src *pagev1alpha1.SecretValueSource) *pagev1alpha1.InfoWidget {
	return &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: "demo"},
			Type:        "openmeteo",
			Secrets:     map[string]pagev1alpha1.SecretValueSource{secretField: *src},
		},
	}
}

func ptrString(s string) *string { return &s }

// applyManifest decodes a multi-document YAML file and creates each object,
// tolerating AlreadyExists so the suite can be re-run against a warm apiserver.
func applyManifest(path string) {
	raw, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())

	decoder := utilyaml.NewYAMLOrJSONDecoder(bufio.NewReader(bytes.NewReader(raw)), 4096)
	for {
		var ext runtime.RawExtension
		if err := decoder.Decode(&ext); err != nil {
			if err == io.EOF {
				break
			}
			Expect(err).NotTo(HaveOccurred())
		}
		if len(ext.Raw) == 0 {
			continue
		}
		obj, _, err := scheme.Codecs.UniversalDeserializer().Decode(ext.Raw, nil, nil)
		Expect(err).NotTo(HaveOccurred())
		co, ok := obj.(client.Object)
		Expect(ok).To(BeTrue())
		if err := k8sClient.Create(ctx, co); err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	}
}

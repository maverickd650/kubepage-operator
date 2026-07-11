package controller

import (
	"bufio"
	"bytes"
	"io"
	"os"

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
	// referenced key within the Secret; the validation doesn't care which.
	secretField = "token"
	// policyDashboardRef is the dashboardRef.name the schema-validation test
	// fixtures use; CRD schema CEL never resolves it, so it need not exist.
	policyDashboardRef = "demo"
)

// These specs verify the SecretValueSource CRD schema CEL rule
// (api/v1alpha1/common_types.go's XValidation marker on that struct) actually
// rejects the shapes it forbids. Schema validation is baked into the CRD
// itself and enforced synchronously by the apiserver on every Create/Update,
// so unlike the ValidatingAdmissionPolicies this test suite used to load,
// there's no propagation delay to poll past.
var _ = Describe("SecretValueSource CRD schema validation", func() {
	secretKeyRef := func() *pagev1alpha1.SecretValueSource {
		return &pagev1alpha1.SecretValueSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: testSecretRefName},
				Key:                  secretField,
			},
		}
	}

	Describe("ServiceCard widget secrets (services[].widgets[].secrets)", func() {
		It("rejects a secret that sets neither value nor secretKeyRef", func() {
			se := serviceCardWithSecret("se-neither", pagev1alpha1.SecretValueSource{})
			err := k8sClient.Create(ctx, se)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("rejects a secret that sets both value and secretKeyRef", func() {
			both := pagev1alpha1.SecretValueSource{
				Value: new("inline"),
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: testSecretRefName},
					Key:                  secretField,
				},
			}
			se := serviceCardWithSecret("se-both", both)
			err := k8sClient.Create(ctx, se)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("admits a secret that sets only secretKeyRef", func() {
			se := serviceCardWithSecret("se-ref-only", *secretKeyRef())
			Expect(k8sClient.Create(ctx, se)).To(Succeed())
			Expect(k8sClient.Delete(ctx, se)).To(Succeed())
		})
	})

	Describe("InfoWidget secrets (widgets[].secrets)", func() {
		It("rejects a secret that sets neither value nor secretKeyRef", func() {
			iw := infoWidgetWithSecret("iw-neither", pagev1alpha1.SecretValueSource{})
			err := k8sClient.Create(ctx, iw)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("rejects a secret that sets both value and secretKeyRef", func() {
			both := pagev1alpha1.SecretValueSource{
				Value: new("inline"),
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: testSecretRefName},
					Key:                  secretField,
				},
			}
			iw := infoWidgetWithSecret("iw-both", both)
			err := k8sClient.Create(ctx, iw)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("admits a secret that sets only secretKeyRef", func() {
			iw := infoWidgetWithSecret("iw-ref-only", *secretKeyRef())
			Expect(k8sClient.Create(ctx, iw)).To(Succeed())
			Expect(k8sClient.Delete(ctx, iw)).To(Succeed())
		})
	})
})

// serviceCardWithSecret builds a minimally-valid ServiceCard (spec.services)
// whose single entry's single widget carries one secret keyed secretField
// set to src.
func serviceCardWithSecret(name string, src pagev1alpha1.SecretValueSource) *pagev1alpha1.ServiceCard {
	widget := pagev1alpha1.ServiceWidget{
		Type:    testWidgetTypePrometheus,
		Secrets: map[string]pagev1alpha1.SecretValueSource{secretField: src},
	}
	return &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
			Group:        policyTestGroup,
			Services: []pagev1alpha1.ServiceEntry{
				{Name: name, Widgets: []pagev1alpha1.ServiceWidget{widget}},
			},
		},
	}
}

// infoWidgetWithSecret builds a minimally-valid InfoWidget (spec.widgets)
// whose single entry carries one secret keyed secretField set to src.
func infoWidgetWithSecret(name string, src pagev1alpha1.SecretValueSource) *pagev1alpha1.InfoWidget {
	return &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
			Widgets: []pagev1alpha1.InfoWidgetEntry{
				{Type: testWidgetTypeOpenMeteo, Secrets: map[string]pagev1alpha1.SecretValueSource{secretField: src}},
			},
		},
	}
}

// applyManifest decodes a multi-document YAML file and creates each object,
// tolerating AlreadyExists so the suite can be re-run against a warm
// apiserver. Used by the remaining ValidatingAdmissionPolicy specs
// (credential_shaped_value_policy_test.go) for the one Warn-action heuristic
// policy that can't be expressed as a schema CEL rule.
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

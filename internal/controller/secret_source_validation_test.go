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

	Describe("ServiceCard widget secrets", func() {
		It("rejects a secret that sets neither value nor secretKeyRef", func() {
			se := serviceEntryWithSecret("se-neither", &pagev1alpha1.SecretValueSource{})
			err := k8sClient.Create(ctx, se)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("rejects a secret that sets both value and secretKeyRef", func() {
			both := &pagev1alpha1.SecretValueSource{
				Value: ptrString("inline"),
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: testSecretRefName},
					Key:                  secretField,
				},
			}
			se := serviceEntryWithSecret("se-both", both)
			err := k8sClient.Create(ctx, se)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
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

	Describe("ServiceCard multi-card form (services[].widgets[].secrets)", func() {
		// SecretValueSource's XValidation is a type-level marker, so it
		// applies wherever a SecretValueSource appears in the schema — this
		// proves it also fires one level deeper than the single-card form,
		// under spec.services[].widgets[].secrets.
		It("rejects a services entry's widget secret that sets both value and secretKeyRef", func() {
			both := pagev1alpha1.SecretValueSource{
				Value: ptrString("inline"),
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: testSecretRefName},
					Key:                  secretField,
				},
			}
			se := multiServiceCardWithNestedSecret("se-multi-both", both)
			err := k8sClient.Create(ctx, se)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("admits a services entry's widget secret that sets only secretKeyRef", func() {
			se := multiServiceCardWithNestedSecret("se-multi-ref-only", *secretKeyRef())
			Expect(k8sClient.Create(ctx, se)).To(Succeed())
			Expect(k8sClient.Delete(ctx, se)).To(Succeed())
		})
	})

	Describe("InfoWidget secrets", func() {
		It("rejects a secret that sets neither value nor secretKeyRef", func() {
			iw := infoWidgetWithSecret("iw-neither", &pagev1alpha1.SecretValueSource{})
			err := k8sClient.Create(ctx, iw)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("admits a secret that sets only secretKeyRef", func() {
			iw := infoWidgetWithSecret("iw-ref-only", secretKeyRef())
			Expect(k8sClient.Create(ctx, iw)).To(Succeed())
			Expect(k8sClient.Delete(ctx, iw)).To(Succeed())
		})
	})

	Describe("InfoWidget multi-widget form (widgets[].secrets)", func() {
		// SecretValueSource's XValidation is a type-level marker, so it
		// applies wherever a SecretValueSource appears in the schema — this
		// proves it also fires one level deeper than the single-widget form,
		// under spec.widgets[].secrets.
		It("rejects a widgets entry secret that sets both value and secretKeyRef", func() {
			both := pagev1alpha1.SecretValueSource{
				Value: ptrString("inline"),
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: testSecretRefName},
					Key:                  secretField,
				},
			}
			iw := multiInfoWidgetWithNestedSecret("iw-multi-both", both)
			err := k8sClient.Create(ctx, iw)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("admits a widgets entry secret that sets only secretKeyRef", func() {
			iw := multiInfoWidgetWithNestedSecret("iw-multi-ref-only", *secretKeyRef())
			Expect(k8sClient.Create(ctx, iw)).To(Succeed())
			Expect(k8sClient.Delete(ctx, iw)).To(Succeed())
		})
	})
})

// serviceEntryWithSecret builds a minimally-valid ServiceCard whose single
// widget carries one secret keyed secretField set to src (nil src => no
// secrets).
func serviceEntryWithSecret(name string, src *pagev1alpha1.SecretValueSource) *pagev1alpha1.ServiceCard {
	widget := pagev1alpha1.ServiceWidget{Type: testWidgetTypePrometheus}
	if src != nil {
		widget.Secrets = map[string]pagev1alpha1.SecretValueSource{secretField: *src}
	}
	return &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
			Group:        policyTestGroup,
			Name:         name,
			Widgets:      []pagev1alpha1.ServiceWidget{widget},
		},
	}
}

// multiServiceCardWithNestedSecret builds a minimally-valid multi-card-form
// ServiceCard (spec.services) whose single entry's single widget carries one
// secret keyed secretField set to src.
func multiServiceCardWithNestedSecret(name string, src pagev1alpha1.SecretValueSource) *pagev1alpha1.ServiceCard {
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

// infoWidgetWithSecret builds a minimally-valid InfoWidget carrying one secret
// keyed secretField set to src.
func infoWidgetWithSecret(name string, src *pagev1alpha1.SecretValueSource) *pagev1alpha1.InfoWidget {
	return &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
			Type:         testWidgetTypeOpenMeteo,
			Secrets:      map[string]pagev1alpha1.SecretValueSource{secretField: *src},
		},
	}
}

// multiInfoWidgetWithNestedSecret builds a minimally-valid multi-widget-form
// InfoWidget (spec.widgets) whose single entry carries one secret keyed
// secretField set to src.
func multiInfoWidgetWithNestedSecret(name string, src pagev1alpha1.SecretValueSource) *pagev1alpha1.InfoWidget {
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

func ptrString(s string) *string { return &s }

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

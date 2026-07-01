package dashboard

import (
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

func TestDiscoverServicesFiltersAndDefaults(t *testing.T) {
	scheme := testScheme(t)

	enabled := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "app", Namespace: testNamespace,
			Annotations: map[string]string{
				"kubepage.io/enabled":     "true",
				"kubepage.io/name":        "My App",
				"kubepage.io/group":       "Apps",
				"kubepage.io/description": "An app",
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{{Host: "app.example.invalid"}},
			TLS:   []networkingv1.IngressTLS{{Hosts: []string{"app.example.invalid"}}},
		},
	}
	noDefaultName := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "bare", Namespace: testNamespace,
			Annotations: map[string]string{"kubepage.io/enabled": "true"},
		},
		Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "bare.example.invalid"}}},
	}
	notEnabled := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "skip", Namespace: testNamespace},
		Spec:       networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "skip.example.invalid"}}},
	}
	otherNamespace := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "other-ns", Namespace: "other",
			Annotations: map[string]string{"kubepage.io/enabled": "true"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(enabled, noDefaultName, notEnabled, otherNamespace).Build()

	services, err := discoverServices(t.Context(), cl, testNamespace, pagev1alpha1.DiscoverySpec{})
	if err != nil {
		t.Fatalf("discoverServices() error = %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("discoverServices() = %+v, want 2 (only annotated Ingresses in namespace)", services)
	}

	byKey := map[string]discoveredService{}
	for _, s := range services {
		byKey[s.Key] = s
	}

	app, ok := byKey["discovery/"+testNamespace+"/app"]
	if !ok {
		t.Fatalf("discoverServices() missing app entry: %+v", services)
	}
	if app.Name != "My App" || app.Group != "Apps" || app.Description != "An app" || app.Href != "https://app.example.invalid/" {
		t.Errorf("app service = %+v, want name/group/description/https href derived from TLS", app)
	}

	bare, ok := byKey["discovery/"+testNamespace+"/bare"]
	if !ok {
		t.Fatalf("discoverServices() missing bare entry: %+v", services)
	}
	if bare.Name != "bare" || bare.Group != defaultDiscoveryGroup || bare.Href != "http://bare.example.invalid/" {
		t.Errorf("bare service = %+v, want Ingress name/default group/http href (no TLS)", bare)
	}
}

func TestDiscoverServicesHomepageCompat(t *testing.T) {
	scheme := testScheme(t)
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "legacy", Namespace: testNamespace,
			Annotations: map[string]string{
				"gethomepage.dev/enabled": "true",
				"gethomepage.dev/name":    "Legacy App",
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ing).Build()

	compat := pagev1alpha1.Enabled
	services, err := discoverServices(t.Context(), cl, testNamespace, pagev1alpha1.DiscoverySpec{HomepageCompat: &compat})
	if err != nil {
		t.Fatalf("discoverServices() error = %v", err)
	}
	if len(services) != 1 || services[0].Name != "Legacy App" {
		t.Fatalf("discoverServices() with HomepageCompat = %+v, want one Legacy App entry", services)
	}

	// Without the compat toggle, the gethomepage.dev/* annotations are ignored.
	services, err = discoverServices(t.Context(), cl, testNamespace, pagev1alpha1.DiscoverySpec{})
	if err != nil {
		t.Fatalf("discoverServices() error = %v", err)
	}
	if len(services) != 0 {
		t.Errorf("discoverServices() without HomepageCompat = %+v, want none discovered", services)
	}
}

func TestDiscoverServicesCustomAnnotationPrefix(t *testing.T) {
	scheme := testScheme(t)
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "custom", Namespace: testNamespace,
			Annotations: map[string]string{"acme.io/enabled": "true", "acme.io/name": "Custom"},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ing).Build()

	prefix := "acme.io/"
	services, err := discoverServices(t.Context(), cl, testNamespace, pagev1alpha1.DiscoverySpec{AnnotationPrefix: &prefix})
	if err != nil {
		t.Fatalf("discoverServices() error = %v", err)
	}
	if len(services) != 1 || services[0].Name != "Custom" {
		t.Fatalf("discoverServices() with custom prefix = %+v, want one Custom entry", services)
	}
}

func TestIngressHrefNoRules(t *testing.T) {
	ing := &networkingv1.Ingress{}
	if got := ingressHref(ing); got != "" {
		t.Errorf("ingressHref(no rules) = %q, want empty", got)
	}
}

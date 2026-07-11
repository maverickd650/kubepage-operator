package dashboard

import (
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// TestExtraDiscoveryNamespacesFiltersOwnNamespace verifies the read-side
// counterpart to internal/controller's discoveryNamespaces: a Dashboard's
// own namespace listed (redundantly) in spec.discovery.namespaces isn't
// passed through to the extra-namespace reader, since it's already covered
// by the primary namespace-scoped reader.
func TestExtraDiscoveryNamespacesFiltersOwnNamespace(t *testing.T) {
	spec := pagev1alpha1.DiscoverySpec{Namespaces: []string{"media", testNamespace, "monitoring"}}
	got := extraDiscoveryNamespaces(spec, testNamespace)
	want := []string{"media", "monitoring"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("extraDiscoveryNamespaces() = %v, want %v (own namespace filtered)", got, want)
	}
}

func TestDiscoverServicesFiltersAndDefaults(t *testing.T) {
	scheme := testScheme(t)

	enabled := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNamespace,
			Annotations: map[string]string{
				testDiscoveryEnabledAnnotation: annotationValueTrue,
				testKubepageNameAnnotation:     testMyAppDisplayName,
				"kubepage.io/group":            testDiscoveryGroup,
				"kubepage.io/description":      testAnAppDescription,
				"kubepage.io/icon":             testGrafanaIconSlug,
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{{Host: testAppExampleHost}},
			TLS:   []networkingv1.IngressTLS{{Hosts: []string{testAppExampleHost}}},
		},
	}
	noDefaultName := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredBareKey, Namespace: testNamespace,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue},
		},
		Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "bare.example.invalid"}}},
	}
	notEnabled := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: testDiscoverySkipKey, Namespace: testNamespace},
		Spec:       networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "skip.example.invalid"}}},
	}
	otherNamespace := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "other-ns", Namespace: testNameOther,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(enabled, noDefaultName, notEnabled, otherNamespace).Build()

	services, err := discoverServices(t.Context(), cl, testNamespace, nil, nil, pagev1alpha1.DiscoverySpec{})
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

	app, ok := byKey["discovery/"+testNamespace+"/"+testDiscoveredAppKey]
	if !ok {
		t.Fatalf("discoverServices() missing app entry: %+v", services)
	}
	if app.Name != testMyAppDisplayName || app.Group != testDiscoveryGroup || app.Description != testAnAppDescription || app.Href != "https://"+testAppExampleHost+"/" {
		t.Errorf("app service = %+v, want name/group/description/https href derived from TLS", app)
	}
	if app.IconURL != IconURL(new(testGrafanaIconSlug)) {
		t.Errorf("app.IconURL = %q, want the resolved icon annotation", app.IconURL)
	}

	bare, ok := byKey["discovery/"+testNamespace+"/"+testDiscoveredBareKey]
	if !ok {
		t.Fatalf("discoverServices() missing bare entry: %+v", services)
	}
	if bare.Name != testDiscoveredBareKey || bare.Group != defaultDiscoveryGroup || bare.Href != "http://bare.example.invalid/" {
		t.Errorf("bare service = %+v, want Ingress name/default group/http href (no TLS)", bare)
	}
}

// TestDiscoverServicesScansExtraNamespaces verifies the cross-namespace
// discovery read path (DiscoverySpec.Namespaces): an annotated Ingress in a
// namespace other than the Dashboard's own is included, via extraReader,
// only when that namespace is passed as an extra namespace, and its key is
// namespace-qualified so it can't collide with a same-named Ingress in the
// Dashboard's own namespace.
func TestDiscoverServicesScansExtraNamespaces(t *testing.T) {
	scheme := testScheme(t)

	own := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNamespace,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue},
		},
	}
	other := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNameOther,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(own, other).Build()

	t.Run("without extra namespaces, only the Dashboard's own is scanned", func(t *testing.T) {
		services, err := discoverServices(t.Context(), cl, testNamespace, cl, nil, pagev1alpha1.DiscoverySpec{})
		if err != nil {
			t.Fatalf("discoverServices() error = %v", err)
		}
		if len(services) != 1 {
			t.Fatalf("discoverServices() = %+v, want 1 (own namespace only)", services)
		}
	})

	t.Run("with extra namespaces, both are scanned with distinct namespace-qualified keys", func(t *testing.T) {
		services, err := discoverServices(t.Context(), cl, testNamespace, cl, []string{testNameOther}, pagev1alpha1.DiscoverySpec{})
		if err != nil {
			t.Fatalf("discoverServices() error = %v", err)
		}
		if len(services) != 2 {
			t.Fatalf("discoverServices() = %+v, want 2 (own + extra namespace)", services)
		}

		wantOwnKey := "discovery/" + testNamespace + "/" + testDiscoveredAppKey
		wantOtherKey := "discovery/" + testNameOther + "/" + testDiscoveredAppKey
		byKey := map[string]discoveredService{}
		for _, s := range services {
			byKey[s.Key] = s
		}
		if _, ok := byKey[wantOwnKey]; !ok {
			t.Errorf("discoverServices() missing own-namespace entry %q: %+v", wantOwnKey, services)
		}
		if _, ok := byKey[wantOtherKey]; !ok {
			t.Errorf("discoverServices() missing extra-namespace entry %q: %+v", wantOtherKey, services)
		}
	})
}

// TestDiscoverServicesExtraNamespaceFailureDegradesGracefully verifies a
// failure listing one extra namespace (e.g. its RBAC RoleBinding hasn't been
// created yet, or the namespace no longer exists) doesn't fail the whole
// discovery pass — only the primary (own) namespace's failure should, since
// otherwise a single misconfigured extra namespace would prune every
// previously-discovered card, including healthy own-namespace ones, on
// every poll cycle.
func TestDiscoverServicesExtraNamespaceFailureDegradesGracefully(t *testing.T) {
	scheme := testScheme(t)
	own := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNamespace,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(own).Build()

	failingExtra := errInjectingReader{
		Reader: cl,
		failList: func(list client.ObjectList) bool {
			_, ok := list.(*networkingv1.IngressList)
			return ok
		},
	}

	services, err := discoverServices(t.Context(), cl, testNamespace, failingExtra, []string{"nonexistent-ns"}, pagev1alpha1.DiscoverySpec{})
	if err != nil {
		t.Fatalf("discoverServices() error = %v, want nil (extra-namespace failure should degrade, not fail the pass)", err)
	}
	if len(services) != 1 || services[0].Key != "discovery/"+testNamespace+"/"+testDiscoveredAppKey {
		t.Errorf("discoverServices() = %+v, want the own-namespace entry despite the extra namespace failing", services)
	}
}

func TestDiscoverServicesHomepageCompat(t *testing.T) {
	scheme := testScheme(t)
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "legacy", Namespace: testNamespace,
			Annotations: map[string]string{
				"gethomepage.dev/enabled": annotationValueTrue,
				"gethomepage.dev/name":    "Legacy App",
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ing).Build()

	compat := pagev1alpha1.Enabled
	services, err := discoverServices(t.Context(), cl, testNamespace, nil, nil, pagev1alpha1.DiscoverySpec{HomepageCompat: &compat})
	if err != nil {
		t.Fatalf("discoverServices() error = %v", err)
	}
	if len(services) != 1 || services[0].Name != "Legacy App" {
		t.Fatalf("discoverServices() with HomepageCompat = %+v, want one Legacy App entry", services)
	}

	// Without the compat toggle, the gethomepage.dev/* annotations are ignored.
	services, err = discoverServices(t.Context(), cl, testNamespace, nil, nil, pagev1alpha1.DiscoverySpec{})
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
			Annotations: map[string]string{"acme.io/enabled": annotationValueTrue, "acme.io/name": testCustomName},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ing).Build()

	prefix := "acme.io/"
	services, err := discoverServices(t.Context(), cl, testNamespace, nil, nil, pagev1alpha1.DiscoverySpec{AnnotationPrefix: &prefix})
	if err != nil {
		t.Fatalf("discoverServices() error = %v", err)
	}
	if len(services) != 1 || services[0].Name != testCustomName {
		t.Fatalf("discoverServices() with custom prefix = %+v, want one Custom entry", services)
	}
}

func TestIngressHrefNoRules(t *testing.T) {
	ing := &networkingv1.Ingress{}
	if got := ingressHref(ing); got != "" {
		t.Errorf("ingressHref(no rules) = %q, want empty", got)
	}
}

func TestDiscoverServicesListError(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	failing := errInjectingReader{
		Reader: cl,
		failList: func(list client.ObjectList) bool {
			_, ok := list.(*networkingv1.IngressList)
			return ok
		},
	}

	_, err := discoverServices(t.Context(), failing, testNamespace, nil, nil, pagev1alpha1.DiscoverySpec{})
	if err == nil {
		t.Fatal("discoverServices() error = nil, want non-nil when listing Ingresses fails")
	}
}

// TestDiscoverHTTPRoutesFiltersAndDefaults mirrors
// TestDiscoverServicesFiltersAndDefaults for the HTTPRoute discovery
// fast-follow (gap-analysis §4.7): same annotation convention and defaults,
// but href derives from the route's first hostname (always "https", since
// an HTTPRoute carries no TLS info of its own) instead of an Ingress rule.
func TestDiscoverHTTPRoutesFiltersAndDefaults(t *testing.T) {
	scheme := testScheme(t)

	enabled := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNamespace,
			Annotations: map[string]string{
				testDiscoveryEnabledAnnotation: annotationValueTrue,
				testKubepageNameAnnotation:     testMyAppDisplayName,
				"kubepage.io/group":            testDiscoveryGroup,
				"kubepage.io/description":      testAnAppDescription,
				"kubepage.io/icon":             testGrafanaIconSlug,
			},
		},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{testAppExampleHost},
		},
	}
	noDefaultName := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredBareKey, Namespace: testNamespace,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue},
		},
		Spec: gatewayv1.HTTPRouteSpec{Hostnames: []gatewayv1.Hostname{"bare.example.invalid"}},
	}
	notEnabled := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: testDiscoverySkipKey, Namespace: testNamespace},
		Spec:       gatewayv1.HTTPRouteSpec{Hostnames: []gatewayv1.Hostname{"skip.example.invalid"}},
	}
	otherNamespace := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name: "other-ns", Namespace: testNameOther,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(enabled, noDefaultName, notEnabled, otherNamespace).Build()

	services, err := discoverHTTPRoutes(t.Context(), cl, testNamespace, nil, nil, pagev1alpha1.DiscoverySpec{})
	if err != nil {
		t.Fatalf("discoverHTTPRoutes() error = %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("discoverHTTPRoutes() = %+v, want 2 (only annotated HTTPRoutes in namespace)", services)
	}

	byKey := map[string]discoveredService{}
	for _, s := range services {
		byKey[s.Key] = s
	}

	app, ok := byKey["discovery/httproute/"+testNamespace+"/"+testDiscoveredAppKey]
	if !ok {
		t.Fatalf("discoverHTTPRoutes() missing app entry: %+v", services)
	}
	if app.Name != testMyAppDisplayName || app.Group != testDiscoveryGroup || app.Description != testAnAppDescription || app.Href != "https://"+testAppExampleHost+"/" {
		t.Errorf("app service = %+v, want name/group/description/href derived from the first hostname", app)
	}
	if app.IconURL != IconURL(new(testGrafanaIconSlug)) {
		t.Errorf("app.IconURL = %q, want the resolved icon annotation", app.IconURL)
	}

	bare, ok := byKey["discovery/httproute/"+testNamespace+"/"+testDiscoveredBareKey]
	if !ok {
		t.Fatalf("discoverHTTPRoutes() missing bare entry: %+v", services)
	}
	if bare.Name != testDiscoveredBareKey || bare.Group != defaultDiscoveryGroup || bare.Href != "https://bare.example.invalid/" {
		t.Errorf("bare service = %+v, want HTTPRoute name/default group/https href", bare)
	}
}

// TestDiscoverHTTPRoutesScansExtraNamespaces mirrors
// TestDiscoverServicesScansExtraNamespaces for discoverHTTPRoutes.
func TestDiscoverHTTPRoutesScansExtraNamespaces(t *testing.T) {
	scheme := testScheme(t)

	own := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNamespace,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue},
		},
	}
	other := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNameOther,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(own, other).Build()

	services, err := discoverHTTPRoutes(t.Context(), cl, testNamespace, cl, []string{testNameOther}, pagev1alpha1.DiscoverySpec{})
	if err != nil {
		t.Fatalf("discoverHTTPRoutes() error = %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("discoverHTTPRoutes() = %+v, want 2 (own + extra namespace)", services)
	}

	wantOwnKey := "discovery/httproute/" + testNamespace + "/" + testDiscoveredAppKey
	wantOtherKey := "discovery/httproute/" + testNameOther + "/" + testDiscoveredAppKey
	byKey := map[string]discoveredService{}
	for _, s := range services {
		byKey[s.Key] = s
	}
	if _, ok := byKey[wantOwnKey]; !ok {
		t.Errorf("discoverHTTPRoutes() missing own-namespace entry %q: %+v", wantOwnKey, services)
	}
	if _, ok := byKey[wantOtherKey]; !ok {
		t.Errorf("discoverHTTPRoutes() missing extra-namespace entry %q: %+v", wantOtherKey, services)
	}
}

// TestDiscoverHTTPRoutesExtraNamespaceFailureDegradesGracefully mirrors
// TestDiscoverServicesExtraNamespaceFailureDegradesGracefully for
// discoverHTTPRoutes.
func TestDiscoverHTTPRoutesExtraNamespaceFailureDegradesGracefully(t *testing.T) {
	scheme := testScheme(t)
	own := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNamespace,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(own).Build()

	failingExtra := errInjectingReader{
		Reader: cl,
		failList: func(list client.ObjectList) bool {
			_, ok := list.(*gatewayv1.HTTPRouteList)
			return ok
		},
	}

	services, err := discoverHTTPRoutes(t.Context(), cl, testNamespace, failingExtra, []string{"nonexistent-ns"}, pagev1alpha1.DiscoverySpec{})
	if err != nil {
		t.Fatalf("discoverHTTPRoutes() error = %v, want nil (extra-namespace failure should degrade, not fail the pass)", err)
	}
	if len(services) != 1 || services[0].Key != "discovery/httproute/"+testNamespace+"/"+testDiscoveredAppKey {
		t.Errorf("discoverHTTPRoutes() = %+v, want the own-namespace entry despite the extra namespace failing", services)
	}
}

func TestHTTPRouteHrefNoHostnames(t *testing.T) {
	route := &gatewayv1.HTTPRoute{}
	if got := httpRouteHref(route); got != "" {
		t.Errorf("httpRouteHref(no hostnames) = %q, want empty", got)
	}
}

func TestDiscoverHTTPRoutesListError(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	failing := errInjectingReader{
		Reader: cl,
		failList: func(list client.ObjectList) bool {
			_, ok := list.(*gatewayv1.HTTPRouteList)
			return ok
		},
	}

	_, err := discoverHTTPRoutes(t.Context(), failing, testNamespace, nil, nil, pagev1alpha1.DiscoverySpec{})
	if err == nil {
		t.Fatal("discoverHTTPRoutes() error = nil, want non-nil when listing HTTPRoutes fails")
	}
}

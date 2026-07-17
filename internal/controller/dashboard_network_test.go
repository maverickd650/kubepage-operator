package controller

import (
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

func networkTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := pagev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

// TestDashboardURL verifies status.url's documented precedence: Ingress >
// Gateway > cluster-internal Service DNS.
func TestDashboardURL(t *testing.T) {
	base := func() *pagev1alpha1.Dashboard {
		return &pagev1alpha1.Dashboard{
			ObjectMeta: metav1.ObjectMeta{Name: "d", Namespace: "ns"},
			Spec:       pagev1alpha1.DashboardSpec{ContainerPort: 8080},
		}
	}

	t.Run("no exposure falls back to cluster Service DNS", func(t *testing.T) {
		got := dashboardURL(base())
		want := "http://d.ns.svc.cluster.local:8080/"
		if got != want {
			t.Errorf("dashboardURL() = %q, want %q", got, want)
		}
	})

	t.Run("Ingress without TLS uses http", func(t *testing.T) {
		instance := base()
		instance.Spec.Ingress = &pagev1alpha1.IngressSpec{Enabled: true, Host: testDashboardHost}
		got := dashboardURL(instance)
		want := "http://" + testDashboardHost + "/"
		if got != want {
			t.Errorf("dashboardURL() = %q, want %q", got, want)
		}
	})

	t.Run("Ingress with TLS uses https and takes priority over Gateway", func(t *testing.T) {
		instance := base()
		instance.Spec.Ingress = &pagev1alpha1.IngressSpec{
			Enabled: true, Host: testDashboardHost,
			TLS: &pagev1alpha1.IngressTLSSpec{SecretName: "dash-tls"},
		}
		instance.Spec.Gateway = &pagev1alpha1.GatewaySpec{Enabled: true, Hostnames: []string{"gw.example.com"}}
		got := dashboardURL(instance)
		want := "https://" + testDashboardHost + "/"
		if got != want {
			t.Errorf("dashboardURL() = %q, want %q", got, want)
		}
	})

	t.Run("disabled Ingress falls through to Gateway", func(t *testing.T) {
		instance := base()
		instance.Spec.Ingress = &pagev1alpha1.IngressSpec{Enabled: false, Host: testDashboardHost}
		instance.Spec.Gateway = &pagev1alpha1.GatewaySpec{Enabled: true, Hostnames: []string{"gw.example.com", "other.example.com"}}
		got := dashboardURL(instance)
		want := "https://gw.example.com/"
		if got != want {
			t.Errorf("dashboardURL() = %q, want %q", got, want)
		}
	})
}

// TestReconcileServicePreservesForeignAnnotations ensures reconcileService
// uses mergeManagedAnnotations, the same as reconcileIngress/
// reconcileHTTPRoute, rather than a wholesale
// found.Annotations = desired.Annotations that would clobber annotations a
// foreign controller (e.g. a cloud load-balancer controller) writes onto the
// Service.
func TestReconcileServicePreservesForeignAnnotations(t *testing.T) {
	scheme := networkTestScheme(t)
	ns := "svc-annotations-ns"
	ctx := t.Context()

	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: ns},
		Spec: pagev1alpha1.DashboardSpec{
			ContainerPort: 8080,
			Service: &pagev1alpha1.ServiceSpec{
				Annotations: map[string]string{"managed.example.com/key": "v1"},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}
	nn := types.NamespacedName{Name: instance.Name, Namespace: ns}

	if err := r.reconcileService(ctx, instance); err != nil {
		t.Fatalf("reconcileService() unexpected error: %v", err)
	}

	// Simulate a foreign controller (e.g. a cloud LB controller) writing its
	// own annotation onto the Service after creation.
	svc := &corev1.Service{}
	if err := cl.Get(ctx, nn, svc); err != nil {
		t.Fatalf("getting Service: %v", err)
	}
	svc.Annotations[testForeignAnnotationKey] = testForeignAnnotationValue
	if err := cl.Update(ctx, svc); err != nil {
		t.Fatalf("seeding foreign annotation: %v", err)
	}

	// Change the managed annotation to trigger a drift-driven Update.
	updated := instance.DeepCopy()
	updated.Spec.Service.Annotations = map[string]string{"managed.example.com/key": "v2"}
	if err := r.reconcileService(ctx, updated); err != nil {
		t.Fatalf("reconcileService() unexpected error on drift correction: %v", err)
	}

	got := &corev1.Service{}
	if err := cl.Get(ctx, nn, got); err != nil {
		t.Fatalf("getting Service after update: %v", err)
	}
	if got.Annotations[testForeignAnnotationKey] != testForeignAnnotationValue {
		t.Errorf("foreign annotation lost, got Annotations = %+v, want the foreign annotation preserved", got.Annotations)
	}
	if got.Annotations["managed.example.com/key"] != "v2" {
		t.Errorf("managed annotation not updated, got Annotations = %+v, want managed.example.com/key=v2", got.Annotations)
	}

	// Removing the managed annotation from the spec should prune it, while
	// still leaving the foreign annotation alone.
	pruned := instance.DeepCopy()
	pruned.Spec.Service.Annotations = nil
	if err := r.reconcileService(ctx, pruned); err != nil {
		t.Fatalf("reconcileService() unexpected error on prune: %v", err)
	}
	final := &corev1.Service{}
	if err := cl.Get(ctx, nn, final); err != nil {
		t.Fatalf("getting Service after prune: %v", err)
	}
	if _, ok := final.Annotations["managed.example.com/key"]; ok {
		t.Errorf("managed annotation not pruned, got Annotations = %+v", final.Annotations)
	}
	if final.Annotations[testForeignAnnotationKey] != testForeignAnnotationValue {
		t.Errorf("foreign annotation lost after prune, got Annotations = %+v, want the foreign annotation preserved", final.Annotations)
	}
}

func TestServiceForDashboardAppliesServiceSpec(t *testing.T) {
	r := &DashboardReconciler{Scheme: networkTestScheme(t)}
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: "svcspec"},
		Spec: pagev1alpha1.DashboardSpec{
			ContainerPort: 8080,
			Service: &pagev1alpha1.ServiceSpec{
				Type:        "LoadBalancer",
				Annotations: map[string]string{"metallb.universe.tf/address-pool": "default"},
			},
		},
	}

	svc, err := r.serviceForDashboard(instance)
	if err != nil {
		t.Fatalf("serviceForDashboard() error = %v", err)
	}
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		t.Errorf("Service.Spec.Type = %q, want %q", svc.Spec.Type, corev1.ServiceTypeLoadBalancer)
	}
	if svc.Annotations["metallb.universe.tf/address-pool"] != "default" {
		t.Errorf("Service.Annotations = %+v, want the metallb annotation", svc.Annotations)
	}
}

// TestServiceForDashboardAddsMetricsPortWhenEnabled verifies spec.metrics.enabled
// adds the dashboard's metrics port to the Service, opt-in since the
// dashboard's /metrics has no authn/authz of its own (see MetricsSpec's doc
// comment).
func TestServiceForDashboardAddsMetricsPortWhenEnabled(t *testing.T) {
	r := &DashboardReconciler{Scheme: networkTestScheme(t)}
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: "metricssvc"},
		Spec: pagev1alpha1.DashboardSpec{
			ContainerPort: 8080,
			Metrics:       &pagev1alpha1.MetricsSpec{Enabled: true},
		},
	}

	svc, err := r.serviceForDashboard(instance)
	if err != nil {
		t.Fatalf("serviceForDashboard() error = %v", err)
	}
	found := false
	for _, p := range svc.Spec.Ports {
		if p.Name == dashboardMetricsPortName && p.Port == dashboardMetricsPort {
			found = true
		}
	}
	if !found {
		t.Errorf("Service.Spec.Ports = %+v, want a %q port %d", svc.Spec.Ports, dashboardMetricsPortName, dashboardMetricsPort)
	}
}

func TestServiceForDashboardDefaultsToClusterIP(t *testing.T) {
	r := &DashboardReconciler{Scheme: networkTestScheme(t)}
	instance := newNetworkErrorTestDashboard()

	svc, err := r.serviceForDashboard(instance)
	if err != nil {
		t.Fatalf("serviceForDashboard() error = %v", err)
	}
	if svc.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("Service.Spec.Type = %q, want %q when spec.service is unset", svc.Spec.Type, corev1.ServiceTypeClusterIP)
	}
}

func TestHTTPRouteForDashboard(t *testing.T) {
	ns := "ghr"
	r := &DashboardReconciler{Scheme: networkTestScheme(t)}
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: ns},
		Spec: pagev1alpha1.DashboardSpec{
			ContainerPort: 8080,
			Gateway: &pagev1alpha1.GatewaySpec{
				Enabled:   true,
				Hostnames: []string{testDashboardHost, testDashboardHost},
				ParentRef: pagev1alpha1.GatewayParentRef{
					Name:        "eg",
					Namespace:   new("gateway-system"),
					SectionName: new("https"),
				},
				Annotations: map[string]string{testAnnotationKey: "hi"},
			},
		},
	}

	route, err := r.httpRouteForDashboard(instance)
	if err != nil {
		t.Fatalf("httpRouteForDashboard() unexpected error: %v", err)
	}

	if route.Name != testDashboardObjName || route.Namespace != ns {
		t.Errorf("route name/namespace = %s/%s, want %s/%s", route.Namespace, route.Name, ns, testDashboardObjName)
	}
	if route.Annotations[testAnnotationKey] != "hi" {
		t.Errorf("route annotations = %+v, want example.com/note=hi", route.Annotations)
	}

	wantHostnames := []gatewayv1.Hostname{testDashboardHost, testDashboardHost}
	if !slices.Equal(route.Spec.Hostnames, wantHostnames) {
		t.Errorf("route.Spec.Hostnames = %v, want %v", route.Spec.Hostnames, wantHostnames)
	}

	if len(route.Spec.ParentRefs) != 1 {
		t.Fatalf("route.Spec.ParentRefs = %+v, want 1 entry", route.Spec.ParentRefs)
	}
	pr := route.Spec.ParentRefs[0]
	if pr.Name != "eg" {
		t.Errorf("ParentRefs[0].Name = %q, want %q", pr.Name, "eg")
	}
	if pr.Namespace == nil || *pr.Namespace != "gateway-system" {
		t.Errorf("ParentRefs[0].Namespace = %v, want %q", pr.Namespace, "gateway-system")
	}
	if pr.SectionName == nil || *pr.SectionName != testPortNameHTTPS {
		t.Errorf("ParentRefs[0].SectionName = %v, want %q", pr.SectionName, "https")
	}

	if len(route.Spec.Rules) != 1 || len(route.Spec.Rules[0].BackendRefs) != 1 {
		t.Fatalf("route.Spec.Rules = %+v, want exactly one rule with one backendRef", route.Spec.Rules)
	}
	backend := route.Spec.Rules[0].BackendRefs[0].BackendRef
	if backend.Name != testDashboardObjName {
		t.Errorf("backend.Name = %q, want %q", backend.Name, testDashboardObjName)
	}
	if backend.Port == nil || *backend.Port != 8080 {
		t.Errorf("backend.Port = %v, want 8080", backend.Port)
	}

	if len(route.OwnerReferences) != 1 || route.OwnerReferences[0].Name != testDashboardObjName {
		t.Errorf("route.OwnerReferences = %+v, want owned by Dashboard %q", route.OwnerReferences, testDashboardObjName)
	}
}

func TestHTTPRouteForDashboardNoParentNamespaceOrSection(t *testing.T) {
	r := &DashboardReconciler{Scheme: networkTestScheme(t)}
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: "ns"},
		Spec: pagev1alpha1.DashboardSpec{
			ContainerPort: 8080,
			Gateway: &pagev1alpha1.GatewaySpec{
				Enabled:   true,
				Hostnames: []string{testDashboardHost},
				ParentRef: pagev1alpha1.GatewayParentRef{Name: "eg"},
			},
		},
	}

	route, err := r.httpRouteForDashboard(instance)
	if err != nil {
		t.Fatalf("httpRouteForDashboard() unexpected error: %v", err)
	}
	pr := route.Spec.ParentRefs[0]
	if pr.Namespace != nil {
		t.Errorf("ParentRefs[0].Namespace = %v, want nil", pr.Namespace)
	}
	if pr.SectionName != nil {
		t.Errorf("ParentRefs[0].SectionName = %v, want nil", pr.SectionName)
	}
}

func TestHTTPRouteSpecsEqual(t *testing.T) {
	base := gatewayv1.HTTPRouteSpec{
		CommonRouteSpec: gatewayv1.CommonRouteSpec{
			ParentRefs: []gatewayv1.ParentReference{{Name: "eg"}},
		},
		Hostnames: []gatewayv1.Hostname{testDashboardHost},
		Rules: []gatewayv1.HTTPRouteRule{{
			BackendRefs: []gatewayv1.HTTPBackendRef{{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{Name: testDashboardObjName, Port: ptr.To[gatewayv1.PortNumber](8080)},
				},
			}},
		}},
	}

	// A copy that simulates API-server defaulting: Group/Kind on the
	// ParentReference and BackendObjectReference, Weight on the BackendRef.
	// These must NOT count as a difference.
	defaulted := base
	defaultedParentRef := base.ParentRefs[0]
	defaultedParentRef.Group = ptr.To(gatewayv1.Group("gateway.networking.k8s.io"))
	defaultedParentRef.Kind = ptr.To(gatewayv1.Kind("Gateway"))
	defaulted.ParentRefs = []gatewayv1.ParentReference{defaultedParentRef}
	defaultedBackendRef := base.Rules[0].BackendRefs[0]
	defaultedBackendRef.Group = ptr.To(gatewayv1.Group(""))
	defaultedBackendRef.Kind = ptr.To(gatewayv1.Kind("Service"))
	defaultedBackendRef.Weight = new(int32(1))
	defaulted.Rules = []gatewayv1.HTTPRouteRule{{BackendRefs: []gatewayv1.HTTPBackendRef{defaultedBackendRef}}}

	if !httpRouteSpecsEqual(base, defaulted) {
		t.Errorf("httpRouteSpecsEqual(base, defaulted) = false, want true (API-server-defaulted fields must be ignored)")
	}

	changedHost := base
	changedHost.Hostnames = []gatewayv1.Hostname{testOtherHost}
	if httpRouteSpecsEqual(base, changedHost) {
		t.Errorf("httpRouteSpecsEqual(base, changedHost) = true, want false (hostname differs)")
	}

	changedPort := base
	changedRule := base.Rules[0]
	changedBackend := base.Rules[0].BackendRefs[0]
	changedBackend.Port = ptr.To[gatewayv1.PortNumber](9090)
	changedRule.BackendRefs = []gatewayv1.HTTPBackendRef{changedBackend}
	changedPort.Rules = []gatewayv1.HTTPRouteRule{changedRule}
	if httpRouteSpecsEqual(base, changedPort) {
		t.Errorf("httpRouteSpecsEqual(base, changedPort) = true, want false (backend port differs)")
	}

	changedParentName := base
	changedParentRef := base.ParentRefs[0]
	changedParentRef.Name = "other-gw"
	changedParentName.ParentRefs = []gatewayv1.ParentReference{changedParentRef}
	if httpRouteSpecsEqual(base, changedParentName) {
		t.Errorf("httpRouteSpecsEqual() = true, want false (parentRef name differs)")
	}

	changedParentNamespace := base
	changedParentRefNS := base.ParentRefs[0]
	changedParentRefNS.Namespace = ptr.To(gatewayv1.Namespace("gateway-system"))
	changedParentNamespace.ParentRefs = []gatewayv1.ParentReference{changedParentRefNS}
	if httpRouteSpecsEqual(base, changedParentNamespace) {
		t.Errorf("httpRouteSpecsEqual() = true, want false (parentRef namespace differs)")
	}

	changedBackendName := base
	changedRule2 := base.Rules[0]
	changedBackend2 := base.Rules[0].BackendRefs[0]
	changedBackend2.Name = "other-svc"
	changedRule2.BackendRefs = []gatewayv1.HTTPBackendRef{changedBackend2}
	changedBackendName.Rules = []gatewayv1.HTTPRouteRule{changedRule2}
	if httpRouteSpecsEqual(base, changedBackendName) {
		t.Errorf("httpRouteSpecsEqual() = true, want false (backend name differs)")
	}

	changedBackendRefCount := base
	changedRuleCount := base.Rules[0]
	changedRuleCount.BackendRefs = append(slices.Clone(base.Rules[0].BackendRefs), base.Rules[0].BackendRefs[0])
	changedBackendRefCount.Rules = []gatewayv1.HTTPRouteRule{changedRuleCount}
	if httpRouteSpecsEqual(base, changedBackendRefCount) {
		t.Errorf("httpRouteSpecsEqual() = true, want false (backendRefs count differs)")
	}

	nilPort := base
	nilPortRule := base.Rules[0]
	nilPortBackend := base.Rules[0].BackendRefs[0]
	nilPortBackend.Port = nil
	nilPortRule.BackendRefs = []gatewayv1.HTTPBackendRef{nilPortBackend}
	nilPort.Rules = []gatewayv1.HTTPRouteRule{nilPortRule}
	if httpRouteSpecsEqual(base, nilPort) {
		t.Errorf("httpRouteSpecsEqual() = true, want false (one backend port is nil)")
	}
}

func TestEqualStringPtr(t *testing.T) {
	a, b := "x", "x"
	c := "y"
	tests := map[string]struct {
		a, b *string
		want bool
	}{
		testCaseBothNil:         {a: nil, b: nil, want: true},
		testCaseOneNil:          {a: &a, b: nil, want: false},
		"other nil":             {a: nil, b: &a, want: false},
		testCaseEqualValues:     {a: &a, b: &b, want: true},
		testCaseDifferentValues: {a: &a, b: &c, want: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := equalStringPtr(tc.a, tc.b); got != tc.want {
				t.Errorf("equalStringPtr(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestEqualGatewayNamespacePtr(t *testing.T) {
	a := gatewayv1.Namespace("ns-a")
	b := gatewayv1.Namespace("ns-a")
	c := gatewayv1.Namespace("ns-c")
	tests := map[string]struct {
		a, b *gatewayv1.Namespace
		want bool
	}{
		testCaseBothNil:         {a: nil, b: nil, want: true},
		testCaseOneNil:          {a: &a, b: nil, want: false},
		testCaseEqualValues:     {a: &a, b: &b, want: true},
		testCaseDifferentValues: {a: &a, b: &c, want: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := equalGatewayNamespacePtr(tc.a, tc.b); got != tc.want {
				t.Errorf("equalGatewayNamespacePtr() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEqualGatewaySectionNamePtr(t *testing.T) {
	a := gatewayv1.SectionName("https")
	b := gatewayv1.SectionName("https")
	c := gatewayv1.SectionName("http")
	tests := map[string]struct {
		a, b *gatewayv1.SectionName
		want bool
	}{
		testCaseBothNil:         {a: nil, b: nil, want: true},
		testCaseOneNil:          {a: &a, b: nil, want: false},
		testCaseEqualValues:     {a: &a, b: &b, want: true},
		testCaseDifferentValues: {a: &a, b: &c, want: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := equalGatewaySectionNamePtr(tc.a, tc.b); got != tc.want {
				t.Errorf("equalGatewaySectionNamePtr() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPortsEqualLengthMismatch(t *testing.T) {
	a := []corev1.ServicePort{{Name: testPortNameHTTP, Port: 80}}
	b := []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}}
	if portsEqual(a, b) {
		t.Errorf("portsEqual() = true, want false (different lengths)")
	}
}

func TestIngressSpecsEqual(t *testing.T) {
	pathType := networkingv1.PathTypePrefix
	base := networkingv1.IngressSpec{
		IngressClassName: new("nginx"),
		Rules: []networkingv1.IngressRule{{
			Host: testDashboardHost,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{
						Path:     "/",
						PathType: &pathType,
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: testDashboardObjName,
								Port: networkingv1.ServiceBackendPort{Number: 8080},
							},
						},
					}},
				},
			},
		}},
		TLS: []networkingv1.IngressTLS{{SecretName: "tls-secret", Hosts: []string{testDashboardHost}}},
	}

	t.Run("identical specs are equal", func(t *testing.T) {
		if !ingressSpecsEqual(base, base) {
			t.Errorf("ingressSpecsEqual(base, base) = false, want true")
		}
	})

	t.Run("different IngressClassName", func(t *testing.T) {
		other := base
		other.IngressClassName = new("other-class")
		if ingressSpecsEqual(base, other) {
			t.Errorf("ingressSpecsEqual() = true, want false (IngressClassName differs)")
		}
	})

	t.Run("different rule count", func(t *testing.T) {
		other := base
		other.Rules = append([]networkingv1.IngressRule{}, base.Rules...)
		other.Rules = append(other.Rules, base.Rules[0])
		if ingressSpecsEqual(base, other) {
			t.Errorf("ingressSpecsEqual() = true, want false (rule count differs)")
		}
	})

	t.Run("different host", func(t *testing.T) {
		other := base
		otherRule := base.Rules[0]
		otherRule.Host = testOtherHost
		other.Rules = []networkingv1.IngressRule{otherRule}
		if ingressSpecsEqual(base, other) {
			t.Errorf("ingressSpecsEqual() = true, want false (host differs)")
		}
	})

	t.Run("nil HTTP rule value", func(t *testing.T) {
		other := base
		otherRule := base.Rules[0]
		otherRule.IngressRuleValue = networkingv1.IngressRuleValue{}
		other.Rules = []networkingv1.IngressRule{otherRule}
		if ingressSpecsEqual(base, other) {
			t.Errorf("ingressSpecsEqual() = true, want false (HTTP is nil)")
		}
	})

	t.Run("different path", func(t *testing.T) {
		other := base
		otherRule := base.Rules[0]
		otherPath := otherRule.HTTP.Paths[0]
		otherPath.Path = "/other"
		otherRule.HTTP = &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{otherPath}}
		other.Rules = []networkingv1.IngressRule{otherRule}
		if ingressSpecsEqual(base, other) {
			t.Errorf("ingressSpecsEqual() = true, want false (path differs)")
		}
	})

	t.Run("nil backend service", func(t *testing.T) {
		other := base
		otherRule := base.Rules[0]
		otherPath := otherRule.HTTP.Paths[0]
		otherPath.Backend.Service = nil
		otherRule.HTTP = &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{otherPath}}
		other.Rules = []networkingv1.IngressRule{otherRule}
		if ingressSpecsEqual(base, other) {
			t.Errorf("ingressSpecsEqual() = true, want false (backend service is nil)")
		}
	})

	t.Run("different backend service name", func(t *testing.T) {
		other := base
		otherRule := base.Rules[0]
		otherPath := otherRule.HTTP.Paths[0]
		otherPath.Backend.Service = &networkingv1.IngressServiceBackend{Name: "other-svc", Port: otherPath.Backend.Service.Port}
		otherRule.HTTP = &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{otherPath}}
		other.Rules = []networkingv1.IngressRule{otherRule}
		if ingressSpecsEqual(base, other) {
			t.Errorf("ingressSpecsEqual() = true, want false (backend service name differs)")
		}
	})

	t.Run("different TLS count", func(t *testing.T) {
		other := base
		other.TLS = nil
		if ingressSpecsEqual(base, other) {
			t.Errorf("ingressSpecsEqual() = true, want false (TLS count differs)")
		}
	})

	t.Run("different TLS secret name", func(t *testing.T) {
		other := base
		other.TLS = []networkingv1.IngressTLS{{SecretName: "other-secret", Hosts: []string{testDashboardHost}}}
		if ingressSpecsEqual(base, other) {
			t.Errorf("ingressSpecsEqual() = true, want false (TLS secret name differs)")
		}
	})

	t.Run("different TLS hosts", func(t *testing.T) {
		other := base
		other.TLS = []networkingv1.IngressTLS{{SecretName: "tls-secret", Hosts: []string{testOtherHost}}}
		if ingressSpecsEqual(base, other) {
			t.Errorf("ingressSpecsEqual() = true, want false (TLS hosts differ)")
		}
	})
}

// TestReconcileHTTPRouteLifecycle exercises reconcileHTTPRoute's
// create/update/delete/no-op branches directly against a fake client with
// GatewayAPIEnabled true. envtest (see instance_controller_test.go) can't
// cover this: it only installs this project's own CRDs, so
// GatewayAPIEnabled is always false there, leaving this whole code path
// (everything past the "Gateway API not installed" check) untested.
func TestReconcileHTTPRouteLifecycle(t *testing.T) {
	scheme := networkTestScheme(t)
	ns := "ghr-fake"
	ctx := t.Context()

	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: ns, UID: "uid-1"},
		Spec: pagev1alpha1.DashboardSpec{
			ContainerPort: 8080,
			Gateway: &pagev1alpha1.GatewaySpec{
				Enabled:   true,
				Hostnames: []string{testDashboardHost},
				ParentRef: pagev1alpha1.GatewayParentRef{Name: "eg"},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, GatewayAPIEnabled: true}
	nn := types.NamespacedName{Name: instance.Name, Namespace: ns}

	t.Run("creates the HTTPRoute when enabled and missing", func(t *testing.T) {
		if err := r.reconcileHTTPRoute(ctx, instance); err != nil {
			t.Fatalf("reconcileHTTPRoute() unexpected error: %v", err)
		}
		route := &gatewayv1.HTTPRoute{}
		if err := cl.Get(ctx, nn, route); err != nil {
			t.Fatalf("expected HTTPRoute to be created: %v", err)
		}
		if len(route.Spec.Hostnames) != 1 || route.Spec.Hostnames[0] != gatewayv1.Hostname(testDashboardHost) {
			t.Errorf("route.Spec.Hostnames = %v, want [%s]", route.Spec.Hostnames, testDashboardHost)
		}
	})

	t.Run("corrects drift on an existing HTTPRoute", func(t *testing.T) {
		route := &gatewayv1.HTTPRoute{}
		if err := cl.Get(ctx, nn, route); err != nil {
			t.Fatalf("getting HTTPRoute: %v", err)
		}
		route.Spec.Hostnames = []gatewayv1.Hostname{"stale.example.com"}
		if err := cl.Update(ctx, route); err != nil {
			t.Fatalf("seeding drift: %v", err)
		}

		if err := r.reconcileHTTPRoute(ctx, instance); err != nil {
			t.Fatalf("reconcileHTTPRoute() unexpected error: %v", err)
		}

		corrected := &gatewayv1.HTTPRoute{}
		if err := cl.Get(ctx, nn, corrected); err != nil {
			t.Fatalf("getting HTTPRoute: %v", err)
		}
		if len(corrected.Spec.Hostnames) != 1 || corrected.Spec.Hostnames[0] != gatewayv1.Hostname(testDashboardHost) {
			t.Errorf("corrected.Spec.Hostnames = %v, want drift corrected back to [%s]", corrected.Spec.Hostnames, testDashboardHost)
		}
	})

	t.Run("deletes the HTTPRoute once spec.gateway is disabled", func(t *testing.T) {
		disabled := instance.DeepCopy()
		disabled.Spec.Gateway.Enabled = false

		if err := r.reconcileHTTPRoute(ctx, disabled); err != nil {
			t.Fatalf("reconcileHTTPRoute() unexpected error: %v", err)
		}
		err := cl.Get(ctx, nn, &gatewayv1.HTTPRoute{})
		if !apierrors.IsNotFound(err) {
			t.Errorf("expected HTTPRoute to be deleted, Get() returned err=%v", err)
		}
	})

	t.Run("is a no-op when disabled and already absent", func(t *testing.T) {
		disabled := instance.DeepCopy()
		disabled.Spec.Gateway.Enabled = false

		if err := r.reconcileHTTPRoute(ctx, disabled); err != nil {
			t.Errorf("reconcileHTTPRoute() unexpected error: %v", err)
		}
	})
}

func TestMapToDashboard(t *testing.T) {
	extract := func(b *pagev1alpha1.Bookmark) string { return b.Spec.DashboardRef.Name }
	mapFn := mapToDashboard(extract)
	ctx := t.Context()

	t.Run("enqueues the referenced Dashboard in the object's namespace", func(t *testing.T) {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm", Namespace: "ns"},
			Spec:       pagev1alpha1.BookmarkSpec{DashboardRef: pagev1alpha1.DashboardRef{Name: testRefDashboardName}},
		}
		reqs := mapFn(ctx, bm)
		if len(reqs) != 1 || reqs[0].Name != testRefDashboardName || reqs[0].Namespace != "ns" {
			t.Errorf("mapFn() = %+v, want a single request for ns/inst", reqs)
		}
	})

	t.Run("returns nil when the dashboardRef name is empty", func(t *testing.T) {
		bm := &pagev1alpha1.Bookmark{ObjectMeta: metav1.ObjectMeta{Name: "bm", Namespace: "ns"}}
		if reqs := mapFn(ctx, bm); reqs != nil {
			t.Errorf("mapFn() = %+v, want nil", reqs)
		}
	})

	t.Run("returns nil for an object of the wrong type", func(t *testing.T) {
		cfg := &pagev1alpha1.ServiceCard{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "ns"}}
		if reqs := mapFn(ctx, cfg); reqs != nil {
			t.Errorf("mapFn() = %+v, want nil for a non-Bookmark object", reqs)
		}
	})
}

func TestMergeManagedAnnotations(t *testing.T) {
	t.Run("preserves a foreign annotation no spec ever set", func(t *testing.T) {
		existing := map[string]string{testForeignAnnotationKey: testForeignAnnotationValue}
		desired := map[string]string{}
		got := mergeManagedAnnotations(existing, desired)
		if got[testForeignAnnotationKey] != testForeignAnnotationValue {
			t.Errorf("mergeManagedAnnotations() = %+v, want %s preserved", got, testForeignAnnotationKey)
		}
	})

	t.Run("sets a newly desired key and records it as managed", func(t *testing.T) {
		got := mergeManagedAnnotations(nil, map[string]string{"a": "1"})
		if got["a"] != "1" {
			t.Errorf("mergeManagedAnnotations() = %+v, want a=1", got)
		}
		if got[managedAnnotationsKey] != "a" {
			t.Errorf("mergeManagedAnnotations()[%s] = %q, want %q", managedAnnotationsKey, got[managedAnnotationsKey], "a")
		}
	})

	t.Run("prunes a previously-managed key removed from desired, without touching a foreign key", func(t *testing.T) {
		existing := map[string]string{
			"a":                      "1",
			testForeignAnnotationKey: testForeignAnnotationValue,
			managedAnnotationsKey:    "a",
		}
		got := mergeManagedAnnotations(existing, map[string]string{})
		if _, ok := got["a"]; ok {
			t.Errorf("mergeManagedAnnotations() = %+v, want key %q removed", got, "a")
		}
		if got[testForeignAnnotationKey] != testForeignAnnotationValue {
			t.Errorf("mergeManagedAnnotations() = %+v, want %s preserved", got, testForeignAnnotationKey)
		}
		if _, ok := got[managedAnnotationsKey]; ok {
			t.Errorf("mergeManagedAnnotations() = %+v, want marker removed once nothing is managed", got)
		}
	})

	t.Run("updates the value of an already-managed key", func(t *testing.T) {
		existing := map[string]string{"a": "1", managedAnnotationsKey: "a"}
		got := mergeManagedAnnotations(existing, map[string]string{"a": "2"})
		if got["a"] != "2" {
			t.Errorf("mergeManagedAnnotations() = %+v, want a=2", got)
		}
	})
}

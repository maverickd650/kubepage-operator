package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
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

func TestHTTPRouteForInstance(t *testing.T) {
	ns := "ghr"
	r := &InstanceReconciler{Scheme: networkTestScheme(t)}
	instance := &pagev1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceObjName, Namespace: ns},
		Spec: pagev1alpha1.InstanceSpec{
			ContainerPort: 8080,
			Gateway: &pagev1alpha1.GatewaySpec{
				Enabled:   true,
				Hostnames: []string{testDashboardHost, "dash.example.com"},
				ParentRef: pagev1alpha1.GatewayParentRef{
					Name:        "eg",
					Namespace:   ptr.To("gateway-system"),
					SectionName: ptr.To("https"),
				},
				Annotations: map[string]string{"example.com/note": "hi"},
			},
		},
	}

	route, err := r.httpRouteForInstance(instance)
	if err != nil {
		t.Fatalf("httpRouteForInstance() unexpected error: %v", err)
	}

	if route.Name != testInstanceObjName || route.Namespace != ns {
		t.Errorf("route name/namespace = %s/%s, want %s/%s", route.Namespace, route.Name, ns, testInstanceObjName)
	}
	if route.Annotations["example.com/note"] != "hi" {
		t.Errorf("route annotations = %+v, want example.com/note=hi", route.Annotations)
	}

	wantHostnames := []gatewayv1.Hostname{testDashboardHost, "dash.example.com"}
	if len(route.Spec.Hostnames) != len(wantHostnames) {
		t.Fatalf("route.Spec.Hostnames = %v, want %v", route.Spec.Hostnames, wantHostnames)
	}
	for i, h := range wantHostnames {
		if route.Spec.Hostnames[i] != h {
			t.Errorf("route.Spec.Hostnames[%d] = %q, want %q", i, route.Spec.Hostnames[i], h)
		}
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
	if pr.SectionName == nil || *pr.SectionName != "https" {
		t.Errorf("ParentRefs[0].SectionName = %v, want %q", pr.SectionName, "https")
	}

	if len(route.Spec.Rules) != 1 || len(route.Spec.Rules[0].BackendRefs) != 1 {
		t.Fatalf("route.Spec.Rules = %+v, want exactly one rule with one backendRef", route.Spec.Rules)
	}
	backend := route.Spec.Rules[0].BackendRefs[0].BackendRef
	if backend.Name != testInstanceObjName {
		t.Errorf("backend.Name = %q, want %q", backend.Name, testInstanceObjName)
	}
	if backend.Port == nil || *backend.Port != 8080 {
		t.Errorf("backend.Port = %v, want 8080", backend.Port)
	}

	if len(route.OwnerReferences) != 1 || route.OwnerReferences[0].Name != testInstanceObjName {
		t.Errorf("route.OwnerReferences = %+v, want owned by Instance %q", route.OwnerReferences, testInstanceObjName)
	}
}

func TestHTTPRouteForInstanceNoParentNamespaceOrSection(t *testing.T) {
	r := &InstanceReconciler{Scheme: networkTestScheme(t)}
	instance := &pagev1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceObjName, Namespace: "ns"},
		Spec: pagev1alpha1.InstanceSpec{
			ContainerPort: 8080,
			Gateway: &pagev1alpha1.GatewaySpec{
				Enabled:   true,
				Hostnames: []string{testDashboardHost},
				ParentRef: pagev1alpha1.GatewayParentRef{Name: "eg"},
			},
		},
	}

	route, err := r.httpRouteForInstance(instance)
	if err != nil {
		t.Fatalf("httpRouteForInstance() unexpected error: %v", err)
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
					BackendObjectReference: gatewayv1.BackendObjectReference{Name: testInstanceObjName, Port: ptr.To[gatewayv1.PortNumber](8080)},
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
	defaultedBackendRef.Weight = ptr.To(int32(1))
	defaulted.Rules = []gatewayv1.HTTPRouteRule{{BackendRefs: []gatewayv1.HTTPBackendRef{defaultedBackendRef}}}

	if !httpRouteSpecsEqual(base, defaulted) {
		t.Errorf("httpRouteSpecsEqual(base, defaulted) = false, want true (API-server-defaulted fields must be ignored)")
	}

	changedHost := base
	changedHost.Hostnames = []gatewayv1.Hostname{"other.example.com"}
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
}

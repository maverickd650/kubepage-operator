package controller

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// instance_network.go's reconcileService/reconcileIngress happy paths are
// already covered by the envtest-backed Ginkgo specs in
// instance_controller_test.go (reconcileHTTPRoute's own lifecycle is covered
// by TestReconcileHTTPRouteLifecycle above). The error branches below need a
// client that can be made to fail Get/Create/Update/Delete on demand, or a
// scheme missing pagev1alpha1 to make SetControllerReference fail - neither
// of which a real apiserver can do, hence the fake client + interceptor.Funcs
// pattern used throughout this package.

const networkErrorTestNamespace = "net-err-ns"

func newNetworkErrorTestInstance() *pagev1alpha1.Instance {
	return &pagev1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceObjName, Namespace: networkErrorTestNamespace},
		Spec:       pagev1alpha1.InstanceSpec{ContainerPort: 8080},
	}
}

// --- serviceForInstance / reconcileService ---

func TestServiceForInstanceSetControllerReferenceError(t *testing.T) {
	instance := newNetworkErrorTestInstance()
	r := &InstanceReconciler{Scheme: schemeWithoutInstance(t)}

	if _, err := r.serviceForInstance(instance); err == nil {
		t.Error("serviceForInstance() error = nil, want the SetControllerReference error")
	}
}

func TestReconcileServiceDefineError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkErrorTestInstance()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &InstanceReconciler{Client: cl, Scheme: schemeWithoutInstance(t)}

	if err := r.reconcileService(t.Context(), instance); err == nil {
		t.Error("reconcileService() error = nil, want the SetControllerReference error wrapped")
	}
}

func TestReconcileServiceGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkErrorTestInstance()
	wantErr := errors.New("get Service boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*corev1.Service); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileService(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileService() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileServiceCreateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkErrorTestInstance()
	wantErr := errors.New("create Service boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*corev1.Service); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileService(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileService() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileServiceUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkErrorTestInstance()
	wantErr := errors.New("update Service boom")

	drifted := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"stale": "true"},
			Ports:    []corev1.ServicePort{{Name: "old", Port: 1234}},
		},
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, drifted).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*corev1.Service); ok {
				return wantErr
			}
			return c.Update(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileService(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileService() error = %v, want %v", err, wantErr)
	}
}

// --- ingressForInstance / reconcileIngress ---

func TestIngressForInstanceSetControllerReferenceError(t *testing.T) {
	instance := newNetworkErrorTestInstance()
	instance.Spec.Ingress = &pagev1alpha1.IngressSpec{Enabled: true, Host: testDashboardHost}
	r := &InstanceReconciler{Scheme: schemeWithoutInstance(t)}

	if _, err := r.ingressForInstance(instance); err == nil {
		t.Error("ingressForInstance() error = nil, want the SetControllerReference error")
	}
}

func TestReconcileIngressDefineErrorOnCreate(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkErrorTestInstance()
	instance.Spec.Ingress = &pagev1alpha1.IngressSpec{Enabled: true, Host: testDashboardHost}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &InstanceReconciler{Client: cl, Scheme: schemeWithoutInstance(t)}

	if err := r.reconcileIngress(t.Context(), instance); err == nil {
		t.Error("reconcileIngress() error = nil, want the SetControllerReference error wrapped")
	}
}

func TestReconcileIngressGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkErrorTestInstance()
	wantErr := errors.New("get Ingress boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*networkingv1.Ingress); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileIngress(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileIngress() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileIngressCreateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkErrorTestInstance()
	instance.Spec.Ingress = &pagev1alpha1.IngressSpec{Enabled: true, Host: testDashboardHost}
	wantErr := errors.New("create Ingress boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*networkingv1.Ingress); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileIngress(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileIngress() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileIngressDeleteError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkErrorTestInstance() // spec.ingress is nil: not enabled
	wantErr := errors.New("delete Ingress boom")

	existing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace}}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, existing).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.DeleteOption) error {
			if _, ok := o.(*networkingv1.Ingress); ok {
				return wantErr
			}
			return c.Delete(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileIngress(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileIngress() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileIngressUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkErrorTestInstance()
	instance.Spec.Ingress = &pagev1alpha1.IngressSpec{Enabled: true, Host: testDashboardHost}
	wantErr := errors.New("update Ingress boom")

	drifted := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{{Host: testOtherHost}},
		},
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, drifted).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*networkingv1.Ingress); ok {
				return wantErr
			}
			return c.Update(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileIngress(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileIngress() error = %v, want %v", err, wantErr)
	}
}

// TestReconcileIngressDefineErrorOnUpdate covers reconcileIngress's second
// ingressForInstance call (once an Ingress already exists and drift is
// checked), distinct from TestReconcileIngressDefineErrorOnCreate which only
// exercises the create-path call.
func TestReconcileIngressDefineErrorOnUpdate(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkErrorTestInstance()
	instance.Spec.Ingress = &pagev1alpha1.IngressSpec{Enabled: true, Host: testDashboardHost}

	existing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, existing).Build()
	r := &InstanceReconciler{Client: cl, Scheme: schemeWithoutInstance(t)}

	if err := r.reconcileIngress(t.Context(), instance); err == nil {
		t.Error("reconcileIngress() error = nil, want the SetControllerReference error wrapped")
	}
}

// --- reconcileHTTPRoute (GatewayAPIEnabled: true) ---

func newGatewayEnabledInstance() *pagev1alpha1.Instance {
	instance := newNetworkErrorTestInstance()
	instance.Spec.Gateway = &pagev1alpha1.GatewaySpec{
		Enabled:   true,
		Hostnames: []string{testDashboardHost},
		ParentRef: pagev1alpha1.GatewayParentRef{Name: "eg"},
	}
	return instance
}

func TestReconcileHTTPRouteDisabledGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkErrorTestInstance() // spec.gateway is nil: not enabled
	wantErr := errors.New("get HTTPRoute boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*gatewayv1.HTTPRoute); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme, GatewayAPIEnabled: true}

	if err := r.reconcileHTTPRoute(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileHTTPRoute() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileHTTPRouteDisabledDeleteError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkErrorTestInstance() // spec.gateway is nil: not enabled
	wantErr := errors.New("delete HTTPRoute boom")

	existing := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace}}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, existing).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.DeleteOption) error {
			if _, ok := o.(*gatewayv1.HTTPRoute); ok {
				return wantErr
			}
			return c.Delete(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme, GatewayAPIEnabled: true}

	if err := r.reconcileHTTPRoute(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileHTTPRoute() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileHTTPRouteDefineError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newGatewayEnabledInstance()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &InstanceReconciler{Client: cl, Scheme: schemeWithoutInstance(t), GatewayAPIEnabled: true}

	if err := r.reconcileHTTPRoute(t.Context(), instance); err == nil {
		t.Error("reconcileHTTPRoute() error = nil, want the SetControllerReference error wrapped")
	}
}

func TestReconcileHTTPRouteGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newGatewayEnabledInstance()
	wantErr := errors.New("get HTTPRoute boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*gatewayv1.HTTPRoute); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme, GatewayAPIEnabled: true}

	if err := r.reconcileHTTPRoute(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileHTTPRoute() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileHTTPRouteCreateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newGatewayEnabledInstance()
	wantErr := errors.New("create HTTPRoute boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*gatewayv1.HTTPRoute); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme, GatewayAPIEnabled: true}

	if err := r.reconcileHTTPRoute(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileHTTPRoute() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileHTTPRouteUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newGatewayEnabledInstance()
	wantErr := errors.New("update HTTPRoute boom")

	drifted := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{testOtherHost},
		},
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, drifted).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*gatewayv1.HTTPRoute); ok {
				return wantErr
			}
			return c.Update(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme, GatewayAPIEnabled: true}

	if err := r.reconcileHTTPRoute(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileHTTPRoute() error = %v, want %v", err, wantErr)
	}
}

// --- httpRouteForInstance ---

func TestHTTPRouteForInstanceSetControllerReferenceError(t *testing.T) {
	instance := newNetworkErrorTestInstance()
	instance.Spec.Gateway = &pagev1alpha1.GatewaySpec{
		Enabled:   true,
		Hostnames: []string{testDashboardHost},
		ParentRef: pagev1alpha1.GatewayParentRef{Name: "eg"},
	}
	r := &InstanceReconciler{Scheme: schemeWithoutInstance(t)}

	if _, err := r.httpRouteForInstance(instance); err == nil {
		t.Error("httpRouteForInstance() error = nil, want the SetControllerReference error")
	}
}

// --- portsEqual ---

func TestPortsEqualFieldMismatch(t *testing.T) {
	base := []corev1.ServicePort{{Name: testPortNameHTTP, Port: 80}}

	if !portsEqual(base, base) {
		t.Errorf("portsEqual(base, base) = false, want true")
	}

	nameDiffers := []corev1.ServicePort{{Name: testPortNameHTTPS, Port: 80}}
	if portsEqual(base, nameDiffers) {
		t.Errorf("portsEqual() = true, want false (name differs)")
	}

	portDiffers := []corev1.ServicePort{{Name: testPortNameHTTP, Port: 8080}}
	if portsEqual(base, portDiffers) {
		t.Errorf("portsEqual() = true, want false (port differs)")
	}
}

package controller

import (
	"context"
	"errors"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const networkPolicyTestNamespace = "np-ns"

const testEgressCIDR = "10.0.0.0/8"

func newNetworkPolicyTestDashboard() *pagev1alpha1.Dashboard {
	return &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: networkPolicyTestNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			ContainerPort: 8080,
			NetworkPolicy: &pagev1alpha1.NetworkPolicySpec{Enabled: pagev1alpha1.Enabled},
		},
	}
}

func TestNetworkPolicyForDashboardUnrestrictedIngressNoEgressByDefault(t *testing.T) {
	r := &DashboardReconciler{Scheme: networkTestScheme(t)}
	instance := newNetworkPolicyTestDashboard()

	np, err := r.networkPolicyForDashboard(instance)
	if err != nil {
		t.Fatalf("networkPolicyForDashboard() error = %v", err)
	}

	if len(np.Spec.PolicyTypes) != 1 || np.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Errorf("PolicyTypes = %v, want just [Ingress] when egressCIDRs is unset", np.Spec.PolicyTypes)
	}
	if len(np.Spec.Ingress) != 1 {
		t.Fatalf("Ingress rules = %+v, want exactly 1 (metrics not enabled)", np.Spec.Ingress)
	}
	if np.Spec.Ingress[0].From != nil {
		t.Errorf("Ingress[0].From = %+v, want nil (unrestricted) when ingressNamespaceSelector is unset", np.Spec.Ingress[0].From)
	}
	if len(np.Spec.Egress) != 0 {
		t.Errorf("Egress = %+v, want none by default", np.Spec.Egress)
	}
	if len(np.OwnerReferences) != 1 || np.OwnerReferences[0].Name != testDashboardObjName {
		t.Errorf("OwnerReferences = %+v, want owned by Dashboard %q", np.OwnerReferences, testDashboardObjName)
	}
}

func TestNetworkPolicyForDashboardMetricsPortOnlyWhenMetricsEnabled(t *testing.T) {
	r := &DashboardReconciler{Scheme: networkTestScheme(t)}
	instance := newNetworkPolicyTestDashboard()
	instance.Spec.Metrics = &pagev1alpha1.MetricsSpec{Enabled: pagev1alpha1.Enabled}

	np, err := r.networkPolicyForDashboard(instance)
	if err != nil {
		t.Fatalf("networkPolicyForDashboard() error = %v", err)
	}
	if len(np.Spec.Ingress) != 2 {
		t.Fatalf("Ingress rules = %+v, want 2 (dashboard + metrics)", np.Spec.Ingress)
	}
	if np.Spec.Ingress[1].Ports[0].Port.IntVal != dashboardMetricsPort {
		t.Errorf("Ingress[1] port = %v, want %d", np.Spec.Ingress[1].Ports[0].Port, dashboardMetricsPort)
	}
}

func TestNetworkPolicyForDashboardIngressNamespaceSelectorScoped(t *testing.T) {
	r := &DashboardReconciler{Scheme: networkTestScheme(t)}
	instance := newNetworkPolicyTestDashboard()
	instance.Spec.NetworkPolicy.IngressNamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "ingress-nginx"}}

	np, err := r.networkPolicyForDashboard(instance)
	if err != nil {
		t.Fatalf("networkPolicyForDashboard() error = %v", err)
	}
	if len(np.Spec.Ingress[0].From) != 1 || np.Spec.Ingress[0].From[0].NamespaceSelector == nil {
		t.Fatalf("Ingress[0].From = %+v, want a single namespaceSelector peer", np.Spec.Ingress[0].From)
	}
}

func TestNetworkPolicyForDashboardEgressCIDRsAddEgressPolicyType(t *testing.T) {
	r := &DashboardReconciler{Scheme: networkTestScheme(t)}
	instance := newNetworkPolicyTestDashboard()
	instance.Spec.NetworkPolicy.EgressCIDRs = []string{testEgressCIDR, "192.168.1.0/24"}

	np, err := r.networkPolicyForDashboard(instance)
	if err != nil {
		t.Fatalf("networkPolicyForDashboard() error = %v", err)
	}
	if len(np.Spec.PolicyTypes) != 2 {
		t.Fatalf("PolicyTypes = %v, want [Ingress, Egress]", np.Spec.PolicyTypes)
	}
	// DNS rule + API server rule + one rule per CIDR.
	if len(np.Spec.Egress) != 4 {
		t.Fatalf("Egress rules = %+v, want 4 (DNS, API server, 2 CIDRs)", np.Spec.Egress)
	}
	lastTwoCIDRs := []string{np.Spec.Egress[2].To[0].IPBlock.CIDR, np.Spec.Egress[3].To[0].IPBlock.CIDR}
	want := []string{testEgressCIDR, "192.168.1.0/24"}
	for i := range want {
		if lastTwoCIDRs[i] != want[i] {
			t.Errorf("Egress CIDR[%d] = %q, want %q", i, lastTwoCIDRs[i], want[i])
		}
	}
}

func TestReconcileNetworkPolicyLifecycle(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkPolicyTestDashboard()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileNetworkPolicy(t.Context(), instance); err != nil {
		t.Fatalf("reconcileNetworkPolicy() create error = %v", err)
	}
	var np networkingv1.NetworkPolicy
	if err := cl.Get(t.Context(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, &np); err != nil {
		t.Fatalf("getting created NetworkPolicy: %v", err)
	}
}

// TestReconcileNetworkPolicyDisabledSkipsClientEntirely asserts the
// least-privilege property documented on reconcileNetworkPolicy: when
// spec.networkPolicy is unset (the default for every existing Dashboard),
// reconcileNetworkPolicy must not call the client at all, so a Dashboard
// that never opts into this field is never affected by it — including by a
// manager whose ClusterRole doesn't yet grant networkpolicies RBAC.
func TestReconcileNetworkPolicyDisabledSkipsClientEntirely(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkPolicyTestDashboard()
	instance.Spec.NetworkPolicy = nil

	cl := interceptor.NewClient(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build(),
		interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				t.Fatal("reconcileNetworkPolicy() called Get, want no client calls when spec.networkPolicy is unset")
				return nil
			},
		},
	)
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileNetworkPolicy(t.Context(), instance); err != nil {
		t.Fatalf("reconcileNetworkPolicy() error = %v", err)
	}
}

func TestReconcileNetworkPolicySetControllerReferenceError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkPolicyTestDashboard()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: schemeWithoutDashboard(t)}

	if err := r.reconcileNetworkPolicy(t.Context(), instance); err == nil {
		t.Error("reconcileNetworkPolicy() error = nil, want the SetControllerReference error")
	}
}

func TestReconcileNetworkPolicyGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkPolicyTestDashboard()
	wantErr := errors.New("get NetworkPolicy boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*networkingv1.NetworkPolicy); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileNetworkPolicy(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileNetworkPolicy() error = %v, want wrapping %v", err, wantErr)
	}
}

// TestReconcileNetworkPolicyUpdatesOnDrift verifies an existing NetworkPolicy
// whose Spec no longer matches spec.networkPolicy (e.g. egressCIDRs changed)
// gets updated in place.
func TestReconcileNetworkPolicyUpdatesOnDrift(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkPolicyTestDashboard()
	instance.Spec.NetworkPolicy.EgressCIDRs = []string{testEgressCIDR}

	stale := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: selectorLabelsForDashboard()},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, stale).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileNetworkPolicy(t.Context(), instance); err != nil {
		t.Fatalf("reconcileNetworkPolicy() error = %v", err)
	}

	got := &networkingv1.NetworkPolicy{}
	if err := cl.Get(t.Context(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, got); err != nil {
		t.Fatalf("Get() after reconcile error = %v", err)
	}
	if len(got.Spec.Egress) == 0 {
		t.Errorf("NetworkPolicy.Spec.Egress = %+v, want it updated to include the egressCIDRs rule", got.Spec.Egress)
	}
}

func TestReconcileNetworkPolicyUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkPolicyTestDashboard()
	instance.Spec.NetworkPolicy.EgressCIDRs = []string{testEgressCIDR}
	wantErr := errors.New("update NetworkPolicy boom")

	stale := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: selectorLabelsForDashboard()},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
		},
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, stale).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*networkingv1.NetworkPolicy); ok {
				return wantErr
			}
			return c.Update(ctx, o, opts...)
		},
	})
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileNetworkPolicy(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileNetworkPolicy() error = %v, want wrapping %v", err, wantErr)
	}
}

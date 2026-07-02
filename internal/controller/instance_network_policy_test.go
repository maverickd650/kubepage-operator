package controller

import (
	"context"
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

func newNetworkPolicyTestInstance() *pagev1alpha1.Instance {
	return &pagev1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceObjName, Namespace: networkPolicyTestNamespace},
		Spec: pagev1alpha1.InstanceSpec{
			ContainerPort: 8080,
			NetworkPolicy: &pagev1alpha1.NetworkPolicySpec{Enabled: pagev1alpha1.Enabled},
		},
	}
}

func TestNetworkPolicyForInstanceUnrestrictedIngressNoEgressByDefault(t *testing.T) {
	r := &InstanceReconciler{Scheme: networkTestScheme(t)}
	instance := newNetworkPolicyTestInstance()

	np, err := r.networkPolicyForInstance(instance)
	if err != nil {
		t.Fatalf("networkPolicyForInstance() error = %v", err)
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
	if len(np.OwnerReferences) != 1 || np.OwnerReferences[0].Name != testInstanceObjName {
		t.Errorf("OwnerReferences = %+v, want owned by Instance %q", np.OwnerReferences, testInstanceObjName)
	}
}

func TestNetworkPolicyForInstanceMetricsPortOnlyWhenMetricsEnabled(t *testing.T) {
	r := &InstanceReconciler{Scheme: networkTestScheme(t)}
	instance := newNetworkPolicyTestInstance()
	instance.Spec.Metrics = &pagev1alpha1.MetricsSpec{Enabled: pagev1alpha1.Enabled}

	np, err := r.networkPolicyForInstance(instance)
	if err != nil {
		t.Fatalf("networkPolicyForInstance() error = %v", err)
	}
	if len(np.Spec.Ingress) != 2 {
		t.Fatalf("Ingress rules = %+v, want 2 (dashboard + metrics)", np.Spec.Ingress)
	}
	if np.Spec.Ingress[1].Ports[0].Port.IntVal != dashboardMetricsPort {
		t.Errorf("Ingress[1] port = %v, want %d", np.Spec.Ingress[1].Ports[0].Port, dashboardMetricsPort)
	}
}

func TestNetworkPolicyForInstanceIngressNamespaceSelectorScoped(t *testing.T) {
	r := &InstanceReconciler{Scheme: networkTestScheme(t)}
	instance := newNetworkPolicyTestInstance()
	instance.Spec.NetworkPolicy.IngressNamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "ingress-nginx"}}

	np, err := r.networkPolicyForInstance(instance)
	if err != nil {
		t.Fatalf("networkPolicyForInstance() error = %v", err)
	}
	if len(np.Spec.Ingress[0].From) != 1 || np.Spec.Ingress[0].From[0].NamespaceSelector == nil {
		t.Fatalf("Ingress[0].From = %+v, want a single namespaceSelector peer", np.Spec.Ingress[0].From)
	}
}

func TestNetworkPolicyForInstanceEgressCIDRsAddEgressPolicyType(t *testing.T) {
	r := &InstanceReconciler{Scheme: networkTestScheme(t)}
	instance := newNetworkPolicyTestInstance()
	instance.Spec.NetworkPolicy.EgressCIDRs = []string{"10.0.0.0/8", "192.168.1.0/24"}

	np, err := r.networkPolicyForInstance(instance)
	if err != nil {
		t.Fatalf("networkPolicyForInstance() error = %v", err)
	}
	if len(np.Spec.PolicyTypes) != 2 {
		t.Fatalf("PolicyTypes = %v, want [Ingress, Egress]", np.Spec.PolicyTypes)
	}
	// DNS rule + API server rule + one rule per CIDR.
	if len(np.Spec.Egress) != 4 {
		t.Fatalf("Egress rules = %+v, want 4 (DNS, API server, 2 CIDRs)", np.Spec.Egress)
	}
	lastTwoCIDRs := []string{np.Spec.Egress[2].To[0].IPBlock.CIDR, np.Spec.Egress[3].To[0].IPBlock.CIDR}
	want := []string{"10.0.0.0/8", "192.168.1.0/24"}
	for i := range want {
		if lastTwoCIDRs[i] != want[i] {
			t.Errorf("Egress CIDR[%d] = %q, want %q", i, lastTwoCIDRs[i], want[i])
		}
	}
}

func TestReconcileNetworkPolicyLifecycle(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkPolicyTestInstance()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileNetworkPolicy(t.Context(), instance); err != nil {
		t.Fatalf("reconcileNetworkPolicy() create error = %v", err)
	}
	var np networkingv1.NetworkPolicy
	if err := cl.Get(t.Context(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, &np); err != nil {
		t.Fatalf("getting created NetworkPolicy: %v", err)
	}
}

// TestReconcileNetworkPolicyDisabledSkipsClientEntirely guards the fix for a
// real regression this feature caused (docs/security-review.md's
// spec.networkPolicy shipped without its RBAC yet, since
// config/rbac/role.yaml can only be regenerated by `mise run manifests`):
// when spec.networkPolicy is unset (the default for every existing
// Instance), reconcileNetworkPolicy must not call the client at all, or a
// manager whose ClusterRole doesn't yet grant networkpolicies RBAC would 403
// on every reconcile of every Instance, not just ones that opt into this
// field.
func TestReconcileNetworkPolicyDisabledSkipsClientEntirely(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newNetworkPolicyTestInstance()
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
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileNetworkPolicy(t.Context(), instance); err != nil {
		t.Fatalf("reconcileNetworkPolicy() error = %v", err)
	}
}

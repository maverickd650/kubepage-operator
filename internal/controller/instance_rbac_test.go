package controller

import (
	"slices"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// TestClusterRBACNameNoCollision guards against a bare hyphen-join of
// namespace and name producing the same cluster-scoped RBAC name for two
// different Instances, since both namespace and name are valid DNS-1123
// labels that may themselves contain hyphens.
func TestClusterRBACNameNoCollision(t *testing.T) {
	a := &pagev1alpha1.Instance{ObjectMeta: metav1.ObjectMeta{Namespace: "a", Name: "b-c"}}
	b := &pagev1alpha1.Instance{ObjectMeta: metav1.ObjectMeta{Namespace: "a-b", Name: "c"}}

	if clusterRBACName(a) == clusterRBACName(b) {
		t.Fatalf("clusterRBACName collided for distinct Instances: %q", clusterRBACName(a))
	}
}

// TestClusterRBACNameStaysWithinKubernetesLimit guards against a long
// namespace/name pair producing a name Kubernetes rejects at Create time: a
// namespace name may be up to 63 characters and an Instance name (an
// ordinary Kubernetes object name) up to 253, so the length-prefixed
// encoding alone could exceed clusterRBACNameMaxLength for the longest
// legal inputs.
func TestClusterRBACNameStaysWithinKubernetesLimit(t *testing.T) {
	longNamespace := strings.Repeat("a", 63)
	longName := strings.Repeat("b", 253)
	instance := &pagev1alpha1.Instance{ObjectMeta: metav1.ObjectMeta{Namespace: longNamespace, Name: longName}}

	name := clusterRBACName(instance)
	if len(name) > clusterRBACNameMaxLength {
		t.Fatalf("clusterRBACName(%q/%q) = %q (%d chars), want <= %d", longNamespace, longName, name, len(name), clusterRBACNameMaxLength)
	}

	// Two different long Instances that would truncate to the same prefix
	// must still get distinct names.
	otherName := strings.Repeat("c", 253)
	other := &pagev1alpha1.Instance{ObjectMeta: metav1.ObjectMeta{Namespace: longNamespace, Name: otherName}}
	if clusterRBACName(instance) == clusterRBACName(other) {
		t.Fatalf("clusterRBACName collided for distinct long Instances: %q", clusterRBACName(instance))
	}

	// Re-deriving the name from the same Instance must be stable across
	// reconciles (reconcileClusterRole/reconcileClusterRoleBinding Get by
	// this name every time).
	if clusterRBACName(instance) != name {
		t.Fatalf("clusterRBACName is not deterministic: got %q and %q for the same Instance", name, clusterRBACName(instance))
	}
}

// TestDashboardRolesGrantsPods guards against the per-Instance Role losing
// its pods access, which would silently break PodSelector-based status for
// every Instance: internal/dashboard/poller.go's monitor lists Pods through
// this Role regardless of whether any bound ServiceEntry uses PodSelector.
func TestDashboardRolesGrantsPods(t *testing.T) {
	for _, secretNames := range [][]string{nil, {"some-secret"}} {
		rules := dashboardRoles(secretNames, false)
		found := slices.ContainsFunc(rules, func(r rbacv1.PolicyRule) bool {
			return slices.Contains(r.Resources, resourcePods) &&
				slices.Contains(r.Verbs, verbGet) &&
				slices.Contains(r.Verbs, verbList) &&
				slices.Contains(r.Verbs, "watch")
		})
		if !found {
			t.Errorf("dashboardRoles(%v, false) has no pods get/list/watch rule", secretNames)
		}
	}
}

// TestDashboardRolesGrantsIngressOnlyWhenDiscoveryEnabled guards the
// least-privilege intent of DiscoverySpec: the per-Instance Role should only
// carry Ingress read access while Ingress annotation discovery is actually
// turned on for that Instance.
func TestDashboardRolesGrantsIngressOnlyWhenDiscoveryEnabled(t *testing.T) {
	hasIngressRule := func(rules []rbacv1.PolicyRule) bool {
		return slices.ContainsFunc(rules, func(r rbacv1.PolicyRule) bool {
			return slices.Contains(r.Resources, "ingresses") && slices.Contains(r.Verbs, verbGet)
		})
	}

	if hasIngressRule(dashboardRoles(nil, false)) {
		t.Error("dashboardRoles(nil, false) unexpectedly grants ingresses access")
	}
	if !hasIngressRule(dashboardRoles(nil, true)) {
		t.Error("dashboardRoles(nil, true) has no ingresses get/list/watch rule")
	}
}

func TestPolicyRulesEqual(t *testing.T) {
	base := []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{resourcePods}, Verbs: []string{verbGet, verbList}}}

	tests := map[string]struct {
		a, b []rbacv1.PolicyRule
		want bool
	}{
		"equal":                  {base, []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{resourcePods}, Verbs: []string{verbList, verbGet}}}, true},
		"different length":       {base, nil, false},
		"different resources":    {base, []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{verbGet, verbList}}}, false},
		"different api groups":   {base, []rbacv1.PolicyRule{{APIGroups: []string{"apps"}, Resources: []string{resourcePods}, Verbs: []string{verbGet, verbList}}}, false},
		"different resourceName": {base, []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{resourcePods}, Verbs: []string{verbGet, verbList}, ResourceNames: []string{"x"}}}, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := policyRulesEqual(tc.a, tc.b); got != tc.want {
				t.Errorf("policyRulesEqual() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStringSlicesEqualSorted(t *testing.T) {
	tests := map[string]struct {
		a, b []string
		want bool
	}{
		"equal unordered":   {[]string{"b", "a"}, []string{"a", "b"}, true},
		"different length":  {[]string{"a"}, []string{"a", "b"}, false},
		"different content": {[]string{"a", "c"}, []string{"a", "b"}, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := stringSlicesEqualSorted(tc.a, tc.b); got != tc.want {
				t.Errorf("stringSlicesEqualSorted(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

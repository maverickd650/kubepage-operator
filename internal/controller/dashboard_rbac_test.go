package controller

import (
	"slices"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// testAmbiguousJoinNamespace/Name and testAmbiguousJoinOtherNamespace/Name
// are the classic "hyphen-join ambiguity" fixture — namespace "a"/name
// "b-c" vs. namespace "a-b"/name "c" collide under a bare
// "<namespace>-<name>" join — shared by every *RBACNameNoCollision test
// (cluster/discovery/monitor) so they don't each repeat the same literals
// (goconst).
const (
	testAmbiguousJoinNamespace      = "a"
	testAmbiguousJoinName           = "b-c"
	testAmbiguousJoinOtherNamespace = "a-b"
	testAmbiguousJoinOtherName      = "c"
)

// TestClusterRBACNameNoCollision guards against a bare hyphen-join of
// namespace and name producing the same cluster-scoped RBAC name for two
// different Dashboards, since both namespace and name are valid DNS-1123
// labels that may themselves contain hyphens.
func TestClusterRBACNameNoCollision(t *testing.T) {
	a := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Namespace: testAmbiguousJoinNamespace, Name: testAmbiguousJoinName}}
	b := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Namespace: testAmbiguousJoinOtherNamespace, Name: testAmbiguousJoinOtherName}}

	if clusterRBACName(a) == clusterRBACName(b) {
		t.Fatalf("clusterRBACName collided for distinct Dashboards: %q", clusterRBACName(a))
	}
}

// TestClusterRBACNameStaysWithinKubernetesLimit guards against a long
// namespace/name pair producing a name Kubernetes rejects at Create time: a
// namespace name may be up to 63 characters and a Dashboard name (an
// ordinary Kubernetes object name) up to 253, so the length-prefixed
// encoding alone could exceed clusterRBACNameMaxLength for the longest
// legal inputs.
func TestClusterRBACNameStaysWithinKubernetesLimit(t *testing.T) {
	longNamespace := strings.Repeat("a", 63)
	longName := strings.Repeat("b", 253)
	instance := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Namespace: longNamespace, Name: longName}}

	name := clusterRBACName(instance)
	if len(name) > clusterRBACNameMaxLength {
		t.Fatalf("clusterRBACName(%q/%q) = %q (%d chars), want <= %d", longNamespace, longName, name, len(name), clusterRBACNameMaxLength)
	}

	// Two different long Dashboards that would truncate to the same prefix
	// must still get distinct names.
	otherName := strings.Repeat("c", 253)
	other := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Namespace: longNamespace, Name: otherName}}
	if clusterRBACName(instance) == clusterRBACName(other) {
		t.Fatalf("clusterRBACName collided for distinct long Dashboards: %q", clusterRBACName(instance))
	}

	// Re-deriving the name from the same Dashboard must be stable across
	// reconciles (reconcileClusterRole/reconcileClusterRoleBinding Get by
	// this name every time).
	if clusterRBACName(instance) != name {
		t.Fatalf("clusterRBACName is not deterministic: got %q and %q for the same Dashboard", name, clusterRBACName(instance))
	}
}

// TestDiscoveryRBACNameNoCollision mirrors TestClusterRBACNameNoCollision for
// discoveryRBACName, and additionally guards against it colliding with
// clusterRBACName for the same Dashboard — a Dashboard using both kubemetrics
// and cross-namespace discovery needs two distinct cluster-scoped RBAC names.
func TestDiscoveryRBACNameNoCollision(t *testing.T) {
	a := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Namespace: testAmbiguousJoinNamespace, Name: testAmbiguousJoinName}}
	b := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Namespace: testAmbiguousJoinOtherNamespace, Name: testAmbiguousJoinOtherName}}

	if discoveryRBACName(a) == discoveryRBACName(b) {
		t.Fatalf("discoveryRBACName collided for distinct Dashboards: %q", discoveryRBACName(a))
	}
	if discoveryRBACName(a) == clusterRBACName(a) {
		t.Fatalf("discoveryRBACName collided with clusterRBACName for the same Dashboard: %q", discoveryRBACName(a))
	}
}

// TestDiscoveryRBACNameStaysWithinKubernetesLimit mirrors
// TestClusterRBACNameStaysWithinKubernetesLimit for discoveryRBACName.
func TestDiscoveryRBACNameStaysWithinKubernetesLimit(t *testing.T) {
	longNamespace := strings.Repeat("a", 63)
	longName := strings.Repeat("b", 253)
	instance := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Namespace: longNamespace, Name: longName}}

	name := discoveryRBACName(instance)
	if len(name) > clusterRBACNameMaxLength {
		t.Fatalf("discoveryRBACName(%q/%q) = %q (%d chars), want <= %d", longNamespace, longName, name, len(name), clusterRBACNameMaxLength)
	}
	if discoveryRBACName(instance) != name {
		t.Fatalf("discoveryRBACName is not deterministic: got %q and %q for the same Dashboard", name, discoveryRBACName(instance))
	}
}

// testCrossNSTargetNamespace, testOwnDashNamespace, and
// testSharedThirdNamespace are shared by
// TestDiscoveryNamespacesFiltersOwnNamespaceAndDisabled and
// TestMonitorNamespacesFiltersOwnNamespace (their spec.discovery.namespaces/
// spec.monitorNamespaces filtering logic is otherwise identical), pulled out
// to package scope so the two tests share one literal each instead of
// tripping goconst on near-duplicate string constants.
const (
	testCrossNSTargetNamespace = "cross-ns-target"
	testOwnDashNamespace       = "dash-ns"
	testSharedThirdNamespace   = "monitoring"
)

// TestDiscoveryNamespacesFiltersOwnNamespaceAndDisabled verifies
// discoveryNamespaces returns nil when discovery is off/unset, and filters
// the Dashboard's own namespace out of spec.discovery.namespaces (it's
// already covered by the per-Dashboard Role — see dashboardRoles — so a
// redundant cross-namespace RoleBinding there would just be noise).
func TestDiscoveryNamespacesFiltersOwnNamespaceAndDisabled(t *testing.T) {
	base := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Namespace: testOwnDashNamespace, Name: "d"}}

	if got := discoveryNamespaces(base); got != nil {
		t.Fatalf("discoveryNamespaces() with no Discovery = %v, want nil", got)
	}

	base.Spec.Discovery = &pagev1alpha1.DiscoverySpec{Namespaces: []string{testCrossNSTargetNamespace}}
	if got := discoveryNamespaces(base); got != nil {
		t.Fatalf("discoveryNamespaces() with Discovery not Enabled = %v, want nil", got)
	}

	base.Spec.Discovery.Enabled = true
	base.Spec.Discovery.Namespaces = []string{testCrossNSTargetNamespace, testOwnDashNamespace, testSharedThirdNamespace, testCrossNSTargetNamespace}
	got := discoveryNamespaces(base)
	want := []string{testCrossNSTargetNamespace, testSharedThirdNamespace}
	if !slices.Equal(got, want) {
		t.Fatalf("discoveryNamespaces() = %v, want %v (own namespace filtered, de-duplicated, sorted)", got, want)
	}
}

// TestDiscoveryClusterRoleRules verifies discoveryClusterRoleRules includes
// the Ingress rule whenever Sources selects it, and the HTTPRoute rule only
// when Sources selects it *and* the cluster has Gateway API installed —
// mirroring dashboardRoles' same in-namespace gating (dashboardHTTPRouteRule
// must never be granted for a Kind the apiserver doesn't recognize).
func TestDiscoveryClusterRoleRules(t *testing.T) {
	ingressOnly := &pagev1alpha1.DiscoverySpec{}
	rules := discoveryClusterRoleRules(ingressOnly, true)
	if len(rules) != 1 || !slices.Contains(rules[0].Resources, "ingresses") {
		t.Fatalf("discoveryClusterRoleRules() with default Sources = %+v, want just the Ingress rule", rules)
	}

	both := &pagev1alpha1.DiscoverySpec{Sources: []string{pagev1alpha1.DiscoverySourceIngress, pagev1alpha1.DiscoverySourceHTTPRoute}}

	rules = discoveryClusterRoleRules(both, true)
	if len(rules) != 2 {
		t.Fatalf("discoveryClusterRoleRules() with both Sources and Gateway API enabled = %+v, want 2 rules", rules)
	}
	foundHTTPRoute := false
	for _, r := range rules {
		if slices.Contains(r.Resources, "httproutes") {
			foundHTTPRoute = true
		}
	}
	if !foundHTTPRoute {
		t.Errorf("discoveryClusterRoleRules() with HTTPRoute source and Gateway API enabled = %+v, want an httproutes rule", rules)
	}

	rules = discoveryClusterRoleRules(both, false)
	if len(rules) != 1 {
		t.Fatalf("discoveryClusterRoleRules() with HTTPRoute source but Gateway API disabled = %+v, want just the Ingress rule", rules)
	}
}

// TestMonitorRBACNameNoCollision mirrors TestDiscoveryRBACNameNoCollision for
// monitorRBACName, and additionally guards against it colliding with
// clusterRBACName/discoveryRBACName for the same Dashboard — a Dashboard
// using kubemetrics, discovery, and cross-namespace pod monitoring at once
// needs three distinct cluster-scoped RBAC names.
func TestMonitorRBACNameNoCollision(t *testing.T) {
	a := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Namespace: testAmbiguousJoinNamespace, Name: testAmbiguousJoinName}}
	b := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Namespace: testAmbiguousJoinOtherNamespace, Name: testAmbiguousJoinOtherName}}

	if monitorRBACName(a) == monitorRBACName(b) {
		t.Fatalf("monitorRBACName collided for distinct Dashboards: %q", monitorRBACName(a))
	}
	if monitorRBACName(a) == clusterRBACName(a) {
		t.Fatalf("monitorRBACName collided with clusterRBACName for the same Dashboard: %q", monitorRBACName(a))
	}
	if monitorRBACName(a) == discoveryRBACName(a) {
		t.Fatalf("monitorRBACName collided with discoveryRBACName for the same Dashboard: %q", monitorRBACName(a))
	}
}

// TestMonitorRBACNameStaysWithinKubernetesLimit mirrors
// TestDiscoveryRBACNameStaysWithinKubernetesLimit for monitorRBACName.
func TestMonitorRBACNameStaysWithinKubernetesLimit(t *testing.T) {
	longNamespace := strings.Repeat("a", 63)
	longName := strings.Repeat("b", 253)
	instance := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Namespace: longNamespace, Name: longName}}

	name := monitorRBACName(instance)
	if len(name) > clusterRBACNameMaxLength {
		t.Fatalf("monitorRBACName(%q/%q) = %q (%d chars), want <= %d", longNamespace, longName, name, len(name), clusterRBACNameMaxLength)
	}
	if monitorRBACName(instance) != name {
		t.Fatalf("monitorRBACName is not deterministic: got %q and %q for the same Dashboard", name, monitorRBACName(instance))
	}
}

// TestMonitorNamespacesFiltersOwnNamespace mirrors
// TestDiscoveryNamespacesFiltersOwnNamespaceAndDisabled for monitorNamespaces
// — spec.monitorNamespaces has no separate "enabled" toggle (unlike
// DiscoverySpec), so this only needs to check the own-namespace filter,
// de-duplication, and sorting.
func TestMonitorNamespacesFiltersOwnNamespace(t *testing.T) {
	base := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Namespace: testOwnDashNamespace, Name: "d"}}
	if got := monitorNamespaces(base); len(got) != 0 {
		t.Fatalf("monitorNamespaces() with no MonitorNamespaces = %v, want empty", got)
	}

	base.Spec.MonitorNamespaces = []string{testCrossNSTargetNamespace, testOwnDashNamespace, testSharedThirdNamespace, testCrossNSTargetNamespace}
	got := monitorNamespaces(base)
	want := []string{testCrossNSTargetNamespace, testSharedThirdNamespace}
	if !slices.Equal(got, want) {
		t.Fatalf("monitorNamespaces() = %v, want %v (own namespace filtered, de-duplicated, sorted)", got, want)
	}
}

// TestDashboardRolesGrantsPods guards against the per-Dashboard Role losing
// its pods access, which would silently break PodSelector-based status for
// every Dashboard: internal/dashboard/poller.go's monitor lists Pods through
// this Role regardless of whether any bound ServiceCard uses PodSelector.
func TestDashboardRolesGrantsPods(t *testing.T) {
	for _, secretNames := range [][]string{nil, {"some-secret"}} {
		rules := dashboardRoles(secretNames, nil, false)
		found := slices.ContainsFunc(rules, func(r rbacv1.PolicyRule) bool {
			return slices.Contains(r.Resources, resourcePods) &&
				slices.Contains(r.Verbs, verbGet) &&
				slices.Contains(r.Verbs, verbList) &&
				slices.Contains(r.Verbs, "watch")
		})
		if !found {
			t.Errorf("dashboardRoles(%v, nil, false) has no pods get/list/watch rule", secretNames)
		}
	}
}

func discoverySpec(enabled bool, sources ...string) *pagev1alpha1.DiscoverySpec {
	if !enabled {
		return nil
	}
	return &pagev1alpha1.DiscoverySpec{Enabled: true, Sources: sources}
}

// TestDashboardRolesGrantsIngressOnlyWhenDiscoveryEnabled guards the
// least-privilege intent of DiscoverySpec: the per-Dashboard Role should only
// carry Ingress read access while Ingress annotation discovery is actually
// turned on for that Dashboard, and Sources includes "Ingress" (the default
// when Sources is unset).
func TestDashboardRolesGrantsIngressOnlyWhenDiscoveryEnabled(t *testing.T) {
	hasIngressRule := func(rules []rbacv1.PolicyRule) bool {
		return slices.ContainsFunc(rules, func(r rbacv1.PolicyRule) bool {
			return slices.Contains(r.Resources, "ingresses") && slices.Contains(r.Verbs, verbGet)
		})
	}

	if hasIngressRule(dashboardRoles(nil, discoverySpec(false), false)) {
		t.Error("dashboardRoles with discovery disabled unexpectedly grants ingresses access")
	}
	if !hasIngressRule(dashboardRoles(nil, discoverySpec(true), false)) {
		t.Error("dashboardRoles with discovery enabled (no sources) has no ingresses get/list/watch rule")
	}
	if !hasIngressRule(dashboardRoles(nil, discoverySpec(true, pagev1alpha1.DiscoverySourceIngress), false)) {
		t.Error("dashboardRoles with sources=[Ingress] has no ingresses get/list/watch rule")
	}
	if hasIngressRule(dashboardRoles(nil, discoverySpec(true, pagev1alpha1.DiscoverySourceHTTPRoute), true)) {
		t.Error("dashboardRoles with sources=[HTTPRoute] unexpectedly grants ingresses access")
	}
}

// TestDashboardRolesGrantsHTTPRouteOnlyWhenDiscoveryAndGatewayAPIEnabled
// mirrors TestDashboardRolesGrantsIngressOnlyWhenDiscoveryEnabled for the
// HTTPRoute discovery source: the Role should only carry HTTPRoute read
// access when discovery is on, Sources includes "HTTPRoute", and the cluster
// actually has Gateway API installed — granting it otherwise would be a
// permission the dashboard pod could never use.
func TestDashboardRolesGrantsHTTPRouteOnlyWhenDiscoveryAndGatewayAPIEnabled(t *testing.T) {
	hasHTTPRouteRule := func(rules []rbacv1.PolicyRule) bool {
		return slices.ContainsFunc(rules, func(r rbacv1.PolicyRule) bool {
			return slices.Contains(r.Resources, "httproutes") && slices.Contains(r.Verbs, verbGet)
		})
	}

	withSources := discoverySpec(true, pagev1alpha1.DiscoverySourceIngress, pagev1alpha1.DiscoverySourceHTTPRoute)
	if hasHTTPRouteRule(dashboardRoles(nil, withSources, false)) {
		t.Error("dashboardRoles with sources=[Ingress,HTTPRoute] unexpectedly grants httproutes access without Gateway API")
	}
	if hasHTTPRouteRule(dashboardRoles(nil, discoverySpec(false), true)) {
		t.Error("dashboardRoles with discovery disabled unexpectedly grants httproutes access")
	}
	if hasHTTPRouteRule(dashboardRoles(nil, discoverySpec(true), true)) {
		t.Error("dashboardRoles with discovery enabled (no sources, defaults to Ingress) unexpectedly grants httproutes access")
	}
	if !hasHTTPRouteRule(dashboardRoles(nil, withSources, true)) {
		t.Error("dashboardRoles with sources=[Ingress,HTTPRoute] and Gateway API enabled has no httproutes get/list/watch rule")
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
		"different resources":    {base, []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{resourceSecrets}, Verbs: []string{verbGet, verbList}}}, false},
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

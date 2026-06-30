package controller

import (
	"slices"
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

// TestDashboardRolesGrantsPods guards against the per-Instance Role losing
// its pods access, which would silently break PodSelector-based status for
// every Instance: internal/dashboard/poller.go's monitor lists Pods through
// this Role regardless of whether any bound ServiceEntry uses PodSelector.
func TestDashboardRolesGrantsPods(t *testing.T) {
	for _, secretNames := range [][]string{nil, {"some-secret"}} {
		rules := dashboardRoles(secretNames)
		found := slices.ContainsFunc(rules, func(r rbacv1.PolicyRule) bool {
			return slices.Contains(r.Resources, "pods") &&
				slices.Contains(r.Verbs, "get") &&
				slices.Contains(r.Verbs, "list") &&
				slices.Contains(r.Verbs, "watch")
		})
		if !found {
			t.Errorf("dashboardRoles(%v) has no pods get/list/watch rule", secretNames)
		}
	}
}

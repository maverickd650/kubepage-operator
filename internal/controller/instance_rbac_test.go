package controller

import (
	"testing"

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

package controller

import (
	"context"
	"errors"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// reconcileMonitorRBAC mirrors reconcileDiscoveryRBAC (dashboard_rbac.go), so
// these specs mirror dashboard_discovery_rbac_error_paths_test.go's fake-
// client + interceptor.Funcs approach — no envtest needed, unlike the
// Ginkgo-backed happy-path specs in dashboard_controller_test.go.

const monitorErrorTestNamespace = "monitor-target"

func newMonitorTestDashboard(namespaces ...string) *pagev1alpha1.Dashboard {
	return &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: rbacTestNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			ContainerPort:     8080,
			MonitorNamespaces: namespaces,
		},
	}
}

// TestReconcileMonitorRBACCreatesClusterRoleAndRoleBinding verifies the
// happy path: a Dashboard naming a foreign namespace in
// spec.monitorNamespaces gets a ClusterRole (dashboardPodsRule) and a
// RoleBinding in that namespace, and status.monitorNamespaces records it.
func TestReconcileMonitorRBACCreatesClusterRoleAndRoleBinding(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newMonitorTestDashboard(monitorErrorTestNamespace)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileMonitorRBAC(t.Context(), instance); err != nil {
		t.Fatalf("reconcileMonitorRBAC() error = %v", err)
	}

	name := monitorRBACName(instance)
	var cr rbacv1.ClusterRole
	if err := cl.Get(t.Context(), types.NamespacedName{Name: name}, &cr); err != nil {
		t.Fatalf("Get monitor ClusterRole: %v", err)
	}
	if !policyRulesEqual(cr.Rules, monitorClusterRoleRules) {
		t.Errorf("monitor ClusterRole.Rules = %+v, want %+v", cr.Rules, monitorClusterRoleRules)
	}

	var rb rbacv1.RoleBinding
	if err := cl.Get(t.Context(), types.NamespacedName{Name: name, Namespace: monitorErrorTestNamespace}, &rb); err != nil {
		t.Fatalf("Get monitor RoleBinding: %v", err)
	}
	if rb.RoleRef.Kind != clusterRoleKind || rb.RoleRef.Name != name {
		t.Errorf("monitor RoleBinding.RoleRef = %+v, want ClusterRole %q", rb.RoleRef, name)
	}

	if len(instance.Status.MonitorNamespaces) != 1 || instance.Status.MonitorNamespaces[0] != monitorErrorTestNamespace {
		t.Errorf("instance.Status.MonitorNamespaces = %v, want [%q]", instance.Status.MonitorNamespaces, monitorErrorTestNamespace)
	}
}

// TestReconcileMonitorRBACRemovesRoleBindingWhenNamespaceDropped verifies a
// namespace removed from spec.monitorNamespaces has its RoleBinding deleted
// on the next reconcile, using instance.Status.MonitorNamespaces (not a live
// List) to find it — this operator never requests cluster-wide RoleBinding
// list/watch.
func TestReconcileMonitorRBACRemovesRoleBindingWhenNamespaceDropped(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newMonitorTestDashboard(monitorErrorTestNamespace)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileMonitorRBAC(t.Context(), instance); err != nil {
		t.Fatalf("reconcileMonitorRBAC() (create) error = %v", err)
	}

	instance.Spec.MonitorNamespaces = nil
	if err := r.reconcileMonitorRBAC(t.Context(), instance); err != nil {
		t.Fatalf("reconcileMonitorRBAC() (empty) error = %v", err)
	}

	name := monitorRBACName(instance)
	var rb rbacv1.RoleBinding
	err := cl.Get(t.Context(), types.NamespacedName{Name: name, Namespace: monitorErrorTestNamespace}, &rb)
	if err == nil {
		t.Fatal("monitor RoleBinding still exists after monitorNamespaces emptied, want it deleted")
	}
	var cr rbacv1.ClusterRole
	if err := cl.Get(t.Context(), types.NamespacedName{Name: name}, &cr); err == nil {
		t.Fatal("monitor ClusterRole still exists after monitorNamespaces emptied, want it deleted")
	}
	if len(instance.Status.MonitorNamespaces) != 0 {
		t.Errorf("instance.Status.MonitorNamespaces = %v, want empty", instance.Status.MonitorNamespaces)
	}
}

// TestDoFinalizerOperationsForDashboardDeletesMonitorRBAC verifies the
// Dashboard finalizer path cleans up monitor RBAC (deleteMonitorRoleBindings
// + deleteMonitorClusterRole), which can't carry an owner reference since a
// namespaced Dashboard can't own a cluster-scoped ClusterRole or a
// RoleBinding in another namespace.
func TestDoFinalizerOperationsForDashboardDeletesMonitorRBAC(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newMonitorTestDashboard(monitorErrorTestNamespace)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, Recorder: events.NewFakeRecorder(10)}

	if err := r.reconcileMonitorRBAC(t.Context(), instance); err != nil {
		t.Fatalf("reconcileMonitorRBAC() error = %v", err)
	}

	if err := r.doFinalizerOperationsForDashboard(t.Context(), instance); err != nil {
		t.Fatalf("doFinalizerOperationsForDashboard() error = %v", err)
	}

	name := monitorRBACName(instance)
	var rb rbacv1.RoleBinding
	if err := cl.Get(t.Context(), types.NamespacedName{Name: name, Namespace: monitorErrorTestNamespace}, &rb); err == nil {
		t.Fatal("monitor RoleBinding still exists after finalizer cleanup, want it deleted")
	}
	var cr rbacv1.ClusterRole
	if err := cl.Get(t.Context(), types.NamespacedName{Name: name}, &cr); err == nil {
		t.Fatal("monitor ClusterRole still exists after finalizer cleanup, want it deleted")
	}
}

func TestReconcileMonitorRBACStatusUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newMonitorTestDashboard(monitorErrorTestNamespace)
	wantErr := errors.New("status update boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			if _, ok := obj.(*pagev1alpha1.Dashboard); ok && subResourceName == "status" {
				return wantErr
			}
			return c.SubResource(subResourceName).Update(ctx, obj, opts...)
		},
	})
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileMonitorRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileMonitorRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileMonitorRBACClusterRoleError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newMonitorTestDashboard(monitorErrorTestNamespace)
	wantErr := errors.New("create monitor ClusterRole boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*rbacv1.ClusterRole); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileMonitorRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileMonitorRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

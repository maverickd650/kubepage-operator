package controller

import (
	"context"
	"errors"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// reconcileDiscoveryRBAC and its helpers (dashboard_rbac.go) are exercised
// happy-path by the envtest-backed Ginkgo specs in dashboard_controller_test.go.
// The error branches below need a client that can be made to fail on demand,
// hence the same fake client + interceptor.Funcs pattern as
// dashboard_rbac_error_paths_test.go.

const discoveryErrorTestNamespace = "discovery-target"

func newDiscoveryTestDashboard(namespaces ...string) *pagev1alpha1.Dashboard {
	return &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: rbacTestNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			ContainerPort: 8080,
			Discovery: &pagev1alpha1.DiscoverySpec{
				Enabled:    pagev1alpha1.Enabled,
				Namespaces: namespaces,
			},
		},
	}
}

func TestReconcileDiscoveryRBACStatusUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDiscoveryTestDashboard(discoveryErrorTestNamespace)
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

	if err := r.reconcileDiscoveryRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileDiscoveryRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileDiscoveryRBACClusterRoleError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDiscoveryTestDashboard(discoveryErrorTestNamespace)
	wantErr := errors.New("create discovery ClusterRole boom")

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

	if err := r.reconcileDiscoveryRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileDiscoveryRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

// TestReconcileDiscoveryRBACPartialRemoval verifies dropping one of two
// previously-applied namespaces (rather than emptying the list entirely)
// deletes only the dropped namespace's RoleBinding and narrows
// status.discoveryNamespaces down to what remains.
func TestReconcileDiscoveryRBACPartialRemoval(t *testing.T) {
	scheme := networkTestScheme(t)
	const keep, drop = "keep-ns", "drop-ns"
	instance := newDiscoveryTestDashboard(keep, drop)

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	r := &DashboardReconciler{Client: base, Scheme: scheme}

	if err := r.reconcileDiscoveryRBAC(t.Context(), instance); err != nil {
		t.Fatalf("reconcileDiscoveryRBAC() first call error = %v", err)
	}

	name := discoveryRBACName(instance)
	for _, ns := range []string{keep, drop} {
		rb := &rbacv1.RoleBinding{}
		if err := base.Get(t.Context(), client.ObjectKey{Name: name, Namespace: ns}, rb); err != nil {
			t.Fatalf("RoleBinding in namespace %s not created: %v", ns, err)
		}
	}

	instance.Spec.Discovery.Namespaces = []string{keep}
	if err := r.reconcileDiscoveryRBAC(t.Context(), instance); err != nil {
		t.Fatalf("reconcileDiscoveryRBAC() second call error = %v", err)
	}

	if err := base.Get(t.Context(), client.ObjectKey{Name: name, Namespace: keep}, &rbacv1.RoleBinding{}); err != nil {
		t.Errorf("RoleBinding in kept namespace %s should still exist: %v", keep, err)
	}
	if err := base.Get(t.Context(), client.ObjectKey{Name: name, Namespace: drop}, &rbacv1.RoleBinding{}); err == nil {
		t.Errorf("RoleBinding in dropped namespace %s should have been deleted", drop)
	}
	if got := instance.Status.DiscoveryNamespaces; len(got) != 1 || got[0] != keep {
		t.Errorf("Status.DiscoveryNamespaces = %v, want [%s]", got, keep)
	}
}

func TestReconcileDiscoveryRBACPartialRemovalDeleteError(t *testing.T) {
	scheme := networkTestScheme(t)
	const keep, drop = "keep-ns", "drop-ns"
	instance := newDiscoveryTestDashboard(keep, drop)
	wantErr := errors.New("delete discovery RoleBinding boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	setupReconciler := &DashboardReconciler{Client: base, Scheme: scheme}
	if err := setupReconciler.reconcileDiscoveryRBAC(t.Context(), instance); err != nil {
		t.Fatalf("setup reconcileDiscoveryRBAC() error = %v", err)
	}

	name := discoveryRBACName(instance)
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.DeleteOption) error {
			if rb, ok := o.(*rbacv1.RoleBinding); ok && rb.Name == name && rb.Namespace == drop {
				return wantErr
			}
			return c.Delete(ctx, o, opts...)
		},
	})
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	instance.Spec.Discovery.Namespaces = []string{keep}
	if err := r.reconcileDiscoveryRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileDiscoveryRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileDiscoveryRBACEmptyDeleteError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDiscoveryTestDashboard(discoveryErrorTestNamespace)
	wantErr := errors.New("delete discovery RoleBinding boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	setupReconciler := &DashboardReconciler{Client: base, Scheme: scheme}
	if err := setupReconciler.reconcileDiscoveryRBAC(t.Context(), instance); err != nil {
		t.Fatalf("setup reconcileDiscoveryRBAC() error = %v", err)
	}

	cl := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.DeleteOption) error {
			if _, ok := o.(*rbacv1.RoleBinding); ok {
				return wantErr
			}
			return c.Delete(ctx, o, opts...)
		},
	})
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	instance.Spec.Discovery.Namespaces = nil
	if err := r.reconcileDiscoveryRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileDiscoveryRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

// --- reconcileDiscoveryClusterRole ---

func TestReconcileDiscoveryClusterRoleGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDiscoveryTestDashboard(discoveryErrorTestNamespace)
	wantErr := errors.New("get discovery ClusterRole boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*rbacv1.ClusterRole); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileDiscoveryClusterRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileDiscoveryClusterRole() error = %v, want %v", err, wantErr)
	}
}

// TestReconcileDiscoveryClusterRoleUpdatesOnRuleDrift verifies a ClusterRole
// whose stored Rules no longer match discoveryClusterRoleRules' current
// output (e.g. spec.discovery.sources changed between reconciles) gets
// updated in place rather than left stale.
func TestReconcileDiscoveryClusterRoleUpdatesOnRuleDrift(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDiscoveryTestDashboard(discoveryErrorTestNamespace)

	stale := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: discoveryRBACName(instance)},
		Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{resourcePods}, Verbs: []string{verbGet}}},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, stale).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileDiscoveryClusterRole(t.Context(), instance); err != nil {
		t.Fatalf("reconcileDiscoveryClusterRole() error = %v", err)
	}

	got := &rbacv1.ClusterRole{}
	if err := cl.Get(t.Context(), client.ObjectKey{Name: discoveryRBACName(instance)}, got); err != nil {
		t.Fatalf("Get() after reconcile error = %v", err)
	}
	if !policyRulesEqual(got.Rules, discoveryClusterRoleRules(instance.Spec.Discovery, false)) {
		t.Errorf("ClusterRole.Rules = %+v, want it updated to match discoveryClusterRoleRules()", got.Rules)
	}
}

func TestReconcileDiscoveryClusterRoleUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDiscoveryTestDashboard(discoveryErrorTestNamespace)
	wantErr := errors.New("update discovery ClusterRole boom")

	stale := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: discoveryRBACName(instance)},
		Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{resourcePods}, Verbs: []string{verbGet}}},
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, stale).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*rbacv1.ClusterRole); ok {
				return wantErr
			}
			return c.Update(ctx, o, opts...)
		},
	})
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileDiscoveryClusterRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileDiscoveryClusterRole() error = %v, want %v", err, wantErr)
	}
}

// --- deleteDiscoveryClusterRole ---

func TestDeleteDiscoveryClusterRoleNoOpWhenAbsent(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDiscoveryTestDashboard()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.deleteDiscoveryClusterRole(t.Context(), instance); err != nil {
		t.Errorf("deleteDiscoveryClusterRole() error = %v, want nil when nothing exists", err)
	}
}

func TestDeleteDiscoveryClusterRoleDeleteError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDiscoveryTestDashboard()
	wantErr := errors.New("delete discovery ClusterRole boom")

	cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: discoveryRBACName(instance)}}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, cr).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.DeleteOption) error {
			if _, ok := o.(*rbacv1.ClusterRole); ok {
				return wantErr
			}
			return c.Delete(ctx, o, opts...)
		},
	})
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.deleteDiscoveryClusterRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("deleteDiscoveryClusterRole() error = %v, want wrapping %v", err, wantErr)
	}
}

// --- deleteDiscoveryRoleBindings ---

func TestDeleteDiscoveryRoleBindingsNoOpWhenAbsent(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDiscoveryTestDashboard()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.deleteDiscoveryRoleBindings(t.Context(), instance, []string{discoveryErrorTestNamespace}); err != nil {
		t.Errorf("deleteDiscoveryRoleBindings() error = %v, want nil when nothing exists", err)
	}
}

func TestDeleteDiscoveryRoleBindingsDeleteError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDiscoveryTestDashboard()
	wantErr := errors.New("delete discovery RoleBinding boom")

	rb := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: discoveryRBACName(instance), Namespace: discoveryErrorTestNamespace}}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, rb).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.DeleteOption) error {
			if _, ok := o.(*rbacv1.RoleBinding); ok {
				return wantErr
			}
			return c.Delete(ctx, o, opts...)
		},
	})
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.deleteDiscoveryRoleBindings(t.Context(), instance, []string{discoveryErrorTestNamespace}); !errors.Is(err, wantErr) {
		t.Errorf("deleteDiscoveryRoleBindings() error = %v, want wrapping %v", err, wantErr)
	}
}

// --- doFinalizerOperationsForDashboard's discovery cleanup ---

func TestDoFinalizerOperationsForDashboardDiscoveryRoleBindingError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDiscoveryTestDashboard()
	instance.Status.DiscoveryNamespaces = []string{discoveryErrorTestNamespace}
	wantErr := errors.New("delete discovery RoleBinding boom")

	rb := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: discoveryRBACName(instance), Namespace: discoveryErrorTestNamespace}}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, rb).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.DeleteOption) error {
			if _, ok := o.(*rbacv1.RoleBinding); ok {
				return wantErr
			}
			return c.Delete(ctx, o, opts...)
		},
	})
	r := &DashboardReconciler{Client: cl, Scheme: scheme, Recorder: events.NewFakeRecorder(10)}

	if err := r.doFinalizerOperationsForDashboard(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("doFinalizerOperationsForDashboard() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestDoFinalizerOperationsForDashboardDiscoveryClusterRoleError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDiscoveryTestDashboard()
	wantErr := errors.New("delete discovery ClusterRole boom")

	cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: discoveryRBACName(instance)}}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, cr).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.DeleteOption) error {
			if _, ok := o.(*rbacv1.ClusterRole); ok {
				return wantErr
			}
			return c.Delete(ctx, o, opts...)
		},
	})
	r := &DashboardReconciler{Client: cl, Scheme: scheme, Recorder: events.NewFakeRecorder(10)}

	if err := r.doFinalizerOperationsForDashboard(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("doFinalizerOperationsForDashboard() error = %v, want wrapping %v", err, wantErr)
	}
}

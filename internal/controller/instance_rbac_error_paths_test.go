package controller

import (
	"context"
	"errors"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// instance_rbac.go's RBAC reconcile functions are exercised happy-path by
// the envtest-backed Ginkgo specs in instance_controller_test.go. The error
// branches below (Get/Create/Update failures, SetControllerReference
// failures, List failures) need a client that can be made to fail on
// demand, hence the same fake client + interceptor.Funcs pattern as
// instance_deployment_test.go and reconcile_error_paths_test.go.

const rbacTestNamespace = "rbac-ns"

func newRBACTestInstance() *pagev1alpha1.Instance {
	return &pagev1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceObjName, Namespace: rbacTestNamespace},
		Spec:       pagev1alpha1.InstanceSpec{ContainerPort: 8080},
	}
}

func newKubeMetricsInfoWidget(instance *pagev1alpha1.Instance) *pagev1alpha1.InfoWidget {
	return &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testInfoWidgetNameMetrics, Namespace: instance.Namespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: instance.Name},
			Type:        kubeMetricsWidgetType,
		},
	}
}

// --- reconcileDashboardRBAC error wrapping ---

func TestReconcileDashboardRBACServiceAccountError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("create SA boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*corev1.ServiceAccount); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	err := r.reconcileDashboardRBAC(t.Context(), instance)
	if !errors.Is(err, wantErr) {
		t.Errorf("reconcileDashboardRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileDashboardRBACRoleError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("create Role boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*rbacv1.Role); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	err := r.reconcileDashboardRBAC(t.Context(), instance)
	if !errors.Is(err, wantErr) {
		t.Errorf("reconcileDashboardRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileDashboardRBACRoleBindingError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("create RoleBinding boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*rbacv1.RoleBinding); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	err := r.reconcileDashboardRBAC(t.Context(), instance)
	if !errors.Is(err, wantErr) {
		t.Errorf("reconcileDashboardRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReferencedSecretNamesIgnoresOtherInstancesAndCollectsKeyRefs(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()

	wantSecret := "creds"
	matchingEntry := &pagev1alpha1.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: instance.Namespace},
		Spec: pagev1alpha1.ServiceEntrySpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: instance.Name},
			Group:       "G", Name: "N",
			Widgets: []pagev1alpha1.ServiceWidget{{
				Type: testWidgetTypeGrafana,
				Secrets: map[string]pagev1alpha1.SecretValueSource{
					secretField: {SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: wantSecret},
						Key:                  secretField,
					}},
				},
			}},
		},
	}
	otherInstanceEntry := &pagev1alpha1.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-other", Namespace: instance.Namespace},
		Spec: pagev1alpha1.ServiceEntrySpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testOtherInstanceName},
			Group:       "G", Name: "N",
			Widgets: []pagev1alpha1.ServiceWidget{{
				Type: testWidgetTypeGrafana,
				Secrets: map[string]pagev1alpha1.SecretValueSource{
					secretField: {SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "should-not-appear"},
						Key:                  secretField,
					}},
				},
			}},
		},
	}
	otherInstanceWidget := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "iw-other", Namespace: instance.Namespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testOtherInstanceName},
			Type:        testWidgetTypeOpenMeteo,
		},
	}
	wantWidgetSecret := "widget-creds"
	matchingWidget := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "iw", Namespace: instance.Namespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: instance.Name},
			Type:        testWidgetTypeOpenMeteo,
			Secrets: map[string]pagev1alpha1.SecretValueSource{
				secretField: {SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: wantWidgetSecret},
					Key:                  secretField,
				}},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(instance, matchingEntry, otherInstanceEntry, otherInstanceWidget, matchingWidget).Build()
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() unexpected error: %v", err)
	}
	want := []string{wantSecret, wantWidgetSecret}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("referencedSecretNames() = %v, want %v (other instance's refs and inline values excluded)", got, want)
	}
}

// --- reconcileServiceAccount ---

func TestReconcileServiceAccountSetControllerReferenceError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &InstanceReconciler{Client: cl, Scheme: schemeWithoutInstance(t)}

	if err := r.reconcileServiceAccount(t.Context(), instance); err == nil {
		t.Error("reconcileServiceAccount() error = nil, want the SetControllerReference error")
	}
}

func TestReconcileServiceAccountGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("get SA boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*corev1.ServiceAccount); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileServiceAccount(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileServiceAccount() error = %v, want %v", err, wantErr)
	}
}

// --- reconcileRole ---

func TestReconcileRoleSecretNamesServiceEntryListError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("list ServiceEntries boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*pagev1alpha1.ServiceEntryList); ok {
				return wantErr
			}
			return c.List(ctx, list, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileRole() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileRoleSecretNamesInfoWidgetListError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("list InfoWidgets boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*pagev1alpha1.InfoWidgetList); ok {
				return wantErr
			}
			return c.List(ctx, list, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileRole() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileRoleSetControllerReferenceError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &InstanceReconciler{Client: cl, Scheme: schemeWithoutInstance(t)}

	if err := r.reconcileRole(t.Context(), instance); err == nil {
		t.Error("reconcileRole() error = nil, want the SetControllerReference error")
	}
}

func TestReconcileRoleGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("get Role boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*rbacv1.Role); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileRole() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileRoleUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("update Role boom")

	drifted := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
		Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{resourcePods}, Verbs: []string{verbGet}}},
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, drifted).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*rbacv1.Role); ok {
				return wantErr
			}
			return c.Update(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileRole() error = %v, want %v", err, wantErr)
	}
}

// --- reconcileRoleBinding ---

func TestReconcileRoleBindingSetControllerReferenceError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &InstanceReconciler{Client: cl, Scheme: schemeWithoutInstance(t)}

	if err := r.reconcileRoleBinding(t.Context(), instance); err == nil {
		t.Error("reconcileRoleBinding() error = nil, want the SetControllerReference error")
	}
}

func TestReconcileRoleBindingGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("get RoleBinding boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*rbacv1.RoleBinding); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileRoleBinding(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileRoleBinding() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileRoleBindingCreateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("create RoleBinding boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*rbacv1.RoleBinding); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileRoleBinding(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileRoleBinding() error = %v, want %v", err, wantErr)
	}
}

// --- reconcileClusterMetricsRBAC / instanceHasKubeMetricsWidget ---

func TestReconcileClusterMetricsRBACListError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("list InfoWidgets boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*pagev1alpha1.InfoWidgetList); ok {
				return wantErr
			}
			return c.List(ctx, list, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileClusterMetricsRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileClusterMetricsRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileClusterMetricsRBACClusterRoleError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	widget := newKubeMetricsInfoWidget(instance)
	wantErr := errors.New("create ClusterRole boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, widget).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*rbacv1.ClusterRole); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	err := r.reconcileClusterMetricsRBAC(t.Context(), instance)
	if !errors.Is(err, wantErr) {
		t.Errorf("reconcileClusterMetricsRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileClusterMetricsRBACClusterRoleBindingError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	widget := newKubeMetricsInfoWidget(instance)
	wantErr := errors.New("create ClusterRoleBinding boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, widget).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*rbacv1.ClusterRoleBinding); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	err := r.reconcileClusterMetricsRBAC(t.Context(), instance)
	if !errors.Is(err, wantErr) {
		t.Errorf("reconcileClusterMetricsRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

// --- reconcileClusterRole ---

func TestReconcileClusterRoleGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("get ClusterRole boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*rbacv1.ClusterRole); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileClusterRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileClusterRole() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileClusterRoleUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("update ClusterRole boom")

	drifted := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRBACName(instance)},
		Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{resourcePods}, Verbs: []string{verbGet}}},
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, drifted).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*rbacv1.ClusterRole); ok {
				return wantErr
			}
			return c.Update(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileClusterRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileClusterRole() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileClusterRoleNoDrift(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()

	matching := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRBACName(instance)},
		Rules:      clusterMetricsRules,
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, matching).Build()
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileClusterRole(t.Context(), instance); err != nil {
		t.Errorf("reconcileClusterRole() error = %v, want nil when rules already match", err)
	}
}

// --- reconcileClusterRoleBinding ---

func TestReconcileClusterRoleBindingGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("get ClusterRoleBinding boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*rbacv1.ClusterRoleBinding); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileClusterRoleBinding(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileClusterRoleBinding() error = %v, want %v", err, wantErr)
	}
}

// --- deleteClusterMetricsRBAC ---

func TestDeleteClusterMetricsRBACNoOpWhenAbsent(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.deleteClusterMetricsRBAC(t.Context(), instance); err != nil {
		t.Errorf("deleteClusterMetricsRBAC() error = %v, want nil when nothing exists", err)
	}
}

func TestDeleteClusterMetricsRBACGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("get ClusterRoleBinding boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*rbacv1.ClusterRoleBinding); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.deleteClusterMetricsRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("deleteClusterMetricsRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestDeleteClusterMetricsRBACDeleteCRBError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("delete ClusterRoleBinding boom")
	name := clusterRBACName(instance)

	crb := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name}}
	cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name}}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, crb, cr).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.DeleteOption) error {
			if _, ok := o.(*rbacv1.ClusterRoleBinding); ok {
				return wantErr
			}
			return c.Delete(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.deleteClusterMetricsRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("deleteClusterMetricsRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestDeleteClusterMetricsRBACDeleteCRError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestInstance()
	wantErr := errors.New("delete ClusterRole boom")
	name := clusterRBACName(instance)

	crb := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name}}
	cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name}}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, crb, cr).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.DeleteOption) error {
			if _, ok := o.(*rbacv1.ClusterRole); ok {
				return wantErr
			}
			return c.Delete(ctx, o, opts...)
		},
	})
	r := &InstanceReconciler{Client: cl, Scheme: scheme}

	if err := r.deleteClusterMetricsRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("deleteClusterMetricsRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

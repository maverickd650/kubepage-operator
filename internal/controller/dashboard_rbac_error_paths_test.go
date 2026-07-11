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

func newRBACTestDashboard() *pagev1alpha1.Dashboard {
	return &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: rbacTestNamespace},
		Spec:       pagev1alpha1.DashboardSpec{ContainerPort: 8080},
	}
}

func newKubeMetricsInfoWidget(instance *pagev1alpha1.Dashboard) *pagev1alpha1.InfoWidget {
	return &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testInfoWidgetNameMetrics, Namespace: instance.Namespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: instance.Name},
			Widgets: []pagev1alpha1.InfoWidgetEntry{
				{Type: kubeMetricsWidgetType},
			},
		},
	}
}

// --- reconcileDashboardRBAC error wrapping ---

func TestReconcileDashboardRBACServiceAccountError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	err := r.reconcileDashboardRBAC(t.Context(), instance)
	if !errors.Is(err, wantErr) {
		t.Errorf("reconcileDashboardRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileDashboardRBACRoleError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	err := r.reconcileDashboardRBAC(t.Context(), instance)
	if !errors.Is(err, wantErr) {
		t.Errorf("reconcileDashboardRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileDashboardRBACRoleBindingError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	err := r.reconcileDashboardRBAC(t.Context(), instance)
	if !errors.Is(err, wantErr) {
		t.Errorf("reconcileDashboardRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReferencedSecretNamesIgnoresOtherDashboardsAndCollectsKeyRefs(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()

	wantSecret := "creds"
	matchingEntry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testServiceCardObjName, Namespace: instance.Namespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: instance.Name},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: "N",
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type: testWidgetTypeGrafana,
					Secrets: map[string]pagev1alpha1.SecretValueSource{
						secretField: {SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: wantSecret},
							Key:                  secretField,
						}},
					},
				}},
			}},
		},
	}
	otherDashboardEntry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-other", Namespace: instance.Namespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testOtherDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: "N",
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type: testWidgetTypeGrafana,
					Secrets: map[string]pagev1alpha1.SecretValueSource{
						secretField: {SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "should-not-appear"},
							Key:                  secretField,
						}},
					},
				}},
			}},
		},
	}
	otherDashboardWidget := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "iw-other", Namespace: instance.Namespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testOtherDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{
				{Type: testWidgetTypeOpenMeteo},
			},
		},
	}
	wantWidgetSecret := "widget-creds"
	matchingWidget := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "iw", Namespace: instance.Namespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: instance.Name},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: testWidgetTypeOpenMeteo,
				Secrets: map[string]pagev1alpha1.SecretValueSource{
					secretField: {SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: wantWidgetSecret},
						Key:                  secretField,
					}},
				},
			}},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(instance, matchingEntry, otherDashboardEntry, otherDashboardWidget, matchingWidget).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

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

// TestReferencedSecretNamesCollectsCACertKeyRefs verifies referencedSecretNames
// collects a Secret named via caCert.secretKeyRef, not just via a widget's
// secrets map — from a ServiceCard widget, an InfoWidget entry, and
// Spec.WidgetDefaults, the three places CACert can be set.
func TestReferencedSecretNamesCollectsCACertKeyRefs(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
	instance.Spec.WidgetDefaults = map[string]pagev1alpha1.WidgetDefaultsEntry{
		testWidgetTypeGrafana: {
			CACert: &pagev1alpha1.SecretValueSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "defaults-ca"},
				Key:                  testCACertKey,
			}},
		},
	}

	serviceCard := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testServiceCardObjName, Namespace: instance.Namespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: instance.Name},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: "N",
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type: testWidgetTypeGrafana,
					CACert: &pagev1alpha1.SecretValueSource{SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "servicecard-ca"},
						Key:                  testCACertKey,
					}},
				}},
			}},
		},
	}
	infoWidget := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "iw", Namespace: instance.Namespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: instance.Name},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: testWidgetTypeOpenMeteo,
				CACert: &pagev1alpha1.SecretValueSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "infowidget-ca"},
					Key:                  testCACertKey,
				}},
			}},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, serviceCard, infoWidget).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() unexpected error: %v", err)
	}
	want := []string{"defaults-ca", "infowidget-ca", "servicecard-ca"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("referencedSecretNames() = %v, want %v (caCert.secretKeyRef from all three sources)", got, want)
	}
}

// --- reconcileServiceAccount ---

func TestReconcileServiceAccountSetControllerReferenceError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: schemeWithoutDashboard(t)}

	if err := r.reconcileServiceAccount(t.Context(), instance); err == nil {
		t.Error("reconcileServiceAccount() error = nil, want the SetControllerReference error")
	}
}

func TestReconcileServiceAccountGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileServiceAccount(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileServiceAccount() error = %v, want %v", err, wantErr)
	}
}

// --- reconcileRole ---

func TestReconcileRoleSecretNamesServiceCardListError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
	wantErr := errors.New("list ServiceCards boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*pagev1alpha1.ServiceCardList); ok {
				return wantErr
			}
			return c.List(ctx, list, opts...)
		},
	})
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileRole() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileRoleSecretNamesInfoWidgetListError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileRole() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileRoleSetControllerReferenceError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: schemeWithoutDashboard(t)}

	if err := r.reconcileRole(t.Context(), instance); err == nil {
		t.Error("reconcileRole() error = nil, want the SetControllerReference error")
	}
}

func TestReconcileRoleGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileRole() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileRoleUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileRole() error = %v, want %v", err, wantErr)
	}
}

// --- reconcileRoleBinding ---

func TestReconcileRoleBindingSetControllerReferenceError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: schemeWithoutDashboard(t)}

	if err := r.reconcileRoleBinding(t.Context(), instance); err == nil {
		t.Error("reconcileRoleBinding() error = nil, want the SetControllerReference error")
	}
}

func TestReconcileRoleBindingGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileRoleBinding(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileRoleBinding() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileRoleBindingCreateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileRoleBinding(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileRoleBinding() error = %v, want %v", err, wantErr)
	}
}

// --- reconcileClusterMetricsRBAC / instanceHasKubeMetricsWidget ---

func TestReconcileClusterMetricsRBACListError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileClusterMetricsRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileClusterMetricsRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconcileClusterMetricsRBACClusterRoleError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	err := r.reconcileClusterMetricsRBAC(t.Context(), instance)
	if !errors.Is(err, wantErr) {
		t.Errorf("reconcileClusterMetricsRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

// TestInstanceHasKubeMetricsWidgetIgnoresOtherDashboards verifies a
// kubemetrics InfoWidget bound to a *different* Dashboard in the same
// namespace doesn't cause reconcileClusterMetricsRBAC to grant this
// instance the cluster-scoped nodes/metrics.k8s.io RBAC.
func TestInstanceHasKubeMetricsWidgetIgnoresOtherDashboards(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
	otherWidget := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testInfoWidgetNameMetrics, Namespace: instance.Namespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testOtherDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{
				{Type: kubeMetricsWidgetType},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, otherWidget).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	got, err := r.instanceHasKubeMetricsWidget(t.Context(), instance)
	if err != nil {
		t.Fatalf("instanceHasKubeMetricsWidget() error = %v", err)
	}
	if got {
		t.Error("instanceHasKubeMetricsWidget() = true, want false: the kubemetrics widget is bound to a different Dashboard")
	}
}

func TestReconcileClusterMetricsRBACClusterRoleBindingError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	err := r.reconcileClusterMetricsRBAC(t.Context(), instance)
	if !errors.Is(err, wantErr) {
		t.Errorf("reconcileClusterMetricsRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

// --- reconcileClusterRole ---

func TestReconcileClusterRoleGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileClusterRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileClusterRole() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileClusterRoleUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileClusterRole(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileClusterRole() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileClusterRoleNoDrift(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()

	matching := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRBACName(instance)},
		Rules:      clusterMetricsRules,
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, matching).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileClusterRole(t.Context(), instance); err != nil {
		t.Errorf("reconcileClusterRole() error = %v, want nil when rules already match", err)
	}
}

// --- reconcileClusterRoleBinding ---

func TestReconcileClusterRoleBindingGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.reconcileClusterRoleBinding(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("reconcileClusterRoleBinding() error = %v, want %v", err, wantErr)
	}
}

// --- deleteClusterMetricsRBAC ---

func TestDeleteClusterMetricsRBACNoOpWhenAbsent(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.deleteClusterMetricsRBAC(t.Context(), instance); err != nil {
		t.Errorf("deleteClusterMetricsRBAC() error = %v, want nil when nothing exists", err)
	}
}

func TestDeleteClusterMetricsRBACGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.deleteClusterMetricsRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("deleteClusterMetricsRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestDeleteClusterMetricsRBACDeleteCRBError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.deleteClusterMetricsRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("deleteClusterMetricsRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestDeleteClusterMetricsRBACDeleteCRError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newRBACTestDashboard()
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
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	if err := r.deleteClusterMetricsRBAC(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("deleteClusterMetricsRBAC() error = %v, want wrapping %v", err, wantErr)
	}
}

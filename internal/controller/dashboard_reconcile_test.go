package controller

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// DashboardReconciler.Reconcile's happy paths (create, drift correction,
// finalizer add/remove on a real deletion) are already covered by the
// envtest-backed Ginkgo specs in instance_controller_test.go. The error
// branches below need a client that can be made to fail Get/Update/List on
// demand, which a real apiserver can't - hence the fake client +
// interceptor.Funcs pattern used throughout this package.

const instanceReconcileTestNamespace = "reconcile-ns"

// newDashboardReconcileTestDashboard returns a Dashboard that already carries
// the Available condition set by Reconcile's first block, so a test can
// isolate a later branch without also exercising the initial
// condition-bootstrap Get/Update pair.
func newDashboardReconcileTestDashboard() *pagev1alpha1.Dashboard {
	return &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testDashboardObjName,
			Namespace:  instanceReconcileTestNamespace,
			Finalizers: []string{instanceFinalizer},
		},
		// Size must be non-nil: reconcileDeployment's drift check treats a nil
		// desired replica count as always-drifted, which would make
		// reconcileDeployment report handled=true (a requeue) before Reconcile
		// ever reaches the Service/Ingress/HTTPRoute/status logic under test
		// further down this file.
		Spec: pagev1alpha1.DashboardSpec{ContainerPort: 8080, Replicas: new(int32)},
		Status: pagev1alpha1.DashboardStatus{
			Conditions: []metav1.Condition{{
				Type: typeAvailableDashboard, Status: metav1.ConditionTrue,
				Reason: reasonReconciling, Message: "seed",
			}},
		},
	}
}

func newDashboardReconciler(c client.Client) *DashboardReconciler {
	return &DashboardReconciler{
		Client:         c,
		Scheme:         c.Scheme(),
		DashboardImage: testDashboardImage,
		Recorder:       events.NewFakeRecorder(10),
	}
}

func reconcileDashboard(t *testing.T, r *DashboardReconciler, name string) (ctrl.Result, error) {
	t.Helper()
	return r.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: instanceReconcileTestNamespace},
	})
}

// seedMatchingDeployment creates the exact Deployment reconcileDeployment
// would build for instance, so Reconcile's own call to reconcileDeployment
// reports no drift (handled=false) and execution falls through to the
// Service/Ingress/HTTPRoute/status-update logic that follows it - the
// branches under test further down this file.
func seedMatchingDeployment(t *testing.T, r *DashboardReconciler, instance *pagev1alpha1.Dashboard) {
	t.Helper()
	dep, err := r.deploymentForDashboard(instance)
	if err != nil {
		t.Fatalf("deploymentForDashboard() unexpected error: %v", err)
	}
	if err := r.Create(t.Context(), dep); err != nil {
		t.Fatalf("seeding matching Deployment: %v", err)
	}
}

func TestDashboardReconcileNotFoundIsIgnored(t *testing.T) {
	scheme := networkTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDoesNotExistDashboardName); err != nil {
		t.Errorf("Reconcile() error = %v, want nil for a NotFound Get", err)
	}
}

func TestDashboardReconcileGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	wantErr := errors.New("get boom")

	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*pagev1alpha1.Dashboard); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// TestDashboardReconcileInitialStatusUpdateError covers the branch where a
// freshly-created Dashboard (no conditions yet) fails the very first
// Status().Update that bootstraps the Available=Unknown condition.
func TestDashboardReconcileInitialStatusUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: instanceReconcileTestNamespace},
		Spec:       pagev1alpha1.DashboardSpec{ContainerPort: 8080},
	}
	wantErr := errors.New("status update boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, o client.Object, opts ...client.SubResourceUpdateOption) error {
			return wantErr
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// TestDashboardReconcileInitialStatusRefetchError covers the re-fetch Get
// that immediately follows the initial condition bootstrap Update.
func TestDashboardReconcileInitialStatusRefetchError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: instanceReconcileTestNamespace},
		Spec:       pagev1alpha1.DashboardSpec{ContainerPort: 8080},
	}
	wantErr := errors.New("refetch boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	getCalls := 0
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*pagev1alpha1.Dashboard); ok {
				getCalls++
				if getCalls == 2 {
					return wantErr
				}
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// TestDashboardReconcileAddFinalizerUpdateError covers the Update that
// persists the finalizer being added to a brand-new Dashboard.
func TestDashboardReconcileAddFinalizerUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: instanceReconcileTestNamespace},
		Spec:       pagev1alpha1.DashboardSpec{ContainerPort: 8080},
		Status: pagev1alpha1.DashboardStatus{
			Conditions: []metav1.Condition{{Type: typeAvailableDashboard, Status: metav1.ConditionUnknown, Reason: reasonReconciling}},
		},
	}
	wantErr := errors.New("update boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*pagev1alpha1.Dashboard); ok {
				return wantErr
			}
			return c.Update(ctx, o, opts...)
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestDashboardReconcileRBACError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	wantErr := errors.New("create SA boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*corev1.ServiceAccount); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}

	updated := &pagev1alpha1.Dashboard{}
	if err := base.Get(t.Context(), client.ObjectKeyFromObject(instance), updated); err != nil {
		t.Fatalf("getting Dashboard: %v", err)
	}
	if cond := findCondition(updated, typeAvailableDashboard); cond == nil || cond.Status != metav1.ConditionFalse {
		t.Errorf("Available condition = %+v, want False", cond)
	}
}

func TestDashboardReconcileClusterMetricsRBACError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	widget := newKubeMetricsInfoWidget(instance)
	wantErr := errors.New("create ClusterRole boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, widget).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*rbacv1.ClusterRole); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := newDashboardReconciler(cl)
	t.Cleanup(func() { _ = r.deleteClusterMetricsRBAC(t.Context(), instance) })

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestDashboardReconcileServiceError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	wantErr := errors.New("create Service boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	seedMatchingDeployment(t, newDashboardReconciler(base), instance)
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*corev1.Service); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestDashboardReconcileIngressError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	instance.Spec.Ingress = &pagev1alpha1.IngressSpec{Enabled: pagev1alpha1.Enabled, Host: testDashboardHost}
	wantErr := errors.New("create Ingress boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	seedMatchingDeployment(t, newDashboardReconciler(base), instance)
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*networkingv1.Ingress); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// TestDashboardReconcileHTTPRouteGatewayNotInstalled covers the
// errGatewayAPINotInstalled branch surfacing as a failAvailable call, without
// needing the Gateway API CRDs registered in the client scheme.
func TestDashboardReconcileHTTPRouteGatewayNotInstalled(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	instance.Spec.Gateway = &pagev1alpha1.GatewaySpec{
		Enabled:   pagev1alpha1.Enabled,
		Hostnames: []string{testDashboardHost},
		ParentRef: pagev1alpha1.GatewayParentRef{Name: "eg"},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	r := newDashboardReconciler(cl)
	r.GatewayAPIEnabled = false
	seedMatchingDeployment(t, r, instance)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, errGatewayAPINotInstalled) {
		t.Errorf("Reconcile() error = %v, want %v", err, errGatewayAPINotInstalled)
	}
}

func TestDashboardReconcileBoundCountsError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	wantErr := errors.New("list DashboardStyles boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	seedMatchingDeployment(t, newDashboardReconciler(base), instance)
	cl := interceptor.NewClient(base, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*pagev1alpha1.DashboardStyleList); ok {
				return wantErr
			}
			return c.List(ctx, list, opts...)
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// TestDashboardReconcileDeploymentNotReady covers deploymentReady reporting
// fewer ready replicas than desired (e.g. a crash-looping or unpullable
// dashboard image): Available must go False with reasonDeploymentNotReady
// rather than True just because the Deployment object itself matches spec,
// and Reconcile must requeue rather than erroring, since this isn't a
// reconcile failure - it's a pending state that a future Deployment status
// update (or the fallback requeue) will re-check.
func TestDashboardReconcileDeploymentNotReady(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	instance.Spec.Replicas = new(int32(1))

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	r := newDashboardReconciler(cl)
	seedMatchingDeployment(t, r, instance)
	// seedMatchingDeployment's Deployment has no status set, so ReadyReplicas
	// defaults to 0 - fewer than the 1 replica instance.Spec.Replicas requests.

	result, err := reconcileDashboard(t, r, testDashboardObjName)
	if err != nil {
		t.Fatalf("Reconcile() unexpected error: %v", err)
	}
	if result.RequeueAfter != deploymentNotReadyRequeueInterval {
		t.Errorf("Reconcile() RequeueAfter = %v, want %v", result.RequeueAfter, deploymentNotReadyRequeueInterval)
	}

	updated := &pagev1alpha1.Dashboard{}
	if err := cl.Get(t.Context(), types.NamespacedName{Name: testDashboardObjName, Namespace: instanceReconcileTestNamespace}, updated); err != nil {
		t.Fatalf("getting Dashboard: %v", err)
	}
	cond := meta.FindStatusCondition(updated.Status.Conditions, typeAvailableDashboard)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != reasonDeploymentNotReady {
		t.Errorf("Dashboard Available condition = %+v, want False/%s", cond, reasonDeploymentNotReady)
	}
}

// TestDashboardReconcileDeploymentReadyGetError covers deploymentReady's own
// Get failing after every earlier reconcile step (RBAC, Deployment, Service,
// Ingress, HTTPRoute, bound counts) already succeeded.
func TestDashboardReconcileDeploymentReadyGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	instance.Spec.Replicas = new(int32(1))
	wantErr := errors.New("get deployment boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	seedMatchingDeployment(t, newDashboardReconciler(base), instance)

	var deploymentGets int
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*appsv1.Deployment); ok {
				deploymentGets++
				// Let reconcileDeployment's own Get (the 1st) through so it
				// reports no drift and falls through to the rest of
				// Reconcile; only fail deploymentReady's Get (the 2nd).
				if deploymentGets == 2 {
					return wantErr
				}
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestDashboardReconcileFinalStatusUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	wantErr := errors.New("final status update boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	seedMatchingDeployment(t, newDashboardReconciler(base), instance)
	cl := interceptor.NewClient(base, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, o client.Object, opts ...client.SubResourceUpdateOption) error {
			return wantErr
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// TestDashboardReconcileFailAvailableStatusUpdateError covers failAvailable's
// own Status().Update failing, on top of the resource error that triggered
// it - both errors should be observable, but Reconcile must return the
// Status().Update failure since that's the one the caller can act on.
func TestDashboardReconcileFailAvailableStatusUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	wantStatusErr := errors.New("status update boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*corev1.ServiceAccount); ok {
				return errors.New("create SA boom")
			}
			return c.Create(ctx, o, opts...)
		},
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, o client.Object, opts ...client.SubResourceUpdateOption) error {
			return wantStatusErr
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantStatusErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantStatusErr)
	}
}

// --- deletion path ---

func TestDashboardReconcileFinalizerDegradedStatusUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	now := metav1.Now()
	instance.DeletionTimestamp = &now
	wantErr := errors.New("status update boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, o client.Object, opts ...client.SubResourceUpdateOption) error {
			return wantErr
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestDashboardReconcileFinalizerOperationsError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	now := metav1.Now()
	instance.DeletionTimestamp = &now
	name := clusterRBACName(instance)
	wantErr := errors.New("get ClusterRoleBinding boom")

	crb := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name}}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, crb).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*rbacv1.ClusterRoleBinding); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestDashboardReconcileFinalizerRefetchError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	now := metav1.Now()
	instance.DeletionTimestamp = &now
	wantErr := errors.New("refetch boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	getCalls := 0
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*pagev1alpha1.Dashboard); ok {
				getCalls++
				if getCalls == 2 {
					return wantErr
				}
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestDashboardReconcileFinalizerFinalStatusUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	now := metav1.Now()
	instance.DeletionTimestamp = &now
	wantErr := errors.New("final degraded status update boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	updateCalls := 0
	cl := interceptor.NewClient(base, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, o client.Object, opts ...client.SubResourceUpdateOption) error {
			updateCalls++
			if updateCalls == 2 {
				return wantErr
			}
			return c.SubResource(subResourceName).Update(ctx, o, opts...)
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestDashboardReconcileRemoveFinalizerUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()
	now := metav1.Now()
	instance.DeletionTimestamp = &now
	wantErr := errors.New("remove finalizer update boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*pagev1alpha1.Dashboard); ok {
				return wantErr
			}
			return c.Update(ctx, o, opts...)
		},
	})
	r := newDashboardReconciler(cl)

	if _, err := reconcileDashboard(t, r, testDashboardObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// --- boundCountsForDashboard List errors ---

func TestBoundCountsForDashboardListErrors(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()

	tests := []struct {
		name   string
		fails  func(list client.ObjectList) bool
		errMsg string
	}{
		{"ServiceCards", func(l client.ObjectList) bool { _, ok := l.(*pagev1alpha1.ServiceCardList); return ok }, "list ServiceCards boom"},
		{"Bookmarks", func(l client.ObjectList) bool { _, ok := l.(*pagev1alpha1.BookmarkList); return ok }, "list Bookmarks boom"},
		{"InfoWidgets", func(l client.ObjectList) bool { _, ok := l.(*pagev1alpha1.InfoWidgetList); return ok }, "list InfoWidgets boom"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantErr := errors.New(tc.errMsg)
			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
			cl := interceptor.NewClient(base, interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if tc.fails(list) {
						return wantErr
					}
					return c.List(ctx, list, opts...)
				},
			})
			r := newDashboardReconciler(cl)

			if _, err := r.boundCountsForDashboard(t.Context(), instance); !errors.Is(err, wantErr) {
				t.Errorf("boundCountsForDashboard() error = %v, want %v", err, wantErr)
			}
		})
	}
}

// TestBoundCountsForDashboardCounts covers the DashboardStyle and InfoWidget
// increment branches specifically: the envtest-backed Ginkgo specs exercise
// ServiceCard/Bookmark counting, but leave configs/infoWidgets matching
// instance.Name untested.
func TestBoundCountsForDashboardCounts(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newDashboardReconcileTestDashboard()

	matchingCfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: instance.Namespace},
		Spec:       pagev1alpha1.DashboardStyleSpec{DashboardRef: pagev1alpha1.DashboardRef{Name: instance.Name}},
	}
	otherCfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg-other", Namespace: instance.Namespace},
		Spec:       pagev1alpha1.DashboardStyleSpec{DashboardRef: pagev1alpha1.DashboardRef{Name: testOtherDashboardName}},
	}
	matchingWidget := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "iw", Namespace: instance.Namespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: instance.Name},
			Widgets: []pagev1alpha1.InfoWidgetEntry{
				{Type: testWidgetTypeDatetime},
			},
		},
	}
	otherWidget := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "iw-other", Namespace: instance.Namespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testOtherDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{
				{Type: testWidgetTypeDatetime},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(instance, matchingCfg, otherCfg, matchingWidget, otherWidget).Build()
	r := newDashboardReconciler(cl)

	counts, err := r.boundCountsForDashboard(t.Context(), instance)
	if err != nil {
		t.Fatalf("boundCountsForDashboard() unexpected error: %v", err)
	}
	if counts.configurations != 1 {
		t.Errorf("counts.configurations = %d, want 1 (other instance's DashboardStyle excluded)", counts.configurations)
	}
	if counts.infoWidgets != 1 {
		t.Errorf("counts.infoWidgets = %d, want 1 (other instance's InfoWidget excluded)", counts.infoWidgets)
	}
}

func findCondition(instance *pagev1alpha1.Dashboard, condType string) *metav1.Condition {
	for i := range instance.Status.Conditions {
		if instance.Status.Conditions[i].Type == condType {
			return &instance.Status.Conditions[i]
		}
	}
	return nil
}

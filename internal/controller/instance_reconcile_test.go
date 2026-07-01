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
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// InstanceReconciler.Reconcile's happy paths (create, drift correction,
// finalizer add/remove on a real deletion) are already covered by the
// envtest-backed Ginkgo specs in instance_controller_test.go. The error
// branches below need a client that can be made to fail Get/Update/List on
// demand, which a real apiserver can't - hence the fake client +
// interceptor.Funcs pattern used throughout this package.

const instanceReconcileTestNamespace = "reconcile-ns"

// newInstanceReconcileTestInstance returns an Instance that already carries
// the Available condition set by Reconcile's first block, so a test can
// isolate a later branch without also exercising the initial
// condition-bootstrap Get/Update pair.
func newInstanceReconcileTestInstance() *pagev1alpha1.Instance {
	return &pagev1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testInstanceObjName,
			Namespace:  instanceReconcileTestNamespace,
			Finalizers: []string{instanceFinalizer},
		},
		// Size must be non-nil: reconcileDeployment's drift check treats a nil
		// desired replica count as always-drifted, which would make
		// reconcileDeployment report handled=true (a requeue) before Reconcile
		// ever reaches the Service/Ingress/HTTPRoute/status logic under test
		// further down this file.
		Spec: pagev1alpha1.InstanceSpec{ContainerPort: 8080, Size: new(int32)},
		Status: pagev1alpha1.InstanceStatus{
			Conditions: []metav1.Condition{{
				Type: typeAvailableInstance, Status: metav1.ConditionTrue,
				Reason: reasonReconciling, Message: "seed",
			}},
		},
	}
}

func newInstanceReconciler(c client.Client) *InstanceReconciler {
	return &InstanceReconciler{
		Client:         c,
		Scheme:         c.Scheme(),
		DashboardImage: testDashboardImage,
		Recorder:       events.NewFakeRecorder(10),
	}
}

func reconcileInstance(t *testing.T, r *InstanceReconciler, name string) (ctrl.Result, error) {
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
func seedMatchingDeployment(t *testing.T, r *InstanceReconciler, instance *pagev1alpha1.Instance) {
	t.Helper()
	dep, err := r.deploymentForInstance(instance)
	if err != nil {
		t.Fatalf("deploymentForInstance() unexpected error: %v", err)
	}
	if err := r.Create(t.Context(), dep); err != nil {
		t.Fatalf("seeding matching Deployment: %v", err)
	}
}

func TestInstanceReconcileNotFoundIsIgnored(t *testing.T) {
	scheme := networkTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testDoesNotExistInstanceName); err != nil {
		t.Errorf("Reconcile() error = %v, want nil for a NotFound Get", err)
	}
}

func TestInstanceReconcileGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	wantErr := errors.New("get boom")

	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*pagev1alpha1.Instance); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// TestInstanceReconcileInitialStatusUpdateError covers the branch where a
// freshly-created Instance (no conditions yet) fails the very first
// Status().Update that bootstraps the Available=Unknown condition.
func TestInstanceReconcileInitialStatusUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := &pagev1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceObjName, Namespace: instanceReconcileTestNamespace},
		Spec:       pagev1alpha1.InstanceSpec{ContainerPort: 8080},
	}
	wantErr := errors.New("status update boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, o client.Object, opts ...client.SubResourceUpdateOption) error {
			return wantErr
		},
	})
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// TestInstanceReconcileInitialStatusRefetchError covers the re-fetch Get
// that immediately follows the initial condition bootstrap Update.
func TestInstanceReconcileInitialStatusRefetchError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := &pagev1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceObjName, Namespace: instanceReconcileTestNamespace},
		Spec:       pagev1alpha1.InstanceSpec{ContainerPort: 8080},
	}
	wantErr := errors.New("refetch boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	getCalls := 0
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*pagev1alpha1.Instance); ok {
				getCalls++
				if getCalls == 2 {
					return wantErr
				}
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// TestInstanceReconcileAddFinalizerUpdateError covers the Update that
// persists the finalizer being added to a brand-new Instance.
func TestInstanceReconcileAddFinalizerUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := &pagev1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceObjName, Namespace: instanceReconcileTestNamespace},
		Spec:       pagev1alpha1.InstanceSpec{ContainerPort: 8080},
		Status: pagev1alpha1.InstanceStatus{
			Conditions: []metav1.Condition{{Type: typeAvailableInstance, Status: metav1.ConditionUnknown, Reason: reasonReconciling}},
		},
	}
	wantErr := errors.New("update boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*pagev1alpha1.Instance); ok {
				return wantErr
			}
			return c.Update(ctx, o, opts...)
		},
	})
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestInstanceReconcileRBACError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
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
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}

	updated := &pagev1alpha1.Instance{}
	if err := base.Get(t.Context(), client.ObjectKeyFromObject(instance), updated); err != nil {
		t.Fatalf("getting Instance: %v", err)
	}
	if cond := findCondition(updated, typeAvailableInstance); cond == nil || cond.Status != metav1.ConditionFalse {
		t.Errorf("Available condition = %+v, want False", cond)
	}
}

func TestInstanceReconcileClusterMetricsRBACError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
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
	r := newInstanceReconciler(cl)
	t.Cleanup(func() { _ = r.deleteClusterMetricsRBAC(t.Context(), instance) })

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestInstanceReconcileServiceError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
	wantErr := errors.New("create Service boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	seedMatchingDeployment(t, newInstanceReconciler(base), instance)
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*corev1.Service); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestInstanceReconcileIngressError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
	instance.Spec.Ingress = &pagev1alpha1.IngressSpec{Enabled: true, Host: testDashboardHost}
	wantErr := errors.New("create Ingress boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	seedMatchingDeployment(t, newInstanceReconciler(base), instance)
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*networkingv1.Ingress); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// TestInstanceReconcileHTTPRouteGatewayNotInstalled covers the
// errGatewayAPINotInstalled branch surfacing as a failAvailable call, without
// needing the Gateway API CRDs registered in the client scheme.
func TestInstanceReconcileHTTPRouteGatewayNotInstalled(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
	instance.Spec.Gateway = &pagev1alpha1.GatewaySpec{
		Enabled:   true,
		Hostnames: []string{testDashboardHost},
		ParentRef: pagev1alpha1.GatewayParentRef{Name: "eg"},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	r := newInstanceReconciler(cl)
	r.GatewayAPIEnabled = false
	seedMatchingDeployment(t, r, instance)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, errGatewayAPINotInstalled) {
		t.Errorf("Reconcile() error = %v, want %v", err, errGatewayAPINotInstalled)
	}
}

func TestInstanceReconcileBoundCountsError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
	wantErr := errors.New("list Configurations boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	seedMatchingDeployment(t, newInstanceReconciler(base), instance)
	cl := interceptor.NewClient(base, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*pagev1alpha1.ConfigurationList); ok {
				return wantErr
			}
			return c.List(ctx, list, opts...)
		},
	})
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// TestInstanceReconcileDeploymentNotReady covers deploymentReady reporting
// fewer ready replicas than desired (e.g. a crash-looping or unpullable
// dashboard image): Available must go False with reasonDeploymentNotReady
// rather than True just because the Deployment object itself matches spec,
// and Reconcile must requeue rather than erroring, since this isn't a
// reconcile failure - it's a pending state that a future Deployment status
// update (or the fallback requeue) will re-check.
func TestInstanceReconcileDeploymentNotReady(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
	instance.Spec.Size = ptr.To(int32(1))

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	r := newInstanceReconciler(cl)
	seedMatchingDeployment(t, r, instance)
	// seedMatchingDeployment's Deployment has no status set, so ReadyReplicas
	// defaults to 0 - fewer than the 1 replica instance.Spec.Size requests.

	result, err := reconcileInstance(t, r, testInstanceObjName)
	if err != nil {
		t.Fatalf("Reconcile() unexpected error: %v", err)
	}
	if result.RequeueAfter != deploymentNotReadyRequeueInterval {
		t.Errorf("Reconcile() RequeueAfter = %v, want %v", result.RequeueAfter, deploymentNotReadyRequeueInterval)
	}

	updated := &pagev1alpha1.Instance{}
	if err := cl.Get(t.Context(), types.NamespacedName{Name: testInstanceObjName, Namespace: instanceReconcileTestNamespace}, updated); err != nil {
		t.Fatalf("getting Instance: %v", err)
	}
	cond := meta.FindStatusCondition(updated.Status.Conditions, typeAvailableInstance)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != reasonDeploymentNotReady {
		t.Errorf("Instance Available condition = %+v, want False/%s", cond, reasonDeploymentNotReady)
	}
}

// TestInstanceReconcileDeploymentReadyGetError covers deploymentReady's own
// Get failing after every earlier reconcile step (RBAC, Deployment, Service,
// Ingress, HTTPRoute, bound counts) already succeeded.
func TestInstanceReconcileDeploymentReadyGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
	instance.Spec.Size = ptr.To(int32(1))
	wantErr := errors.New("get deployment boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	seedMatchingDeployment(t, newInstanceReconciler(base), instance)

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
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestInstanceReconcileFinalStatusUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
	wantErr := errors.New("final status update boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	seedMatchingDeployment(t, newInstanceReconciler(base), instance)
	cl := interceptor.NewClient(base, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, o client.Object, opts ...client.SubResourceUpdateOption) error {
			return wantErr
		},
	})
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// TestInstanceReconcileFailAvailableStatusUpdateError covers failAvailable's
// own Status().Update failing, on top of the resource error that triggered
// it - both errors should be observable, but Reconcile must return the
// Status().Update failure since that's the one the caller can act on.
func TestInstanceReconcileFailAvailableStatusUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
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
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantStatusErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantStatusErr)
	}
}

// --- deletion path ---

func TestInstanceReconcileFinalizerDegradedStatusUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
	now := metav1.Now()
	instance.DeletionTimestamp = &now
	wantErr := errors.New("status update boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, o client.Object, opts ...client.SubResourceUpdateOption) error {
			return wantErr
		},
	})
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestInstanceReconcileFinalizerOperationsError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
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
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestInstanceReconcileFinalizerRefetchError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
	now := metav1.Now()
	instance.DeletionTimestamp = &now
	wantErr := errors.New("refetch boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	getCalls := 0
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*pagev1alpha1.Instance); ok {
				getCalls++
				if getCalls == 2 {
					return wantErr
				}
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestInstanceReconcileFinalizerFinalStatusUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
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
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

func TestInstanceReconcileRemoveFinalizerUpdateError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()
	now := metav1.Now()
	instance.DeletionTimestamp = &now
	wantErr := errors.New("remove finalizer update boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*pagev1alpha1.Instance); ok {
				return wantErr
			}
			return c.Update(ctx, o, opts...)
		},
	})
	r := newInstanceReconciler(cl)

	if _, err := reconcileInstance(t, r, testInstanceObjName); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
	}
}

// --- boundCountsForInstance List errors ---

func TestBoundCountsForInstanceListErrors(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()

	tests := []struct {
		name   string
		fails  func(list client.ObjectList) bool
		errMsg string
	}{
		{"ServiceEntries", func(l client.ObjectList) bool { _, ok := l.(*pagev1alpha1.ServiceEntryList); return ok }, "list ServiceEntries boom"},
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
			r := newInstanceReconciler(cl)

			if _, err := r.boundCountsForInstance(t.Context(), instance); !errors.Is(err, wantErr) {
				t.Errorf("boundCountsForInstance() error = %v, want %v", err, wantErr)
			}
		})
	}
}

// TestBoundCountsForInstanceCounts covers the Configuration and InfoWidget
// increment branches specifically: the envtest-backed Ginkgo specs exercise
// ServiceEntry/Bookmark counting, but leave configs/infoWidgets matching
// instance.Name untested.
func TestBoundCountsForInstanceCounts(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newInstanceReconcileTestInstance()

	matchingCfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: instance.Namespace},
		Spec:       pagev1alpha1.ConfigurationSpec{InstanceRef: pagev1alpha1.InstanceRef{Name: instance.Name}},
	}
	otherCfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg-other", Namespace: instance.Namespace},
		Spec:       pagev1alpha1.ConfigurationSpec{InstanceRef: pagev1alpha1.InstanceRef{Name: testOtherInstanceName}},
	}
	matchingWidget := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "iw", Namespace: instance.Namespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: instance.Name},
			Type:        testWidgetTypeDatetime,
		},
	}
	otherWidget := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "iw-other", Namespace: instance.Namespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testOtherInstanceName},
			Type:        testWidgetTypeDatetime,
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(instance, matchingCfg, otherCfg, matchingWidget, otherWidget).Build()
	r := newInstanceReconciler(cl)

	counts, err := r.boundCountsForInstance(t.Context(), instance)
	if err != nil {
		t.Fatalf("boundCountsForInstance() unexpected error: %v", err)
	}
	if counts.configurations != 1 {
		t.Errorf("counts.configurations = %d, want 1 (other instance's Configuration excluded)", counts.configurations)
	}
	if counts.infoWidgets != 1 {
		t.Errorf("counts.infoWidgets = %d, want 1 (other instance's InfoWidget excluded)", counts.infoWidgets)
	}
}

func findCondition(instance *pagev1alpha1.Instance, condType string) *metav1.Condition {
	for i := range instance.Status.Conditions {
		if instance.Status.Conditions[i].Type == condType {
			return &instance.Status.Conditions[i]
		}
	}
	return nil
}

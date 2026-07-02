package controller

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// reconcileDeployment's happy paths (create, no drift, drift corrected) are
// already covered by the envtest-backed Ginkgo specs in
// instance_controller_test.go. The error branches below need a client that
// can be made to fail Get/Create/Update/Status-Update on demand, which a
// real apiserver can't — hence the fake client + interceptor.Funcs pattern
// used here, matching instance_network_test.go's plain-testing.T style.

const deploymentTestNamespace = "dep-ns"

func newDeploymentTestDashboard() *pagev1alpha1.Dashboard {
	return &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: deploymentTestNamespace},
		Spec:       pagev1alpha1.DashboardSpec{ContainerPort: 8080},
	}
}

// schemeWithoutDashboard registers everything deploymentForDashboard and
// reconcileDeployment need except pagev1alpha1, so
// ctrl.SetControllerReference(instance, dep, scheme) fails to look up the
// Dashboard's GroupVersionKind — reproducing deploymentForDashboard's only
// error return without needing a contrived Dashboard/Deployment state.
func schemeWithoutDashboard(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func TestReconcileDeploymentDefineError(t *testing.T) {
	clientScheme := networkTestScheme(t)
	instance := newDeploymentTestDashboard()
	cl := fake.NewClientBuilder().WithScheme(clientScheme).WithObjects(instance).WithStatusSubresource(instance).Build()

	r := &DashboardReconciler{Client: cl, Scheme: schemeWithoutDashboard(t), DashboardImage: testDashboardImage}

	_, handled, err := r.reconcileDeployment(t.Context(), instance)
	if !handled {
		t.Error("reconcileDeployment() handled = false, want true when deploymentForDashboard fails")
	}
	if err == nil {
		t.Error("reconcileDeployment() error = nil, want the SetControllerReference error")
	}

	updated := &pagev1alpha1.Dashboard{}
	if getErr := cl.Get(t.Context(), client.ObjectKeyFromObject(instance), updated); getErr != nil {
		t.Fatalf("getting Dashboard: %v", getErr)
	}
	if cond := meta.FindStatusCondition(updated.Status.Conditions, typeAvailableDashboard); cond == nil || cond.Status != metav1.ConditionFalse {
		t.Errorf("Dashboard status condition = %+v, want Available=False", cond)
	}
}

func TestReconcileDeploymentDefineErrorAndStatusUpdateError(t *testing.T) {
	clientScheme := networkTestScheme(t)
	instance := newDeploymentTestDashboard()
	wantErr := errors.New("status update boom")

	base := fake.NewClientBuilder().WithScheme(clientScheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, o client.Object, opts ...client.SubResourceUpdateOption) error {
			return wantErr
		},
	})

	r := &DashboardReconciler{Client: cl, Scheme: schemeWithoutDashboard(t), DashboardImage: testDashboardImage}

	_, handled, err := r.reconcileDeployment(t.Context(), instance)
	if !handled {
		t.Error("reconcileDeployment() handled = false, want true")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("reconcileDeployment() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileDeploymentGetError(t *testing.T) {
	clientScheme := networkTestScheme(t)
	instance := newDeploymentTestDashboard()
	wantErr := errors.New("get boom")

	base := fake.NewClientBuilder().WithScheme(clientScheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*appsv1.Deployment); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})

	r := &DashboardReconciler{Client: cl, Scheme: clientScheme, DashboardImage: testDashboardImage}

	_, handled, err := r.reconcileDeployment(t.Context(), instance)
	if !handled {
		t.Error("reconcileDeployment() handled = false, want true")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("reconcileDeployment() error = %v, want %v", err, wantErr)
	}
}

func TestReconcileDeploymentCreateError(t *testing.T) {
	clientScheme := networkTestScheme(t)
	instance := newDeploymentTestDashboard()
	wantErr := errors.New("create boom")

	base := fake.NewClientBuilder().WithScheme(clientScheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
			if _, ok := o.(*appsv1.Deployment); ok {
				return wantErr
			}
			return c.Create(ctx, o, opts...)
		},
	})

	r := &DashboardReconciler{Client: cl, Scheme: clientScheme, DashboardImage: testDashboardImage}

	_, handled, err := r.reconcileDeployment(t.Context(), instance)
	if !handled {
		t.Error("reconcileDeployment() handled = false, want true")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("reconcileDeployment() error = %v, want %v", err, wantErr)
	}
}

// seedDriftedDeployment builds the Deployment reconcileDeployment would
// create, then mutates it so a drift check fires, and creates it in cl.
func seedDriftedDeployment(t *testing.T, r *DashboardReconciler, instance *pagev1alpha1.Dashboard) *appsv1.Deployment {
	t.Helper()
	dep, err := r.deploymentForDashboard(instance)
	if err != nil {
		t.Fatalf("deploymentForDashboard() unexpected error: %v", err)
	}
	dep.Spec.Replicas = ptr.To(int32(7)) // desired default is 1 via webhook default, nil here without it
	dep.ResourceVersion = ""
	if err := r.Create(t.Context(), dep); err != nil {
		t.Fatalf("seeding drifted Deployment: %v", err)
	}
	return dep
}

func TestReconcileDeploymentUpdateErrorRefetchDashboardError(t *testing.T) {
	clientScheme := networkTestScheme(t)
	instance := newDeploymentTestDashboard()
	wantErr := errors.New("update boom")
	wantRefetchErr := errors.New("refetch boom")

	base := fake.NewClientBuilder().WithScheme(clientScheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	r := &DashboardReconciler{Client: base, Scheme: clientScheme, DashboardImage: testDashboardImage}
	seedDriftedDeployment(t, r, instance)

	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*appsv1.Deployment); ok {
				return wantErr
			}
			return c.Update(ctx, o, opts...)
		},
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*pagev1alpha1.Dashboard); ok {
				return wantRefetchErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r.Client = cl

	_, handled, err := r.reconcileDeployment(t.Context(), instance)
	if !handled {
		t.Error("reconcileDeployment() handled = false, want true")
	}
	if !errors.Is(err, wantRefetchErr) {
		t.Errorf("reconcileDeployment() error = %v, want %v", err, wantRefetchErr)
	}
}

func TestReconcileDeploymentUpdateErrorStatusUpdateError(t *testing.T) {
	clientScheme := networkTestScheme(t)
	instance := newDeploymentTestDashboard()
	wantUpdateErr := errors.New("update boom")
	wantStatusErr := errors.New("status update boom")

	base := fake.NewClientBuilder().WithScheme(clientScheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	r := &DashboardReconciler{Client: base, Scheme: clientScheme, DashboardImage: testDashboardImage}
	seedDriftedDeployment(t, r, instance)

	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*appsv1.Deployment); ok {
				return wantUpdateErr
			}
			return c.Update(ctx, o, opts...)
		},
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, o client.Object, opts ...client.SubResourceUpdateOption) error {
			return wantStatusErr
		},
	})
	r.Client = cl

	_, handled, err := r.reconcileDeployment(t.Context(), instance)
	if !handled {
		t.Error("reconcileDeployment() handled = false, want true")
	}
	if !errors.Is(err, wantStatusErr) {
		t.Errorf("reconcileDeployment() error = %v, want %v", err, wantStatusErr)
	}
}

func TestReconcileDeploymentUpdateErrorStatusUpdateSucceeds(t *testing.T) {
	clientScheme := networkTestScheme(t)
	instance := newDeploymentTestDashboard()
	wantUpdateErr := errors.New("update boom")

	base := fake.NewClientBuilder().WithScheme(clientScheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	r := &DashboardReconciler{Client: base, Scheme: clientScheme, DashboardImage: testDashboardImage}
	seedDriftedDeployment(t, r, instance)

	cl := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.UpdateOption) error {
			if _, ok := o.(*appsv1.Deployment); ok {
				return wantUpdateErr
			}
			return c.Update(ctx, o, opts...)
		},
	})
	r.Client = cl

	_, handled, err := r.reconcileDeployment(t.Context(), instance)
	if !handled {
		t.Error("reconcileDeployment() handled = false, want true")
	}
	if !errors.Is(err, wantUpdateErr) {
		t.Errorf("reconcileDeployment() error = %v, want the original update error %v", err, wantUpdateErr)
	}

	updated := &pagev1alpha1.Dashboard{}
	if getErr := base.Get(t.Context(), client.ObjectKeyFromObject(instance), updated); getErr != nil {
		t.Fatalf("getting Dashboard: %v", getErr)
	}
	if cond := meta.FindStatusCondition(updated.Status.Conditions, typeAvailableDashboard); cond == nil || cond.Reason != reasonDeploymentUpdateFailed {
		t.Errorf("Dashboard status condition = %+v, want reason %s", cond, reasonDeploymentUpdateFailed)
	}
}

func TestReconcileDeploymentNotFoundGet(t *testing.T) {
	clientScheme := networkTestScheme(t)
	instance := newDeploymentTestDashboard()
	cl := fake.NewClientBuilder().WithScheme(clientScheme).WithObjects(instance).WithStatusSubresource(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: clientScheme, DashboardImage: testDashboardImage}

	result, handled, err := r.reconcileDeployment(t.Context(), instance)
	if err != nil {
		t.Fatalf("reconcileDeployment() unexpected error: %v", err)
	}
	if !handled {
		t.Error("reconcileDeployment() handled = false, want true on first creation")
	}
	if result.RequeueAfter <= 0 {
		t.Errorf("reconcileDeployment() RequeueAfter = %v, want > 0", result.RequeueAfter)
	}

	dep := &appsv1.Deployment{}
	if getErr := cl.Get(t.Context(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, dep); getErr != nil {
		t.Fatalf("expected Deployment to be created: %v", getErr)
	}
}

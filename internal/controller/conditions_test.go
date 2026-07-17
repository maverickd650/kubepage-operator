package controller

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

func TestBoundDashboardCondition(t *testing.T) {
	scheme := networkTestScheme(t)
	const ns = "cond-ns"

	t.Run("empty dashboardRef.name", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		cond, err := boundDashboardCondition(t.Context(), cl, ns, "", 3)
		if err != nil {
			t.Fatalf("boundDashboardCondition() unexpected error: %v", err)
		}
		if cond.Status != metav1.ConditionFalse || cond.Reason != reasonDashboardNotFound {
			t.Errorf("cond = %+v, want False/%s", cond, reasonDashboardNotFound)
		}
	})

	t.Run("referenced Dashboard does not exist", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		cond, err := boundDashboardCondition(t.Context(), cl, ns, testRefDashboardName, 3)
		if err != nil {
			t.Fatalf("boundDashboardCondition() unexpected error: %v", err)
		}
		if cond.Status != metav1.ConditionFalse || cond.Reason != reasonDashboardNotFound {
			t.Errorf("cond = %+v, want False/%s", cond, reasonDashboardNotFound)
		}
	})

	t.Run("referenced Dashboard exists", func(t *testing.T) {
		instance := &pagev1alpha1.Dashboard{
			ObjectMeta: metav1.ObjectMeta{Name: testRefDashboardName, Namespace: ns},
			Spec:       pagev1alpha1.DashboardSpec{ContainerPort: 8080},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
		cond, err := boundDashboardCondition(t.Context(), cl, ns, testRefDashboardName, 3)
		if err != nil {
			t.Fatalf("boundDashboardCondition() unexpected error: %v", err)
		}
		if cond.Status != metav1.ConditionTrue || cond.Reason != reasonBound {
			t.Errorf("cond = %+v, want True/%s", cond, reasonBound)
		}
		if cond.ObservedGeneration != 3 {
			t.Errorf("cond.ObservedGeneration = %d, want 3", cond.ObservedGeneration)
		}
	})

	t.Run("Get fails for a reason other than NotFound", func(t *testing.T) {
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
		if _, err := boundDashboardCondition(t.Context(), cl, ns, testRefDashboardName, 3); !errors.Is(err, wantErr) {
			t.Errorf("boundDashboardCondition() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("unset dashboardRef, no Dashboard in namespace", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		cond, err := boundDashboardCondition(t.Context(), cl, ns, "", 3)
		if err != nil {
			t.Fatalf("boundDashboardCondition() unexpected error: %v", err)
		}
		if cond.Status != metav1.ConditionFalse || cond.Reason != reasonDashboardNotFound {
			t.Errorf("cond = %+v, want False/%s", cond, reasonDashboardNotFound)
		}
	})

	t.Run("unset dashboardRef, sole Dashboard in namespace", func(t *testing.T) {
		instance := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Name: testRefDashboardName, Namespace: ns}}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
		cond, err := boundDashboardCondition(t.Context(), cl, ns, "", 3)
		if err != nil {
			t.Fatalf("boundDashboardCondition() unexpected error: %v", err)
		}
		if cond.Status != metav1.ConditionTrue || cond.Reason != reasonBound {
			t.Errorf("cond = %+v, want True/%s (defaulted to sole Dashboard)", cond, reasonBound)
		}
	})

	t.Run("unset dashboardRef, multiple Dashboards in namespace", func(t *testing.T) {
		one := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Name: testRefDashboardName, Namespace: ns}}
		other := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Name: testOtherDashboardName, Namespace: ns}}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(one, other).Build()
		cond, err := boundDashboardCondition(t.Context(), cl, ns, "", 3)
		if err != nil {
			t.Fatalf("boundDashboardCondition() unexpected error: %v", err)
		}
		if cond.Status != metav1.ConditionFalse || cond.Reason != reasonAmbiguousDashboardRef {
			t.Errorf("cond = %+v, want False/%s", cond, reasonAmbiguousDashboardRef)
		}
	})
}

// TestDashboardWatchMayAffect covers the predicate each config CRD
// controller's own Watches(&Dashboard{}) uses to decide which of its
// objects to re-reconcile on a Dashboard event: an explicit ref only cares
// about the Dashboard it names, but an unset ref always needs a fresh look,
// since the Dashboard event itself (create/delete) is what can flip whether
// that unset ref resolves to a sole Dashboard.
func TestDashboardWatchMayAffect(t *testing.T) {
	tests := []struct {
		name         string
		refName      string
		instanceName string
		want         bool
	}{
		{"explicit ref naming the event's Dashboard", testRefDashboardName, testRefDashboardName, true},
		{"explicit ref naming a different Dashboard", testOtherDashboardName, testRefDashboardName, false},
		{"unset ref", "", testRefDashboardName, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dashboardWatchMayAffect(tt.refName, tt.instanceName); got != tt.want {
				t.Errorf("dashboardWatchMayAffect(%q, %q) = %v, want %v", tt.refName, tt.instanceName, got, tt.want)
			}
		})
	}
}

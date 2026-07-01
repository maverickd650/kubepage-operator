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

func TestBoundInstanceCondition(t *testing.T) {
	scheme := networkTestScheme(t)
	const ns = "cond-ns"

	t.Run("empty instanceRef.name", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		cond, err := boundInstanceCondition(t.Context(), cl, ns, "")
		if err != nil {
			t.Fatalf("boundInstanceCondition() unexpected error: %v", err)
		}
		if cond.Status != metav1.ConditionFalse || cond.Reason != reasonInstanceNotFound {
			t.Errorf("cond = %+v, want False/%s", cond, reasonInstanceNotFound)
		}
	})

	t.Run("referenced Instance does not exist", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		cond, err := boundInstanceCondition(t.Context(), cl, ns, testRefInstanceName)
		if err != nil {
			t.Fatalf("boundInstanceCondition() unexpected error: %v", err)
		}
		if cond.Status != metav1.ConditionFalse || cond.Reason != reasonInstanceNotFound {
			t.Errorf("cond = %+v, want False/%s", cond, reasonInstanceNotFound)
		}
	})

	t.Run("referenced Instance exists", func(t *testing.T) {
		instance := &pagev1alpha1.Instance{
			ObjectMeta: metav1.ObjectMeta{Name: testRefInstanceName, Namespace: ns},
			Spec:       pagev1alpha1.InstanceSpec{ContainerPort: 8080},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
		cond, err := boundInstanceCondition(t.Context(), cl, ns, testRefInstanceName)
		if err != nil {
			t.Fatalf("boundInstanceCondition() unexpected error: %v", err)
		}
		if cond.Status != metav1.ConditionTrue || cond.Reason != reasonBound {
			t.Errorf("cond = %+v, want True/%s", cond, reasonBound)
		}
	})

	t.Run("Get fails for a reason other than NotFound", func(t *testing.T) {
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
		if _, err := boundInstanceCondition(t.Context(), cl, ns, testRefInstanceName); !errors.Is(err, wantErr) {
			t.Errorf("boundInstanceCondition() error = %v, want %v", err, wantErr)
		}
	})
}

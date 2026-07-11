package controller

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// reconcilerErrorPathCase describes one of the four thin config-CRD
// controllers (Bookmark/DashboardStyle/InfoWidget/ServiceCard), which all
// share the same Reconcile shape: Get the CRD, resolve its DashboardRef via
// boundDashboardCondition, then Status().Update. The table-driven tests below
// exercise each of those three error returns once per controller, since
// envtest's real apiserver (used by the Ginkgo specs in this package) can't
// be made to fail Get/Update on demand the way a fake client with
// interceptor.Funcs can.
type reconcilerErrorPathCase struct {
	name          string
	newReconciler func(c client.Client) reconcile.Reconciler
	newObject     func(ns, name, instanceRefName string) client.Object
}

func reconcilerErrorPathCases() []reconcilerErrorPathCase {
	return []reconcilerErrorPathCase{
		{
			name: "Bookmark",
			newReconciler: func(c client.Client) reconcile.Reconciler {
				return &BookmarkReconciler{Client: c, Scheme: c.Scheme()}
			},
			newObject: func(ns, name, ref string) client.Object {
				return &pagev1alpha1.Bookmark{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
					Spec: pagev1alpha1.BookmarkSpec{
						DashboardRef: pagev1alpha1.DashboardRef{Name: ref},
						Group:        "G",
						Bookmarks: []pagev1alpha1.BookmarkEntry{
							{Name: "N", Href: "https://example.com"},
						},
					},
				}
			},
		},
		{
			name: "DashboardStyle",
			newReconciler: func(c client.Client) reconcile.Reconciler {
				return &DashboardStyleReconciler{Client: c, Scheme: c.Scheme()}
			},
			newObject: func(ns, name, ref string) client.Object {
				return &pagev1alpha1.DashboardStyle{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
					Spec: pagev1alpha1.DashboardStyleSpec{
						DashboardRef: pagev1alpha1.DashboardRef{Name: ref},
					},
				}
			},
		},
		{
			name: "InfoWidget",
			newReconciler: func(c client.Client) reconcile.Reconciler {
				return &InfoWidgetReconciler{Client: c, Scheme: c.Scheme()}
			},
			newObject: func(ns, name, ref string) client.Object {
				return &pagev1alpha1.InfoWidget{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
					Spec: pagev1alpha1.InfoWidgetSpec{
						DashboardRef: pagev1alpha1.DashboardRef{Name: ref},
						Widgets: []pagev1alpha1.InfoWidgetEntry{
							{Type: "datetime"},
						},
					},
				}
			},
		},
		{
			name: "ServiceCard",
			newReconciler: func(c client.Client) reconcile.Reconciler {
				return &ServiceCardReconciler{Client: c, Scheme: c.Scheme()}
			},
			newObject: func(ns, name, ref string) client.Object {
				return &pagev1alpha1.ServiceCard{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
					Spec: pagev1alpha1.ServiceCardSpec{
						DashboardRef: pagev1alpha1.DashboardRef{Name: ref},
						Group:        "G",
						Services: []pagev1alpha1.ServiceEntry{
							{Name: "N"},
						},
					},
				}
			},
		},
	}
}

func isDashboardObject(o client.Object) bool {
	_, ok := o.(*pagev1alpha1.Dashboard)
	return ok
}

func reconcileNamespacedObject(t *testing.T, r reconcile.Reconciler, name string) error {
	t.Helper()
	_, err := r.Reconcile(t.Context(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "ns"}})
	return err
}

// TestReconcileNotFoundIsIgnored covers the branch where the CRD was
// deleted before Reconcile got to it: Get returns NotFound, which is not
// itself an error.
func TestReconcileNotFoundIsIgnored(t *testing.T) {
	for _, tc := range reconcilerErrorPathCases() {
		t.Run(tc.name, func(t *testing.T) {
			scheme := networkTestScheme(t)
			cl := fake.NewClientBuilder().WithScheme(scheme).Build()

			err := reconcileNamespacedObject(t, tc.newReconciler(cl), "does-not-exist")
			if err != nil {
				t.Errorf("Reconcile() error = %v, want nil for a NotFound Get", err)
			}
		})
	}
}

// TestReconcileGetError covers the branch where Get-ing the CRD itself fails
// for a reason other than NotFound (a real apiserver/etcd error).
func TestReconcileGetError(t *testing.T) {
	for _, tc := range reconcilerErrorPathCases() {
		t.Run(tc.name, func(t *testing.T) {
			const ns, name = "ns", "obj"
			scheme := networkTestScheme(t)
			obj := tc.newObject(ns, name, testRefDashboardName)
			wantErr := errors.New("get boom")

			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(obj).Build()
			cl := interceptor.NewClient(base, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
					if !isDashboardObject(o) {
						return wantErr
					}
					return c.Get(ctx, key, o, opts...)
				},
			})

			err := reconcileNamespacedObject(t, tc.newReconciler(cl), name)
			if !errors.Is(err, wantErr) {
				t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
			}
		})
	}
}

// TestReconcileBoundDashboardConditionError covers the branch where
// boundDashboardCondition's Get of the referenced Dashboard fails for a
// reason other than NotFound.
func TestReconcileBoundDashboardConditionError(t *testing.T) {
	for _, tc := range reconcilerErrorPathCases() {
		t.Run(tc.name, func(t *testing.T) {
			const ns, name = "ns", "obj"
			scheme := networkTestScheme(t)
			obj := tc.newObject(ns, name, testRefDashboardName)
			wantErr := errors.New("instance get boom")

			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(obj).Build()
			cl := interceptor.NewClient(base, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
					if isDashboardObject(o) {
						return wantErr
					}
					return c.Get(ctx, key, o, opts...)
				},
			})

			err := reconcileNamespacedObject(t, tc.newReconciler(cl), name)
			if !errors.Is(err, wantErr) {
				t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
			}
		})
	}
}

// TestReconcileStatusUpdateError covers the branch where Status().Update
// fails. No Dashboard object is created, so instanceRefName resolves to a
// not-found condition (boundDashboardCondition returns no error for that
// case) and Status().Update is reached.
func TestReconcileStatusUpdateError(t *testing.T) {
	for _, tc := range reconcilerErrorPathCases() {
		t.Run(tc.name, func(t *testing.T) {
			const ns, name = "ns", "obj"
			scheme := networkTestScheme(t)
			obj := tc.newObject(ns, name, testRefDashboardName)
			wantErr := errors.New("status update boom")

			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(obj).Build()
			cl := interceptor.NewClient(base, interceptor.Funcs{
				SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, o client.Object, opts ...client.SubResourceUpdateOption) error {
					return wantErr
				},
			})

			err := reconcileNamespacedObject(t, tc.newReconciler(cl), name)
			if !errors.Is(err, wantErr) {
				t.Errorf("Reconcile() error = %v, want %v", err, wantErr)
			}
		})
	}
}

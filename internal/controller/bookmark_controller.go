package controller

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// BookmarkReconciler reconciles a Bookmark object.
//
// Thin, like ServiceCardReconciler: it only validates that dashboardRef
// resolves to an existing Dashboard and reflects that in status. Rendering
// bookmarks.yaml happens in the dashboard pod (internal/dashboard), which
// reads Bookmarks directly through its own cache. The actual reconcile/watch
// logic is shared with ServiceCardReconciler/InfoWidgetReconciler via
// boundConfigReconciler.
type BookmarkReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=page.kubepage.dev,resources=bookmarks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=bookmarks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=bookmarks/finalizers,verbs=update
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=dashboards,verbs=get;list;watch

func (r *BookmarkReconciler) adapter() *boundConfigReconciler[*pagev1alpha1.Bookmark] {
	return &boundConfigReconciler[*pagev1alpha1.Bookmark]{
		Client:         r.Client,
		Scheme:         r.Scheme,
		displayName:    "Bookmark",
		controllerName: "bookmark",
		newObj:         func() *pagev1alpha1.Bookmark { return &pagev1alpha1.Bookmark{} },
		newList:        func() client.ObjectList { return &pagev1alpha1.BookmarkList{} },
		listItems: func(l client.ObjectList) []*pagev1alpha1.Bookmark {
			return toPointerSlice(l.(*pagev1alpha1.BookmarkList).Items)
		},
		refName:      func(b *pagev1alpha1.Bookmark) string { return pagev1alpha1.RefName(b.Spec.DashboardRef) },
		conditions:   func(b *pagev1alpha1.Bookmark) *[]metav1.Condition { return &b.Status.Conditions },
		applyEntries: func(b *pagev1alpha1.Bookmark) { b.Status.Entries = int32(len(b.Spec.Entries())) },
	}
}

// Reconcile validates that the Bookmark's dashboardRef resolves to an existing
// Dashboard in the same namespace and sets the Available status condition
// accordingly.
func (r *BookmarkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.adapter().Reconcile(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *BookmarkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return r.adapter().SetupWithManager(mgr, &pagev1alpha1.Bookmark{})
}

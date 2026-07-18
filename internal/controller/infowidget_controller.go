package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// InfoWidgetReconciler reconciles a InfoWidget object.
//
// Thin, like ServiceCardReconciler and BookmarkReconciler: it only validates
// that dashboardRef resolves to an existing Dashboard and reflects that in
// status. Actually polling and rendering an InfoWidget (datetime, greeting,
// openmeteo, kubemetrics) happens in the native dashboard's poller
// (internal/dashboard/poller.go), which reads InfoWidgets directly through
// its own namespace-scoped cache. The actual reconcile/watch logic is shared
// with BookmarkReconciler/ServiceCardReconciler via boundConfigReconciler.
type InfoWidgetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=page.kubepage.dev,resources=infowidgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=infowidgets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=infowidgets/finalizers,verbs=update
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=dashboards,verbs=get;list;watch

func (r *InfoWidgetReconciler) adapter() *boundConfigReconciler[*pagev1alpha1.InfoWidget] {
	return &boundConfigReconciler[*pagev1alpha1.InfoWidget]{
		Client:         r.Client,
		Scheme:         r.Scheme,
		displayName:    "InfoWidget",
		controllerName: "infowidget",
		newObj:         func() *pagev1alpha1.InfoWidget { return &pagev1alpha1.InfoWidget{} },
		newList:        func() client.ObjectList { return &pagev1alpha1.InfoWidgetList{} },
		listItems: func(l client.ObjectList) []*pagev1alpha1.InfoWidget {
			return toPointerSlice(l.(*pagev1alpha1.InfoWidgetList).Items)
		},
		refName:      func(w *pagev1alpha1.InfoWidget) string { return pagev1alpha1.RefName(w.Spec.DashboardRef) },
		conditions:   func(w *pagev1alpha1.InfoWidget) *[]metav1.Condition { return &w.Status.Conditions },
		applyEntries: func(w *pagev1alpha1.InfoWidget) { w.Status.Entries = int32(len(w.Spec.Entries())) },
		widgetConfigs: func(w *pagev1alpha1.InfoWidget) []widgetConfigInstance {
			return infoWidgetConfigInstances(w.Spec.Entries())
		},
	}
}

// Reconcile validates that the InfoWidget's dashboardRef resolves to an
// existing Dashboard in the same namespace and sets the Available status
// condition accordingly.
func (r *InfoWidgetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.adapter().Reconcile(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *InfoWidgetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return r.adapter().SetupWithManager(mgr, &pagev1alpha1.InfoWidget{})
}

// infoWidgetConfigInstances flattens entries into the widgetConfigInstance
// form validateWidgetConfigs expects.
func infoWidgetConfigInstances(entries []pagev1alpha1.InfoWidgetEntry) []widgetConfigInstance {
	instances := make([]widgetConfigInstance, 0, len(entries))
	for widgetIdx, e := range entries {
		instances = append(instances, widgetConfigInstance{
			Location:   fmt.Sprintf("widget[%d] (type %q)", widgetIdx, e.Type),
			WidgetType: e.Type,
			Raw:        e.Config,
		})
	}
	return instances
}

package controller

import (
	"cmp"
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// ServiceCardReconciler reconciles a ServiceCard object.
//
// Thin: it only validates that dashboardRef resolves to an existing
// Dashboard and reflects that in status. The dashboard pod
// (internal/dashboard) reads and polls ServiceCards directly through its own
// cache; this controller never renders or resolves secrets. The actual
// reconcile/watch logic is shared with BookmarkReconciler/
// InfoWidgetReconciler via boundConfigReconciler.
type ServiceCardReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=page.kubepage.dev,resources=servicecards,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=servicecards/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=servicecards/finalizers,verbs=update
// +kubebuilder:rbac:groups=page.kubepage.dev,resources=dashboards,verbs=get;list;watch

func (r *ServiceCardReconciler) adapter() *boundConfigReconciler[*pagev1alpha1.ServiceCard] {
	return &boundConfigReconciler[*pagev1alpha1.ServiceCard]{
		Client:         r.Client,
		Scheme:         r.Scheme,
		displayName:    "ServiceCard",
		controllerName: "servicecard",
		newObj:         func() *pagev1alpha1.ServiceCard { return &pagev1alpha1.ServiceCard{} },
		newList:        func() client.ObjectList { return &pagev1alpha1.ServiceCardList{} },
		listItems: func(l client.ObjectList) []*pagev1alpha1.ServiceCard {
			return toPointerSlice(l.(*pagev1alpha1.ServiceCardList).Items)
		},
		refName:      func(s *pagev1alpha1.ServiceCard) string { return pagev1alpha1.RefName(s.Spec.DashboardRef) },
		conditions:   func(s *pagev1alpha1.ServiceCard) *[]metav1.Condition { return &s.Status.Conditions },
		applyEntries: func(s *pagev1alpha1.ServiceCard) { s.Status.Entries = int32(len(s.Spec.Entries())) },
		widgetConfigs: func(s *pagev1alpha1.ServiceCard) []widgetConfigInstance {
			return serviceCardWidgetConfigInstances(s.Spec.Entries())
		},
	}
}

// Reconcile validates that the ServiceCard's dashboardRef resolves to an
// existing Dashboard in the same namespace and sets the Available status
// condition accordingly.
func (r *ServiceCardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.adapter().Reconcile(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceCardReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return r.adapter().SetupWithManager(mgr, &pagev1alpha1.ServiceCard{})
}

// serviceCardWidgetConfigInstances flattens entries' widgets into the
// widgetConfigInstance form validateWidgetConfigs expects, labeling each by
// its entry name (or index, for an unnamed entry) and widget position.
func serviceCardWidgetConfigInstances(entries []pagev1alpha1.ServiceEntry) []widgetConfigInstance {
	var instances []widgetConfigInstance
	for entryIdx, e := range entries {
		label := cmp.Or(e.Name, fmt.Sprintf("entries[%d]", entryIdx))
		for widgetIdx, w := range e.Widgets {
			instances = append(instances, widgetConfigInstance{
				Location:   fmt.Sprintf("entry %q widget[%d] (type %q)", label, widgetIdx, w.Type),
				WidgetType: w.Type,
				Raw:        w.Config,
			})
		}
	}
	return instances
}

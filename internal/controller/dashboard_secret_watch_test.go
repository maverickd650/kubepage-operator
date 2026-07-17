package controller

import (
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// TestMapSecretToDashboards verifies the map function backing the metadata-
// only Secret watch (SetupWithManager's WatchesMetadata) enqueues every
// Dashboard in the Secret's own namespace, and none in another namespace.
func TestMapSecretToDashboards(t *testing.T) {
	scheme := networkTestScheme(t)
	inNamespace := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"}}
	other := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns"}}
	elsewhere := &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "other-ns"}}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(inNamespace, other, elsewhere).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns"}}
	reqs := r.mapSecretToDashboards(t.Context(), secret)

	names := make([]string, 0, len(reqs))
	for _, req := range reqs {
		if req.Namespace != "ns" {
			t.Errorf("mapSecretToDashboards() enqueued %v, want only Dashboards in namespace %q", req, "ns")
		}
		names = append(names, req.Name)
	}
	slices.Sort(names)
	if want := []string{"a", "b"}; !slices.Equal(names, want) {
		t.Errorf("mapSecretToDashboards() enqueued names %v, want %v", names, want)
	}
}

// TestSecretAllowWidgetsLabelChanged locks in secretAllowWidgetsLabelChanged's
// filtering: creation and deletion always pass, an update only passes when
// the allow-widgets label's presence or value differs, and any other
// metadata-only change (a different label, no change at all) is filtered
// out so it doesn't trigger a reconcile of every Dashboard in the namespace.
func TestSecretAllowWidgetsLabelChanged(t *testing.T) {
	withLabel := func(v string) *corev1.Secret {
		s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns"}}
		if v != "" {
			s.Labels = map[string]string{pagev1alpha1.SecretAllowWidgetsLabel: v}
		}
		return s
	}

	if !secretAllowWidgetsLabelChanged.Create(event.TypedCreateEvent[client.Object]{Object: withLabel(testValueTrue)}) {
		t.Errorf("Create() = false, want true")
	}
	if !secretAllowWidgetsLabelChanged.Delete(event.TypedDeleteEvent[client.Object]{Object: withLabel(testValueTrue)}) {
		t.Errorf("Delete() = false, want true")
	}
	if secretAllowWidgetsLabelChanged.Generic(event.TypedGenericEvent[client.Object]{Object: withLabel(testValueTrue)}) {
		t.Errorf("Generic() = true, want false")
	}

	t.Run("label added", func(t *testing.T) {
		e := event.TypedUpdateEvent[client.Object]{ObjectOld: withLabel(""), ObjectNew: withLabel(testValueTrue)}
		if !secretAllowWidgetsLabelChanged.Update(e) {
			t.Errorf("Update() = false, want true when the allow-widgets label is added")
		}
	})

	t.Run("label removed", func(t *testing.T) {
		e := event.TypedUpdateEvent[client.Object]{ObjectOld: withLabel(testValueTrue), ObjectNew: withLabel("")}
		if !secretAllowWidgetsLabelChanged.Update(e) {
			t.Errorf("Update() = false, want true when the allow-widgets label is removed")
		}
	})

	t.Run("label value changed", func(t *testing.T) {
		e := event.TypedUpdateEvent[client.Object]{ObjectOld: withLabel("false"), ObjectNew: withLabel(testValueTrue)}
		if !secretAllowWidgetsLabelChanged.Update(e) {
			t.Errorf("Update() = false, want true when the allow-widgets label value changes")
		}
	})

	t.Run("unrelated change", func(t *testing.T) {
		old := withLabel(testValueTrue)
		newObj := withLabel(testValueTrue)
		newObj.Labels["unrelated"] = "x"
		e := event.TypedUpdateEvent[client.Object]{ObjectOld: old, ObjectNew: newObj}
		if secretAllowWidgetsLabelChanged.Update(e) {
			t.Errorf("Update() = true, want false for an unrelated label change")
		}
	})

	t.Run("no change", func(t *testing.T) {
		e := event.TypedUpdateEvent[client.Object]{ObjectOld: withLabel(testValueTrue), ObjectNew: withLabel(testValueTrue)}
		if secretAllowWidgetsLabelChanged.Update(e) {
			t.Errorf("Update() = true, want false when nothing changed")
		}
	})
}

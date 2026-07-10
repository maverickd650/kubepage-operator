package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify that InfoWidgetReconciler sets status.entries to the
// number of entries InfoWidgetSpec.Entries() resolves — 1 for the
// single-widget form, len(spec.widgets) for the multi-widget form — so
// `kubectl get piw`'s Entries printcolumn reads correctly for both forms.
var _ = Describe("InfoWidget status.entries", func() {
	ctx := context.Background()

	It("sets entries to 1 for the single-widget form", func() {
		name := "iw-entries-single"
		iw := &pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDoesNotExistDashboardName},
				Type:         testWidgetTypeDatetime,
			},
		}
		Expect(k8sClient.Create(ctx, iw)).To(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, iw)).To(Succeed()) }()

		reconciler := &InfoWidgetReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: policyTestNamespace}})
		Expect(err).NotTo(HaveOccurred())

		got := &pagev1alpha1.InfoWidget{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: policyTestNamespace}, got)).To(Succeed())
		Expect(got.Status.Entries).To(Equal(int32(1)))
	})

	It("sets entries to len(widgets) for the multi-widget form", func() {
		name := "iw-entries-multi"
		iw := &pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDoesNotExistDashboardName},
				Widgets: []pagev1alpha1.InfoWidgetEntry{
					{Type: testWidgetTypeDatetime},
					{Type: testWidgetTypeOpenMeteo},
				},
			},
		}
		Expect(k8sClient.Create(ctx, iw)).To(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, iw)).To(Succeed()) }()

		reconciler := &InfoWidgetReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: policyTestNamespace}})
		Expect(err).NotTo(HaveOccurred())

		got := &pagev1alpha1.InfoWidget{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: policyTestNamespace}, got)).To(Succeed())
		Expect(got.Status.Entries).To(Equal(int32(2)))
	})
})

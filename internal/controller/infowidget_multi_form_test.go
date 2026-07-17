package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify the InfoWidgetSpec CRD schema (api/v1alpha1/
// infowidget_types.go): widgets is required, and each entry requires type.
// Unlike Bookmark/ServiceCard there is no Group concept, so there's no "no
// group resolvable" case, and InfoWidgetSpec carries no XValidation rules of
// its own.
var _ = Describe("InfoWidget CRD schema validation", func() {
	It("admits widgets with two entries", func() {
		iw := &pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: "iw-multi-ok", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Widgets: []pagev1alpha1.InfoWidgetEntry{
					{Type: testWidgetTypeDatetime},
					{Type: testWidgetTypeOpenMeteo},
				},
			},
		}
		Expect(k8sClient.Create(ctx, iw)).To(Succeed())
		Expect(k8sClient.Delete(ctx, iw)).To(Succeed())
	})

	It("rejects an InfoWidget with no widgets set", func() {
		iw := &pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: "iw-no-widgets", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: policyDashboardRef},
			},
		}
		err := k8sClient.Create(ctx, iw)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects a widgets entry missing type", func() {
		iw := &pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: "iw-entry-no-type", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Widgets:      []pagev1alpha1.InfoWidgetEntry{{}},
			},
		}
		err := k8sClient.Create(ctx, iw)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})
})

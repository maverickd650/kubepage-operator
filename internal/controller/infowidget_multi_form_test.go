package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify the InfoWidgetSpec CRD schema CEL rules
// (api/v1alpha1/infowidget_types.go's XValidation markers on that struct)
// that let an InfoWidget choose between the single-widget form (type set
// directly on spec, unchanged from earlier versions of this API) and the
// multi-widget form (spec.widgets, a list of InfoWidgetEntry) — but never
// both, and never neither. Unlike Bookmark/ServiceCard there is no Group
// concept, so there's no "no group resolvable" case to cover here.
var _ = Describe("InfoWidget single-vs-multi form CRD schema validation", func() {
	It("admits the single-widget form (type set, no widgets)", func() {
		iw := &pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: "iw-single-ok", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Type:         testWidgetTypeDatetime,
			},
		}
		Expect(k8sClient.Create(ctx, iw)).To(Succeed())
		Expect(k8sClient.Delete(ctx, iw)).To(Succeed())
	})

	It("admits the multi-widget form with two entries", func() {
		iw := &pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: "iw-multi-ok", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Widgets: []pagev1alpha1.InfoWidgetEntry{
					{Type: testWidgetTypeDatetime},
					{Type: testWidgetTypeOpenMeteo},
				},
			},
		}
		Expect(k8sClient.Create(ctx, iw)).To(Succeed())
		Expect(k8sClient.Delete(ctx, iw)).To(Succeed())
	})

	It("rejects both type and widgets set", func() {
		iw := &pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: "iw-both-forms", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Type:         testWidgetTypeDatetime,
				Widgets:      []pagev1alpha1.InfoWidgetEntry{{Type: testWidgetTypeOpenMeteo}},
			},
		}
		err := k8sClient.Create(ctx, iw)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects neither type nor widgets set", func() {
		iw := &pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: "iw-neither-form", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
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
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Widgets:      []pagev1alpha1.InfoWidgetEntry{{}},
			},
		}
		err := k8sClient.Create(ctx, iw)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects widgets set alongside an inline single-widget field (order)", func() {
		order := int32(1)
		iw := &pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: "iw-widgets-plus-order", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Order:        &order,
				Widgets:      []pagev1alpha1.InfoWidgetEntry{{Type: testWidgetTypeDatetime}},
			},
		}
		err := k8sClient.Create(ctx, iw)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects widgets set alongside an inline single-widget field (secrets)", func() {
		iw := &pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: "iw-widgets-plus-secrets", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Secrets:      map[string]pagev1alpha1.SecretValueSource{"token": {Value: new("x")}},
				Widgets:      []pagev1alpha1.InfoWidgetEntry{{Type: testWidgetTypeDatetime}},
			},
		}
		err := k8sClient.Create(ctx, iw)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects the single-widget form missing type", func() {
		iw := &pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: "iw-single-no-type", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
			},
		}
		err := k8sClient.Create(ctx, iw)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})
})

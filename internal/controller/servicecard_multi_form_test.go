package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify the ServiceCardSpec CRD schema CEL rules
// (api/v1alpha1/servicecard_types.go's XValidation markers on that struct)
// that let a ServiceCard choose between the single-card form (name set
// directly on spec, unchanged from earlier versions of this API) and the
// multi-card form (spec.services, a list of ServiceEntry) — but never both,
// and never neither.
var _ = Describe("ServiceCard single-vs-multi-card form CRD schema validation", func() {
	It("admits the single-card form (name + group, no services)", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-single-ok", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Name:         testMultiFormNamePlex,
			},
		}
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})

	It("admits the multi-card form with a top-level default group", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-multi-default-group", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Services: []pagev1alpha1.ServiceEntry{
					{Name: testMultiFormNamePlex},
					{Name: testMultiFormNameStash},
				},
			},
		}
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})

	It("admits the multi-card form with per-entry groups and no top-level group", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-multi-per-entry-group", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Services: []pagev1alpha1.ServiceEntry{
					{Name: testMultiFormNamePlex, Group: testMultiFormGroupMedia},
					{Name: "Grafana", Group: "Observability"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})

	It("rejects both name and services set", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-both-forms", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Name:         testMultiFormNamePlex,
				Services:     []pagev1alpha1.ServiceEntry{{Name: testMultiFormNameStash}},
			},
		}
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects neither name nor services set", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-neither-form", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
			},
		}
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects services set alongside an inline single-card field (widgets)", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-services-plus-widgets", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Widgets:      []pagev1alpha1.ServiceWidget{{Type: testWidgetTypePrometheus}},
				Services:     []pagev1alpha1.ServiceEntry{{Name: testMultiFormNamePlex}},
			},
		}
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects services set alongside an inline single-card field (href)", func() {
		href := "https://example.invalid"
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-services-plus-href", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Href:         &href,
				Services:     []pagev1alpha1.ServiceEntry{{Name: testMultiFormNamePlex}},
			},
		}
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects a services entry missing name", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-services-entry-no-name", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Services:     []pagev1alpha1.ServiceEntry{{}},
			},
		}
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects the multi-card form when no group is resolvable anywhere", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-multi-no-group", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Services: []pagev1alpha1.ServiceEntry{
					{Name: testMultiFormNamePlex, Group: testMultiFormGroupMedia},
					{Name: testMultiFormNameStash}, // no own group, and spec.group is unset
				},
			},
		}
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects the single-card form missing name", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-single-no-name", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
			},
		}
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects the single-card form missing group", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-single-no-group", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Name:         testMultiFormNamePlex,
			},
		}
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})
})

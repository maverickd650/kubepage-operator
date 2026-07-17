package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify the ServiceCardSpec CRD schema (api/v1alpha1/
// servicecard_types.go): services is required, each entry requires name,
// and every entry must resolve a group either from its own group or
// spec.group's default (enforced by the type's one remaining XValidation
// rule).
var _ = Describe("ServiceCard CRD schema validation", func() {
	It("admits services with a top-level default group", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-multi-default-group", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: policyDashboardRef},
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

	It("admits services with per-entry groups and no top-level group", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-multi-per-entry-group", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Services: []pagev1alpha1.ServiceEntry{
					{Name: testMultiFormNamePlex, Group: testMultiFormGroupMedia},
					{Name: "Grafana", Group: "Observability"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, se)).To(Succeed())
		Expect(k8sClient.Delete(ctx, se)).To(Succeed())
	})

	It("rejects a ServiceCard with no services set", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-no-services", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
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
				DashboardRef: &pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Services:     []pagev1alpha1.ServiceEntry{{}},
			},
		}
		err := k8sClient.Create(ctx, se)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects services when no group is resolvable anywhere", func() {
		se := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-multi-no-group", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: policyDashboardRef},
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
})

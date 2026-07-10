package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

var _ = Describe("DashboardStyle Controller", func() {
	Context("When reconciling a resource", func() {

		ctx := context.Background()

		var namespace *corev1.Namespace
		var namespaceName string

		BeforeEach(func() {
			// GenerateName, not a fixed Name: this Context runs multiple Its,
			// and envtest doesn't run a namespace controller, so a Delete in
			// AfterEach never actually completes before the next spec's
			// BeforeEach tries to reuse the same name.
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "test-configuration-"},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			namespaceName = namespace.Name
		})

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("sets Available=False/DashboardNotFound when dashboardRef does not resolve", func() {
			cfg := &pagev1alpha1.DashboardStyle{
				ObjectMeta: metav1.ObjectMeta{Name: testDoesNotExistDashboardName, Namespace: namespaceName},
				Spec: pagev1alpha1.DashboardStyleSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: testDoesNotExistDashboardName},
				},
			}
			Expect(k8sClient.Create(ctx, cfg)).To(Succeed())

			reconciler := &DashboardStyleReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: testDoesNotExistDashboardName, Namespace: namespaceName},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testDoesNotExistDashboardName, Namespace: namespaceName}, cfg)).To(Succeed())
			Expect(cfg.Status.Conditions).To(ContainElement(
				HaveField("Type", Equal(typeAvailableBound))))
			cond := cfg.Status.Conditions[0]
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(reasonDashboardNotFound))
		})

		It("sets Available=True once the referenced Dashboard exists", func() {
			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: testRefDashboardName, Namespace: namespaceName},
				Spec:       pagev1alpha1.DashboardSpec{Replicas: new(int32(1)), ContainerPort: 3000},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			cfg := &pagev1alpha1.DashboardStyle{
				ObjectMeta: metav1.ObjectMeta{Name: testRefDashboardName, Namespace: namespaceName},
				Spec: pagev1alpha1.DashboardStyleSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: testRefDashboardName},
				},
			}
			Expect(k8sClient.Create(ctx, cfg)).To(Succeed())

			reconciler := &DashboardStyleReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: testRefDashboardName, Namespace: namespaceName},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testRefDashboardName, Namespace: namespaceName}, cfg)).To(Succeed())
			Expect(cfg.Status.Conditions).To(ContainElement(
				HaveField("Type", Equal(typeAvailableBound))))
			cond := cfg.Status.Conditions[0]
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal(reasonBound))
		})
	})
})

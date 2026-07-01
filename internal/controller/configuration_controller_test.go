package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

var _ = Describe("Configuration Controller", func() {
	Context("When reconciling a resource", func() {

		const (
			configCfgName  = "cfg"
			configCfg2Name = "cfg2"
		)

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

		It("sets Available=False/InstanceNotFound when instanceRef does not resolve", func() {
			cfg := &pagev1alpha1.Configuration{
				ObjectMeta: metav1.ObjectMeta{Name: configCfgName, Namespace: namespaceName},
				Spec: pagev1alpha1.ConfigurationSpec{
					InstanceRef: pagev1alpha1.InstanceRef{Name: testDoesNotExistInstanceName},
				},
			}
			Expect(k8sClient.Create(ctx, cfg)).To(Succeed())

			reconciler := &ConfigurationReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: configCfgName, Namespace: namespaceName},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configCfgName, Namespace: namespaceName}, cfg)).To(Succeed())
			Expect(cfg.Status.Conditions).To(ContainElement(
				HaveField("Type", Equal(typeAvailableBound))))
			cond := cfg.Status.Conditions[0]
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(reasonInstanceNotFound))
		})

		It("sets Available=True once the referenced Instance exists", func() {
			instance := &pagev1alpha1.Instance{
				ObjectMeta: metav1.ObjectMeta{Name: testRefInstanceName, Namespace: namespaceName},
				Spec:       pagev1alpha1.InstanceSpec{Size: ptr.To(int32(1)), ContainerPort: 3000},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			cfg := &pagev1alpha1.Configuration{
				ObjectMeta: metav1.ObjectMeta{Name: configCfg2Name, Namespace: namespaceName},
				Spec: pagev1alpha1.ConfigurationSpec{
					InstanceRef: pagev1alpha1.InstanceRef{Name: testRefInstanceName},
				},
			}
			Expect(k8sClient.Create(ctx, cfg)).To(Succeed())

			reconciler := &ConfigurationReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: configCfg2Name, Namespace: namespaceName},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configCfg2Name, Namespace: namespaceName}, cfg)).To(Succeed())
			Expect(cfg.Status.Conditions).To(ContainElement(
				HaveField("Type", Equal(typeAvailableBound))))
			cond := cfg.Status.Conditions[0]
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal(reasonBound))
		})
	})
})

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify issue #104: ServiceCard/InfoWidget widget config/options
// blocks are validated against widgetschema.ConfigSchemas during reconcile,
// with results surfaced as status conditions rather than silently ignored.
// They need a real, existing Dashboard (unlike most of this package's specs,
// which intentionally leave dashboardRef unresolved) so Available reflects
// the config check rather than DashboardNotFound.
var _ = Describe("Widget config validation", func() {
	ctx := context.Background()
	const widgetConfigDashboardName = "widget-config-dashboard"

	BeforeEach(func() {
		dashboardInstance := &pagev1alpha1.Dashboard{
			ObjectMeta: metav1.ObjectMeta{Name: widgetConfigDashboardName, Namespace: policyTestNamespace},
		}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: widgetConfigDashboardName, Namespace: policyTestNamespace}, &pagev1alpha1.Dashboard{})
		if err != nil {
			Expect(k8sClient.Create(ctx, dashboardInstance)).To(Succeed())
		}
	})

	Describe("ServiceCard", func() {
		It("sets Available=False/InvalidWidgetConfig when a required config key is missing", func() {
			name := "sc-cfg-missing-required"
			sc := &pagev1alpha1.ServiceCard{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
				Spec: pagev1alpha1.ServiceCardSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: widgetConfigDashboardName},
					Group:        policyTestGroup,
					Services: []pagev1alpha1.ServiceEntry{{
						Name: testMultiFormNamePlex,
						Widgets: []pagev1alpha1.ServiceWidget{{
							Type:   testWidgetTypeCloudflared,
							Config: &apiextensionsv1.JSON{Raw: []byte(`{"accountId":"abc"}`)},
						}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, sc)).To(Succeed())
			defer func() { Expect(k8sClient.Delete(ctx, sc)).To(Succeed()) }()

			reconciler := &ServiceCardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: policyTestNamespace}})
			Expect(err).NotTo(HaveOccurred())

			got := &pagev1alpha1.ServiceCard{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: policyTestNamespace}, got)).To(Succeed())

			available := conditionOfType(got.Status.Conditions, typeAvailableBound)
			Expect(available).NotTo(BeNil())
			Expect(available.Status).To(Equal(metav1.ConditionFalse))
			Expect(available.Reason).To(Equal(reasonInvalidWidgetConfig))
			Expect(available.Message).To(ContainSubstring("tunnelId"))
		})

		It("sets ConfigValid=False/UnknownConfigKeys but leaves Available alone for a typo'd extra key", func() {
			name := "sc-cfg-unknown-key"
			sc := &pagev1alpha1.ServiceCard{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
				Spec: pagev1alpha1.ServiceCardSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: widgetConfigDashboardName},
					Group:        policyTestGroup,
					Services: []pagev1alpha1.ServiceEntry{{
						Name: testMultiFormNamePlex,
						Widgets: []pagev1alpha1.ServiceWidget{{
							Type:   "prometheusmetric",
							Config: &apiextensionsv1.JSON{Raw: []byte(`{"query":"up","labell":"typo"}`)},
						}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, sc)).To(Succeed())
			defer func() { Expect(k8sClient.Delete(ctx, sc)).To(Succeed()) }()

			reconciler := &ServiceCardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: policyTestNamespace}})
			Expect(err).NotTo(HaveOccurred())

			got := &pagev1alpha1.ServiceCard{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: policyTestNamespace}, got)).To(Succeed())

			available := conditionOfType(got.Status.Conditions, typeAvailableBound)
			Expect(available).NotTo(BeNil())
			Expect(available.Status).To(Equal(metav1.ConditionTrue))

			configValid := conditionOfType(got.Status.Conditions, typeConfigValid)
			Expect(configValid).NotTo(BeNil())
			Expect(configValid.Status).To(Equal(metav1.ConditionFalse))
			Expect(configValid.Reason).To(Equal(reasonUnknownConfigKeys))
			Expect(configValid.Message).To(ContainSubstring("labell"))
		})

		It("sets ConfigValid=True for a fully valid widget config", func() {
			name := "sc-cfg-valid"
			sc := &pagev1alpha1.ServiceCard{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
				Spec: pagev1alpha1.ServiceCardSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: widgetConfigDashboardName},
					Group:        policyTestGroup,
					Services: []pagev1alpha1.ServiceEntry{{
						Name: testMultiFormNamePlex,
						Widgets: []pagev1alpha1.ServiceWidget{{
							Type:   testWidgetTypeCloudflared,
							Config: &apiextensionsv1.JSON{Raw: []byte(`{"accountId":"abc","tunnelId":"def"}`)},
						}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, sc)).To(Succeed())
			defer func() { Expect(k8sClient.Delete(ctx, sc)).To(Succeed()) }()

			reconciler := &ServiceCardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: policyTestNamespace}})
			Expect(err).NotTo(HaveOccurred())

			got := &pagev1alpha1.ServiceCard{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: policyTestNamespace}, got)).To(Succeed())

			available := conditionOfType(got.Status.Conditions, typeAvailableBound)
			Expect(available).NotTo(BeNil())
			Expect(available.Status).To(Equal(metav1.ConditionTrue))

			configValid := conditionOfType(got.Status.Conditions, typeConfigValid)
			Expect(configValid).NotTo(BeNil())
			Expect(configValid.Status).To(Equal(metav1.ConditionTrue))
			Expect(configValid.Reason).To(Equal(reasonConfigValid))
		})
	})

	Describe("InfoWidget", func() {
		It("sets Available=False/InvalidWidgetConfig when openmeteo is missing latitude", func() {
			name := "iw-cfg-missing-required"
			iw := &pagev1alpha1.InfoWidget{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
				Spec: pagev1alpha1.InfoWidgetSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: widgetConfigDashboardName},
					Widgets: []pagev1alpha1.InfoWidgetEntry{{
						Type:    testWidgetTypeOpenMeteo,
						Options: &apiextensionsv1.JSON{Raw: []byte(`{"longitude":1.2}`)},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, iw)).To(Succeed())
			defer func() { Expect(k8sClient.Delete(ctx, iw)).To(Succeed()) }()

			reconciler := &InfoWidgetReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: policyTestNamespace}})
			Expect(err).NotTo(HaveOccurred())

			got := &pagev1alpha1.InfoWidget{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: policyTestNamespace}, got)).To(Succeed())

			available := conditionOfType(got.Status.Conditions, typeAvailableBound)
			Expect(available).NotTo(BeNil())
			Expect(available.Status).To(Equal(metav1.ConditionFalse))
			Expect(available.Reason).To(Equal(reasonInvalidWidgetConfig))
			Expect(available.Message).To(ContainSubstring(testOptionLatitude))
		})

		It("treats the typed URL field as satisfying glances' required url key", func() {
			name := "iw-cfg-glances-typed-url"
			iw := &pagev1alpha1.InfoWidget{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
				Spec: pagev1alpha1.InfoWidgetSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: widgetConfigDashboardName},
					Widgets: []pagev1alpha1.InfoWidgetEntry{{
						Type: "glances",
						URL:  new("http://glances.example.invalid"),
					}},
				},
			}
			Expect(k8sClient.Create(ctx, iw)).To(Succeed())
			defer func() { Expect(k8sClient.Delete(ctx, iw)).To(Succeed()) }()

			reconciler := &InfoWidgetReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: policyTestNamespace}})
			Expect(err).NotTo(HaveOccurred())

			got := &pagev1alpha1.InfoWidget{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: policyTestNamespace}, got)).To(Succeed())

			available := conditionOfType(got.Status.Conditions, typeAvailableBound)
			Expect(available).NotTo(BeNil())
			Expect(available.Status).To(Equal(metav1.ConditionTrue))

			configValid := conditionOfType(got.Status.Conditions, typeConfigValid)
			Expect(configValid).NotTo(BeNil())
			Expect(configValid.Status).To(Equal(metav1.ConditionTrue))
		})

		It("sets ConfigValid=False/UnknownConfigKeys but leaves Available alone for a typo'd extra key", func() {
			name := "iw-cfg-unknown-key"
			iw := &pagev1alpha1.InfoWidget{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
				Spec: pagev1alpha1.InfoWidgetSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: widgetConfigDashboardName},
					Widgets: []pagev1alpha1.InfoWidgetEntry{{
						Type:    testWidgetTypeOpenMeteo,
						Options: &apiextensionsv1.JSON{Raw: []byte(`{"latitude":1.0,"longitude":2.0,"latitude2":3.0}`)},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, iw)).To(Succeed())
			defer func() { Expect(k8sClient.Delete(ctx, iw)).To(Succeed()) }()

			reconciler := &InfoWidgetReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: policyTestNamespace}})
			Expect(err).NotTo(HaveOccurred())

			got := &pagev1alpha1.InfoWidget{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: policyTestNamespace}, got)).To(Succeed())

			available := conditionOfType(got.Status.Conditions, typeAvailableBound)
			Expect(available).NotTo(BeNil())
			Expect(available.Status).To(Equal(metav1.ConditionTrue))

			configValid := conditionOfType(got.Status.Conditions, typeConfigValid)
			Expect(configValid).NotTo(BeNil())
			Expect(configValid.Status).To(Equal(metav1.ConditionFalse))
			Expect(configValid.Reason).To(Equal(reasonUnknownConfigKeys))
			Expect(configValid.Message).To(ContainSubstring("latitude2"))
		})

		It("sets ConfigValid=True for a fully valid widget config", func() {
			name := "iw-cfg-valid"
			iw := &pagev1alpha1.InfoWidget{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
				Spec: pagev1alpha1.InfoWidgetSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: widgetConfigDashboardName},
					Widgets: []pagev1alpha1.InfoWidgetEntry{{
						Type:    testWidgetTypeOpenMeteo,
						Options: &apiextensionsv1.JSON{Raw: []byte(`{"latitude":1.0,"longitude":2.0}`)},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, iw)).To(Succeed())
			defer func() { Expect(k8sClient.Delete(ctx, iw)).To(Succeed()) }()

			reconciler := &InfoWidgetReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: policyTestNamespace}})
			Expect(err).NotTo(HaveOccurred())

			got := &pagev1alpha1.InfoWidget{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: policyTestNamespace}, got)).To(Succeed())

			available := conditionOfType(got.Status.Conditions, typeAvailableBound)
			Expect(available).NotTo(BeNil())
			Expect(available.Status).To(Equal(metav1.ConditionTrue))

			configValid := conditionOfType(got.Status.Conditions, typeConfigValid)
			Expect(configValid).NotTo(BeNil())
			Expect(configValid.Status).To(Equal(metav1.ConditionTrue))
			Expect(configValid.Reason).To(Equal(reasonConfigValid))
		})
	})
})

// findCondition returns the condition of the given type, or nil if absent.
func conditionOfType(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const testDashboardImage = "example.com/image:test"

var _ = Describe("Instance controller", func() {
	Context("Instance controller test", func() {

		const InstanceName = "test-instance"

		ctx := context.Background()

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      InstanceName,
				Namespace: InstanceName,
			},
		}

		typeNamespacedName := types.NamespacedName{
			Name:      InstanceName,
			Namespace: InstanceName,
		}
		instance := &pagev1alpha1.Instance{}

		SetDefaultEventuallyTimeout(2 * time.Minute)
		SetDefaultEventuallyPollingInterval(time.Second)

		BeforeEach(func() {
			By("Creating the Namespace to perform the tests")
			err := k8sClient.Create(ctx, namespace)
			Expect(err).NotTo(HaveOccurred())

			By("creating the custom resource for the Kind Instance")
			err = k8sClient.Get(ctx, typeNamespacedName, instance)
			if err != nil && errors.IsNotFound(err) {
				// Let's mock our custom resource at the same way that we would
				// apply on the cluster the manifest under config/samples
				instance = &pagev1alpha1.Instance{
					ObjectMeta: metav1.ObjectMeta{
						Name:      InstanceName,
						Namespace: namespace.Name,
					},
					Spec: pagev1alpha1.InstanceSpec{
						Size:          ptr.To(int32(1)),
						ContainerPort: 8080,
					},
				}

				err = k8sClient.Create(ctx, instance)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		AfterEach(func() {
			By("removing the custom resource for the Kind Instance")
			found := &pagev1alpha1.Instance{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Delete(context.TODO(), found)).To(Succeed())
			}).Should(Succeed())

			// TODO(user): Attention if you improve this code by adding other context test you MUST
			// be aware of the current delete namespace limitations.
			// More info: https://book.kubebuilder.io/reference/envtest.html#testing-considerations
			By("Deleting the Namespace to perform the tests")
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("should successfully reconcile a custom resource for Instance", func() {
			By("Checking if the custom resource was successfully created")
			Eventually(func(g Gomega) {
				found := &pagev1alpha1.Instance{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, found)).To(Succeed())
			}).Should(Succeed())

			By("Reconciling the custom resource created")
			instanceReconciler := &InstanceReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				DashboardImage: testDashboardImage,
			}

			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if Deployment was successfully created in the reconciliation")
			Eventually(func(g Gomega) {
				found := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, found)).To(Succeed())
			}).Should(Succeed())

			By("Reconciling the custom resource again")
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the latest Status Condition added to the Instance instance")
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			var conditions []metav1.Condition
			Expect(instance.Status.Conditions).To(ContainElement(
				HaveField("Type", Equal(typeAvailableInstance)), &conditions))
			Expect(conditions).To(HaveLen(1), "Multiple conditions of type %s", typeAvailableInstance)
			Expect(conditions[0].Status).To(Equal(metav1.ConditionTrue), "condition %s", typeAvailableInstance)
			Expect(conditions[0].Reason).To(Equal(reasonReconciling), "condition %s", typeAvailableInstance)
		})
	})

	Context("Instance controller spec field test", func() {

		const InstanceName = "test-instance-spec-fields"

		ctx := context.Background()

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: InstanceName,
			},
		}

		typeNamespacedName := types.NamespacedName{
			Name:      InstanceName,
			Namespace: InstanceName,
		}

		BeforeEach(func() {
			By("Creating the Namespace to perform the tests")
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		})

		AfterEach(func() {
			found := &pagev1alpha1.Instance{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			if err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("should apply optional spec fields to the generated Deployment", func() {
			By("creating an Instance with every optional field set, partially overriding the builtin security defaults")
			readinessProbe := &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/", Port: intstr.FromInt32(8080)},
				},
			}
			livenessProbe := &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt32(8080)},
				},
			}
			resources := corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("100m"),
				},
			}

			instance := &pagev1alpha1.Instance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      InstanceName,
					Namespace: namespace.Name,
				},
				Spec: pagev1alpha1.InstanceSpec{
					Size:          ptr.To(int32(1)),
					ContainerPort: 8080,
					HostUsers:     ptr.To(false),
					Labels: map[string]string{
						"team": "platform",
					},
					Annotations: map[string]string{
						"example.com/note": "hello",
					},
					PodSecurityContext: &corev1.PodSecurityContext{
						FSGroup: ptr.To(int64(1000)),
					},
					ContainerSecurityContext: &corev1.SecurityContext{
						ReadOnlyRootFilesystem: ptr.To(true),
					},
					ReadinessProbe: readinessProbe,
					LivenessProbe:  livenessProbe,
					Resources:      resources,
				},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			By("Reconciling the custom resource")
			instanceReconciler := &InstanceReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				DashboardImage: testDashboardImage,
			}
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the generated Deployment carries the optional fields")
			dep := &appsv1.Deployment{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, dep)).To(Succeed())
			}).Should(Succeed())

			By("HostUsers is set on the PodSpec")
			Expect(dep.Spec.Template.Spec.HostUsers).To(HaveValue(BeFalse()))

			By("the per-Instance ServiceAccount is set on the PodSpec")
			Expect(dep.Spec.Template.Spec.ServiceAccountName).To(Equal(InstanceName))

			By("the dashboard subcommand args name this Instance and its namespace")
			Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(dep.Spec.Template.Spec.Containers[0].Args).To(ConsistOf(
				"dashboard",
				"--namespace="+namespace.Name,
				"--instance-name="+InstanceName,
				"--addr=:8080",
			))
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal(testDashboardImage))

			By("user Labels and Annotations are merged into the pod template metadata")
			Expect(dep.Spec.Template.ObjectMeta.Labels).To(HaveKeyWithValue("team", "platform"))
			Expect(dep.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue("example.com/note", "hello"))

			By("the builtin selector labels are still present alongside the user labels")
			Expect(dep.Spec.Template.ObjectMeta.Labels).To(HaveKeyWithValue("app.kubernetes.io/name", "kubepage-operator"))

			By("PodSecurityContext merges the user override with the builtin defaults")
			podSC := dep.Spec.Template.Spec.SecurityContext
			Expect(podSC).NotTo(BeNil())
			Expect(podSC.FSGroup).To(HaveValue(Equal(int64(1000))))
			Expect(podSC.RunAsNonRoot).To(HaveValue(BeTrue()), "builtin RunAsNonRoot default must survive a partial override")
			Expect(podSC.SeccompProfile).NotTo(BeNil(), "builtin SeccompProfile default must survive a partial override")

			By("ContainerSecurityContext merges the user override with the builtin defaults")
			containerSC := dep.Spec.Template.Spec.Containers[0].SecurityContext
			Expect(containerSC).NotTo(BeNil())
			Expect(containerSC.ReadOnlyRootFilesystem).To(HaveValue(BeTrue()))
			Expect(containerSC.RunAsUser).To(HaveValue(Equal(int64(568))), "builtin RunAsUser default must survive a partial override")
			Expect(containerSC.AllowPrivilegeEscalation).To(HaveValue(BeFalse()), "builtin AllowPrivilegeEscalation default must survive a partial override")
			Expect(containerSC.Capabilities).NotTo(BeNil())
			Expect(containerSC.Capabilities.Drop).To(ContainElement(corev1.Capability("ALL")))

			By("ReadinessProbe, LivenessProbe and Resources are set on the container")
			container := dep.Spec.Template.Spec.Containers[0]
			Expect(container.ReadinessProbe).NotTo(BeNil())
			Expect(container.ReadinessProbe.HTTPGet.Path).To(Equal(readinessProbe.HTTPGet.Path))
			Expect(container.ReadinessProbe.HTTPGet.Port).To(Equal(readinessProbe.HTTPGet.Port))
			Expect(container.LivenessProbe).NotTo(BeNil())
			Expect(container.LivenessProbe.HTTPGet.Path).To(Equal(livenessProbe.HTTPGet.Path))
			Expect(container.LivenessProbe.HTTPGet.Port).To(Equal(livenessProbe.HTTPGet.Port))
			Expect(container.Resources.Requests).To(Equal(resources.Requests))
		})
	})

	Context("Instance controller dashboard RBAC test", func() {

		const InstanceName = "test-instance-rbac"

		ctx := context.Background()

		var namespace *corev1.Namespace
		var typeNamespacedName types.NamespacedName

		BeforeEach(func() {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "test-instance-rbac-"},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			typeNamespacedName = types.NamespacedName{Name: InstanceName, Namespace: namespace.Name}
		})

		AfterEach(func() {
			found := &pagev1alpha1.Instance{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			if err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("creates a ServiceAccount, Role, and RoleBinding granting the dashboard pod read access to the config CRDs and Secrets", func() {
			instance := &pagev1alpha1.Instance{
				ObjectMeta: metav1.ObjectMeta{Name: InstanceName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.InstanceSpec{Size: ptr.To(int32(1)), ContainerPort: 8080},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			instanceReconciler := &InstanceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("a ServiceAccount named after the Instance exists and is owned by it")
			sa := &corev1.ServiceAccount{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, sa)).To(Succeed())
			}).Should(Succeed())
			Expect(sa.OwnerReferences).To(ContainElement(HaveField("Name", InstanceName)))

			By("a Role grants get/list/watch on the config CRDs and Secrets")
			role := &rbacv1.Role{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, role)).To(Succeed())
			}).Should(Succeed())
			Expect(role.Rules).To(ContainElement(HaveField("Resources", ContainElement("serviceentries"))))
			Expect(role.Rules).To(ContainElement(HaveField("Resources", ContainElement("secrets"))))

			By("a RoleBinding binds the Role to the ServiceAccount")
			rb := &rbacv1.RoleBinding{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, rb)).To(Succeed())
			}).Should(Succeed())
			Expect(rb.RoleRef.Name).To(Equal(InstanceName))
			Expect(rb.Subjects).To(ContainElement(HaveField("Name", InstanceName)))
		})
	})

	Context("Instance controller exposure and status test", func() {

		const InstanceName = "test-instance-exposure"

		ctx := context.Background()

		var namespace *corev1.Namespace

		typeNamespacedName := types.NamespacedName{
			Name:      InstanceName,
			Namespace: InstanceName,
		}

		BeforeEach(func() {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "test-instance-exposure-"},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			typeNamespacedName = types.NamespacedName{Name: InstanceName, Namespace: namespace.Name}
		})

		AfterEach(func() {
			found := &pagev1alpha1.Instance{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			if err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("creates a Service fronting the Deployment and populates bound-count status", func() {
			instance := &pagev1alpha1.Instance{
				ObjectMeta: metav1.ObjectMeta{Name: InstanceName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.InstanceSpec{Size: ptr.To(int32(1)), ContainerPort: 8080},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			bm := &pagev1alpha1.Bookmark{
				ObjectMeta: metav1.ObjectMeta{Name: "bm", Namespace: namespace.Name},
				Spec: pagev1alpha1.BookmarkSpec{
					InstanceRef: pagev1alpha1.InstanceRef{Name: InstanceName},
					Group:       "Developer",
					Name:        "Github",
					Href:        "https://github.com/",
				},
			}
			Expect(k8sClient.Create(ctx, bm)).To(Succeed())

			entry := &pagev1alpha1.ServiceEntry{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: namespace.Name},
				Spec: pagev1alpha1.ServiceEntrySpec{
					InstanceRef: pagev1alpha1.InstanceRef{Name: InstanceName},
					Group:       "Media",
					Name:        "Sonarr",
				},
			}
			Expect(k8sClient.Create(ctx, entry)).To(Succeed())

			instanceReconciler := &InstanceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
			// The first Reconcile creates the Deployment and returns early
			// (reconcileDeployment's "handled" requeue path); Service/Ingress
			// reconciliation and status population happen after that point,
			// so a second call is needed for them to run.
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("a ClusterIP Service is created selecting the Instance's pods")
			svc := &corev1.Service{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, svc)).To(Succeed())
			}).Should(Succeed())
			Expect(svc.Spec.Ports).To(ContainElement(HaveField("Port", int32(8080))))
			Expect(svc.Spec.Selector).To(Equal(selectorLabelsForInstance()))

			By("status reflects the bound Bookmark and ServiceEntry counts")
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			Expect(instance.Status.BoundBookmarks).To(Equal(int32(1)))
			Expect(instance.Status.BoundServiceEntries).To(Equal(int32(1)))
			Expect(instance.Status.BoundConfigurations).To(Equal(int32(0)))
			Expect(instance.Status.ObservedGeneration).To(Equal(instance.Generation))
		})

		It("creates an Ingress when spec.ingress.enabled is true and removes it when toggled off", func() {
			instance := &pagev1alpha1.Instance{
				ObjectMeta: metav1.ObjectMeta{Name: InstanceName, Namespace: namespace.Name},
				Spec: pagev1alpha1.InstanceSpec{
					Size:          ptr.To(int32(1)),
					ContainerPort: 8080,
					Ingress: &pagev1alpha1.IngressSpec{
						Enabled: true,
						Host:    testDashboardHost,
						TLS:     &pagev1alpha1.IngressTLSSpec{SecretName: "dashboard-tls"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			instanceReconciler := &InstanceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
			// See the comment in the previous It: the first Reconcile only
			// creates the Deployment and returns early.
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("the Ingress is created with the configured host, backend Service, and TLS")
			ing := &networkingv1.Ingress{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, ing)).To(Succeed())
			}).Should(Succeed())
			Expect(ing.Spec.Rules).To(HaveLen(1))
			Expect(ing.Spec.Rules[0].Host).To(Equal(testDashboardHost))
			Expect(ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(Equal(InstanceName))
			Expect(ing.Spec.TLS).To(ContainElement(HaveField("SecretName", "dashboard-tls")))

			By("disabling spec.ingress.enabled removes the Ingress")
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Ingress.Enabled = false
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, typeNamespacedName, &networkingv1.Ingress{}))).To(BeTrue())
			}).Should(Succeed())
		})
	})

	Context("Instance controller Gateway API test", func() {

		const InstanceName = "test-instance-gateway"

		ctx := context.Background()

		var namespace *corev1.Namespace
		var typeNamespacedName types.NamespacedName

		BeforeEach(func() {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "test-instance-gateway-"},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			typeNamespacedName = types.NamespacedName{Name: InstanceName, Namespace: namespace.Name}
		})

		AfterEach(func() {
			found := &pagev1alpha1.Instance{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			if err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("reports a clear Available=False condition when spec.gateway is enabled but Gateway API CRDs aren't installed", func() {
			// envtest only installs this project's own CRDs (see suite_test.go),
			// so GatewayAPIEnabled is always false here — exactly the
			// degrade-gracefully path reconcileHTTPRoute exists for, since a
			// real cluster without Gateway API CRDs installed is the common
			// case this guards against.
			instance := &pagev1alpha1.Instance{
				ObjectMeta: metav1.ObjectMeta{Name: InstanceName, Namespace: namespace.Name},
				Spec: pagev1alpha1.InstanceSpec{
					Size:          ptr.To(int32(1)),
					ContainerPort: 8080,
					Gateway: &pagev1alpha1.GatewaySpec{
						Enabled:   true,
						Hostnames: []string{testDashboardHost},
						ParentRef: pagev1alpha1.GatewayParentRef{Name: "eg"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			instanceReconciler := &InstanceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage, GatewayAPIEnabled: false}
			// See the comment on the Ingress test: the first Reconcile only
			// creates the Deployment and returns early.
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(MatchError(errGatewayAPINotInstalled))

			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			var conditions []metav1.Condition
			Expect(instance.Status.Conditions).To(ContainElement(
				HaveField("Type", Equal(typeAvailableInstance)), &conditions))
			Expect(conditions[len(conditions)-1].Status).To(Equal(metav1.ConditionFalse))
			Expect(conditions[len(conditions)-1].Message).To(ContainSubstring("Gateway API CRDs are not installed"))
		})

		It("is a no-op when spec.gateway is unset, regardless of GatewayAPIEnabled", func() {
			instance := &pagev1alpha1.Instance{
				ObjectMeta: metav1.ObjectMeta{Name: InstanceName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.InstanceSpec{Size: ptr.To(int32(1)), ContainerPort: 8080},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			instanceReconciler := &InstanceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage, GatewayAPIEnabled: false}
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			var conditions []metav1.Condition
			Expect(instance.Status.Conditions).To(ContainElement(
				HaveField("Type", Equal(typeAvailableInstance)), &conditions))
			Expect(conditions[len(conditions)-1].Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("Instance controller image drift test", func() {

		const InstanceName = "test-instance-image-drift"

		ctx := context.Background()

		var namespace *corev1.Namespace
		var typeNamespacedName types.NamespacedName

		BeforeEach(func() {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "test-instance-image-drift-"},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			typeNamespacedName = types.NamespacedName{Name: InstanceName, Namespace: namespace.Name}
		})

		AfterEach(func() {
			found := &pagev1alpha1.Instance{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			if err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("rolls the Deployment when DashboardImage changes between reconciles (operator upgrade)", func() {
			instance := &pagev1alpha1.Instance{
				ObjectMeta: metav1.ObjectMeta{Name: InstanceName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.InstanceSpec{Size: ptr.To(int32(1)), ContainerPort: 8080},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			reconciler := &InstanceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			dep := &appsv1.Deployment{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, dep)).To(Succeed())
			}).Should(Succeed())
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal(testDashboardImage))

			By("reconciling again with a different DashboardImage simulates an operator upgrade")
			reconciler.DashboardImage = "example.com/image:upgraded"
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, dep)).To(Succeed())
				g.Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("example.com/image:upgraded"))
			}).Should(Succeed())
		})
	})
})

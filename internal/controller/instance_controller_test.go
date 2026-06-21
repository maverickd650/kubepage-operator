package controller

import (
	"context"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

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

			By("Setting the Image ENV VAR which stores the Operand image")
			err = os.Setenv("INSTANCE_IMAGE", "example.com/image:test")
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
						ContainerPort: 3000,
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

			By("Removing the Image ENV VAR which stores the Operand image")
			_ = os.Unsetenv("INSTANCE_IMAGE")
		})

		It("should successfully reconcile a custom resource for Instance", func() {
			By("Checking if the custom resource was successfully created")
			Eventually(func(g Gomega) {
				found := &pagev1alpha1.Instance{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, found)).To(Succeed())
			}).Should(Succeed())

			By("Reconciling the custom resource created")
			instanceReconciler := &InstanceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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

			By("Setting the Image ENV VAR which stores the Operand image")
			Expect(os.Setenv("INSTANCE_IMAGE", "example.com/image:test")).To(Succeed())
		})

		AfterEach(func() {
			found := &pagev1alpha1.Instance{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			if err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}
			_ = k8sClient.Delete(ctx, namespace)
			_ = os.Unsetenv("INSTANCE_IMAGE")
		})

		It("should apply optional spec fields to the generated Deployment", func() {
			By("creating an Instance with every optional field set, partially overriding the builtin security defaults")
			readinessProbe := &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/", Port: intstr.FromInt32(3000)},
				},
			}
			livenessProbe := &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt32(3000)},
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
					ContainerPort: 3000,
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
			Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
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

	Context("Instance controller config rendering test", func() {

		const (
			InstanceName = "test-instance-config"
			configCRName = "cfg"
		)

		ctx := context.Background()

		var namespace *corev1.Namespace
		var typeNamespacedName types.NamespacedName

		BeforeEach(func() {
			By("Creating the Namespace to perform the tests")
			// GenerateName, not a fixed Name: this Context runs multiple Its,
			// and envtest doesn't run a namespace controller, so a Delete in
			// AfterEach never actually completes before the next spec's
			// BeforeEach tries to reuse the same name (see the note on the
			// "Instance controller test" Context above re: envtest's
			// namespace-deletion limitations).
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "test-instance-config-",
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			typeNamespacedName = types.NamespacedName{Name: InstanceName, Namespace: namespace.Name}

			By("Setting the Image ENV VAR which stores the Operand image")
			Expect(os.Setenv("INSTANCE_IMAGE", "example.com/image:test")).To(Succeed())
		})

		AfterEach(func() {
			found := &pagev1alpha1.Instance{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			if err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}
			_ = k8sClient.Delete(ctx, namespace)
			_ = os.Unsetenv("INSTANCE_IMAGE")
		})

		It("renders an owned ConfigMap with kubernetes.yaml disabled even with no Configuration bound", func() {
			instance := &pagev1alpha1.Instance{
				ObjectMeta: metav1.ObjectMeta{Name: InstanceName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.InstanceSpec{Size: ptr.To(int32(1)), ContainerPort: 3000},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			instanceReconciler := &InstanceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("the ConfigMap exists, is owned by the Instance, and has kubernetes.yaml disabled but no settings.yaml")
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, cm)).To(Succeed())
			}).Should(Succeed())
			Expect(cm.Data).To(HaveKeyWithValue("kubernetes.yaml", "mode: disabled\n"))
			Expect(cm.Data).NotTo(HaveKey("settings.yaml"))
			Expect(cm.OwnerReferences).To(ContainElement(HaveField("Name", InstanceName)))

			By("the Deployment mounts the ConfigMap and carries a config-hash annotation")
			dep := &appsv1.Deployment{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, dep)).To(Succeed())
			}).Should(Succeed())
			Expect(dep.Spec.Template.Spec.Volumes).To(ContainElement(HaveField("ConfigMap.LocalObjectReference.Name", InstanceName)))
			Expect(dep.Spec.Template.Spec.Containers[0].VolumeMounts).To(ContainElement(HaveField("MountPath", configMountPath)))
			Expect(dep.Spec.Template.Annotations).To(HaveKey(configHashAnnotation))
		})

		It("renders settings.yaml from the bound Configuration and updates the ConfigMap and rollout hash when it changes", func() {
			instance := &pagev1alpha1.Instance{
				ObjectMeta: metav1.ObjectMeta{Name: InstanceName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.InstanceSpec{Size: ptr.To(int32(1)), ContainerPort: 3000},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			cfg := &pagev1alpha1.Configuration{
				ObjectMeta: metav1.ObjectMeta{Name: configCRName, Namespace: namespace.Name},
				Spec: pagev1alpha1.ConfigurationSpec{
					InstanceRef: pagev1alpha1.InstanceRef{Name: InstanceName},
					Title:       ptr.To("My Homepage"),
				},
			}
			Expect(k8sClient.Create(ctx, cfg)).To(Succeed())

			instanceReconciler := &InstanceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, cm)).To(Succeed())
			}).Should(Succeed())
			// startUrl is present because the CRD defaults it to "/" server-side.
			Expect(cm.Data).To(HaveKeyWithValue("settings.yaml", "startUrl: /\ntitle: My Homepage\n"))

			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, dep)).To(Succeed())
			firstHash := dep.Spec.Template.Annotations[configHashAnnotation]
			Expect(firstHash).NotTo(BeEmpty())

			By("changing the Configuration and reconciling again")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configCRName, Namespace: namespace.Name}, cfg)).To(Succeed())
			cfg.Spec.Title = ptr.To("Updated Title")
			Expect(k8sClient.Update(ctx, cfg)).To(Succeed())

			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, cm)).To(Succeed())
				g.Expect(cm.Data).To(HaveKeyWithValue("settings.yaml", "startUrl: /\ntitle: Updated Title\n"))
			}).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, dep)).To(Succeed())
				g.Expect(dep.Spec.Template.Annotations[configHashAnnotation]).NotTo(Equal(firstHash))
			}).Should(Succeed())
		})
	})
})

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
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const testDashboardImage = "example.com/image:test"

// markDeploymentReady patches the named Deployment's status to report
// readyReplicas, standing in for the kubelet + Deployment controller that
// envtest doesn't run: without this, ReadyReplicas would stay 0 forever and
// deploymentReady (instance_controller.go) would never see Available=True.
func markDeploymentReady(ctx context.Context, key types.NamespacedName, readyReplicas int32) {
	found := &appsv1.Deployment{}
	ExpectWithOffset(1, k8sClient.Get(ctx, key, found)).To(Succeed())
	found.Status.Replicas = readyReplicas
	found.Status.ReadyReplicas = readyReplicas
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, found)).To(Succeed())
}

var _ = Describe("Dashboard controller", func() {
	Context("Dashboard controller test", func() {

		const DashboardName = "test-instance"

		ctx := context.Background()

		var namespace *corev1.Namespace
		var typeNamespacedName types.NamespacedName
		instance := &pagev1alpha1.Dashboard{}

		SetDefaultEventuallyTimeout(2 * time.Minute)
		SetDefaultEventuallyPollingInterval(time.Second)

		BeforeEach(func() {
			By("Creating the Namespace to perform the tests")
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "test-instance-"},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			typeNamespacedName = types.NamespacedName{Name: DashboardName, Namespace: namespace.Name}

			By("creating the custom resource for the Kind Dashboard")
			err := k8sClient.Get(ctx, typeNamespacedName, instance)
			if err != nil && errors.IsNotFound(err) {
				// Let's mock our custom resource at the same way that we would
				// apply on the cluster the manifest under config/samples
				instance = &pagev1alpha1.Dashboard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      DashboardName,
						Namespace: namespace.Name,
					},
					Spec: pagev1alpha1.DashboardSpec{
						Replicas:      new(int32(1)),
						ContainerPort: 8080,
					},
				}

				err = k8sClient.Create(ctx, instance)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		AfterEach(func() {
			By("removing the custom resource for the Kind Dashboard")
			found := &pagev1alpha1.Dashboard{}
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

		It("should successfully reconcile a custom resource for Dashboard", func() {
			By("Checking if the custom resource was successfully created")
			Eventually(func(g Gomega) {
				found := &pagev1alpha1.Dashboard{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, found)).To(Succeed())
			}).Should(Succeed())

			By("Reconciling the custom resource created")
			instanceReconciler := &DashboardReconciler{
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

			// envtest runs no Deployment controller, so ReadyReplicas never
			// advances on its own; simulate pods coming up so the
			// Available condition's new readiness check (deploymentReady)
			// can observe a ready Deployment, same as it would once a real
			// kubelet reports the dashboard pod Ready.
			markDeploymentReady(ctx, typeNamespacedName, 1)

			By("Reconciling the custom resource again")
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the latest Status Condition added to the Dashboard instance")
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			var conditions []metav1.Condition
			Expect(instance.Status.Conditions).To(ContainElement(
				HaveField("Type", Equal(typeAvailableDashboard)), &conditions))
			Expect(conditions).To(HaveLen(1), "Multiple conditions of type %s", typeAvailableDashboard)
			Expect(conditions[0].Status).To(Equal(metav1.ConditionTrue), "condition %s", typeAvailableDashboard)
			Expect(conditions[0].Reason).To(Equal(reasonReconcileSucceeded), "condition %s", typeAvailableDashboard)
		})
	})

	Context("Dashboard controller spec field test", func() {

		const DashboardName = "test-instance-spec-fields"

		ctx := context.Background()

		var namespace *corev1.Namespace
		var typeNamespacedName types.NamespacedName

		BeforeEach(func() {
			By("Creating the Namespace to perform the tests")
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "test-instance-spec-fields-"},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			typeNamespacedName = types.NamespacedName{Name: DashboardName, Namespace: namespace.Name}
		})

		AfterEach(func() {
			found := &pagev1alpha1.Dashboard{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			if err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("should apply optional spec fields to the generated Deployment", func() {
			By("creating a Dashboard with every optional field set, partially overriding the builtin security defaults")
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

			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DashboardName,
					Namespace: namespace.Name,
				},
				Spec: pagev1alpha1.DashboardSpec{
					Replicas:      new(int32(1)),
					ContainerPort: 8080,
					HostUsers:     ptr.To(pagev1alpha1.Disabled),
					Labels: map[string]string{
						"team": "platform",
					},
					Annotations: map[string]string{
						testAnnotationKey: "hello",
					},
					PodSecurityContext: &corev1.PodSecurityContext{
						FSGroup: new(int64(1000)),
					},
					ContainerSecurityContext: &corev1.SecurityContext{
						ReadOnlyRootFilesystem: new(true),
					},
					ReadinessProbe: readinessProbe,
					LivenessProbe:  livenessProbe,
					Resources:      resources,
					NodeSelector: map[string]string{
						"kubernetes.io/arch": "arm64",
					},
					Tolerations: []corev1.Toleration{
						{Key: "node-role", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
					},
					PriorityClassName: new("high-priority"),
				},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			By("Reconciling the custom resource")
			instanceReconciler := &DashboardReconciler{
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

			By("the per-Dashboard ServiceAccount is set on the PodSpec")
			Expect(dep.Spec.Template.Spec.ServiceAccountName).To(Equal(DashboardName))

			By("the dashboard subcommand args name this Dashboard and its namespace")
			Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(dep.Spec.Template.Spec.Containers[0].Args).To(ConsistOf(
				"dashboard",
				"--namespace="+namespace.Name,
				"--dashboard-name="+DashboardName,
				"--addr=:8080",
				"--metrics-addr=:9090",
				"--poll-interval=15s",
			))
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal(testDashboardImage))

			By("user Labels and Annotations are merged into the pod template metadata")
			Expect(dep.Spec.Template.ObjectMeta.Labels).To(HaveKeyWithValue("team", "platform"))
			Expect(dep.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue(testAnnotationKey, "hello"))

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

			By("NodeSelector, Tolerations, and PriorityClassName are set on the PodSpec")
			Expect(dep.Spec.Template.Spec.NodeSelector).To(HaveKeyWithValue("kubernetes.io/arch", "arm64"))
			Expect(dep.Spec.Template.Spec.Tolerations).To(ContainElement(corev1.Toleration{
				Key: "node-role", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule,
			}))
			Expect(dep.Spec.Template.Spec.PriorityClassName).To(Equal("high-priority"))
		})
	})

	Context("Dashboard controller dashboard RBAC test", func() {

		const DashboardName = "test-instance-rbac"

		ctx := context.Background()

		var namespace *corev1.Namespace
		var typeNamespacedName types.NamespacedName

		BeforeEach(func() {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "test-instance-rbac-"},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			typeNamespacedName = types.NamespacedName{Name: DashboardName, Namespace: namespace.Name}
		})

		AfterEach(func() {
			found := &pagev1alpha1.Dashboard{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			if err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("creates a ServiceAccount, Role, and RoleBinding granting the dashboard pod read access to the config CRDs, and no Secret access when nothing references one", func() {
			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.DashboardSpec{Replicas: new(int32(1)), ContainerPort: 8080},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			instanceReconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("a ServiceAccount named after the Dashboard exists and is owned by it")
			sa := &corev1.ServiceAccount{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, sa)).To(Succeed())
			}).Should(Succeed())
			Expect(sa.OwnerReferences).To(ContainElement(HaveField("Name", DashboardName)))

			By("a Role grants get/list/watch on the config CRDs but no Secret access")
			role := &rbacv1.Role{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, role)).To(Succeed())
			}).Should(Succeed())
			Expect(role.Rules).To(ContainElement(HaveField("Resources", ContainElement("servicecards"))))
			Expect(role.Rules).NotTo(ContainElement(HaveField("Resources", ContainElement(resourceSecrets))))

			By("a RoleBinding binds the Role to the ServiceAccount")
			rb := &rbacv1.RoleBinding{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, rb)).To(Succeed())
			}).Should(Succeed())
			Expect(rb.RoleRef.Name).To(Equal(DashboardName))
			Expect(rb.Subjects).To(ContainElement(HaveField("Name", DashboardName)))
		})

		It("scopes the dashboard Role's Secret access to exactly the Secrets its widgets reference", func() {
			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.DashboardSpec{Replicas: new(int32(1)), ContainerPort: 8080},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			url := "http://prometheus.example.com"
			entry := &pagev1alpha1.ServiceCard{
				ObjectMeta: metav1.ObjectMeta{Name: "prom", Namespace: namespace.Name},
				Spec: pagev1alpha1.ServiceCardSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: DashboardName},
					Group:        "Monitoring",
					Services: []pagev1alpha1.ServiceEntry{{
						Name: "Prometheus",
						Widgets: []pagev1alpha1.ServiceWidget{{
							Type: testWidgetTypePrometheus,
							URL:  &url,
							Secrets: map[string]pagev1alpha1.SecretValueSource{
								secretField: {SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "prom-creds"},
									Key:                  secretField,
								}},
							},
						}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, entry)).To(Succeed())

			instanceReconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("the Role grants get on only the referenced Secret, scoped via resourceNames, with no list/watch")
			role := &rbacv1.Role{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, role)).To(Succeed())
				g.Expect(role.Rules).To(ContainElement(SatisfyAll(
					HaveField("Resources", ContainElement(resourceSecrets)),
					HaveField("ResourceNames", ConsistOf("prom-creds")),
					HaveField("Verbs", ConsistOf("get")),
				)))
			}).Should(Succeed())

			By("dropping the widget's secret ref removes the Secret rule on the next reconcile")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "prom", Namespace: namespace.Name}, entry)).To(Succeed())
			entry.Spec.Services[0].Widgets[0].Secrets = nil
			Expect(k8sClient.Update(ctx, entry)).To(Succeed())
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, role)).To(Succeed())
				g.Expect(role.Rules).NotTo(ContainElement(HaveField("Resources", ContainElement(resourceSecrets))))
			}).Should(Succeed())
		})

		It("creates cluster-scoped metrics RBAC only while a kubemetrics InfoWidget is bound", func() {
			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.DashboardSpec{Replicas: new(int32(1)), ContainerPort: 8080},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			widget := &pagev1alpha1.InfoWidget{
				ObjectMeta: metav1.ObjectMeta{Name: "metrics", Namespace: namespace.Name},
				Spec: pagev1alpha1.InfoWidgetSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: DashboardName},
					Widgets: []pagev1alpha1.InfoWidgetEntry{
						{Type: kubeMetricsWidgetType},
					},
				},
			}
			Expect(k8sClient.Create(ctx, widget)).To(Succeed())

			clusterName := clusterRBACName(instance)
			crName := types.NamespacedName{Name: clusterName}

			instanceReconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("a ClusterRole grants node and metrics.k8s.io read access")
			cr := &rbacv1.ClusterRole{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crName, cr)).To(Succeed())
			}).Should(Succeed())
			Expect(cr.Rules).To(ContainElement(HaveField("Resources", ContainElement("nodes"))))
			Expect(cr.Rules).To(ContainElement(HaveField("APIGroups", ContainElement("metrics.k8s.io"))))

			By("a ClusterRoleBinding binds it to the per-Dashboard ServiceAccount")
			crb := &rbacv1.ClusterRoleBinding{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crName, crb)).To(Succeed())
			}).Should(Succeed())
			Expect(crb.RoleRef.Name).To(Equal(clusterName))
			Expect(crb.Subjects).To(ContainElement(SatisfyAll(
				HaveField("Name", DashboardName),
				HaveField("Namespace", namespace.Name),
			)))

			By("removing the widget drops the cluster RBAC on the next reconcile")
			Expect(k8sClient.Delete(ctx, widget)).To(Succeed())
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, crName, cr))).To(BeTrue())
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, crName, crb))).To(BeTrue())
			}).Should(Succeed())
		})

		It("creates a RoleBinding in each spec.discovery.namespaces target, bound to a shared ClusterRole, and removes it when the namespace is dropped", func() {
			target := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-instance-discovery-target-"}}
			Expect(k8sClient.Create(ctx, target)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, target) }()

			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec: pagev1alpha1.DashboardSpec{
					Replicas: new(int32(1)), ContainerPort: 8080,
					Discovery: &pagev1alpha1.DiscoverySpec{
						Enabled:    pagev1alpha1.Enabled,
						Namespaces: []string{target.Name},
					},
				},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			discName := discoveryRBACName(instance)

			instanceReconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			// The first reconcile only creates the Deployment and requeues
			// (reconcileDeployment's not-found branch returns before any
			// Status().Update), so status.discoveryNamespaces isn't
			// persisted yet — a second call, same as the real requeue,
			// carries the reconcile through to the point that commits it.
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("a ClusterRole grants Ingress read access")
			cr := &rbacv1.ClusterRole{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: discName}, cr)).To(Succeed())
			}).Should(Succeed())
			Expect(cr.Rules).To(ContainElement(HaveField("Resources", ContainElement("ingresses"))))

			By("a RoleBinding in the target namespace binds it to the per-Dashboard ServiceAccount")
			rb := &rbacv1.RoleBinding{}
			rbName := types.NamespacedName{Name: discName, Namespace: target.Name}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, rbName, rb)).To(Succeed())
			}).Should(Succeed())
			Expect(rb.RoleRef.Name).To(Equal(discName))
			Expect(rb.Subjects).To(ContainElement(SatisfyAll(
				HaveField("Name", DashboardName),
				HaveField("Namespace", namespace.Name),
			)))

			By("dropping the namespace from spec removes the RoleBinding and ClusterRole on the next reconcile")
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Discovery.Namespaces = nil
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, rbName, rb))).To(BeTrue())
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: discName}, cr))).To(BeTrue())
			}).Should(Succeed())
		})

		It("still tracks and later cleans up a discovery RoleBinding created before a later namespace in the list fails", func() {
			good := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "aaa-discovery-good-"}}
			Expect(k8sClient.Create(ctx, good)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, good) }()

			// discoveryNamespaces sorts its output, and "zzz-..." sorts after
			// "aaa-...", so the good namespace's RoleBinding is created
			// before the nonexistent one is attempted and fails — the
			// partial-failure case status.discoveryNamespaces has to survive
			// for cleanup to ever find the good namespace's RoleBinding
			// again (see reconcileDiscoveryRBAC's unionSortedDeduped call).
			const nonexistentNamespace = "zzz-discovery-does-not-exist"

			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec: pagev1alpha1.DashboardSpec{
					Replicas: new(int32(1)), ContainerPort: 8080,
					Discovery: &pagev1alpha1.DiscoverySpec{
						Enabled:    pagev1alpha1.Enabled,
						Namespaces: []string{good.Name, nonexistentNamespace},
					},
				},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			discName := discoveryRBACName(instance)
			rbName := types.NamespacedName{Name: discName, Namespace: good.Name}

			instanceReconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
			// First reconcile creates the Deployment and requeues before
			// reaching RBAC's Status().Update path in a happy case, but here
			// reconcileDiscoveryRBAC itself fails and persists status via
			// failAvailable *before* the Deployment step ever runs — so one
			// call suffices to both attempt the RoleBindings and persist
			// status.discoveryNamespaces.
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())

			By("the good namespace's RoleBinding was created despite the later failure")
			rb := &rbacv1.RoleBinding{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, rbName, rb)).To(Succeed())
			}).Should(Succeed())

			By("status.discoveryNamespaces tracks the good namespace even though reconciliation failed")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
				g.Expect(instance.Status.DiscoveryNamespaces).To(ContainElement(good.Name))
			}).Should(Succeed())

			By("fixing spec to drop the nonexistent namespace lets the good RoleBinding be cleaned up once removed entirely")
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Discovery.Namespaces = nil
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, rbName, rb))).To(BeTrue())
			}).Should(Succeed())
		})
	})

	Context("Dashboard controller exposure and status test", func() {

		const DashboardName = "test-instance-exposure"

		ctx := context.Background()

		var namespace *corev1.Namespace

		typeNamespacedName := types.NamespacedName{
			Name:      DashboardName,
			Namespace: DashboardName,
		}

		BeforeEach(func() {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "test-instance-exposure-"},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			typeNamespacedName = types.NamespacedName{Name: DashboardName, Namespace: namespace.Name}
		})

		AfterEach(func() {
			found := &pagev1alpha1.Dashboard{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			if err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("creates a Service fronting the Deployment and populates bound-count status", func() {
			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.DashboardSpec{Replicas: new(int32(1)), ContainerPort: 8080},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			bm := &pagev1alpha1.Bookmark{
				ObjectMeta: metav1.ObjectMeta{Name: "bm", Namespace: namespace.Name},
				Spec: pagev1alpha1.BookmarkSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: DashboardName},
					Group:        "Developer",
					Bookmarks: []pagev1alpha1.BookmarkEntry{
						{Name: "Github", Href: "https://github.com/"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, bm)).To(Succeed())

			entry := &pagev1alpha1.ServiceCard{
				ObjectMeta: metav1.ObjectMeta{Name: testServiceCardObjName, Namespace: namespace.Name},
				Spec: pagev1alpha1.ServiceCardSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: DashboardName},
					Group:        testMultiFormGroupMedia,
					Services: []pagev1alpha1.ServiceEntry{
						{Name: "Sonarr"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, entry)).To(Succeed())

			instanceReconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
			// The first Reconcile creates the Deployment and returns early
			// (reconcileDeployment's "handled" requeue path); Service/Ingress
			// reconciliation and status population happen after that point,
			// so a second call is needed for them to run.
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("a ClusterIP Service is created selecting the Dashboard's pods")
			svc := &corev1.Service{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, svc)).To(Succeed())
			}).Should(Succeed())
			Expect(svc.Spec.Ports).To(ContainElement(HaveField("Port", int32(8080))))
			Expect(svc.Spec.Selector).To(Equal(selectorLabelsForDashboard()))

			By("status reflects the bound Bookmark and ServiceCard counts")
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			Expect(instance.Status.BoundBookmarks).To(Equal(int32(1)))
			Expect(instance.Status.BoundServiceCards).To(Equal(int32(1)))
			Expect(instance.Status.BoundDashboardStyles).To(Equal(int32(0)))
			Expect(instance.Status.ObservedGeneration).To(Equal(instance.Generation))
		})

		It("creates an Ingress when spec.ingress.enabled is true and removes it when toggled off", func() {
			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec: pagev1alpha1.DashboardSpec{
					Replicas:      new(int32(1)),
					ContainerPort: 8080,
					Ingress: &pagev1alpha1.IngressSpec{
						Enabled: pagev1alpha1.Enabled,
						Host:    testDashboardHost,
						TLS:     &pagev1alpha1.IngressTLSSpec{SecretName: "dashboard-tls"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			instanceReconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
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
			Expect(ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(Equal(DashboardName))
			Expect(ing.Spec.TLS).To(ContainElement(HaveField("SecretName", "dashboard-tls")))

			By("disabling spec.ingress.enabled removes the Ingress")
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Ingress.Enabled = pagev1alpha1.Disabled
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, typeNamespacedName, &networkingv1.Ingress{}))).To(BeTrue())
			}).Should(Succeed())
		})

		It("updates the existing Ingress in place when spec.ingress fields drift", func() {
			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec: pagev1alpha1.DashboardSpec{
					Replicas:      new(int32(1)),
					ContainerPort: 8080,
					Ingress: &pagev1alpha1.IngressSpec{
						Enabled: pagev1alpha1.Enabled,
						Host:    testDashboardHost,
					},
				},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			instanceReconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			ing := &networkingv1.Ingress{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, ing)).To(Succeed())
			}).Should(Succeed())
			Expect(ing.Spec.Rules[0].Host).To(Equal(testDashboardHost))
			originalUID := ing.UID

			By("changing spec.ingress.host updates the existing Ingress rather than recreating it")
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Ingress.Host = "drifted.example.com"
			instance.Spec.Ingress.Annotations = map[string]string{testAnnotationKey: "updated"}
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, ing)).To(Succeed())
				g.Expect(ing.Spec.Rules[0].Host).To(Equal("drifted.example.com"))
				g.Expect(ing.Annotations).To(HaveKeyWithValue(testAnnotationKey, "updated"))
			}).Should(Succeed())
			Expect(ing.UID).To(Equal(originalUID), "drift should update the existing Ingress, not recreate it")
		})
	})

	Context("Dashboard controller Gateway API test", func() {

		const DashboardName = "test-instance-gateway"

		ctx := context.Background()

		var namespace *corev1.Namespace
		var typeNamespacedName types.NamespacedName

		BeforeEach(func() {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "test-instance-gateway-"},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			typeNamespacedName = types.NamespacedName{Name: DashboardName, Namespace: namespace.Name}
		})

		AfterEach(func() {
			found := &pagev1alpha1.Dashboard{}
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
			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec: pagev1alpha1.DashboardSpec{
					Replicas:      new(int32(1)),
					ContainerPort: 8080,
					Gateway: &pagev1alpha1.GatewaySpec{
						Enabled:   pagev1alpha1.Enabled,
						Hostnames: []string{testDashboardHost},
						ParentRef: pagev1alpha1.GatewayParentRef{Name: "eg"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			instanceReconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage, GatewayAPIEnabled: false}
			// See the comment on the Ingress test: the first Reconcile only
			// creates the Deployment and returns early.
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(MatchError(errGatewayAPINotInstalled))

			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			var conditions []metav1.Condition
			Expect(instance.Status.Conditions).To(ContainElement(
				HaveField("Type", Equal(typeAvailableDashboard)), &conditions))
			Expect(conditions[len(conditions)-1].Status).To(Equal(metav1.ConditionFalse))
			Expect(conditions[len(conditions)-1].Message).To(ContainSubstring("Gateway API CRDs are not installed"))
		})

		It("is a no-op when spec.gateway is unset, regardless of GatewayAPIEnabled", func() {
			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.DashboardSpec{Replicas: new(int32(1)), ContainerPort: 8080},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			instanceReconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage, GatewayAPIEnabled: false}
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			markDeploymentReady(ctx, typeNamespacedName, 1)
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			var conditions []metav1.Condition
			Expect(instance.Status.Conditions).To(ContainElement(
				HaveField("Type", Equal(typeAvailableDashboard)), &conditions))
			Expect(conditions[len(conditions)-1].Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("Dashboard controller discovery HTTPRoute source test", func() {

		const DashboardName = "test-instance-discovery-httproute"

		ctx := context.Background()

		var namespace *corev1.Namespace
		var typeNamespacedName types.NamespacedName

		BeforeEach(func() {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "test-instance-discovery-httproute-"},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			typeNamespacedName = types.NamespacedName{Name: DashboardName, Namespace: namespace.Name}
		})

		AfterEach(func() {
			found := &pagev1alpha1.Dashboard{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			if err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("reports a clear Available=False condition when discovery.sources includes HTTPRoute but Gateway API CRDs aren't installed", func() {
			// envtest only installs this project's own CRDs (see suite_test.go),
			// so GatewayAPIEnabled is always false here — mirrors the
			// spec.gateway/errGatewayAPINotInstalled test above.
			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec: pagev1alpha1.DashboardSpec{
					Replicas:      ptr.To(int32(1)),
					ContainerPort: 8080,
					Discovery: &pagev1alpha1.DiscoverySpec{
						Enabled: pagev1alpha1.Enabled,
						Sources: []string{pagev1alpha1.DiscoverySourceIngress, pagev1alpha1.DiscoverySourceHTTPRoute},
					},
				},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			instanceReconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage, GatewayAPIEnabled: false}
			// Mirrors the spec.gateway test above: the first Reconcile only
			// creates the Deployment and returns early, so the Dashboard
			// still gets a running pod even though discovery.sources can't
			// be fully satisfied — see discoveryHTTPRouteAvailable's doc
			// comment on why this check runs after the Deployment/Service/
			// Ingress/HTTPRoute reconcile rather than short-circuiting it.
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			markDeploymentReady(ctx, typeNamespacedName, 1)
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(MatchError(errDiscoveryHTTPRouteUnavailable))

			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			var conditions []metav1.Condition
			Expect(instance.Status.Conditions).To(ContainElement(
				HaveField("Type", Equal(typeAvailableDashboard)), &conditions))
			Expect(conditions[len(conditions)-1].Status).To(Equal(metav1.ConditionFalse))
			Expect(conditions[len(conditions)-1].Reason).To(Equal(reasonDiscoveryHTTPRouteUnavailable))

			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
		})

		// The RBAC-granting and RBAC-omitting positive paths for
		// discovery.sources are covered as pure unit tests against
		// dashboardRoles directly (dashboard_rbac_test.go) rather than here:
		// envtest only installs this project's own CRDs (see
		// suite_test.go), and reconcileHTTPRoute (called on every Reconcile
		// once GatewayAPIEnabled is true, regardless of spec.gateway) itself
		// Gets the HTTPRoute Kind to decide whether to delete a stale one —
		// which fails outright against envtest's scheme/apiserver when
		// Gateway API CRDs aren't actually present, the same constraint
		// that keeps the spec.gateway tests above from exercising a "Gateway
		// API really is installed" envtest case either.
	})

	Context("Dashboard controller image drift test", func() {

		const DashboardName = "test-instance-image-drift"

		ctx := context.Background()

		var namespace *corev1.Namespace
		var typeNamespacedName types.NamespacedName

		BeforeEach(func() {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "test-instance-image-drift-"},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			typeNamespacedName = types.NamespacedName{Name: DashboardName, Namespace: namespace.Name}
		})

		AfterEach(func() {
			found := &pagev1alpha1.Dashboard{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			if err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("rolls the Deployment when DashboardImage changes between reconciles (operator upgrade)", func() {
			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.DashboardSpec{Replicas: new(int32(1)), ContainerPort: 8080},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			reconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
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

		It("rolls the Deployment when other spec fields (env, resources, labels) drift", func() {
			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.DashboardSpec{Replicas: new(int32(1)), ContainerPort: 8080},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			reconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			dep := &appsv1.Deployment{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, dep)).To(Succeed())
			}).Should(Succeed())
			Expect(dep.Spec.Template.Spec.Containers[0].Env).To(BeEmpty())
			Expect(dep.Spec.Template.Labels).NotTo(HaveKey("custom-label"))

			By("editing spec.env, spec.resources, and spec.labels on the Dashboard")
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Env = []corev1.EnvVar{{Name: "FOO", Value: "bar"}}
			instance.Spec.Resources = corev1.ResourceRequirements{
				Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
			}
			instance.Spec.Labels = map[string]string{"custom-label": "yes"}
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, dep)).To(Succeed())
				g.Expect(dep.Spec.Template.Spec.Containers[0].Env).To(ConsistOf(corev1.EnvVar{Name: "FOO", Value: "bar"}))
				g.Expect(dep.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU]).To(Equal(resource.MustParse("500m")))
				g.Expect(dep.Spec.Template.Labels).To(HaveKeyWithValue("custom-label", "yes"))
			}).Should(Succeed())
		})

		It("does not perpetually re-reconcile a Deployment whose stored Volumes were server-defaulted, and still detects a real volume change", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "ca-bundle", Namespace: namespace.Name},
				Data:       map[string]string{"ca.crt": "dummy"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec: pagev1alpha1.DashboardSpec{
					Replicas: new(int32(1)), ContainerPort: 8080,
					// No defaultMode set — the API server fills one in on
					// the stored object (ConfigMapVolumeSource defaults to
					// 0644), which a naive reflect.DeepEqual against the
					// raw desired spec would see as permanent drift.
					Volumes: []corev1.Volume{{
						Name:         "ca-bundle",
						VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: cm.Name}}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			reconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			dep := &appsv1.Deployment{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, dep)).To(Succeed())
				g.Expect(dep.Spec.Template.Spec.Volumes).To(ContainElement(HaveField("Name", "ca-bundle")))
			}).Should(Succeed())
			resourceVersionAfterCreate := dep.ResourceVersion

			By("reconciling again sees no drift from the server-defaulted Volumes and does not update the Deployment")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, dep)).To(Succeed())
			Expect(dep.ResourceVersion).To(Equal(resourceVersionAfterCreate), "reconciling with an unchanged Volumes spec should not update the Deployment")

			By("a real Volumes change is still detected as drift")
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Volumes = append(instance.Spec.Volumes, corev1.Volume{
				Name:         "extra",
				VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
			})
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, dep)).To(Succeed())
				g.Expect(dep.Spec.Template.Spec.Volumes).To(ContainElement(HaveField("Name", "extra")))
			}).Should(Succeed())
		})
	})

	Context("Dashboard controller finalizer test", func() {

		const DashboardName = "test-instance-finalizer"

		ctx := context.Background()

		var namespace *corev1.Namespace
		var typeNamespacedName types.NamespacedName

		BeforeEach(func() {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "test-instance-finalizer-"},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			typeNamespacedName = types.NamespacedName{Name: DashboardName, Namespace: namespace.Name}
		})

		AfterEach(func() {
			found := &pagev1alpha1.Dashboard{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			if err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("runs finalizer cleanup (deleting cluster-scoped metrics RBAC) and removes the finalizer on deletion", func() {
			instance := &pagev1alpha1.Dashboard{
				ObjectMeta: metav1.ObjectMeta{Name: DashboardName, Namespace: namespace.Name},
				Spec:       pagev1alpha1.DashboardSpec{Replicas: new(int32(1)), ContainerPort: 8080},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			widget := &pagev1alpha1.InfoWidget{
				ObjectMeta: metav1.ObjectMeta{Name: "metrics", Namespace: namespace.Name},
				Spec: pagev1alpha1.InfoWidgetSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: DashboardName},
					Widgets: []pagev1alpha1.InfoWidgetEntry{
						{Type: kubeMetricsWidgetType},
					},
				},
			}
			Expect(k8sClient.Create(ctx, widget)).To(Succeed())

			clusterName := clusterRBACName(instance)
			crName := types.NamespacedName{Name: clusterName}

			instanceReconciler := &DashboardReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), DashboardImage: testDashboardImage, Recorder: events.NewFakeRecorder(10)}

			By("the first reconcile adds the finalizer and creates cluster-scoped metrics RBAC")
			_, err := instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
				g.Expect(controllerutil.ContainsFinalizer(instance, instanceFinalizer)).To(BeTrue())
			}).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crName, &rbacv1.ClusterRole{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, crName, &rbacv1.ClusterRoleBinding{})).To(Succeed())
			}).Should(Succeed())

			By("deleting the Dashboard leaves it present (blocked by the finalizer) with a deletion timestamp")
			Expect(k8sClient.Delete(ctx, instance)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
				g.Expect(instance.GetDeletionTimestamp()).NotTo(BeNil())
			}).Should(Succeed())

			By("reconciling the deletion runs the finalizer, removes the cluster RBAC, and lets the API server remove the Dashboard")
			_, err = instanceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, typeNamespacedName, &pagev1alpha1.Dashboard{}))).To(BeTrue())
			}).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, crName, &rbacv1.ClusterRole{}))).To(BeTrue())
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, crName, &rbacv1.ClusterRoleBinding{}))).To(BeTrue())
			}).Should(Succeed())
		})
	})
})

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

var _ = Describe("Bookmark Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-resource"
			resourceNamespace = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}
		bookmark := &pagev1alpha1.Bookmark{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Bookmark")
			err := k8sClient.Get(ctx, typeNamespacedName, bookmark)
			if err != nil && errors.IsNotFound(err) {
				resource := &pagev1alpha1.Bookmark{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: resourceNamespace,
					},
					Spec: pagev1alpha1.BookmarkSpec{
						DashboardRef: pagev1alpha1.DashboardRef{Name: testDoesNotExistDashboardName},
						Group:        "Group",
						Bookmarks: []pagev1alpha1.BookmarkEntry{
							{Name: "Name", Href: "https://example.com"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &pagev1alpha1.Bookmark{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Bookmark")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &BookmarkReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})

	Context("href scheme validation", func() {
		const hrefTestNamespace = "default"

		ctx := context.Background()

		bookmarkWithHref := func(name, href string) *pagev1alpha1.Bookmark {
			return &pagev1alpha1.Bookmark{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: hrefTestNamespace},
				Spec: pagev1alpha1.BookmarkSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: testDoesNotExistDashboardName},
					Group:        "HrefTest",
					Bookmarks:    []pagev1alpha1.BookmarkEntry{{Name: "Entry", Href: href}},
				},
			}
		}

		DescribeTable("accepted schemes",
			func(name, href string) {
				bm := bookmarkWithHref(name, href)
				Expect(k8sClient.Create(ctx, bm)).To(Succeed())
				Expect(k8sClient.Delete(ctx, bm)).To(Succeed())
			},
			Entry("https", "href-ok-https", "https://example.com/"),
			Entry("http", "href-ok-http", "http://example.com/"),
			Entry("mailto", "href-ok-mailto", "mailto:admin@example.com"),
			Entry("tel", "href-ok-tel", "tel:+15551234567"),
			Entry("ssh", "href-ok-ssh", "ssh://box.example.com/"),
			Entry("rdp", "href-ok-rdp", "rdp://box.example.com/"),
			Entry("smb", "href-ok-smb", "smb://nas.example.com/share"),
		)

		DescribeTable("rejected schemes",
			func(name, href string) {
				bm := bookmarkWithHref(name, href)
				err := k8sClient.Create(ctx, bm)
				Expect(err).To(HaveOccurred())
				Expect(errors.IsInvalid(err)).To(BeTrue())
			},
			Entry("javascript", "href-bad-javascript", "javascript:alert(1)"),
			Entry("data", "href-bad-data", "data:text/html,<script>alert(1)</script>"),
			Entry("vbscript", "href-bad-vbscript", "vbscript:msgbox(1)"),
		)
	})
})

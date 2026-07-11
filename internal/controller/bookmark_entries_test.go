package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// This spec verifies that BookmarkReconciler sets status.entries to the
// number of entries BookmarkSpec.Entries() resolves (len(spec.bookmarks)),
// so `kubectl get pbmk`'s Entries printcolumn reads correctly.
var _ = Describe("Bookmark status.entries", func() {
	ctx := context.Background()

	It("sets entries to len(bookmarks)", func() {
		name := "bm-entries-multi"
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDoesNotExistDashboardName},
				Group:        policyTestGroup,
				Bookmarks: []pagev1alpha1.BookmarkEntry{
					{Name: testBookmarkNameGithub, Href: testBookmarkHrefExample},
					{Name: testBookmarkNameWikipedia, Href: testBookmarkHrefExample},
				},
			},
		}
		Expect(k8sClient.Create(ctx, bm)).To(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, bm)).To(Succeed()) }()

		reconciler := &BookmarkReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: policyTestNamespace}})
		Expect(err).NotTo(HaveOccurred())

		got := &pagev1alpha1.Bookmark{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: policyTestNamespace}, got)).To(Succeed())
		Expect(got.Status.Entries).To(Equal(int32(2)))
	})
})

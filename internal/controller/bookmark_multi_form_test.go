package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify the BookmarkSpec CRD schema (api/v1alpha1/
// bookmark_types.go): bookmarks is required, each entry requires name and
// href, and every entry must resolve a group either from its own group or
// spec.group's default (enforced by the type's one remaining XValidation
// rule).
var _ = Describe("Bookmark CRD schema validation", func() {
	It("admits bookmarks with a top-level default group", func() {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-multi-default-group", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Bookmarks: []pagev1alpha1.BookmarkEntry{
					{Name: testBookmarkNameGithub, Href: testBookmarkHrefExample},
					{Name: testBookmarkNameWikipedia, Href: testBookmarkHrefExample},
				},
			},
		}
		Expect(k8sClient.Create(ctx, bm)).To(Succeed())
		Expect(k8sClient.Delete(ctx, bm)).To(Succeed())
	})

	It("admits bookmarks with per-entry groups and no top-level group", func() {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-multi-per-entry-group", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Bookmarks: []pagev1alpha1.BookmarkEntry{
					{Name: testBookmarkNameGithub, Href: testBookmarkHrefExample, Group: testMultiFormGroupMedia},
					{Name: testBookmarkNameWikipedia, Href: testBookmarkHrefExample, Group: "Reference"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, bm)).To(Succeed())
		Expect(k8sClient.Delete(ctx, bm)).To(Succeed())
	})

	It("rejects a Bookmark with no bookmarks set", func() {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-no-bookmarks", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
			},
		}
		err := k8sClient.Create(ctx, bm)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects a bookmarks entry missing name", func() {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-entry-no-name", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Bookmarks:    []pagev1alpha1.BookmarkEntry{{Href: testBookmarkHrefExample}},
			},
		}
		err := k8sClient.Create(ctx, bm)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects a bookmarks entry missing href", func() {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-entry-no-href", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Bookmarks:    []pagev1alpha1.BookmarkEntry{{Name: testBookmarkNameGithub}},
			},
		}
		err := k8sClient.Create(ctx, bm)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects bookmarks when no group is resolvable anywhere", func() {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-multi-no-group", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Bookmarks: []pagev1alpha1.BookmarkEntry{
					{Name: testBookmarkNameGithub, Href: testBookmarkHrefExample, Group: testMultiFormGroupMedia},
					{Name: testBookmarkNameWikipedia, Href: testBookmarkHrefExample}, // no own group, and spec.group is unset
				},
			},
		}
		err := k8sClient.Create(ctx, bm)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})
})

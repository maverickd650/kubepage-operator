package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// These specs verify the BookmarkSpec CRD schema CEL rules
// (api/v1alpha1/bookmark_types.go's XValidation markers on that struct)
// that let a Bookmark choose between the single-bookmark form (name/href set
// directly on spec, unchanged from earlier versions of this API) and the
// multi-bookmark form (spec.bookmarks, a list of BookmarkEntry) — but never
// both, and never neither.
var _ = Describe("Bookmark single-vs-multi form CRD schema validation", func() {
	It("admits the single-bookmark form (name + href + group, no bookmarks)", func() {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-single-ok", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Name:         testBookmarkNameGithub,
				Href:         testBookmarkHrefExample,
			},
		}
		Expect(k8sClient.Create(ctx, bm)).To(Succeed())
		Expect(k8sClient.Delete(ctx, bm)).To(Succeed())
	})

	It("admits the multi-bookmark form with a top-level default group", func() {
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

	It("admits the multi-bookmark form with per-entry groups and no top-level group", func() {
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

	It("rejects both name and bookmarks set", func() {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-both-forms", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Name:         testBookmarkNameGithub,
				Href:         testBookmarkHrefExample,
				Bookmarks:    []pagev1alpha1.BookmarkEntry{{Name: testBookmarkNameWikipedia, Href: testBookmarkHrefExample}},
			},
		}
		err := k8sClient.Create(ctx, bm)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects neither name nor bookmarks set", func() {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-neither-form", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
			},
		}
		err := k8sClient.Create(ctx, bm)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects bookmarks set alongside an inline single-bookmark field (href)", func() {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-bookmarks-plus-href", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Href:         testBookmarkHrefExample,
				Bookmarks:    []pagev1alpha1.BookmarkEntry{{Name: testBookmarkNameGithub, Href: testBookmarkHrefExample}},
			},
		}
		err := k8sClient.Create(ctx, bm)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects bookmarks set alongside an inline single-bookmark field (abbr)", func() {
		abbr := "GH"
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-bookmarks-plus-abbr", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Abbr:         &abbr,
				Bookmarks:    []pagev1alpha1.BookmarkEntry{{Name: testBookmarkNameGithub, Href: testBookmarkHrefExample}},
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

	It("rejects the multi-bookmark form when no group is resolvable anywhere", func() {
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

	It("rejects the single-bookmark form missing name", func() {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-single-no-name", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Href:         testBookmarkHrefExample,
			},
		}
		err := k8sClient.Create(ctx, bm)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects the single-bookmark form missing href", func() {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-single-no-href", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Group:        policyTestGroup,
				Name:         testBookmarkNameGithub,
			},
		}
		err := k8sClient.Create(ctx, bm)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("rejects the single-bookmark form missing group", func() {
		bm := &pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bm-single-no-group", Namespace: policyTestNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
				Name:         testBookmarkNameGithub,
				Href:         testBookmarkHrefExample,
			},
		}
		err := k8sClient.Create(ctx, bm)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})
})

package dashboard

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

func TestLoadSiteDefaults(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	site, err := LoadSite(context.Background(), cl, testNamespace, testInstanceName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if site.Theme != defaultTheme || site.Color != defaultColor || site.HeaderStyle != defaultHeaderStyle {
		t.Errorf("defaults = %+v, want %s/%s/%s", site, defaultTheme, defaultColor, defaultHeaderStyle)
	}
	if site.Search.Provider != "duckduckgo" || site.Search.Target != "_blank" || !site.Search.FilterCards {
		t.Errorf("search defaults = %+v", site.Search)
	}
	if len(site.BookmarkGroups) != 0 {
		t.Errorf("BookmarkGroups = %+v, want empty", site.BookmarkGroups)
	}
}

func TestLoadSitePicksLexicographicallyFirstConfiguration(t *testing.T) {
	scheme := testScheme(t)
	themeB := "light"
	themeA := defaultTheme
	cfgB := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: "z-cfg", Namespace: testNamespace},
		Spec:       pagev1alpha1.ConfigurationSpec{InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName}, Theme: &themeB},
	}
	cfgA := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: "a-cfg", Namespace: testNamespace},
		Spec:       pagev1alpha1.ConfigurationSpec{InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName}, Theme: &themeA},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfgB, cfgA).Build()

	site, err := LoadSite(context.Background(), cl, testNamespace, testInstanceName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if site.Theme != defaultTheme {
		t.Errorf("Theme = %q, want %q (lexicographically-first Configuration by name)", site.Theme, defaultTheme)
	}
}

func TestLoadSiteGroupsBookmarksByGroupAndOrder(t *testing.T) {
	scheme := testScheme(t)
	order1 := int32(1)
	order2 := int32(2)
	bm1 := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "bm1", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       testBookmarkGroup, Name: "Second", Href: "https://example.invalid/2", Order: &order2,
		},
	}
	bm2 := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "bm2", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       testBookmarkGroup, Name: "First", Href: "https://example.invalid/1", Order: &order1,
		},
	}
	other := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "bm3", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: "not-" + testInstanceName},
			Group:       testBookmarkGroup, Name: "Skip me", Href: "https://example.invalid/skip",
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bm1, bm2, other).Build()

	site, err := LoadSite(context.Background(), cl, testNamespace, testInstanceName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if len(site.BookmarkGroups) != 1 || site.BookmarkGroups[0].Name != testBookmarkGroup {
		t.Fatalf("BookmarkGroups = %+v", site.BookmarkGroups)
	}
	bms := site.BookmarkGroups[0].Bookmarks
	if len(bms) != 2 || bms[0].Name != "First" || bms[1].Name != "Second" {
		t.Errorf("Bookmarks = %+v, want First then Second (ordered by Order)", bms)
	}
}

package dashboard

import (
	"context"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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

func TestLoadSiteAppliesLookFields(t *testing.T) {
	scheme := testScheme(t)
	title := "Home Lab"
	desc := "My services"
	favicon := "https://example.invalid/favicon.ico"
	cardBlur := "md"
	target := "_self"
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Title:       &title,
			Description: &desc,
			Favicon:     &favicon,
			CardBlur:    &cardBlur,
			Target:      &target,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(context.Background(), cl, testNamespace, testInstanceName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if site.Title != title || site.Description != desc || site.Favicon != favicon || site.Target != target {
		t.Errorf("look fields = %+v", site)
	}
	if want := blurPx(cardBlur); site.CardBlur != want {
		t.Errorf("CardBlur = %q, want %q (%s keyword)", site.CardBlur, want, cardBlur)
	}
}

func TestLoadSiteHeaderWidgetsOrdered(t *testing.T) {
	scheme := testScheme(t)
	order1 := int32(1)
	order2 := int32(2)
	greeting := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "greet", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Type:        headerTypeGreeting,
			Order:       &order2,
			Options:     &apiextensionsv1.JSON{Raw: []byte(`{"text":"Hello"}`)},
		},
	}
	clock := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "clock", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Type:        headerTypeDatetime,
			Order:       &order1,
		},
	}
	other := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "skip", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: "not-" + testInstanceName},
			Type:        headerTypeGreeting,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(greeting, clock, other).Build()

	site, err := LoadSite(context.Background(), cl, testNamespace, testInstanceName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if len(site.HeaderWidgets) != 2 {
		t.Fatalf("HeaderWidgets = %+v, want 2 (bound only)", site.HeaderWidgets)
	}
	if site.HeaderWidgets[0].Type != headerTypeDatetime || site.HeaderWidgets[1].Type != headerTypeGreeting {
		t.Errorf("HeaderWidgets order = %+v, want datetime then greeting (by Order)", site.HeaderWidgets)
	}
	if site.HeaderWidgets[1].Options["text"] != "Hello" {
		t.Errorf("greeting options = %+v, want text=Hello", site.HeaderWidgets[1].Options)
	}
}

func TestLoadSiteAppliesLayout(t *testing.T) {
	scheme := testScheme(t)
	cols := int32(4)
	style := testStyleRow
	icon := "grafana"
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Layout: []pagev1alpha1.LayoutTabSpec{
				{
					Name: testInfraTab,
					Groups: []pagev1alpha1.LayoutGroupSpec{
						{Name: testGroup, Columns: &cols, Style: &style, Icon: &icon},
					},
				},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(context.Background(), cl, testNamespace, testInstanceName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if len(site.Layout) != 1 || site.Layout[0].Name != testInfraTab {
		t.Fatalf("Layout = %+v, want one tab named Infrastructure", site.Layout)
	}
	groups := site.Layout[0].Groups
	if len(groups) != 1 || groups[0].Name != testGroup {
		t.Fatalf("Layout[0].Groups = %+v", groups)
	}
	g := groups[0]
	if g.Columns == nil || *g.Columns != cols || g.Style != style || g.IconURL != IconURL(&icon) {
		t.Errorf("Layout[0].Groups[0] = %+v, want columns=4 style=row iconURL=%s", g, IconURL(&icon))
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

func TestBlurPx(t *testing.T) {
	tests := map[string]struct{ keyword, want string }{
		"keyword none":    {keyword: "none", want: ""},
		"default":         {keyword: "", want: "8px"},
		"keyword sm":      {keyword: "sm", want: "4px"},
		"keyword md":      {keyword: "md", want: "12px"},
		"keyword lg":      {keyword: "lg", want: "16px"},
		"keyword xl":      {keyword: "xl", want: "24px"},
		"keyword 2xl":     {keyword: "2xl", want: "40px"},
		"keyword 3xl":     {keyword: "3xl", want: "64px"},
		"unknown keyword": {keyword: "not-a-size", want: ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := blurPx(tc.keyword); got != tc.want {
				t.Errorf("blurPx(%q) = %q, want %q", tc.keyword, got, tc.want)
			}
		})
	}
}

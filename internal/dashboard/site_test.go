package dashboard

import (
	"errors"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

func TestLoadSiteDefaults(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testInstanceName)
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

func TestLoadSiteListError(t *testing.T) {
	tests := map[string]struct {
		failList func(list client.ObjectList) bool
		wantErr  string
	}{
		"Configurations list fails": {
			failList: func(list client.ObjectList) bool { _, ok := list.(*pagev1alpha1.ConfigurationList); return ok },
			wantErr:  "listing Configurations",
		},
		"Bookmarks list fails": {
			failList: func(list client.ObjectList) bool { _, ok := list.(*pagev1alpha1.BookmarkList); return ok },
			wantErr:  "listing Bookmarks",
		},
		"InfoWidgets list fails": {
			failList: func(list client.ObjectList) bool { _, ok := list.(*pagev1alpha1.InfoWidgetList); return ok },
			wantErr:  "listing InfoWidgets",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			scheme := testScheme(t)
			cl := fake.NewClientBuilder().WithScheme(scheme).Build()
			reader := errInjectingReader{Reader: cl, failList: tc.failList}

			_, err := LoadSite(t.Context(), reader, testNamespace, testInstanceName)
			if err == nil {
				t.Fatal("LoadSite() error = nil, want non-nil")
			}
			if !errors.Is(err, errBoom) {
				t.Errorf("LoadSite() error = %v, want wrapping errBoom", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("LoadSite() error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestLoadSitePicksLexicographicallyFirstConfiguration(t *testing.T) {
	scheme := testScheme(t)
	themeB := themeLight
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

	site, err := LoadSite(t.Context(), cl, testNamespace, testInstanceName)
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
	target := testTargetSelf
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

	site, err := LoadSite(t.Context(), cl, testNamespace, testInstanceName)
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
		ObjectMeta: metav1.ObjectMeta{Name: testClockName, Namespace: testNamespace},
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

	site, err := LoadSite(t.Context(), cl, testNamespace, testInstanceName)
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

	site, err := LoadSite(t.Context(), cl, testNamespace, testInstanceName)
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

	site, err := LoadSite(t.Context(), cl, testNamespace, testInstanceName)
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

func TestLoadSiteThemeFixedAndColorFixed(t *testing.T) {
	scheme := testScheme(t)
	theme := themeLight
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Theme:       &theme,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testInstanceName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if !site.ThemeFixed {
		t.Error("ThemeFixed = false, want true when Configuration.Theme is set")
	}
	if site.ColorFixed {
		t.Error("ColorFixed = true, want false when Configuration.Color is unset")
	}
}

func TestLoadSiteAppliesNewLookFields(t *testing.T) {
	scheme := testScheme(t)
	disableCollapse := pagev1alpha1.Disabled
	groupsCollapsed := pagev1alpha1.CollapseCollapsed
	equalHeights := pagev1alpha1.HeightsEqual
	bookmarksStyle := bookmarksStyleIcons
	disableIndexing := pagev1alpha1.IndexingNoIndex
	startURL := "/dash"
	customCSS := "body{color:red}"
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef:              pagev1alpha1.InstanceRef{Name: testInstanceName},
			DisableCollapse:          &disableCollapse,
			GroupsInitiallyCollapsed: &groupsCollapsed,
			UseEqualHeights:          &equalHeights,
			BookmarksStyle:           &bookmarksStyle,
			DisableIndexing:          &disableIndexing,
			StartURL:                 &startURL,
			CustomCSS:                &customCSS,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testInstanceName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if !site.DisableCollapse || !site.GroupsInitiallyCollapsed || !site.UseEqualHeights ||
		!site.BookmarksIconsOnly || !site.DisableIndexing || site.StartURL != startURL || site.CustomCSS != customCSS {
		t.Errorf("new look fields = %+v", site)
	}
}

func TestLoadSiteAppliesLayoutGroupOverrides(t *testing.T) {
	scheme := testScheme(t)
	header := pagev1alpha1.HeaderHidden
	initiallyCollapsed := pagev1alpha1.CollapseCollapsed
	equalHeights := pagev1alpha1.HeightsEqual
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Layout: []pagev1alpha1.LayoutTabSpec{
				{
					Name: testInfraTab,
					Groups: []pagev1alpha1.LayoutGroupSpec{
						{Name: testGroup, Header: &header, InitiallyCollapsed: &initiallyCollapsed, UseEqualHeights: &equalHeights},
					},
				},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testInstanceName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	g := site.Layout[0].Groups[0]
	if g.Header == nil || *g.Header || g.InitiallyCollapsed == nil || !*g.InitiallyCollapsed || g.UseEqualHeights == nil || !*g.UseEqualHeights {
		t.Errorf("Layout[0].Groups[0] overrides = %+v, want Header=false InitiallyCollapsed=true UseEqualHeights=true", g)
	}
}

// TestGroupBookmarksGroupOrderImprovesFromALaterEntry covers groupBookmarks'
// branch where a group's effective Order is set by its first-seen bookmark
// but then improves because a later bookmark in the same group has a lower
// Order — every other groupBookmarks test only ever has one bookmark per
// group, so that update path (the `else if compareOrder(...) < 0` branch in
// site.go) was never actually exercised.
func TestGroupBookmarksGroupOrderImprovesFromALaterEntry(t *testing.T) {
	order5 := int32(5)
	order1 := int32(1)
	abbr := "GH"
	desc := "Code hosting"
	items := []pagev1alpha1.Bookmark{
		{
			Spec: pagev1alpha1.BookmarkSpec{
				InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
				Group:       testBookmarkGroup, Name: "Z", Href: "https://example.invalid/z", Order: &order5,
			},
		},
		{
			Spec: pagev1alpha1.BookmarkSpec{
				InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
				Group:       testBookmarkGroup, Name: "A", Href: "https://example.invalid/a", Order: &order1,
				Abbr: &abbr, Description: &desc,
			},
		},
		{
			Spec: pagev1alpha1.BookmarkSpec{
				InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
				Group:       testOtherGroup, Name: "Only", Href: "https://example.invalid/only",
			},
		},
	}

	groups := groupBookmarks(items, testInstanceName)

	if len(groups) != 2 || groups[0].Name != testBookmarkGroup {
		t.Fatalf("groupBookmarks() groups = %+v, want %s first (lower effective Order after the second entry improves it)", groups, testBookmarkGroup)
	}
	bms := groups[0].Bookmarks
	if len(bms) != 2 || bms[0].Name != "A" || bms[1].Name != "Z" {
		t.Fatalf("groupBookmarks() Reading bookmarks = %+v, want A then Z (sorted by per-bookmark Order)", bms)
	}
	if bms[0].Abbr != abbr || bms[0].Description != desc {
		t.Errorf("groupBookmarks() first bookmark = %+v, want Abbr=%q Description=%q", bms[0], abbr, desc)
	}
}

func TestScalarOptions(t *testing.T) {
	tests := map[string]struct {
		raw  *apiextensionsv1.JSON
		want map[string]string
	}{
		"nil raw":   {raw: nil, want: map[string]string{}},
		"empty raw": {raw: &apiextensionsv1.JSON{}, want: map[string]string{}},
		"scalar types": {
			raw:  &apiextensionsv1.JSON{Raw: []byte(`{"text":"hi","enabled":true,"count":3}`)},
			want: map[string]string{"text": "hi", "enabled": "true", "count": "3"},
		},
		"non-scalar values are skipped": {
			raw:  &apiextensionsv1.JSON{Raw: []byte(`{"text":"hi","nested":{"a":1},"list":[1,2],"empty":null}`)},
			want: map[string]string{"text": "hi"},
		},
		"malformed JSON yields empty map": {
			raw:  &apiextensionsv1.JSON{Raw: []byte(`{not valid json`)},
			want: map[string]string{},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := scalarOptions(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("scalarOptions() = %+v, want %+v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("scalarOptions()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
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

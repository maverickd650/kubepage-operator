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

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if site.Theme != defaultTheme || site.Color != defaultColor || site.HeaderStyle != defaultHeaderStyle {
		t.Errorf("defaults = %+v, want %s/%s/%s", site, defaultTheme, defaultColor, defaultHeaderStyle)
	}
	if site.Search.Provider != "duckduckgo" || site.Search.Target != defaultTarget || !site.Search.FilterCards {
		t.Errorf("search defaults = %+v", site.Search)
	}
	if len(site.BookmarkGroups) != 0 {
		t.Errorf("BookmarkGroups = %+v, want empty", site.BookmarkGroups)
	}
}

func TestLoadSiteListError(t *testing.T) {
	tests := map[string]struct {
		failList func(list client.ObjectList) bool
		failGet  func(key client.ObjectKey, obj client.Object) bool
		wantErr  string
	}{
		"Dashboard get fails": {
			failGet: func(_ client.ObjectKey, obj client.Object) bool {
				_, ok := obj.(*pagev1alpha1.Dashboard)
				return ok
			},
			wantErr: "getting Dashboard",
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
			reader := errInjectingReader{Reader: cl, failList: tc.failList, failGet: tc.failGet}

			_, err := LoadSite(t.Context(), reader, testNamespace, testDashboardName)
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

func TestLoadSiteGetsStyleFromDashboardNamedForIt(t *testing.T) {
	scheme := testScheme(t)
	theme := themeLight
	// LoadSite Gets the Dashboard by name directly; a differently-named
	// Dashboard's style is never looked at.
	wrongName := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: "wrong-name", Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{Theme: &theme},
		},
	}
	rightName := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{Theme: &theme},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(wrongName, rightName).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if site.Theme != themeLight {
		t.Errorf("Theme = %q, want %q (the Dashboard named for it)", site.Theme, themeLight)
	}
}

func TestLoadSiteAppliesLookFields(t *testing.T) {
	scheme := testScheme(t)
	title := "Home Lab"
	desc := "My services"
	favicon := "https://example.invalid/favicon.ico"
	cardBlur := "md"
	target := targetSelf
	cfg := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{
				Title:       &title,
				Description: &desc,
				Favicon:     &favicon,
				CardBlur:    &cardBlur,
				Target:      &target,
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
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

func TestLoadSiteAppliesColorHeaderLanguageFullWidth(t *testing.T) {
	scheme := testScheme(t)
	color := testColor
	headerStyle := "boxed"
	language := "fr"
	fullWidth := true
	cfg := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{
				Color:       &color,
				HeaderStyle: &headerStyle,
				Language:    &language,
				FullWidth:   &fullWidth,
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if site.Color != color || !site.ColorFixed {
		t.Errorf("Color = %q, ColorFixed = %v, want %q, true", site.Color, site.ColorFixed, color)
	}
	if site.HeaderStyle != headerStyle {
		t.Errorf("HeaderStyle = %q, want %q", site.HeaderStyle, headerStyle)
	}
	if site.Language != language {
		t.Errorf("Language = %q, want %q", site.Language, language)
	}
	if !site.FullWidth {
		t.Error("FullWidth = false, want true when spec.style.fullWidth is true")
	}
}

func TestLoadSiteAppliesBackground(t *testing.T) {
	scheme := testScheme(t)
	image := "https://example.invalid/bg.png"
	blur := "xl"
	saturate := int32(50)
	brightness := int32(75)
	opacity := int32(80)
	cfg := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{
				Background: &pagev1alpha1.BackgroundSpec{
					Image:      &image,
					Blur:       &blur,
					Saturate:   &saturate,
					Brightness: &brightness,
					Opacity:    &opacity,
				},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if site.Background == nil {
		t.Fatal("Background = nil, want non-nil")
	}
	// Blur is resolved from the Tailwind keyword to its CSS px length at
	// load time (see Background.Blur's doc comment): "xl" -> "24px".
	if site.Background.Image != image || site.Background.Blur != blurPxXL ||
		*site.Background.Saturate != saturate || *site.Background.Brightness != brightness || *site.Background.Opacity != opacity {
		t.Errorf("Background = %+v", site.Background)
	}
}

func TestLoadSiteAppliesSearch(t *testing.T) {
	scheme := testScheme(t)
	provider := "custom"
	url := "https://search.invalid/q"
	target := targetSelf
	filterCards := false
	cfg := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{
				Search: &pagev1alpha1.SearchSpec{
					Provider:    &provider,
					URL:         &url,
					Target:      &target,
					FilterCards: &filterCards,
				},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if site.Search.Provider != provider || site.Search.URL != url || site.Search.Target != target || site.Search.FilterCards {
		t.Errorf("Search = %+v", site.Search)
	}
}

// TestLoadSiteAppliesQuickLaunchOptions verifies the quick-launch palette
// toggles (gap-analysis §4.2): SearchDescriptions defaults to true (the
// palette's previous always-on behavior) but can be turned off, and
// InternetSearchEntry/VisitURLEntry default to "Shown" (both entries shown)
// but can be turned off with "Hidden".
func TestLoadSiteAppliesQuickLaunchOptions(t *testing.T) {
	scheme := testScheme(t)
	disabled := false
	hidden := false
	cfg := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{
				Search: &pagev1alpha1.SearchSpec{
					SearchDescriptions:  &disabled,
					InternetSearchEntry: &hidden,
					VisitURLEntry:       &hidden,
				},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if site.Search.SearchDescriptions {
		t.Error("Search.SearchDescriptions = true, want false (Disabled)")
	}
	if !site.Search.HideInternetSearch {
		t.Error("Search.HideInternetSearch = false, want true (Hidden)")
	}
	if !site.Search.HideVisitURL {
		t.Error("Search.HideVisitURL = false, want true (Hidden)")
	}
}

func TestLoadSiteQuickLaunchOptionsDefaults(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if !site.Search.SearchDescriptions {
		t.Error("Search.SearchDescriptions default = false, want true")
	}
	if site.Search.HideInternetSearch {
		t.Error("Search.HideInternetSearch default = true, want false")
	}
	if site.Search.HideVisitURL {
		t.Error("Search.HideVisitURL default = true, want false")
	}
}

func TestLoadSiteRejectsNonHTTPSearchURL(t *testing.T) {
	scheme := testScheme(t)
	badURL := testJSSchemeURL
	cfg := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{
				Search: &pagev1alpha1.SearchSpec{URL: &badURL},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if site.Search.URL != "" {
		t.Errorf("Search.URL = %q, want empty (non-http(s) scheme rejected)", site.Search.URL)
	}
}

func TestLoadSiteHeaderWidgetsOrdered(t *testing.T) {
	scheme := testScheme(t)
	order1 := int32(1)
	order2 := int32(2)
	greeting := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "greet", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:   headerTypeGreeting,
				Order:  &order2,
				Config: &apiextensionsv1.JSON{Raw: []byte(`{"text":"Hello"}`)},
			}},
		},
	}
	clock := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testClockName, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:  headerTypeDatetime,
				Order: &order1,
			}},
		},
	}
	other := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testDiscoverySkipKey, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: "not-" + testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: headerTypeGreeting,
			}},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(greeting, clock, other).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if len(site.HeaderWidgets) != 2 {
		t.Fatalf("HeaderWidgets = %+v, want 2 (bound only)", site.HeaderWidgets)
	}
	if site.HeaderWidgets[0].Type != headerTypeDatetime || site.HeaderWidgets[1].Type != headerTypeGreeting {
		t.Errorf("HeaderWidgets order = %+v, want datetime then greeting (by Order)", site.HeaderWidgets)
	}
	if site.HeaderWidgets[1].Config["text"] != "Hello" {
		t.Errorf("greeting config = %+v, want text=Hello", site.HeaderWidgets[1].Config)
	}
}

// TestHeaderWidgetsResolvesAlign verifies the InfoWidgetSpec.Align default
// (greeting/datetime left, everything else right) and its explicit
// left/right override (gap-analysis §4.3).
func TestHeaderWidgetsResolvesAlign(t *testing.T) {
	explicitLeft := pagev1alpha1.AlignLeft
	items := []pagev1alpha1.InfoWidget{
		{ObjectMeta: metav1.ObjectMeta{Name: testGreetName}, Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: headerTypeGreeting,
			}},
		}},
		{ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather}, Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: testOpenMeteoType,
			}},
		}},
		{ObjectMeta: metav1.ObjectMeta{Name: testCPUName}, Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:  testKubeMetricsType,
				Align: &explicitLeft,
			}},
		}},
	}

	out := headerWidgets(items, testDashboardName)
	got := map[string]string{}
	for _, w := range out {
		got[w.Name] = w.Align
	}
	if got[testGreetName] != alignLeft {
		t.Errorf(`Align[testGreetName] = %q, want "left" (default for greeting)`, got[testGreetName])
	}
	if got[testHeaderWeather] != alignRight {
		t.Errorf(`Align[%q] = %q, want "right" (default for a live-value widget)`, testHeaderWeather, got[testHeaderWeather])
	}
	if got[testCPUName] != alignLeft {
		t.Errorf(`Align[testCPUName] = %q, want "left" (explicit InfoWidgetSpec.Align override)`, got[testCPUName])
	}
}

// TestHeaderWidgetsFlattensMultiWidgetFormWithDistinctKeys verifies a single
// InfoWidget object's spec.widgets flattens into one HeaderWidget per entry,
// each with a distinct composite Key (header/<name>/<index>) even though
// they share one object Name — the same correlation key poller.go's
// pollInfoWidget stores each Card's Key under, which buildHeader
// (server.go) needs to tell the entries' live Cards apart.
func TestHeaderWidgetsFlattensMultiWidgetFormWithDistinctKeys(t *testing.T) {
	const multiName = "multi-header"
	items := []pagev1alpha1.InfoWidget{
		{
			ObjectMeta: metav1.ObjectMeta{Name: multiName},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
				Widgets: []pagev1alpha1.InfoWidgetEntry{
					{Type: headerTypeGreeting, Config: &apiextensionsv1.JSON{Raw: []byte(`{"text":"Hi"}`)}},
					{Type: headerTypeDatetime},
				},
			},
		},
	}

	out := headerWidgets(items, testDashboardName)
	if len(out) != 2 {
		t.Fatalf("headerWidgets() = %+v, want 2 entries", out)
	}
	if out[0].Name != multiName || out[1].Name != multiName {
		t.Errorf("both entries' Name = %q/%q, want both %q (they share one InfoWidget object)", out[0].Name, out[1].Name, multiName)
	}
	if out[0].Key == out[1].Key {
		t.Errorf("out[0].Key == out[1].Key (%q); entries sharing an object name must still get distinct composite Keys", out[0].Key)
	}
	wantKey0 := "header/" + multiName + "/0"
	wantKey1 := "header/" + multiName + "/1"
	if out[0].Key != wantKey0 || out[1].Key != wantKey1 {
		t.Errorf("Keys = %q, %q, want %q, %q", out[0].Key, out[1].Key, wantKey0, wantKey1)
	}
}

func TestLoadSiteAppliesLayout(t *testing.T) {
	scheme := testScheme(t)
	cols := int32(4)
	style := testStyleRow
	icon := testGrafanaIconSlug
	cfg := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{
				Layout: []pagev1alpha1.LayoutTabSpec{
					{
						Name: testInfraTab,
						Groups: []pagev1alpha1.LayoutGroupSpec{
							{Name: testGroup, Columns: &cols, Style: &style, Icon: &icon},
						},
					},
				},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testBookmarkGroup,
			Bookmarks: []pagev1alpha1.BookmarkEntry{{
				Name:  "Second",
				Href:  "https://example.invalid/2",
				Order: &order2,
			}},
		},
	}
	bm2 := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "bm2", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testBookmarkGroup,
			Bookmarks: []pagev1alpha1.BookmarkEntry{{
				Name:  testLabelFirst,
				Href:  "https://example.invalid/1",
				Order: &order1,
			}},
		},
	}
	other := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "bm3", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: "not-" + testDashboardName},
			Group:        testBookmarkGroup,
			Bookmarks: []pagev1alpha1.BookmarkEntry{{
				Name: "Skip me",
				Href: "https://example.invalid/skip",
			}},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bm1, bm2, other).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if len(site.BookmarkGroups) != 1 || site.BookmarkGroups[0].Name != testBookmarkGroup {
		t.Fatalf("BookmarkGroups = %+v", site.BookmarkGroups)
	}
	bms := site.BookmarkGroups[0].Bookmarks
	if len(bms) != 2 || bms[0].Name != testLabelFirst || bms[1].Name != "Second" {
		t.Errorf("Bookmarks = %+v, want First then Second (ordered by Order)", bms)
	}
}

func TestLoadSiteThemeFixedAndColorFixed(t *testing.T) {
	scheme := testScheme(t)
	theme := themeLight
	cfg := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{
				Theme: &theme,
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if !site.ThemeFixed {
		t.Error("ThemeFixed = false, want true when spec.style.theme is set")
	}
	if site.ColorFixed {
		t.Error("ColorFixed = true, want false when spec.style.color is unset")
	}
}

func TestLoadSiteAppliesNewLookFields(t *testing.T) {
	scheme := testScheme(t)
	disableCollapse := false
	groupsCollapsed := true
	equalHeights := true
	bookmarksStyle := bookmarksStyleIcons
	disableIndexing := false
	startURL := "/dash"
	customCSS := "body{color:red}"
	cfg := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{
				Collapse:                 &disableCollapse,
				GroupsInitiallyCollapsed: &groupsCollapsed,
				UseEqualHeights:          &equalHeights,
				BookmarksStyle:           &bookmarksStyle,
				Indexing:                 &disableIndexing,
				StartURL:                 &startURL,
				CustomCSS:                &customCSS,
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
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
	header := false
	initiallyCollapsed := true
	equalHeights := true
	cfg := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{
				Layout: []pagev1alpha1.LayoutTabSpec{
					{
						Name: testInfraTab,
						Groups: []pagev1alpha1.LayoutGroupSpec{
							{Name: testGroup, Header: &header, InitiallyCollapsed: &initiallyCollapsed, UseEqualHeights: &equalHeights},
						},
					},
				},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
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
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
				Group:        testBookmarkGroup,
				Bookmarks: []pagev1alpha1.BookmarkEntry{{
					Name:  "Z",
					Href:  "https://example.invalid/z",
					Order: &order5,
				}},
			},
		},
		{
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
				Group:        testBookmarkGroup,
				Bookmarks: []pagev1alpha1.BookmarkEntry{{
					Name:        "A",
					Href:        testBookmarkHrefA,
					Order:       &order1,
					Abbr:        &abbr,
					Description: &desc,
				}},
			},
		},
		{
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
				Group:        testOtherGroup,
				Bookmarks: []pagev1alpha1.BookmarkEntry{{
					Name: "Only",
					Href: "https://example.invalid/only",
				}},
			},
		},
	}

	groups := groupBookmarks(items, testDashboardName, Site{})

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

// TestGroupBookmarksMultiFormGroupDefaultingAndOverride verifies a Bookmark
// object with multiple entries (spec.bookmarks) flattens into per-entry
// cards: an entry without its own group inherits the object's spec.group,
// and an entry that sets its own group renders in that group instead.
func TestGroupBookmarksMultiFormGroupDefaultingAndOverride(t *testing.T) {
	abbr := "WK"
	items := []pagev1alpha1.Bookmark{
		{
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
				Group:        testBookmarkGroup,
				Bookmarks: []pagev1alpha1.BookmarkEntry{
					{Name: testLabelFirst, Href: testBookmarkHrefA},
					{Name: "Wikipedia", Href: "https://example.invalid/wiki", Group: testOtherGroup, Abbr: &abbr},
				},
			},
		},
	}

	groups := groupBookmarks(items, testDashboardName, Site{})

	if len(groups) != 2 {
		t.Fatalf("groupBookmarks() = %d groups, want 2 (%s and %s)", len(groups), testBookmarkGroup, testOtherGroup)
	}

	byName := map[string][]BookmarkCard{}
	for _, g := range groups {
		byName[g.Name] = g.Bookmarks
	}

	readingCards, ok := byName[testBookmarkGroup]
	if !ok || len(readingCards) != 1 || readingCards[0].Name != testLabelFirst {
		t.Errorf("groupBookmarks() %s group = %+v, want one entry (First) inheriting spec.group", testBookmarkGroup, readingCards)
	}

	otherCards, ok := byName[testOtherGroup]
	if !ok || len(otherCards) != 1 || otherCards[0].Name != "Wikipedia" || otherCards[0].Abbr != abbr {
		t.Errorf("groupBookmarks() %s group = %+v, want one entry (Wikipedia) with its own group override", testOtherGroup, otherCards)
	}
}

// TestGroupBookmarksAppliesMatchingLayoutGroup verifies a LayoutGroupSpec
// sharing a bookmark group's name styles it the same way it would a service
// group sharing that name (gap-analysis §4.1): Columns/Style/Icon/Header all
// carry over.
func TestGroupBookmarksAppliesMatchingLayoutGroup(t *testing.T) {
	items := []pagev1alpha1.Bookmark{
		{
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
				Group:        testBookmarkGroup,
				Bookmarks: []pagev1alpha1.BookmarkEntry{{
					Name: "A",
					Href: testBookmarkHrefA,
				}},
			},
		},
	}
	cols := int32(3)
	header := false
	site := Site{
		Layout: []LayoutTab{{
			Name: testTab1,
			Groups: []LayoutGroup{{
				Name:    testBookmarkGroup,
				Columns: &cols,
				Style:   testStyleRow,
				IconURL: testExampleURL,
				Header:  &header,
			}},
		}},
	}

	groups := groupBookmarks(items, testDashboardName, site)
	if len(groups) != 1 {
		t.Fatalf("groupBookmarks() = %d groups, want 1", len(groups))
	}
	g := groups[0]
	if g.Columns == nil || *g.Columns != 3 {
		t.Errorf("g.Columns = %v, want 3", g.Columns)
	}
	if g.Style != testStyleRow {
		t.Errorf("g.Style = %q, want %q", g.Style, testStyleRow)
	}
	if g.IconURL != testExampleURL {
		t.Errorf("g.IconURL = %q, want %q", g.IconURL, testExampleURL)
	}
	if g.Header {
		t.Error("g.Header = true, want false (LayoutGroupSpec.Header=Hidden)")
	}
}

// TestGroupBookmarksUnmatchedGroupUsesSiteDefaults verifies a bookmark group
// with no matching LayoutGroupSpec falls back to the Site-wide
// InitiallyCollapsed/UseEqualHeights defaults and a shown header, exactly
// like groupCards does for service groups.
func TestGroupBookmarksUnmatchedGroupUsesSiteDefaults(t *testing.T) {
	items := []pagev1alpha1.Bookmark{
		{
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
				Group:        testBookmarkGroup,
				Bookmarks: []pagev1alpha1.BookmarkEntry{{
					Name: "A",
					Href: testBookmarkHrefA,
				}},
			},
		},
	}
	site := Site{GroupsInitiallyCollapsed: true, UseEqualHeights: true}

	groups := groupBookmarks(items, testDashboardName, site)
	if len(groups) != 1 {
		t.Fatalf("groupBookmarks() = %d groups, want 1", len(groups))
	}
	g := groups[0]
	if !g.Header {
		t.Error("g.Header = false, want true (default)")
	}
	if !g.InitiallyCollapsed {
		t.Error("g.InitiallyCollapsed = false, want true (from Site.GroupsInitiallyCollapsed)")
	}
	if !g.UseEqualHeights {
		t.Error("g.UseEqualHeights = false, want true (from Site.UseEqualHeights)")
	}
	if g.Columns != nil || g.Style != "" || g.IconURL != "" {
		t.Errorf("g = %+v, want zero Columns/Style/IconURL (no matching LayoutGroupSpec)", g)
	}
}

func TestScalarConfig(t *testing.T) {
	tests := map[string]struct {
		raw  *apiextensionsv1.JSON
		want map[string]string
	}{
		"nil raw":   {raw: nil, want: map[string]string{}},
		"empty raw": {raw: &apiextensionsv1.JSON{}, want: map[string]string{}},
		"scalar types": {
			raw:  &apiextensionsv1.JSON{Raw: []byte(`{"text":"hi","enabled":true,"count":3}`)},
			want: map[string]string{testOptionsText: "hi", "enabled": "true", "count": "3"},
		},
		"non-scalar values are skipped": {
			raw:  &apiextensionsv1.JSON{Raw: []byte(`{"text":"hi","nested":{"a":1},"list":[1,2],"empty":null}`)},
			want: map[string]string{testOptionsText: "hi"},
		},
		"malformed JSON yields empty map": {
			raw:  &apiextensionsv1.JSON{Raw: []byte(`{not valid json`)},
			want: map[string]string{},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := scalarConfig(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("scalarConfig() = %+v, want %+v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("scalarConfig()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestLoadSiteAppliesCustomJSStatusStyleHideErrorsHideVersion(t *testing.T) {
	scheme := testScheme(t)
	customJS := "console.log('hi')"
	statusStyle := statusStyleBasic
	hideErrors := false
	hideVersion := true
	cfg := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{
				CustomJS:     &customJS,
				StatusStyle:  &statusStyle,
				ErrorDisplay: &hideErrors,
				HideVersion:  &hideVersion,
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if site.CustomJS != customJS {
		t.Errorf("CustomJS = %q, want %q", site.CustomJS, customJS)
	}
	if site.StatusStyle != statusStyleBasic {
		t.Errorf("StatusStyle = %q, want %q", site.StatusStyle, statusStyleBasic)
	}
	if !site.HideErrors {
		t.Error("HideErrors = false, want true")
	}
	if !site.HideVersion {
		t.Error("HideVersion = false, want true")
	}
}

func TestLoadSiteStatusStyleDefaultsToDot(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	site, err := LoadSite(t.Context(), cl, testNamespace, testDashboardName)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if site.StatusStyle != statusStyleDot {
		t.Errorf("StatusStyle = %q, want default %q", site.StatusStyle, statusStyleDot)
	}
}

func TestBlurPx(t *testing.T) {
	tests := map[string]struct{ keyword, want string }{
		"keyword none":    {keyword: "none", want: ""},
		"default":         {keyword: "", want: "8px"},
		"keyword sm":      {keyword: "sm", want: blurPxSM},
		"keyword md":      {keyword: "md", want: "12px"},
		"keyword lg":      {keyword: "lg", want: "16px"},
		"keyword xl":      {keyword: "xl", want: blurPxXL},
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

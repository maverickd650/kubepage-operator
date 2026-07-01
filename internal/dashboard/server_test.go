package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

func newTestServer(t *testing.T, store *Store, objs ...client.Object) *Server {
	t.Helper()
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &Server{Store: store, Reader: cl, Namespace: testNamespace, InstanceName: testInstanceName}
}

func TestServerFragmentRendersCards(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/prom/0", Group: testGroup, ServiceName: testServiceName,
		Fields: []Field{{Label: labelStatus, Value: statusHealthy}},
	})
	store.Set(Card{
		Key: "ns/broken/0", Group: testGroup, ServiceName: "Broken",
		Err: "unreachable",
	})

	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Monitoring", "Prometheus", "Healthy", "Broken", "unreachable"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
}

func TestServerAssetServesEmbeddedFont(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/assets/manrope-400.woff2", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "font/woff2" {
		t.Errorf("Content-Type = %q, want font/woff2", ct)
	}
	if rec.Body.Len() == 0 {
		t.Error("asset body is empty")
	}

	missing := httptest.NewRequest(http.MethodGet, "/assets/nope.woff2", nil)
	missingRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(missingRec, missing)
	if missingRec.Code != http.StatusNotFound {
		t.Errorf("missing asset status = %d, want 404", missingRec.Code)
	}
}

func TestServerIndexEmitsPaletteRamp(t *testing.T) {
	color := testColor
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Color:       &color,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"--c900: #1e3a8a", "--c500: #3b82f6", "@font-face", "/assets/manrope-400.woff2"} {
		if !strings.Contains(body, want) {
			t.Errorf("index body missing %q", want)
		}
	}
}

func TestServerFragmentRendersStatsRow(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/prom/0", Group: testGroup, ServiceName: testServiceName,
		Fields: []Field{{Label: labelStatus, Value: statusHealthy}},
	})
	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`class="stats"`, `class="stat"`, `class="value"`, statusHealthy} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
}

func TestServerMetricsRoute(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "go_goroutines") {
		t.Errorf("/metrics body missing expected Prometheus Go-runtime metric:\n%s", rec.Body.String())
	}
}

func TestServerFragmentRendersBookmarks(t *testing.T) {
	bookmark := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "docs", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       testBookmarkGroup,
			Name:        "Docs",
			Href:        "https://example.invalid/docs",
		},
	}
	srv := newTestServer(t, NewStore(), bookmark)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{testBookmarkGroup, "Docs", "https://example.invalid/docs"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
}

func TestServerIndexServesShell(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/fragment") {
		t.Errorf("index body should hx-get /fragment:\n%s", rec.Body.String())
	}
}

func TestServerIndexAppliesConfigurationTheme(t *testing.T) {
	theme := themeLight
	color := testColor
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Theme:       &theme,
			Color:       &color,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`data-theme="light"`, AccentHex(testColor)} {
		if !strings.Contains(body, want) {
			t.Errorf("index body missing %q:\n%s", want, body)
		}
	}
}

func TestServerFragmentRendersCollapsibleGroupsByDefault(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: testCardKeyA, Group: testGroup, ServiceName: testSvcAName})
	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`<details class="group" data-group-name="` + testGroup + `"`, "<summary>"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
}

func TestServerFragmentDisableCollapseRendersPlainHeaders(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: testCardKeyA, Group: testGroup, ServiceName: testSvcAName})
	disable := true
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef:     pagev1alpha1.InstanceRef{Name: testInstanceName},
			DisableCollapse: &disable,
		},
	}
	srv := newTestServer(t, store, cfg)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "<details") {
		t.Errorf("fragment body has <details> with DisableCollapse=true:\n%s", body)
	}
	if !strings.Contains(body, "<h2>") {
		t.Errorf("fragment body missing plain <h2> group header:\n%s", body)
	}
}

func TestServerFragmentBookmarksIconsOnly(t *testing.T) {
	bookmark := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "wiki", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       testBookmarkGroup,
			Name:        "Wiki",
			Href:        "https://example.invalid/wiki",
		},
	}
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef:    pagev1alpha1.InstanceRef{Name: testInstanceName},
			BookmarksStyle: new(bookmarksStyleIcons),
		},
	}
	srv := newTestServer(t, NewStore(), bookmark, cfg)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "bookmark-icons-only") {
		t.Errorf("fragment body missing bookmark-icons-only class:\n%s", body)
	}
	if strings.Contains(body, "<h3>Wiki</h3>") {
		t.Errorf("fragment body should hide bookmark name text in icons-only mode:\n%s", body)
	}
}

func TestServerManifestRoute(t *testing.T) {
	title := "My Lab"
	startURL := "/dashboard"
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Title:       &title,
			StartURL:    &startURL,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/manifest.json", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("Content-Type = %q, want application/manifest+json", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{`"name":"My Lab"`, `"start_url":"/dashboard"`, `"display":"standalone"`} {
		if !strings.Contains(body, want) {
			t.Errorf("manifest body missing %q:\n%s", want, body)
		}
	}
}

func TestServerRobotsRoute(t *testing.T) {
	disable := true
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef:     pagev1alpha1.InstanceRef{Name: testInstanceName},
			DisableIndexing: &disable,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Disallow: /") {
		t.Errorf("robots.txt = %q, want Disallow: / when DisableIndexing", rec.Body.String())
	}
}

func TestServerRobotsRouteDefaultAllows(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "Allow: /") {
		t.Errorf("robots.txt = %q, want Allow: / by default", rec.Body.String())
	}
}

func TestServerIndexAppliesLookFields(t *testing.T) {
	title := "My Lab"
	favicon := "https://example.invalid/fav.ico"
	cardBlur := "lg"
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Title:       &title,
			Favicon:     &favicon,
			CardBlur:    &cardBlur,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"<title>My Lab</title>", favicon, "--card-blur: 16px", `hx-get="/header"`} {
		if !strings.Contains(body, want) {
			t.Errorf("index body missing %q:\n%s", want, body)
		}
	}
}

func TestServerFragmentRendersMonitorAndTarget(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/svc/0", Group: testGroup, ServiceName: testSvcDisplayName,
		Href: "https://svc.invalid", Target: testTargetSelf,
		Status: "Up", StatusStyle: testStatusBasic, Latency: "5ms",
		ShowStats: true,
	})

	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`target="_self"`, "Up", "5ms"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
}

func TestServerHeaderRendersWidgets(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "header/weather", ServiceName: testHeaderWeather, Header: true,
		Fields: []Field{{Label: labelWeather, Value: "10°C"}, {Label: labelConditions, Value: condClear}},
	})

	greeting := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "greet", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Type:        headerTypeGreeting,
			Options:     &apiextensionsv1.JSON{Raw: []byte(`{"text":"Welcome"}`)},
		},
	}
	weather := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Type:        testOpenMeteoType,
		},
	}
	srv := newTestServer(t, store, greeting, weather)
	req := httptest.NewRequest(http.MethodGet, "/header", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Welcome", "10°C", condClear} {
		if !strings.Contains(body, want) {
			t.Errorf("header body missing %q:\n%s", want, body)
		}
	}
}

func TestServiceCardsFiltersHeaderCards(t *testing.T) {
	cards := []Card{
		{Group: "A", ServiceName: testSvcName, Header: false},
		{ServiceName: testHeaderWeather, Header: true},
	}
	got := serviceCards(cards)
	if len(got) != 1 || got[0].ServiceName != testSvcName {
		t.Errorf("serviceCards() = %+v, want only the non-header card", got)
	}
}

func TestGroupCardsPreservesOrder(t *testing.T) {
	cards := []Card{
		{Group: "A", ServiceName: "a1"},
		{Group: "A", ServiceName: "a2"},
		{Group: "B", ServiceName: "b1"},
	}
	groups := groupCards(cards, Site{})
	if len(groups) != 2 || groups[0].Name != "A" || len(groups[0].Cards) != 2 || groups[1].Name != "B" {
		t.Fatalf("groupCards() = %+v", groups)
	}
}

func TestLayoutTabsNoLayoutReturnsSingleUnnamedTab(t *testing.T) {
	cards := []Card{
		{Group: "A", ServiceName: "a1"},
		{Group: "B", ServiceName: "b1"},
	}
	tabs := layoutTabs(cards, Site{})
	if len(tabs) != 1 || tabs[0].Name != "" || len(tabs[0].Groups) != 2 {
		t.Fatalf("layoutTabs() with no layout = %+v, want one unnamed tab with both groups", tabs)
	}
}

func TestLayoutTabsArrangesGroupsAndStyles(t *testing.T) {
	cards := []Card{
		{Group: "A", ServiceName: "a1"},
		{Group: "B", ServiceName: "b1"},
	}
	cols := int32(3)
	layout := []LayoutTab{
		{Name: testTab1, Groups: []LayoutGroup{{Name: "A", Columns: &cols, Style: testStyleRow, IconURL: "https://icon.invalid/a.png"}}},
		{Name: testTab2, Groups: []LayoutGroup{{Name: "B"}}},
	}
	tabs := layoutTabs(cards, Site{Layout: layout})
	if len(tabs) != 2 {
		t.Fatalf("layoutTabs() = %+v, want 2 tabs", tabs)
	}
	if tabs[0].Name != testTab1 || len(tabs[0].Groups) != 1 || tabs[0].Groups[0].Name != "A" {
		t.Fatalf("tabs[0] = %+v", tabs[0])
	}
	g := tabs[0].Groups[0]
	if g.Columns == nil || *g.Columns != cols || g.Style != testStyleRow || g.IconURL != "https://icon.invalid/a.png" {
		t.Errorf("tabs[0].Groups[0] style = %+v, want columns=3 style=row iconURL set", g)
	}
	if tabs[1].Name != testTab2 || len(tabs[1].Groups) != 1 || tabs[1].Groups[0].Name != "B" {
		t.Fatalf("tabs[1] = %+v", tabs[1])
	}
}

func TestLayoutTabsAppendsUnreferencedGroupsToOther(t *testing.T) {
	cards := []Card{
		{Group: "A", ServiceName: "a1"},
		{Group: "B", ServiceName: "b1"},
	}
	layout := []LayoutTab{{Name: testTab1, Groups: []LayoutGroup{{Name: "A"}}}}
	tabs := layoutTabs(cards, Site{Layout: layout})
	if len(tabs) != 2 || tabs[1].Name != testOtherGroup || len(tabs[1].Groups) != 1 || tabs[1].Groups[0].Name != "B" {
		t.Fatalf("layoutTabs() = %+v, want Group B appended to a trailing \"Other\" tab", tabs)
	}
}

func TestLayoutTabsGroupReferencedTwiceUsesFirstTab(t *testing.T) {
	cards := []Card{{Group: "A", ServiceName: "a1"}}
	layout := []LayoutTab{
		{Name: testTab1, Groups: []LayoutGroup{{Name: "A"}}},
		{Name: testTab2, Groups: []LayoutGroup{{Name: "A"}}},
	}
	tabs := layoutTabs(cards, Site{Layout: layout})
	if len(tabs) != 2 || len(tabs[0].Groups) != 1 || len(tabs[1].Groups) != 0 {
		t.Fatalf("layoutTabs() = %+v, want Group A only under Tab1", tabs)
	}
}

func TestServerHealthzRoute(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// failingConfigListServer wraps a fake client so the ConfigurationList read
// LoadSite issues first fails, exercising every handler's LoadSite-error
// branch without needing a real apiserver.
func failingConfigListServer(t *testing.T, store *Store) *Server {
	t.Helper()
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	failing := errInjectingReader{
		Reader: cl,
		failList: func(list client.ObjectList) bool {
			_, ok := list.(*pagev1alpha1.ConfigurationList)
			return ok
		},
	}
	return &Server{Store: store, Reader: failing, Namespace: testNamespace, InstanceName: testInstanceName}
}

func TestServerHandlersReturn500OnLoadSiteError(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"manifest", "/manifest.json"},
		{"robots", "/robots.txt"},
		{"index", "/"},
		{"fragment", "/fragment"},
		{"header", "/header"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := failingConfigListServer(t, NewStore())
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("%s status = %d, want 500", tc.path, rec.Code)
			}
		})
	}
}

func TestServerManifestLightThemeUsesC50Background(t *testing.T) {
	theme := themeLight
	color := testColor
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Theme:       &theme,
			Color:       &color,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/manifest.json", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	want := `"background_color":"` + PaletteRamp(testColor).C50 + `"`
	if !strings.Contains(rec.Body.String(), want) {
		t.Errorf("manifest body = %s, want %q (light theme uses C50 background)", rec.Body.String(), want)
	}
}

func TestBuildHeaderDatetimeWidget(t *testing.T) {
	defs := []HeaderWidget{
		{Type: headerTypeDatetime, Options: map[string]string{"format": "medium"}},
	}
	views := buildHeader(defs, nil)
	if len(views) != 1 || views[0].Format != "medium" {
		t.Fatalf("buildHeader(datetime) = %+v, want Format=medium", views)
	}
}

func TestLayoutTabsAppliesGroupOverridePointers(t *testing.T) {
	cards := []Card{{Group: "A", ServiceName: "a1"}}
	header := false
	collapsed := true
	equalHeights := true
	layout := []LayoutTab{
		{Name: testTab1, Groups: []LayoutGroup{{
			Name: "A", Header: &header, InitiallyCollapsed: &collapsed, UseEqualHeights: &equalHeights,
		}}},
	}
	tabs := layoutTabs(cards, Site{Layout: layout})
	if len(tabs) != 1 || len(tabs[0].Groups) != 1 {
		t.Fatalf("layoutTabs() = %+v", tabs)
	}
	g := tabs[0].Groups[0]
	if g.Header != false || g.InitiallyCollapsed != true || g.UseEqualHeights != true {
		t.Errorf("layoutTabs() group override = %+v, want Header=false InitiallyCollapsed=true UseEqualHeights=true", g)
	}
}

func TestServerFragmentRendersTabs(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: testCardKeyA, Group: testInfraGroup, ServiceName: testSvcAName})
	store.Set(Card{Key: "ns/b/0", Group: "Apps", ServiceName: "Svc B"})

	cols := int32(2)
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Layout: []pagev1alpha1.LayoutTabSpec{
				{Name: testInfraTab, Groups: []pagev1alpha1.LayoutGroupSpec{{Name: testInfraGroup, Columns: &cols}}},
			},
		},
	}
	srv := newTestServer(t, store, cfg)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{testInfraTab, testOtherGroup, testSvcAName, "Svc B", "tab-btn"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
}

func TestServerFragmentRendersStatusDotAndUsageBar(t *testing.T) {
	pct := 42
	store := NewStore()
	store.Set(Card{
		Key: "ns/dot/0", Group: testGroup, ServiceName: "Dotted", IconURL: "https://icon.invalid/dot.png",
		Status: "Up", StatusStyle: statusStyleDot,
		Fields: []Field{{Label: labelStatus, Value: statusHealthy, Percent: &pct}},
	})
	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`class="status-dot status-Up"`, `class="icon"`, `class="usage-bar"`} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
}

func TestServerFragmentRendersStatusPillAndHrefLessCard(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/pill/0", Group: testGroup, ServiceName: "NoLink",
		Status: "Down", StatusStyle: "pill",
	})
	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `class="status-pill status-Down"`) {
		t.Errorf("fragment body missing status-pill:\n%s", body)
	}
	if strings.Contains(body, `<a href=""`) {
		t.Errorf("fragment body rendered a link for a card with no Href:\n%s", body)
	}
}

func TestServerFragmentHeaderlessGroupRendersGridWithoutHeader(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: testCardKeyA, Group: testInfraGroup, ServiceName: testSvcAName})

	header := false
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Layout: []pagev1alpha1.LayoutTabSpec{
				{Name: testInfraTab, Groups: []pagev1alpha1.LayoutGroupSpec{{Name: testInfraGroup, Header: &header}}},
			},
		},
	}
	srv := newTestServer(t, store, cfg)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, `data-group-name="Infra"`) || strings.Contains(body, "<h2>") {
		t.Errorf("fragment body rendered a header for a header-less group:\n%s", body)
	}
	if !strings.Contains(body, testSvcAName) {
		t.Errorf("fragment body missing %q for the header-less group's card grid:\n%s", testSvcAName, body)
	}
}

func TestServerFragmentBookmarkAbbrWithoutIconAndDisableCollapse(t *testing.T) {
	abbr := "W2"
	bookmark := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "wiki2", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       testBookmarkGroup,
			Name:        "Wiki Two",
			Href:        "https://example.invalid/wiki2",
			Abbr:        &abbr,
		},
	}
	disable := true
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef:     pagev1alpha1.InstanceRef{Name: testInstanceName},
			DisableCollapse: &disable,
		},
	}
	srv := newTestServer(t, NewStore(), bookmark, cfg)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`class="abbr"`, "W2", "<h2>" + testBookmarkGroup + "</h2>"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `data-group-name="bookmark:`) {
		t.Errorf("fragment body rendered a collapsible bookmark group with DisableCollapse=true:\n%s", body)
	}
}

func TestServerHeaderRendersErrAndDatetimeWidget(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "header/weather", ServiceName: testHeaderWeather, Header: true,
		Err: "upstream unreachable",
	})

	clock := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testClockName, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Type:        headerTypeDatetime,
			Options:     &apiextensionsv1.JSON{Raw: []byte(`{"format":"medium"}`)},
		},
	}
	weather := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Type:        testOpenMeteoType,
		},
	}
	srv := newTestServer(t, store, clock, weather)
	req := httptest.NewRequest(http.MethodGet, "/header", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`class="err"`, "upstream unreachable", "data-clock", `data-format="medium"`} {
		if !strings.Contains(body, want) {
			t.Errorf("header body missing %q:\n%s", want, body)
		}
	}
}

func TestServerIndexRendersBackgroundAndCustomCSS(t *testing.T) {
	img := "https://example.invalid/bg.png"
	css := "body { color: red; }"
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Background:  &pagev1alpha1.BackgroundSpec{Image: &img},
			CustomCSS:   &css,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"background-image: url(", img, css} {
		if !strings.Contains(body, want) {
			t.Errorf("index body missing %q:\n%s", want, body)
		}
	}
}

func TestServerIndexHidesSwitcherWhenThemeAndColorFixed(t *testing.T) {
	theme := themeLight
	color := testColor
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Theme:       &theme,
			Color:       &color,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, unwanted := range []string{"switcher-config", `<div class="switcher">`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("index body has %q with both Theme and Color fixed, want the switcher script/buttons skipped:\n%s", unwanted, body)
		}
	}
}

func TestServerIndexDefaultTitleOmitsHeadingNoDescription(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, `class="page-title"`) {
		t.Errorf("index body has page-title heading for the default \"kubepage\" title, want it omitted:\n%s", body)
	}
	if strings.Contains(body, `class="page-desc"`) {
		t.Errorf("index body has page-desc with no Description configured, want it omitted:\n%s", body)
	}
}

func TestServerIndexRendersDescriptionMetaAndParagraph(t *testing.T) {
	desc := "Everything self-hosted, in one place."
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Description: &desc,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`<meta name="description" content="` + desc + `"`, `class="page-desc"`, desc} {
		if !strings.Contains(body, want) {
			t.Errorf("index body missing %q, want it rendered when Description is configured:\n%s", want, body)
		}
	}
}

func TestServerIndexAppliesDisableIndexingMetaRobots(t *testing.T) {
	disable := true
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef:     pagev1alpha1.InstanceRef{Name: testInstanceName},
			DisableIndexing: &disable,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	want := `<meta name="robots" content="noindex, nofollow">`
	if body := rec.Body.String(); !strings.Contains(body, want) {
		t.Errorf("index body missing %q, want it emitted when DisableIndexing is set on the page itself (distinct from the /robots.txt route):\n%s", want, body)
	}
}

func TestServerIndexShowsOnlyColorSwitcherWhenThemeFixed(t *testing.T) {
	theme := themeLight
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Theme:       &theme,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `id="color-switcher-btn"`) {
		t.Errorf("index body missing color-switcher-btn, want it rendered when only Theme is fixed:\n%s", body)
	}
	if strings.Contains(body, `id="theme-switcher-btn"`) {
		t.Errorf("index body has theme-switcher-btn, want it omitted when Theme is fixed:\n%s", body)
	}
}

func TestServerIndexShowsOnlyThemeSwitcherWhenColorFixed(t *testing.T) {
	color := testColor
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Color:       &color,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `id="theme-switcher-btn"`) {
		t.Errorf("index body missing theme-switcher-btn, want it rendered when only Color is fixed:\n%s", body)
	}
	if strings.Contains(body, `id="color-switcher-btn"`) {
		t.Errorf("index body has color-switcher-btn, want it omitted when Color is fixed:\n%s", body)
	}
}

func TestServerFragmentRendersHighlightedStatClasses(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/hl/0", Group: testGroup, ServiceName: "Highlighted",
		Fields: []Field{
			{Label: "load", Value: "1", Highlight: HighlightGood},
			{Label: "mem", Value: "2", Highlight: HighlightWarn},
			{Label: "disk", Value: "3", Highlight: HighlightDanger},
		},
	})
	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"stat-good", "stat-warn", "stat-danger"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q, want a stat class per Field.Highlight:\n%s", want, body)
		}
	}
}

func TestServerHeaderRendersHighlightedFieldClasses(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "header/hl", ServiceName: testHeaderWeather, Header: true,
		Fields: []Field{
			{Label: "load", Value: "1", Highlight: HighlightGood},
			{Label: "mem", Value: "2", Highlight: HighlightWarn},
			{Label: "disk", Value: "3", Highlight: HighlightDanger},
		},
	})
	weather := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Type:        testOpenMeteoType,
		},
	}
	srv := newTestServer(t, store, weather)
	req := httptest.NewRequest(http.MethodGet, "/header", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"hl-good", "hl-warn", "hl-danger"} {
		if !strings.Contains(body, want) {
			t.Errorf("header body missing %q, want a header-field class per Field.Highlight:\n%s", want, body)
		}
	}
}

func TestServerFragmentRendersGridRowAndEqualHeightStyles(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: "ns/row/0", Group: testGroup, ServiceName: testServiceName})

	style := "row"
	equalHeights := true
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Layout: []pagev1alpha1.LayoutTabSpec{
				{Groups: []pagev1alpha1.LayoutGroupSpec{{
					Name: testGroup, Style: &style, UseEqualHeights: &equalHeights,
				}}},
			},
		},
	}
	srv := newTestServer(t, store, cfg)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"grid-row", "grid-equal"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q, want it applied from the group's Style/UseEqualHeights override:\n%s", want, body)
		}
	}
}

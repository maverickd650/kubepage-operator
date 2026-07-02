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

// TestServerIndexEmitsCardPixelTuningCSS locks in the visual-parity pass
// (gap-analysis §3.5/4.4): service card icons render larger than header/
// bookmark icons, and equal-height cards push their stats row to the bottom
// via a grid-equal-scoped rule rather than a global one (which would also
// affect non-equal-height cards).
func TestServerIndexEmitsCardPixelTuningCSS(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{".card h3 img.icon { width: 2rem; height: 2rem; }", ".grid.grid-equal .card .stats { margin-top: auto; }"} {
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

// TestServerFragmentRendersIframeWidget verifies a Card whose WidgetType is
// "iframe" renders an actual <iframe> (sandboxed, sized per the widget's
// Fields) instead of the usual stats grid — cards.templ's special case for
// widgetTypeIframe.
func TestServerFragmentRendersIframeWidget(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/dash/0", Group: testGroup, ServiceName: testServiceName,
		WidgetType: widgetTypeIframe,
		Fields: []Field{
			{Label: labelIframeSrc, Value: testIframeURL},
			{Label: labelIframeHeight, Value: testIframeHeight},
		},
	})
	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		`class="card-iframe"`, `src="` + testIframeURL + `"`,
		`sandbox="` + iframeSandbox + `"`, `height: ` + testIframeHeight,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `class="stats"`) {
		t.Errorf("fragment body rendered a stats grid for an iframe widget:\n%s", body)
	}
}

// TestSecurityHeadersAllowsHTTPSFrames guards the iframe ServiceWidget
// (iframe.go/cards.templ): without a frame-src directive, the CSP's
// default-src 'self' would make every browser refuse to load an iframe
// widget's cross-origin src, silently breaking the whole feature despite
// the fragment's rendered markup looking correct.
func TestSecurityHeadersAllowsHTTPSFrames(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-src https:") {
		t.Errorf("Content-Security-Policy = %q, want a frame-src https: directive", csp)
	}
}

// TestSecurityHeadersUsesNonceNotUnsafeInline verifies script-src/style-src
// no longer fall back to 'unsafe-inline' (P2.4 in docs/security-review.md):
// a future escaping regression in a @templ.Raw path should be blocked by
// the browser, not just relying on server-side escaping.
func TestSecurityHeadersUsesNonceNotUnsafeInline(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "unsafe-inline") {
		t.Errorf("Content-Security-Policy = %q, want no 'unsafe-inline'", csp)
	}
	if !strings.Contains(csp, "script-src 'self' 'nonce-") || !strings.Contains(csp, "style-src 'self' 'nonce-") {
		t.Errorf("Content-Security-Policy = %q, want nonce-based script-src/style-src", csp)
	}
}

// TestSecurityHeadersNonceVariesPerRequest guards against a static/reused
// nonce, which would let an attacker who ever captures one page load reuse
// its nonce indefinitely.
func TestSecurityHeadersNonceVariesPerRequest(t *testing.T) {
	srv := newTestServer(t, NewStore())

	get := func() string {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)
		return rec.Header().Get("Content-Security-Policy")
	}

	first, second := get(), get()
	if first == second {
		t.Errorf("Content-Security-Policy nonce did not vary between requests: %q", first)
	}
}

// TestIndexEmbedsNonceOnInlineScriptTags verifies the rendered page shell's
// literal <script>/<style> tags actually carry the same nonce the CSP
// header names — otherwise the browser would refuse every inline block and
// the dashboard would render with no theme switching/search/etc.
func TestIndexEmbedsNonceOnInlineScriptTags(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	start := strings.Index(csp, "'nonce-") + len("'nonce-")
	end := strings.Index(csp[start:], "'") + start
	nonce := csp[start:end]
	if nonce == "" {
		t.Fatal("could not extract nonce from Content-Security-Policy header")
	}

	body := rec.Body.String()
	if !strings.Contains(body, `nonce="`+nonce+`"`) {
		t.Errorf("rendered page does not contain nonce=%q from its own CSP header:\n%s", nonce, body)
	}
}

// TestServerMetricsRouteNotExposed asserts /metrics is not reachable on the
// Server's own router: it's served on a separate listener (dashboard.go's
// Run, on Options.MetricsAddr) specifically so it can't be exposed through
// the same Ingress/HTTPRoute as the dashboard's main port. See
// dashboardMetricsPort's doc comment in internal/controller.
func TestServerMetricsRouteNotExposed(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (metrics should not be served on the main router)", rec.Code)
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

// TestServerIndexEmitsQuickLaunchSearchConfig verifies the Configuration's
// quick-launch toggles reach the page shell's client-side searchConfig JSON
// (gap-analysis §4.2), which index.templ's qlRender reads.
func TestServerIndexEmitsQuickLaunchSearchConfig(t *testing.T) {
	disabled := pagev1alpha1.Disabled
	enabled := pagev1alpha1.Enabled
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Search: &pagev1alpha1.SearchSpec{
				SearchDescriptions: &disabled,
				HideInternetSearch: &enabled,
				HideVisitURL:       &enabled,
			},
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`"searchDescriptions":false`, `"hideInternetSearch":true`, `"hideVisitURL":true`} {
		if !strings.Contains(body, want) {
			t.Errorf("index body missing %q in search-config JSON:\n%s", want, body)
		}
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
	disable := pagev1alpha1.Disabled
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
		t.Errorf("fragment body has <details> with DisableCollapse=Disabled:\n%s", body)
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
	disable := pagev1alpha1.IndexingNoIndex
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
		Href: "https://svc.invalid", Target: targetSelf,
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

// TestServerFragmentNewTabLinksCarryNoopenerNoreferrer guards against
// reverse-tabnabbing/Referer-leak regressions: a card or bookmark whose
// link opens a new browsing context (target="_blank") must carry
// rel="noopener noreferrer" (see isNewTabTarget), while one that stays in
// the same tab (target="_self") must not.
func TestServerFragmentNewTabLinksCarryNoopenerNoreferrer(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/blank/0", Group: testGroup, ServiceName: "BlankCard",
		Href: "https://blank.invalid", Target: defaultTarget, ShowStats: true,
	})
	store.Set(Card{
		Key: "ns/self/0", Group: testGroup, ServiceName: "SelfCard",
		Href: "https://self.invalid", Target: targetSelf, ShowStats: true,
	})

	bookmark := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "bm", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       testBookmarkGroup,
			Name:        "Handbook",
			Href:        "https://docs.invalid",
		},
	}

	srv := newTestServer(t, store, bookmark)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if got := openingTag(t, body, `href="https://blank.invalid"`); !strings.Contains(got, `rel="noopener noreferrer"`) {
		t.Errorf("card with target=_blank missing rel=noopener noreferrer, got tag:\n%s", got)
	}
	if got := openingTag(t, body, `href="https://self.invalid"`); strings.Contains(got, "rel=") {
		t.Errorf("card with target=_self should not carry a rel attribute, got tag:\n%s", got)
	}
	// The bookmark's own target defaults to the site default ("_blank"),
	// so its link should also carry rel="noopener noreferrer".
	if got := openingTag(t, body, `href="https://docs.invalid"`); !strings.Contains(got, `rel="noopener noreferrer"`) {
		t.Errorf("bookmark card missing rel=noopener noreferrer, got tag:\n%s", got)
	}
}

// openingTag returns the `<a ...>` opening tag containing marker (e.g. a
// specific href="..." attribute), for asserting on other attributes of that
// same tag (like rel=) without depending on byte offsets into the whole body.
func openingTag(t *testing.T, body, marker string) string {
	t.Helper()
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("marker %q not found in body:\n%s", marker, body)
	}
	start := strings.LastIndex(body[:i], "<a ")
	if start < 0 {
		t.Fatalf("no preceding <a  for marker %q", marker)
	}
	end := strings.Index(body[start:], ">")
	if end < 0 {
		t.Fatalf("unterminated <a  tag for marker %q", marker)
	}
	return body[start : start+end+1]
}

func TestServerHeaderRendersWidgets(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "header/weather", ServiceName: testHeaderWeather, Header: true,
		Fields: []Field{{Label: labelWeather, Value: "10°C"}, {Label: labelConditions, Value: condClear}},
	})

	greeting := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testGreetName, Namespace: testNamespace},
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

// TestBuildHeaderPartitionsLeftBeforeRight verifies buildHeader stably
// reorders an interleaved left/right/left/right sequence into left-then-
// right, flagging only the first right-aligned widget with PushRight —
// header.templ's CSS-only alignment (see headerWidgetView.PushRight) relies
// on the right-aligned run being contiguous and trailing.
func TestBuildHeaderPartitionsLeftBeforeRight(t *testing.T) {
	defs := []HeaderWidget{
		{Name: "greeting", Type: headerTypeGreeting, Align: alignLeft},
		{Name: "weather", Type: testOpenMeteoType, Align: alignRight},
		{Name: "clock", Type: headerTypeDatetime, Align: alignLeft},
		{Name: testCPUName, Type: testKubeMetricsType, Align: alignRight},
	}
	views := buildHeader(defs, nil)
	if len(views) != 4 {
		t.Fatalf("buildHeader() = %d views, want 4", len(views))
	}

	wantTypes := []string{headerTypeGreeting, headerTypeDatetime, testOpenMeteoType, testKubeMetricsType}
	for i, want := range wantTypes {
		if views[i].Type != want {
			t.Errorf("views[%d].Type = %q, want %q (left widgets first, right widgets after, order preserved within each)", i, views[i].Type, want)
		}
	}
	for i, v := range views {
		wantPushRight := i == 2 // first right-aligned widget in the reordered list
		if v.PushRight != wantPushRight {
			t.Errorf("views[%d] (%s).PushRight = %v, want %v", i, v.Type, v.PushRight, wantPushRight)
		}
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
	store.Set(Card{Key: "ns/b/0", Group: testDiscoveryGroup, ServiceName: "Svc B"})

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

	header := pagev1alpha1.HeaderHidden
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
	disable := pagev1alpha1.Disabled
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
	for _, want := range []string{`class="abbr"`, "W2", "<h2>", testBookmarkGroup} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `data-group-name="bookmark:`) {
		t.Errorf("fragment body rendered a collapsible bookmark group with DisableCollapse=true:\n%s", body)
	}
}

// TestServerFragmentBookmarkGroupStyledByMatchingLayoutGroup is the
// end-to-end version of TestGroupBookmarksAppliesMatchingLayoutGroup: a
// LayoutGroupSpec sharing a bookmark group's name renders that group with
// the matching grid-row/grid-template-columns styling, exactly like it
// would a service group of the same name.
func TestServerFragmentBookmarkGroupStyledByMatchingLayoutGroup(t *testing.T) {
	bookmark := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "wiki3", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       testBookmarkGroup,
			Name:        "Wiki",
			Href:        "https://example.invalid/wiki3",
		},
	}
	style := testStyleRow
	columns := int32(3)
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Layout: []pagev1alpha1.LayoutTabSpec{
				{Groups: []pagev1alpha1.LayoutGroupSpec{{
					Name: testBookmarkGroup, Style: &style, Columns: &columns,
				}}},
			},
		},
	}
	srv := newTestServer(t, NewStore(), bookmark, cfg)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"grid-row", "grid-template-columns: repeat(3, 1fr)"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q, want it applied from the matching LayoutGroupSpec:\n%s", want, body)
		}
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
	disable := pagev1alpha1.IndexingNoIndex
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
			{Label: testLabelDisk, Value: "3", Highlight: HighlightDanger},
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
			{Label: testLabelDisk, Value: "3", Highlight: HighlightDanger},
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

	style := testStyleRow
	equalHeights := pagev1alpha1.HeightsEqual
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

func TestServerFragmentRendersServiceCardDescription(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/desc/0", Group: testGroup, ServiceName: "Described", Description: "A very fine service.",
	})
	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	want := `<div class="desc">A very fine service.</div>`
	if body := rec.Body.String(); !strings.Contains(body, want) {
		t.Errorf("fragment body missing %q, want a service card's Description rendered as visible text:\n%s", want, body)
	}
}

func TestServerIndexRendersVersionFooter(t *testing.T) {
	srv := newTestServer(t, NewStore())
	srv.Version, srv.Commit = "v1.2.3", "abc1234"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	want := `<div class="footer">v1.2.3 (abc1234)</div>`
	if !strings.Contains(body, want) {
		t.Errorf("index body missing %q:\n%s", want, body)
	}
}

func TestServerIndexHidesVersionFooterWhenConfigured(t *testing.T) {
	hide := pagev1alpha1.Enabled
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			HideVersion: &hide,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	srv.Version = "v1.2.3"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), `class="footer"`) {
		t.Errorf("index body has footer with HideVersion set:\n%s", rec.Body.String())
	}
}

func TestServerIndexRendersCustomJS(t *testing.T) {
	js := "console.log('hi'); // </script> attempt"
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			CustomJS:    &js,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "console.log('hi')") {
		t.Errorf("index body missing CustomJS content:\n%s", body)
	}
	if strings.Contains(body, "</script> attempt") {
		t.Errorf("index body has an unescaped </script> from CustomJS, want it neutralized:\n%s", body)
	}
}

// TestServerIndexServerRendersInitialFragment guards 1.7's fix: the card
// grid must be populated straight from the page shell response, not left
// empty until htmx's first /fragment poll completes.
func TestServerIndexServerRendersInitialFragment(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: testCardKeyA, Group: testGroup, ServiceName: testSvcAName})
	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, testSvcAName) {
		t.Errorf("index body missing %q, want the initial card grid server-rendered into the shell:\n%s", testSvcAName, body)
	}
	if !strings.Contains(body, `<div id="cards" hx-get="/fragment" hx-trigger="every`) {
		t.Errorf("index body's #cards div should no longer have a \"load\" hx-trigger now that content is server-rendered:\n%s", body)
	}
}

func TestServerHeaderRendersLogoWidget(t *testing.T) {
	href := "https://example.invalid"
	logo := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "logo", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Type:        headerTypeLogo,
			Icon:        strPtr("https://example.invalid/logo.png"),
			Options:     &apiextensionsv1.JSON{Raw: []byte(`{"href":"` + href + `"}`)},
		},
	}
	srv := newTestServer(t, NewStore(), logo)
	req := httptest.NewRequest(http.MethodGet, "/header", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`src="https://example.invalid/logo.png"`, `<a href="` + href + `"`} {
		if !strings.Contains(body, want) {
			t.Errorf("header body missing %q:\n%s", want, body)
		}
	}
}

func TestServerHeaderRendersLogoWidgetWithoutHref(t *testing.T) {
	logo := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "logo", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Type:        headerTypeLogo,
			Icon:        strPtr("https://example.invalid/logo.png"),
		},
	}
	srv := newTestServer(t, NewStore(), logo)
	req := httptest.NewRequest(http.MethodGet, "/header", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `src="https://example.invalid/logo.png"`) {
		t.Errorf("header body missing logo image:\n%s", body)
	}
	if strings.Contains(body, "<a href=") {
		t.Errorf("header body has a link wrapper with no href option configured:\n%s", body)
	}
}

func strPtr(s string) *string { return &s }

// TestServerIndexBoxedWidgetsStylesHeaderWidgetsNotGroupHeaders guards 1.6's
// fix: "boxedWidgets" should box the header info-widget strip specifically,
// not group headers (which "boxed" already covers) — previously the two
// header styles rendered group headers identically.
func TestServerIndexBoxedWidgetsStylesHeaderWidgetsNotGroupHeaders(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `[data-header-style="boxedWidgets"] .header-widget`) {
		t.Errorf("index body missing boxedWidgets .header-widget rule:\n%s", body)
	}
	if strings.Contains(body, `[data-header-style="boxedWidgets"] h2`) {
		t.Errorf("index body still boxes group headers for boxedWidgets, want that left to \"boxed\" only:\n%s", body)
	}
}

func TestServerFragmentRendersBookmarkIconTakesPrecedenceOverAbbr(t *testing.T) {
	icon := "https://example.invalid/wiki.png"
	abbr := "WK"
	desc := "Internal knowledge base."
	bookmark := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "wiki", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       testBookmarkGroup,
			Name:        "Wiki Three",
			Href:        "https://example.invalid/wiki3",
			Icon:        &icon,
			Abbr:        &abbr,
			Description: &desc,
		},
	}
	srv := newTestServer(t, NewStore(), bookmark)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`<img class="icon" src="` + icon + `"`, `<div class="desc">` + desc + `</div>`} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `class="abbr"`) {
		t.Errorf("fragment body has an abbr span, want Icon to take precedence over Abbr when both are set:\n%s", body)
	}
}

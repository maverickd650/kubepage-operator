package dashboard

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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
	return &Server{Store: store, Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}
}

func TestServerFragmentRendersCards(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: testFragmentCardKey, Group: testGroup, ServiceName: testServiceName,
		Fields: []Field{{Label: labelStatus, Value: statusHealthy}},
	})
	store.Set(Card{
		Key: "ns/broken/0", Group: testGroup, ServiceName: testBrokenServiceName,
		Err: testUnreachableErr,
	})

	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{testGroup, "Prometheus", statusHealthy, testBrokenServiceName, testUnreachableErr} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
}

func TestServerFragmentRevalidatesWithETag(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: testFragmentCardKey, Group: testGroup, ServiceName: testServiceName})
	srv := newTestServer(t, store)

	first := httptest.NewRecorder()
	srv.Routes().ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/fragment", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", first.Code)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first response missing ETag header")
	}

	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	req.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	srv.Routes().ServeHTTP(second, req)
	if second.Code != http.StatusNotModified {
		t.Fatalf("revalidated request status = %d, want 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Errorf("304 response body = %q, want empty", second.Body.String())
	}

	store.Set(Card{Key: testFragmentCardKey, Group: testGroup, ServiceName: testRenamedServiceName})
	third := httptest.NewRecorder()
	srv.Routes().ServeHTTP(third, httptest.NewRequest(http.MethodGet, "/fragment", nil))
	if third.Code != http.StatusOK {
		t.Fatalf("changed-data request status = %d, want 200", third.Code)
	}
	if got := third.Header().Get("ETag"); got == etag {
		t.Error("ETag unchanged after Store content changed")
	}
}

func TestServerFragmentGzipsWhenAccepted(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: testFragmentCardKey, Group: testGroup, ServiceName: testServiceName})
	srv := newTestServer(t, store)

	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	gz, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	body, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	if !strings.Contains(string(body), testServiceName) {
		t.Errorf("decompressed body missing %q:\n%s", testServiceName, body)
	}
}

// failingResponseWriter fails every Write, simulating a client that
// disconnects mid-response — used to exercise writeCachedHTML's gzip
// error-handling branch, which a well-behaved recorder never triggers.
type failingResponseWriter struct {
	header http.Header
	code   int
}

func (w *failingResponseWriter) Header() http.Header        { return w.header }
func (w *failingResponseWriter) WriteHeader(statusCode int) { w.code = statusCode }
func (w *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errTestWrite
}

var errTestWrite = errors.New("simulated write failure")

// TestServerFragmentGzipWriteErrorIsPropagated verifies writeCachedHTML
// surfaces (rather than silently swallows) a failure to write the
// gzip-compressed body, still closing the gzip writer on that path.
func TestServerFragmentGzipWriteErrorIsPropagated(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: testFragmentCardKey, Group: testGroup, ServiceName: testServiceName})
	srv := newTestServer(t, store)

	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := &failingResponseWriter{header: http.Header{}}
	srv.Routes().ServeHTTP(w, req)

	if w.code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 after a write failure", w.code)
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
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Color:        &color,
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

// TestServerIndexSampleDataBanner verifies the visible --sample-data marker
// (docs/design/local-preview.md phase 4): a preview running with
// Server.SampleData set must render an unmistakable banner, so a screenshot
// of sample data is never confused for a live dashboard; the ordinary
// in-cluster/preview path (SampleData false, the zero value) must not.
func TestServerIndexSampleDataBanner(t *testing.T) {
	const banner = "Sample data"

	plain := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	plain.Routes().ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), banner) {
		t.Error("index body contains the sample-data banner when SampleData is false")
	}

	sampled := newTestServer(t, NewStore())
	sampled.SampleData = true
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	sampled.Routes().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), banner) {
		t.Error("index body missing the sample-data banner when SampleData is true")
	}
}

func TestServerFragmentRendersStatsRow(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: testFragmentCardKey, Group: testGroup, ServiceName: testServiceName,
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
		`id="iframe-ns/dash/0"`, `hx-preserve="true"`,
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
// (the directives that govern <script>/<style> *elements*) no longer fall
// back to 'unsafe-inline' (P2.4 in docs/security-review.md): a future
// escaping regression in a @templ.Raw path should be blocked by the
// browser, not just relying on server-side escaping. script-src-attr/
// style-src-attr are a deliberate, narrower exception — see
// contentSecurityPolicy's doc comment for why inline attributes (style=,
// onclick=) need 'unsafe-inline' regardless of the nonce, since CSP nonces
// don't cover attribute values at all.
func TestSecurityHeadersUsesNonceNotUnsafeInline(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self' 'nonce-") || !strings.Contains(csp, "style-src 'self' 'nonce-") {
		t.Errorf("Content-Security-Policy = %q, want nonce-based script-src/style-src", csp)
	}
	for directive := range strings.SplitSeq(csp, "; ") {
		if (strings.HasPrefix(directive, "script-src ") || strings.HasPrefix(directive, "style-src ")) && strings.Contains(directive, "unsafe-inline") {
			t.Errorf("Content-Security-Policy directive %q must not carry 'unsafe-inline' (only the -attr variants may)", directive)
		}
	}
	for _, want := range []string{"style-src-attr 'unsafe-inline'", "script-src-attr 'unsafe-inline'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("Content-Security-Policy = %q, want %q (CSP nonces don't cover inline attributes like style= or onclick=)", csp, want)
		}
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

// TestIndexPollingStopsInBackgroundTab verifies the #cards and #header
// hx-trigger attributes fire on a plain interval (no bracketed event filter —
// htmx evaluates that via the Function constructor, which the page's
// nonce-based CSP rejects; see the htmx:beforeRequest listener in
// index.templ's inline script for the eval-free replacement), and that the
// page ships the JS-side guard that stops those requests when the tab is
// backgrounded.
func TestIndexPollingStopsInBackgroundTab(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		`id="cards" hx-ext="morph" hx-get="/fragment" hx-trigger="every 10s"`,
		`hx-ext="morph" hx-get="/header" hx-trigger="load, every 10s"`,
		`htmx.config.allowEval = false;`,
		`document.visibilityState !== "visible"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index body missing %q:\n%s", want, body)
		}
	}
}

// TestIndexReferencesVendoredHTMX verifies the page shell loads the
// vendored htmx build shipped under assets/, and that it's actually
// servable — a stale script src (e.g. left pointing at a since-removed
// version) would silently break every htmx-polled interaction.
func TestIndexReferencesVendoredHTMX(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	const script = "/assets/htmx-2.0.10.min.js"
	if !strings.Contains(body, `<script src="`+script+`">`) {
		t.Errorf("index body missing htmx script tag for %q:\n%s", script, body)
	}

	assetReq := httptest.NewRequest(http.MethodGet, script, nil)
	assetRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(assetRec, assetReq)
	if assetRec.Code != http.StatusOK {
		t.Errorf("GET %s status = %d, want 200", script, assetRec.Code)
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testBookmarkGroup,
			Bookmarks: []pagev1alpha1.BookmarkEntry{{
				Name: "Docs",
				Href: "https://example.invalid/docs",
			}},
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

// TestServerFragmentBookmarkGroupsRenderSideBySide verifies multiple
// bookmark groups render as siblings inside one ".bookmark-groups" grid
// wrapper (homepage parity: bookmark groups sit side by side across the
// page width, not stacked full-width) — see cards.templ's Cards templ and
// index.templ's .bookmark-groups rule.
func TestServerFragmentBookmarkGroupsRenderSideBySide(t *testing.T) {
	bookmarks := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "multi", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "Entertainment",
			Bookmarks:    []pagev1alpha1.BookmarkEntry{{Name: "Plex", Href: "https://example.invalid/plex"}},
		},
	}
	bookmarks2 := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "multi2", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "News",
			Bookmarks:    []pagev1alpha1.BookmarkEntry{{Name: "RSS", Href: "https://example.invalid/rss"}},
		},
	}
	srv := newTestServer(t, NewStore(), bookmarks, bookmarks2)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`class="bookmark-groups"`, `class="bookmark-group-item"`, "Entertainment", "News", "Plex", "RSS"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
	wrapperIdx := strings.Index(body, `class="bookmark-groups"`)
	group1Idx := strings.Index(body, "Entertainment")
	group2Idx := strings.Index(body, "News")
	if wrapperIdx < 0 || group1Idx < wrapperIdx || group2Idx < wrapperIdx {
		t.Errorf("fragment body: both bookmark groups must be nested inside .bookmark-groups (wrapper@%d, Entertainment@%d, News@%d):\n%s", wrapperIdx, group1Idx, group2Idx, body)
	}
}

// TestServerFragmentNoBookmarksOmitsWrapper verifies the .bookmark-groups
// wrapper div is only emitted when there's at least one bookmark group, so
// a Dashboard with no Bookmark CRDs bound keeps identical markup to before
// bookmark-groups-side-by-side existed.
func TestServerFragmentNoBookmarksOmitsWrapper(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "bookmark-groups") {
		t.Errorf("fragment body rendered .bookmark-groups wrapper with no bookmark groups:\n%s", body)
	}
}

// TestServerFragmentRendersNonHTTPBookmarkHref guards against templ's own
// URL sanitizer (which only allows http/https/mailto/tel/ftp/ftps, see
// vendored github.com/a-h/templ's url.go) silently neutering a bookmark href
// using one of the other CRD-allowlisted schemes (ssh/rdp/vnc/smb) into
// "about:invalid#TemplFailedSanitizationURL" — cards.templ's
// bookmarkCardView must cast Href to templ.SafeURL to bypass that second,
// narrower gate and trust the CRD-level allowlist as the sole check (see
// BookmarkEntry.Href's doc comment).
func TestServerFragmentRendersNonHTTPBookmarkHref(t *testing.T) {
	bookmark := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "nas", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testBookmarkGroup,
			Bookmarks: []pagev1alpha1.BookmarkEntry{{
				Name: "NAS Share",
				Href: "smb://nas.example.invalid/share",
			}},
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
	if !strings.Contains(body, `href="smb://nas.example.invalid/share"`) {
		t.Errorf("fragment body missing unsanitized smb:// href (got templ's sanitizer fallback instead?):\n%s", body)
	}
	if strings.Contains(body, "TemplFailedSanitizationURL") {
		t.Errorf("fragment body was neutered by templ's own URL sanitizer:\n%s", body)
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

// TestServerIndexEmitsQuickLaunchSearchConfig verifies the DashboardStyle's
// quick-launch toggles reach the page shell's client-side searchConfig JSON
// (gap-analysis §4.2), which index.templ's qlRender reads.
func TestServerIndexEmitsQuickLaunchSearchConfig(t *testing.T) {
	disabled := false
	hidden := false
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Search: &pagev1alpha1.SearchSpec{
				SearchDescriptions:  &disabled,
				InternetSearchEntry: &hidden,
				VisitURLEntry:       &hidden,
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

func TestServerIndexAppliesDashboardStyleTheme(t *testing.T) {
	theme := themeLight
	color := testColor
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Theme:        &theme,
			Color:        &color,
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
	disable := false
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Collapse:     &disable,
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testBookmarkGroup,
			Bookmarks: []pagev1alpha1.BookmarkEntry{{
				Name: "Wiki",
				Href: "https://example.invalid/wiki",
			}},
		},
	}
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef:   pagev1alpha1.DashboardRef{Name: testDashboardName},
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
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Title:        &title,
			StartURL:     &startURL,
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
	for _, want := range []string{`"name":"My Lab"`, `"start_url":"/dashboard"`, `"display":"standalone"`, `"src":"/assets/icon.svg"`, `"sizes":"any"`} {
		if !strings.Contains(body, want) {
			t.Errorf("manifest body missing %q:\n%s", want, body)
		}
	}
}

func TestServerAssetServesSVGIcon(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/assets/icon.svg", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want image/svg+xml", ct)
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Errorf("asset body doesn't look like SVG:\n%s", rec.Body.String())
	}
}

// TestServerServiceWorkerRoute verifies the offline app-shell service
// worker is served at the root path (not under /assets/, whose default
// registration scope wouldn't cover "/", "/manifest.json", etc. — see
// handleServiceWorker's doc comment) with a content type browsers accept
// for a service worker script, and that it doesn't get the long-lived
// immutable caching handleAsset gives every other asset.
func TestServerServiceWorkerRoute(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("Content-Type = %q, want text/javascript", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc == "" || strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want revalidate-on-every-request (not immutable)", cc)
	}
	body := rec.Body.String()
	for _, want := range []string{`addEventListener("fetch"`, "/fragment", "/header", "/events"} {
		if !strings.Contains(body, want) {
			t.Errorf("sw.js body missing %q:\n%s", want, body)
		}
	}
}

// TestServerAssetRejectsServiceWorkerFilename guards against sw.js being
// double-served under /assets/ (immutably cached, since handleAsset's
// embedded filesystem glob picks it up like any other .js asset) alongside
// its real GET /sw.js route (handleServiceWorker, no-cache) — see
// handleAsset's doc comment for why only the latter is a valid way to reach
// this script.
func TestServerAssetRejectsServiceWorkerFilename(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/assets/sw.js", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestSecurityHeadersAllowsSelfWorkerSrc guards the service worker
// registration (index.templ's navigator.serviceWorker.register("/sw.js"))
// against a future CSP tightening silently blocking it.
func TestSecurityHeadersAllowsSelfWorkerSrc(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "worker-src 'self'") {
		t.Errorf("Content-Security-Policy = %q, want a worker-src 'self' directive", csp)
	}
}

// TestIndexRegistersServiceWorker guards the page shell's registration
// script itself, not just the route/CSP it depends on.
func TestIndexRegistersServiceWorker(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `navigator.serviceWorker.register("/sw.js")`) {
		t.Errorf("index body missing service worker registration:\n%s", body)
	}
}

func TestServerRobotsRoute(t *testing.T) {
	disable := false
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Indexing:     &disable,
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
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Title:        &title,
			Favicon:      &favicon,
			CardBlur:     &cardBlur,
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testBookmarkGroup,
			Bookmarks: []pagev1alpha1.BookmarkEntry{{
				Name: "Handbook",
				Href: "https://docs.invalid",
			}},
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
		Key: "header/" + testHeaderWeather + "/0", ServiceName: testHeaderWeather, Header: true,
		Fields: []Field{{Label: labelWeather, Value: "10°C"}, {Label: labelConditions, Value: condClear}},
	})

	greeting := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testGreetName, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:    headerTypeGreeting,
				Options: &apiextensionsv1.JSON{Raw: []byte(`{"text":"Welcome"}`)},
			}},
		},
	}
	weather := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: testOpenMeteoType,
			}},
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

// TestNestGroupsMaterializesMissingAncestor verifies a card at testGroupMediaMovies
// with no direct card in testGroupMedia still gets a testGroupMedia parent group
// (header shown, zero direct cards) to render its subgroup under.
func TestNestGroupsMaterializesMissingAncestor(t *testing.T) {
	flat := groupCards([]Card{{Group: testGroupMediaMovies, ServiceName: testNameRadarr}}, Site{})
	tree := nestGroups(flat, Site{})

	if len(tree) != 1 || tree[0].Path != testGroupMedia || tree[0].Name != testGroupMedia {
		t.Fatalf("nestGroups() = %+v, want one materialized root group %q", tree, testGroupMedia)
	}
	root := tree[0]
	if len(root.Cards) != 0 {
		t.Errorf("materialized root Cards = %+v, want none", root.Cards)
	}
	if !root.Header {
		t.Error("materialized root Header = false, want true (shown)")
	}
	if len(root.Subgroups) != 1 || root.Subgroups[0].Path != testGroupMediaMovies || root.Subgroups[0].Name != testNameMovies {
		t.Fatalf("root.Subgroups = %+v, want one subgroup Path=Media/Movies Name=Movies", root.Subgroups)
	}
	if len(root.Subgroups[0].Cards) != 1 || root.Subgroups[0].Cards[0].ServiceName != testNameRadarr {
		t.Errorf("root.Subgroups[0].Cards = %+v, want [Radarr]", root.Subgroups[0].Cards)
	}
}

// TestNestGroupsRealAncestorKeepsOwnCards verifies a real testGroupMedia flat group
// (with its own direct cards) is used as the parent node rather than being
// overwritten by a zero-card materialized placeholder, regardless of
// relative order between the parent and child paths in the flat input.
func TestNestGroupsRealAncestorKeepsOwnCards(t *testing.T) {
	flat := groupCards([]Card{
		{Group: testGroupMediaMovies, ServiceName: testNameRadarr},
		{Group: testGroupMedia, ServiceName: testMultiEntryNamePlex},
	}, Site{})
	tree := nestGroups(flat, Site{})

	if len(tree) != 1 || tree[0].Path != testGroupMedia {
		t.Fatalf("nestGroups() = %+v, want one root group Media", tree)
	}
	root := tree[0]
	if len(root.Cards) != 1 || root.Cards[0].ServiceName != testMultiEntryNamePlex {
		t.Errorf("root.Cards = %+v, want [Plex] (Media's own direct card)", root.Cards)
	}
	if len(root.Subgroups) != 1 || root.Subgroups[0].Name != testNameMovies {
		t.Fatalf("root.Subgroups = %+v, want one Movies subgroup", root.Subgroups)
	}
}

// TestNestGroupsPreservesFirstSeenOrder verifies both root groups and
// sibling subgroups within a parent keep the order they first appear in the
// flat, already store-ordered input — not alphabetical.
func TestNestGroupsPreservesFirstSeenOrder(t *testing.T) {
	flat := groupCards([]Card{
		{Group: testGroupMediaTV, ServiceName: testNameSonarr},
		{Group: "Zeta", ServiceName: "z1"},
		{Group: testGroupMediaMovies, ServiceName: testNameRadarr},
		{Group: "Alpha", ServiceName: "a1"},
	}, Site{})
	tree := nestGroups(flat, Site{})

	if len(tree) != 3 || tree[0].Path != testGroupMedia || tree[1].Path != "Zeta" || tree[2].Path != "Alpha" {
		t.Fatalf("nestGroups() root order = %v, want [Media Zeta Alpha]", groupPaths(tree))
	}
	media := tree[0]
	if len(media.Subgroups) != 2 || media.Subgroups[0].Name != "TV" || media.Subgroups[1].Name != testNameMovies {
		t.Fatalf("Media.Subgroups order = %v, want [TV Movies] (first-seen)", groupPaths(media.Subgroups))
	}
}

// TestNestGroupsDepth3 verifies a 3-level path (testGroupABC, the CRD's own
// maximum depth) nests two levels deep under its root.
func TestNestGroupsDepth3(t *testing.T) {
	flat := groupCards([]Card{{Group: testGroupABC, ServiceName: testSvcName}}, Site{})
	tree := nestGroups(flat, Site{})

	if len(tree) != 1 || tree[0].Path != "A" {
		t.Fatalf("nestGroups() = %+v, want one root A", tree)
	}
	b := tree[0]
	if len(b.Subgroups) != 1 || b.Subgroups[0].Path != "A/B" || b.Subgroups[0].Name != "B" {
		t.Fatalf("A.Subgroups = %+v, want one subgroup A/B", b.Subgroups)
	}
	c := b.Subgroups[0]
	if len(c.Subgroups) != 1 || c.Subgroups[0].Path != testGroupABC || c.Subgroups[0].Name != "C" {
		t.Fatalf("A/B.Subgroups = %+v, want one subgroup A/B/C", c.Subgroups)
	}
	if len(c.Subgroups[0].Cards) != 1 || c.Subgroups[0].Cards[0].ServiceName != testSvcName {
		t.Errorf("A/B/C.Cards = %+v, want [svc]", c.Subgroups[0].Cards)
	}
}

// groupPaths collects each group's Path, for asserting on order/shape
// without repeating the full struct in a failure message.
func groupPaths(groups []cardGroup) []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = g.Path
	}
	return out
}

// TestLayoutTabsPlacesRootWithWholeSubtree verifies tab placement keys on
// root paths only: a tab listing just testGroupMedia (no path entry at all) still
// places Media's whole nested subtree, unstyled.
func TestLayoutTabsPlacesRootWithWholeSubtree(t *testing.T) {
	cards := []Card{{Group: testGroupMediaMovies, ServiceName: testNameRadarr}}
	layout := []LayoutTab{{Name: testTab1, Groups: []LayoutGroup{{Name: testGroupMedia}}}}
	tabs := layoutTabs(cards, Site{Layout: layout})

	if len(tabs) != 1 || tabs[0].Name != testTab1 || len(tabs[0].Groups) != 1 {
		t.Fatalf("layoutTabs() = %+v, want 1 tab with Media placed", tabs)
	}
	root := tabs[0].Groups[0]
	if root.Path != testGroupMedia || len(root.Subgroups) != 1 || root.Subgroups[0].Path != testGroupMediaMovies {
		t.Fatalf("tabs[0].Groups[0] = %+v, want Media with its Movies subtree intact", root)
	}
}

// TestLayoutTabsStylesSubgroupByPathEntry verifies a tab's path-named
// LayoutGroupSpec entry (e.g. testGroupMediaMovies) styles that subgroup in place
// once its root has been placed by its own root-named entry in the same tab.
func TestLayoutTabsStylesSubgroupByPathEntry(t *testing.T) {
	cards := []Card{{Group: testGroupMediaMovies, ServiceName: testNameRadarr}}
	cols := int32(2)
	layout := []LayoutTab{{Name: testTab1, Groups: []LayoutGroup{
		{Name: testGroupMedia},
		{Name: testGroupMediaMovies, Columns: &cols, Style: testStyleRow},
	}}}
	tabs := layoutTabs(cards, Site{Layout: layout})

	if len(tabs) != 1 || len(tabs[0].Groups) != 1 {
		t.Fatalf("layoutTabs() = %+v, want 1 tab with Media placed once", tabs)
	}
	root := tabs[0].Groups[0]
	if len(root.Subgroups) != 1 {
		t.Fatalf("root.Subgroups = %+v, want 1 subgroup", root.Subgroups)
	}
	movies := root.Subgroups[0]
	if movies.Columns == nil || *movies.Columns != cols || movies.Style != testStyleRow {
		t.Errorf("Media/Movies style = %+v, want columns=2 style=row (from the path-named layout entry)", movies)
	}
}

// TestLayoutTabsUnreferencedRootKeepsSubtreeUnderOther verifies a root group
// not placed by any tab is appended to the trailing "Other" tab with its
// whole nested subtree, not just its own direct cards.
func TestLayoutTabsUnreferencedRootKeepsSubtreeUnderOther(t *testing.T) {
	cards := []Card{
		{Group: "A", ServiceName: "a1"},
		{Group: testGroupMediaMovies, ServiceName: testNameRadarr},
	}
	layout := []LayoutTab{{Name: testTab1, Groups: []LayoutGroup{{Name: "A"}}}}
	tabs := layoutTabs(cards, Site{Layout: layout})

	if len(tabs) != 2 || tabs[1].Name != testOtherGroup || len(tabs[1].Groups) != 1 {
		t.Fatalf("layoutTabs() = %+v, want Media appended to Other", tabs)
	}
	other := tabs[1].Groups[0]
	if other.Path != testGroupMedia || len(other.Subgroups) != 1 || other.Subgroups[0].Path != testGroupMediaMovies {
		t.Fatalf("Other tab's Media group = %+v, want its Movies subtree intact", other)
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

// failingConfigListServer wraps a fake client so the DashboardStyleList read
// LoadSite issues first fails, exercising every handler's LoadSite-error
// branch without needing a real apiserver.
func failingConfigListServer(t *testing.T, store *Store) *Server {
	t.Helper()
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	failing := errInjectingReader{
		Reader: cl,
		failGet: func(_ client.ObjectKey, obj client.Object) bool {
			_, ok := obj.(*pagev1alpha1.DashboardStyle)
			return ok
		},
	}
	return &Server{Store: store, Reader: failing, Namespace: testNamespace, DashboardName: testDashboardName}
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
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Theme:        &theme,
			Color:        &color,
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

// TestBuildHeaderGreetingAndDatetimeHaveNoIcon guards homepage's own
// behavior: unlike every other header widget type, "greeting"/"datetime"
// never render an icon, even though neither sets its own Icon here (an unset
// Icon on a polled type like kubemetrics instead falls back to a default —
// see TestBuildHeaderDefaultIcons).
func TestBuildHeaderGreetingAndDatetimeHaveNoIcon(t *testing.T) {
	defs := []HeaderWidget{
		{Type: headerTypeGreeting, Options: map[string]string{testOptionsText: "hi"}},
		{Type: headerTypeDatetime},
	}
	views := buildHeader(defs, nil)
	for _, v := range views {
		if v.IconURL != "" {
			t.Errorf("buildHeader(%s).IconURL = %q, want \"\" (greeting/datetime never show an icon)", v.Type, v.IconURL)
		}
	}
}

// TestBuildHeaderDefaultIcons verifies every polled header widget type falls
// back to a built-in icon when its InfoWidget doesn't set its own Icon,
// matching homepage's own header widgets (which always show one) as verified
// against homepage's source: kubemetrics gets the Kubernetes logo, longhorn
// the generic disk glyph homepage's own longhorn/node.jsx draws (not a
// project logo), and openmeteo/openweathermap the Weather Icons glyph
// (homepage's own icon set for these — see openmeteo-condition-map.js/
// owm-condition-map.js) matching their current Conditions field. glances
// gets no group icon at all, same as homepage's glances.jsx: its icon lives
// per-field instead (see TestBuildHeaderFieldIcons). An explicit Icon still
// wins over the default.
func TestBuildHeaderDefaultIcons(t *testing.T) {
	tests := []struct {
		name       string
		typ        string
		icon       string
		fields     []Field
		wantSubstr string
	}{
		{name: "kubemetrics default", typ: testKubeMetricsType, wantSubstr: "simple-icons/kubernetes.svg"},
		{name: "longhorn default", typ: widgetTypeLonghorn, wantSubstr: "lucide/hard-drive.svg"},
		{name: "glances has no group icon", typ: "glances", wantSubstr: ""},
		{name: "openmeteo clear", typ: testOpenMeteoType, fields: []Field{{Label: labelConditions, Value: condClear}}, wantSubstr: testSvgDaySunny},
		{name: "openmeteo partly cloudy", typ: testOpenMeteoType, fields: []Field{{Label: labelConditions, Value: condPartlyCloudy}}, wantSubstr: testSvgDayCloudy},
		{name: "openmeteo rain showers", typ: testOpenMeteoType, fields: []Field{{Label: labelConditions, Value: condRainShowers}}, wantSubstr: "wi/day-showers.svg"},
		{name: "openweathermap thunderstorm", typ: widgetTypeOpenWeatherMap, fields: []Field{{Label: labelConditions, Value: condThunderstorm}}, wantSubstr: "wi/day-thunderstorm.svg"},
		{name: "openweathermap clouds", typ: widgetTypeOpenWeatherMap, fields: []Field{{Label: labelConditions, Value: "Clouds"}}, wantSubstr: testSvgDayCloudy},
		{name: "explicit icon wins", typ: testKubeMetricsType, icon: "https://example.com/custom.png", wantSubstr: "example.com/custom.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defs := []HeaderWidget{{Name: "w", Type: tt.typ, IconURL: tt.icon}}
			var cards []Card
			if tt.fields != nil {
				cards = []Card{{ServiceName: "w", Header: true, Fields: tt.fields}}
			}
			views := buildHeader(defs, cards)
			if len(views) != 1 {
				t.Fatalf("buildHeader(%s) = %d views, want 1", tt.typ, len(views))
			}
			got := views[0].IconURL
			if tt.wantSubstr == "" {
				if got != "" {
					t.Fatalf("buildHeader(%s).IconURL = %q, want \"\" (no default icon)", tt.typ, got)
				}
				return
			}
			if !strings.Contains(got, tt.wantSubstr) {
				t.Fatalf("buildHeader(%s).IconURL = %q, want substring %q", tt.typ, got, tt.wantSubstr)
			}
		})
	}
}

// TestBuildHeaderCorrelatesByKeyNotName is the direct buildHeader-level
// regression guard for the multi-widget-form correlation fix: two
// HeaderWidget defs sharing one Name (as every entry of a multi-widget
// InfoWidget does) must each still be joined to their own live Card, keyed
// by the distinct composite Key poller.go's pollInfoWidget and site.go's
// headerWidgets both derive — not by Name, which would collide and either
// duplicate one entry's data onto both views or silently drop the other's.
func TestBuildHeaderCorrelatesByKeyNotName(t *testing.T) {
	const sharedName = "multi-header"
	defs := []HeaderWidget{
		{Key: "header/" + sharedName + "/0", Name: sharedName, Type: testKubeMetricsType},
		{Key: "header/" + sharedName + "/1", Name: sharedName, Type: testKubeMetricsType},
	}
	cards := []Card{
		{Key: "header/" + sharedName + "/0", ServiceName: sharedName, Header: true, Fields: []Field{{Label: labelCPU, Value: "11%"}}},
		{Key: "header/" + sharedName + "/1", ServiceName: sharedName, Header: true, Fields: []Field{{Label: labelCPU, Value: "22%"}}},
	}

	views := buildHeader(defs, cards)
	if len(views) != 2 {
		t.Fatalf("buildHeader() = %d views, want 2", len(views))
	}
	got0 := fieldValues(views[0].Fields)
	got1 := fieldValues(views[1].Fields)
	if !strings.Contains(strings.Join(got0, ","), "11%") {
		t.Errorf("views[0].Fields = %v, want entry 0's own value (11%%), not entry 1's, name-based collision", got0)
	}
	if !strings.Contains(strings.Join(got1, ","), "22%") {
		t.Errorf("views[1].Fields = %v, want entry 1's own value (22%%), not entry 0's, name-based collision", got1)
	}
}

// fieldValues extracts each headerFieldView's rendered value text, for
// asserting which entry's live data a view actually carries.
func fieldValues(fields []headerFieldView) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.Value
	}
	return out
}

// TestWeatherIconURL exercises every weatherIconURL condition branch
// directly (rather than only the handful buildHeader's own tests happen to
// poll), including the case-insensitive match and the plain-sun fallback for
// an unrecognized/empty condition (e.g. "Unknown", a poll error's Field
// fallback).
func TestWeatherIconURL(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		wantSlug  string
	}{
		{name: "clear", condition: condClear, wantSlug: testSvgDaySunny},
		{name: "thunderstorm condition", condition: condThunderstorm, wantSlug: "wi/day-thunderstorm.svg"},
		{name: "showers", condition: condRainShowers, wantSlug: "wi/day-showers.svg"},
		{name: "drizzle", condition: condDrizzle, wantSlug: "wi/day-sprinkle.svg"},
		{name: "rain", condition: condRain, wantSlug: "wi/day-rain.svg"},
		{name: "snow", condition: condSnow, wantSlug: "wi/day-snow.svg"},
		{name: "smoke", condition: "Smoke", wantSlug: "wi/smoke.svg"},
		{name: "haze", condition: "Haze", wantSlug: "wi/day-haze.svg"},
		{name: "dust", condition: "Dust", wantSlug: "wi/dust.svg"},
		{name: "ash", condition: "Volcanic Ash", wantSlug: "wi/dust.svg"},
		{name: "sand", condition: "Sand", wantSlug: "wi/sandstorm.svg"},
		{name: "fog", condition: condFog, wantSlug: "wi/day-fog.svg"},
		{name: "mist", condition: "Mist", wantSlug: "wi/day-fog.svg"},
		{name: "tornado", condition: "Tornado", wantSlug: "wi/tornado.svg"},
		{name: "squall", condition: "Squall", wantSlug: "wi/strong-wind.svg"},
		{name: "partly cloudy", condition: condPartlyCloudy, wantSlug: testSvgDayCloudy},
		{name: "clouds", condition: "Clouds", wantSlug: testSvgDayCloudy},
		{name: "case insensitive", condition: "CLEAR", wantSlug: testSvgDaySunny},
		{name: "unrecognized falls back to sunny", condition: statusUnknown, wantSlug: testSvgDaySunny},
		{name: "empty falls back to sunny", condition: "", wantSlug: testSvgDaySunny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := weatherIconURL(tt.condition)
			if !strings.Contains(got, tt.wantSlug) {
				t.Errorf("weatherIconURL(%q) = %q, want substring %q", tt.condition, got, tt.wantSlug)
			}
		})
	}
}

// TestBuildHeaderFieldIcons verifies headerFields swaps a recognized
// resource-style field label (CPU/Memory/Storage) for a small icon instead
// of showing the label text, and drops field labels entirely for a weather
// widget type (openmeteo/openweathermap) — matching homepage's own compact,
// largely label-less header widgets. An unrecognized label (e.g. the
// "Status" fallback field a poll error or empty cluster produces) keeps its
// text label since there's no icon for it.
func TestBuildHeaderFieldIcons(t *testing.T) {
	tests := []struct {
		name           string
		typ            string
		field          Field
		wantIconSubstr string
		wantLabel      string
	}{
		{name: "cpu gets icon", typ: testKubeMetricsType, field: Field{Label: labelCPU, Value: "12%"}, wantIconSubstr: "lucide/cpu.svg"},
		{name: "memory gets icon", typ: testKubeMetricsType, field: Field{Label: labelMemory, Value: "45 GiB"}, wantIconSubstr: "fa6-solid/memory.svg"},
		{name: "storage gets icon", typ: widgetTypeLonghorn, field: Field{Label: labelStorage, Value: "750 / 1000 GiB"}, wantIconSubstr: "lucide/hard-drive.svg"},
		{name: "status keeps text label", typ: testKubeMetricsType, field: Field{Label: labelStatus, Value: statusUnknown}, wantLabel: labelStatus},
		{name: "weather field has no label", typ: testOpenMeteoType, field: Field{Label: labelConditions, Value: condClear}, wantLabel: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			views := headerFields(tt.typ, []Field{tt.field})
			if len(views) != 1 {
				t.Fatalf("headerFields() = %d views, want 1", len(views))
			}
			v := views[0]
			if tt.wantIconSubstr != "" {
				if !strings.Contains(v.IconURL, tt.wantIconSubstr) {
					t.Errorf("IconURL = %q, want substring %q", v.IconURL, tt.wantIconSubstr)
				}
				if v.Label != "" {
					t.Errorf("Label = %q, want \"\" when an icon is shown", v.Label)
				}
			} else {
				if v.IconURL != "" {
					t.Errorf("IconURL = %q, want \"\"", v.IconURL)
				}
				if v.Label != tt.wantLabel {
					t.Errorf("Label = %q, want %q", v.Label, tt.wantLabel)
				}
			}
		})
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
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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

// TestServerFragmentRendersNestedGroup verifies a card at "Media/Movies"
// renders a "Media" parent <details> containing a nested "Movies" <details>
// inside a "subgroups" wrapper, with the leaf's data-group-name carrying the
// full path (not just "Movies") so index.templ's collapse-state capture/
// restore keys on a value unique across same-named subgroups under
// different parents.
func TestServerFragmentRendersNestedGroup(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: "ns/radarr/0", Group: "Media/Movies", ServiceName: testNameRadarr})

	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		`<details class="group" data-group-name="Media"`,
		`class="subgroups grid"`,
		`class="subgroup-item"`,
		`<details class="group" data-group-name="Media/Movies"`,
		"Movies", testNameRadarr,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
	// Media has no direct cards (only the materialized-ancestor subgroup
	// Movies), so its own direct-cards grid must not render at all.
	if strings.Contains(body, `<div class="grid"></div>`) {
		t.Errorf("fragment body rendered an empty grid div for card-less parent group:\n%s", body)
	}
	// The parent's <details> must open before its nested Movies <details>,
	// and the "subgroups" wrapper must sit inside the parent's block (not a
	// sibling of it), for the collapse-hides-children behavior to hold.
	parentIdx := strings.Index(body, `data-group-name="Media"`)
	subgroupsIdx := strings.Index(body, `class="subgroups grid"`)
	childIdx := strings.Index(body, `data-group-name="Media/Movies"`)
	if parentIdx < 0 || subgroupsIdx < 0 || childIdx < 0 || (parentIdx >= subgroupsIdx || subgroupsIdx >= childIdx) {
		t.Errorf("fragment body ordering = parent@%d subgroups@%d child@%d, want parent < subgroups < child:\n%s", parentIdx, subgroupsIdx, childIdx, body)
	}
}

// TestServerFragmentNestedSubgroupsUseParentLayout verifies a parent group's
// Columns/Style layout entry is applied to the "subgroups" grid wrapper
// itself (not just its own direct-cards grid), so homepage-parity nested
// groups flow side by side in the parent's column count rather than
// stacking vertically — https://gethomepage.dev/configs/services/#nested-groups.
func TestServerFragmentNestedSubgroupsUseParentLayout(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: "ns/radarr/0", Group: "Media/Video Services", ServiceName: testNameRadarr})
	store.Set(Card{Key: "ns/paperless/0", Group: "Media/E-Book and File Management", ServiceName: "Paperless"})

	var cols int32 = 2
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Layout: []pagev1alpha1.LayoutTabSpec{
				{Name: testInfraTab, Groups: []pagev1alpha1.LayoutGroupSpec{{Name: "Media", Columns: &cols}}},
			},
		},
	}
	srv := newTestServer(t, store, cfg)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	want := `class="subgroups grid" style="grid-template-columns: repeat(2, 1fr);"`
	if !strings.Contains(body, want) {
		t.Errorf("fragment body missing %q:\n%s", want, body)
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

// TestServerFragmentRendersCombinedDotIndicators verifies a card with both
// an HTTP monitor (Status) and a pod monitor (PodStatus) renders two
// status-dot spans inside one status-indicators wrapper, each with its own
// class/tooltip — docs/design/combined-monitor.md's "two independent
// indicators" requirement.
func TestServerFragmentRendersCombinedDotIndicators(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/combined/0", Group: testGroup, ServiceName: testCombinedServiceName,
		Status: "Up", StatusStyle: statusStyleDot, Latency: "12ms",
		PodStatus: statusPartial, PodReadyText: "2/3 ready",
	})
	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		`class="status-indicators"`,
		`class="status-dot status-Up"`,
		`class="status-dot status-Partial"`,
		`title="Up · 12ms"`,
		`title="Partial (2/3 ready)"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
}

// TestServerFragmentRendersCombinedBasicStatusLine verifies a card with both
// monitors and StatusStyle "basic" renders one combined status line
// including both the HTTP latency and the pod ready text.
func TestServerFragmentRendersCombinedBasicStatusLine(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/combined-basic/0", Group: testGroup, ServiceName: "CombinedBasic",
		Status: "Up", StatusStyle: statusStyleBasic, Latency: "12ms",
		PodStatus: "Up", PodReadyText: "2/2 ready",
	})
	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Up (12ms) · 2/2 ready") {
		t.Errorf("fragment body missing combined basic status line:\n%s", body)
	}
}

// TestServerFragmentRendersPodOnlyStatus verifies a card with only a pod
// monitor (no HTTP monitor configured) still renders its single dot/pill —
// entries that today use podSelector alone move their result to the new
// PodStatus/PodReadyText fields (see Card's doc comment).
func TestServerFragmentRendersPodOnlyStatus(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/pod-only/0", Group: testGroup, ServiceName: "PodOnly",
		StatusStyle: statusStyleDot, PodStatus: "Up", PodReadyText: "1/1 ready",
	})
	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `class="status-dot status-Up"`) {
		t.Errorf("fragment body missing pod-only status dot:\n%s", body)
	}
	if strings.Count(body, `class="status-dot`) != 1 {
		t.Errorf("fragment body = %d status-dot spans, want exactly 1 for a pod-only card:\n%s", strings.Count(body, `class="status-dot`), body)
	}
}

func TestServerFragmentHeaderlessGroupRendersGridWithoutHeader(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: testCardKeyA, Group: testInfraGroup, ServiceName: testSvcAName})

	header := false
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testBookmarkGroup,
			Bookmarks: []pagev1alpha1.BookmarkEntry{{
				Name: "Wiki Two",
				Href: "https://example.invalid/wiki2",
				Abbr: &abbr,
			}},
		},
	}
	disable := false
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Collapse:     &disable,
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
// the matching grid-template-columns styling, exactly like it would a
// service group of the same name. It also pins the homepage-semantics rule
// that `style: row` + `columns: N` means a wrapping N-column grid — the
// grid-row scroller class must NOT be emitted when columns are set (see
// gridClasses), or the inline repeat(N, 1fr) never gets a row to wrap into.
func TestServerFragmentBookmarkGroupStyledByMatchingLayoutGroup(t *testing.T) {
	bookmark := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "wiki3", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testBookmarkGroup,
			Bookmarks: []pagev1alpha1.BookmarkEntry{{
				Name: "Wiki",
				Href: "https://example.invalid/wiki3",
			}},
		},
	}
	style := testStyleRow
	columns := int32(3)
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
	if !strings.Contains(body, "grid-template-columns: repeat(3, 1fr)") {
		t.Errorf("fragment body missing repeat(3, 1fr), want it applied from the matching LayoutGroupSpec:\n%s", body)
	}
	if strings.Contains(body, "grid-row") {
		t.Errorf("fragment body has grid-row despite explicit columns — style:row + columns must render a wrapping grid, not the single-row scroller:\n%s", body)
	}
}

func TestServerHeaderRendersErrAndDatetimeWidget(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "header/" + testHeaderWeather + "/0", ServiceName: testHeaderWeather, Header: true,
		Err: "upstream unreachable",
	})

	clock := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testClockName, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:    headerTypeDatetime,
				Options: &apiextensionsv1.JSON{Raw: []byte(`{"format":"medium"}`)},
			}},
		},
	}
	weather := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: testOpenMeteoType,
			}},
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
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Background:   &pagev1alpha1.BackgroundSpec{Image: &img},
			CustomCSS:    &css,
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
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Theme:        &theme,
			Color:        &color,
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
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Description:  &desc,
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
	disable := false
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Indexing:     &disable,
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
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Theme:        &theme,
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
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Color:        &color,
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
		Key: "header/" + testHeaderWeather + "/0", ServiceName: testHeaderWeather, Header: true,
		Fields: []Field{
			{Label: "load", Value: "1", Highlight: HighlightGood},
			{Label: "mem", Value: "2", Highlight: HighlightWarn},
			{Label: testLabelDisk, Value: "3", Highlight: HighlightDanger},
		},
	})
	weather := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: testOpenMeteoType,
			}},
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

// TestServerHeaderRendersFieldIcon exercises header.templ's own IconURL !=
// "" branch end to end through the /header endpoint — TestBuildHeaderFieldIcons
// only checks headerFields' output data, not that header.templ actually
// renders an <img class="field-icon"> for it instead of falling through to
// the plain-text label branch.
func TestServerHeaderRendersFieldIcon(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "header/" + testKubeMetricsType + "/0", ServiceName: testKubeMetricsType, Header: true,
		Fields: []Field{{Label: labelCPU, Value: "12%"}},
	})
	kube := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testKubeMetricsType, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: testKubeMetricsType,
			}},
		},
	}
	srv := newTestServer(t, store, kube)
	req := httptest.NewRequest(http.MethodGet, "/header", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `class="field-icon"`) {
		t.Errorf("header body missing a field-icon <img> for a CPU field:\n%s", body)
	}
	if strings.Contains(body, `class="label"`) {
		t.Errorf("header body has a text label alongside a field icon, want the icon to replace it:\n%s", body)
	}
}

func TestServerFragmentRendersGridRowAndEqualHeightStyles(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: "ns/row/0", Group: testGroup, ServiceName: testServiceName})

	style := testStyleRow
	equalHeights := true
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
	hide := true
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			HideVersion:  &hide,
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
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			CustomJS:     &js,
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
	if !strings.Contains(body, `<div id="cards" hx-ext="morph" hx-get="/fragment" hx-trigger="every`) {
		t.Errorf("index body's #cards div should no longer have a \"load\" hx-trigger now that content is server-rendered:\n%s", body)
	}
}

func TestServerHeaderRendersLogoWidget(t *testing.T) {
	href := "https://example.invalid"
	logo := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "logo", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:    headerTypeLogo,
				Icon:    new("https://example.invalid/logo.png"),
				Options: &apiextensionsv1.JSON{Raw: []byte(`{"href":"` + href + `"}`)},
			}},
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: headerTypeLogo,
				Icon: new("https://example.invalid/logo.png"),
			}},
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

// TestServerIndexBoxedWidgetsStylesHeaderWidgetsNotGroupHeaders keeps the
// Homepage contract: ordinary information widgets are unboxed, and only
// "boxedWidgets" gives them the card treatment. "boxed"/"underlined" style
// the header info-widget strip as a whole, not the service/bookmark group
// headers, which stay unstyled by headerStyle entirely.
func TestServerIndexBoxedWidgetsStylesHeaderWidgetsNotGroupHeaders(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `[data-header-style="boxedWidgets"] .header-widget`) {
		t.Errorf("index body missing boxedWidgets .header-widget rule:\n%s", body)
	}
	if !strings.Contains(body, `.header-widget { display: flex; align-items: center; gap: 0.5rem; font-size: 0.9rem; }`) {
		t.Errorf("index body gives ordinary (unscoped) header widgets a card background:\n%s", body)
	}
	if !strings.Contains(body, "[data-header-style=\"boxedWidgets\"] .header-widget {\n\t\t\t\t\tbackground: var(--panel);") {
		t.Errorf("index body does not give boxedWidgets header widgets a card background:\n%s", body)
	}
	if strings.Contains(body, `[data-header-style="boxedWidgets"] h2`) {
		t.Errorf("index body boxes group headers for boxedWidgets, want group headers left unstyled by headerStyle:\n%s", body)
	}
	if !strings.Contains(body, `[data-header-style="boxed"] .header-strip`) {
		t.Errorf("index body missing boxed .header-strip rule:\n%s", body)
	}
	if !strings.Contains(body, `[data-header-style="underlined"] .header-strip`) {
		t.Errorf("index body missing underlined .header-strip rule:\n%s", body)
	}
	if strings.Contains(body, `[data-header-style="boxed"] h2`) || strings.Contains(body, `[data-header-style="underlined"] h2`) {
		t.Errorf("index body still styles group headers via headerStyle, want that left to .header-strip only:\n%s", body)
	}
}

func TestServerFragmentRendersBookmarkIconTakesPrecedenceOverAbbr(t *testing.T) {
	icon := "https://example.invalid/wiki.png"
	abbr := "WK"
	desc := "Internal knowledge base."
	bookmark := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "wiki", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testBookmarkGroup,
			Bookmarks: []pagev1alpha1.BookmarkEntry{{
				Name:        "Wiki Three",
				Href:        "https://example.invalid/wiki3",
				Icon:        &icon,
				Abbr:        &abbr,
				Description: &desc,
			}},
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

// TestServerEventsWithoutBroadcastReturns501 verifies GET /events fails
// clearly (rather than hanging or panicking) for a Server built without a
// Broadcast wired up, e.g. by direct construction in another test.
func TestServerEventsWithoutBroadcastReturns501(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

// waitUntilTimeout is the fixed deadline every waitUntil call in this file
// uses — long enough to absorb scheduling jitter on a loaded CI runner,
// short enough that a genuinely stuck test still fails promptly.
const waitUntilTimeout = time.Second

// waitUntil polls cond every 5ms until it reports true or waitUntilTimeout
// elapses, failing the test in the latter case.
func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitUntilTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", waitUntilTimeout)
}

// syncRecorder is httptest.ResponseRecorder's role for a handler under test
// that writes from a background goroutine (handleEvents' SSE loop) while the
// test goroutine concurrently reads its state — httptest.ResponseRecorder's
// own Body/Code fields aren't safe for that.
type syncRecorder struct {
	mu     sync.Mutex
	header http.Header
	code   int
	body   bytes.Buffer
}

func newSyncRecorder() *syncRecorder { return &syncRecorder{header: http.Header{}} }

func (r *syncRecorder) Header() http.Header { return r.header }

func (r *syncRecorder) WriteHeader(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.code = code
}

func (r *syncRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.body.Write(b)
}

func (r *syncRecorder) Flush() {}

func (r *syncRecorder) Code() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.code
}

func (r *syncRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.String()
}

// TestServerEventsPushesFragmentChanged verifies handleEvents only emits a
// fragmentChanged SSE event once the fragment's rendered content actually
// differs from what it was when the connection opened — the same
// content-hash rule writeCachedHTML's ETag uses — and that a Publish whose
// underlying content didn't change stays silent.
func TestServerEventsPushesFragmentChanged(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: testFragmentCardKey, Group: testGroup, ServiceName: testServiceName})
	srv := newTestServer(t, store)
	broadcast := NewBroadcaster()
	srv.Broadcast = broadcast

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	rec := newSyncRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleEvents(rec, req)
	}()

	waitUntil(t, func() bool { return rec.Code() == http.StatusOK })
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}

	// A cycle that doesn't change anything (no Store mutation) must not
	// produce an event. Real callers (Poller.pollOnce) compute the hashes
	// once per cycle and pass them to Publish; the test does the same via
	// the package-level currentHashes helper.
	fragment, header := currentHashes(ctx, srv.Reader, srv.Namespace, srv.DashboardName, srv.Store)
	broadcast.Publish(fragment, header)
	time.Sleep(20 * time.Millisecond)
	if strings.Contains(rec.String(), "event:") {
		t.Errorf("unchanged Publish produced an SSE event:\n%s", rec.String())
	}

	store.Set(Card{Key: testFragmentCardKey, Group: testGroup, ServiceName: testRenamedServiceName})
	fragment, header = currentHashes(ctx, srv.Reader, srv.Namespace, srv.DashboardName, srv.Store)
	broadcast.Publish(fragment, header)
	waitUntil(t, func() bool { return strings.Contains(rec.String(), "event: fragmentChanged") })

	cancel()
	<-done
}

// TestServerEventsPushesHeaderChanged is TestServerEventsPushesFragmentChanged's
// counterpart for the header side: mutating a header-only card (Header:
// true, so it's excluded from the fragment's card grid — see serviceCards)
// must fire headerChanged without also firing fragmentChanged.
func TestServerEventsPushesHeaderChanged(t *testing.T) {
	store := NewStore()
	headerKey := "header/" + testHeaderWeather + "/0"
	store.Set(Card{
		Key: headerKey, ServiceName: testHeaderWeather, Header: true,
		Fields: []Field{{Label: labelWeather, Value: "5°C"}},
	})
	weather := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: testOpenMeteoType,
			}},
		},
	}
	srv := newTestServer(t, store, weather)
	broadcast := NewBroadcaster()
	srv.Broadcast = broadcast

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	rec := newSyncRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleEvents(rec, req)
	}()

	waitUntil(t, func() bool { return rec.Code() == http.StatusOK })

	store.Set(Card{
		Key: headerKey, ServiceName: testHeaderWeather, Header: true,
		Fields: []Field{{Label: labelWeather, Value: "20°C"}},
	})
	fragment, header := currentHashes(ctx, srv.Reader, srv.Namespace, srv.DashboardName, srv.Store)
	broadcast.Publish(fragment, header)
	waitUntil(t, func() bool { return strings.Contains(rec.String(), "event: headerChanged") })

	if strings.Contains(rec.String(), "event: fragmentChanged") {
		t.Errorf("a header-only card change fired fragmentChanged too:\n%s", rec.String())
	}

	cancel()
	<-done
}

// failingWriteRecorder is an http.ResponseWriter/http.Flusher whose Write
// always fails after the response header, simulating a client that stopped
// reading (or a connection reset) so a heartbeat/event write to it errors
// out immediately — standing in for the real failure mode sseWriteTimeout
// guards against (a stalled write blocking forever instead of erroring).
type failingWriteRecorder struct {
	header http.Header

	mu   sync.Mutex
	code int
}

func newFailingWriteRecorder() *failingWriteRecorder {
	return &failingWriteRecorder{header: http.Header{}}
}

func (r *failingWriteRecorder) Header() http.Header { return r.header }

func (r *failingWriteRecorder) WriteHeader(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.code = code
}

func (r *failingWriteRecorder) Code() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.code
}

func (r *failingWriteRecorder) Write([]byte) (int, error) {
	return 0, errors.New("simulated write failure: peer not reading")
}

func (r *failingWriteRecorder) Flush() {}

// TestServerEventsWriteErrorReleasesSubscriberSlot verifies that a write
// failure on the SSE connection (e.g. sseWriteTimeout firing because a
// client stopped reading) makes handleEvents return promptly and release its
// Broadcaster subscriber slot, rather than blocking forever and permanently
// pinning one of the maxSSESubscribers slots.
func TestServerEventsWriteErrorReleasesSubscriberSlot(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: testFragmentCardKey, Group: testGroup, ServiceName: testServiceName})
	srv := newTestServer(t, store)
	broadcast := NewBroadcaster()
	srv.Broadcast = broadcast

	ctx := t.Context()
	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	rec := newFailingWriteRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleEvents(rec, req)
	}()

	waitUntil(t, func() bool { return rec.Code() == http.StatusOK })

	// Mutate the store so the published hashes differ from the connection's
	// baseline (computed at connect time from the same store) — otherwise
	// handleEvents would see no change and never attempt the write that's
	// meant to fail here. The initial header write (w.WriteHeader +
	// flusher.Flush) doesn't go through sseWrite, but this event write does.
	store.Set(Card{Key: testFragmentCardKey, Group: testGroup, ServiceName: testRenamedServiceName})
	fragment, header := currentHashes(ctx, srv.Reader, srv.Namespace, srv.DashboardName, srv.Store)
	broadcast.Publish(fragment, header)

	select {
	case <-done:
	case <-time.After(waitUntilTimeout):
		t.Fatal("handleEvents did not return after a failed write")
	}

	broadcast.mu.Lock()
	remaining := len(broadcast.subs)
	broadcast.mu.Unlock()
	if remaining != 0 {
		t.Errorf("subscribers = %d after handleEvents returned, want 0 (slot leaked)", remaining)
	}
}

// TestServerEventsRejectsPastMaxSubscribers verifies handleEvents responds
// 503 (rather than opening a goroutine and channel) once Broadcast is
// already at maxSSESubscribers — bounding the resource an unauthenticated
// client can force the dashboard pod to hold open indefinitely.
func TestServerEventsRejectsPastMaxSubscribers(t *testing.T) {
	store := NewStore()
	srv := newTestServer(t, store)
	broadcast := NewBroadcaster()
	srv.Broadcast = broadcast

	for i := range maxSSESubscribers {
		if _, ok := broadcast.Subscribe(); !ok {
			t.Fatalf("Subscribe() ok = false at subscriber %d, want true below the cap", i)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	rec := httptest.NewRecorder()
	srv.handleEvents(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

// Key fixtures for the mergeServiceCards tests below: three widgets of one
// ServiceCard entry (entryIdx 0), a second entry (entryIdx 1) that must never
// merge with the first, and that entry's monitor-only variant.
const (
	testMergeKeyW0      = "ns/multi/0/0"
	testMergeKeyW1      = "ns/multi/0/1"
	testMergeKeyW2      = "ns/multi/0/2"
	testMergeKeyEntry1  = "ns/multi/1/0"
	testMergeKeyMonitor = "ns/multi/0/monitor"
)

// TestMergeServiceCardsCombinesMultiWidgetEntry verifies a ServiceCard entry
// with several widgets (poller.go stores one Card per widget instance, key
// namespace/crName/entryIdx/widgetIdx) renders as one merged card carrying
// every widget's Fields, in widget-index order — the bug-1 fix (homepage
// parity: one card per service, not one per widget).
func TestMergeServiceCardsCombinesMultiWidgetEntry(t *testing.T) {
	cards := []Card{
		{Key: testMergeKeyW0, Group: testGroup, ServiceName: testServiceName, Fields: []Field{{Label: "A", Value: "1"}}},
		{Key: testMergeKeyW1, Group: testGroup, ServiceName: testServiceName, Fields: []Field{{Label: "B", Value: "2"}}},
		{Key: testMergeKeyW2, Group: testGroup, ServiceName: testServiceName, Fields: []Field{{Label: "C", Value: "3"}}},
	}
	got := mergeServiceCards(cards)
	if len(got) != 1 {
		t.Fatalf("mergeServiceCards() = %d cards, want 1", len(got))
	}
	merged := got[0]
	if merged.Key != testMergeKeyW0 {
		t.Errorf("merged.Key = %q, want %q (lowest widget index)", merged.Key, testMergeKeyW0)
	}
	if len(merged.Fields) != 3 {
		t.Fatalf("merged.Fields = %+v, want 3 fields", merged.Fields)
	}
	for i, wantLabel := range []string{"A", "B", "C"} {
		if merged.Fields[i].Label != wantLabel {
			t.Errorf("merged.Fields[%d].Label = %q, want %q (widget-index order)", i, merged.Fields[i].Label, wantLabel)
		}
	}
}

// TestMergeServiceCardsLeavesSingleWidgetEntryUnchanged verifies a
// single-widget entry passes through mergeServiceCards untouched.
func TestMergeServiceCardsLeavesSingleWidgetEntryUnchanged(t *testing.T) {
	cards := []Card{{Key: testMergeKeyW0, Group: testGroup, ServiceName: testServiceName, Fields: []Field{{Label: "A", Value: "1"}}}}
	got := mergeServiceCards(cards)
	if len(got) != 1 || got[0].Key != testMergeKeyW0 || len(got[0].Fields) != 1 {
		t.Fatalf("mergeServiceCards() = %+v, want the single card unchanged", got)
	}
}

// TestMergeServiceCardsLeavesMonitorOnlyEntryUnchanged verifies a
// monitor-only entry (no widgets, key .../monitor — see poller.go's
// pollOnce) passes through unchanged; it never merges with anything else
// since it's the only card at its group key.
func TestMergeServiceCardsLeavesMonitorOnlyEntryUnchanged(t *testing.T) {
	cards := []Card{{Key: testMergeKeyMonitor, Group: testGroup, ServiceName: testServiceName, Status: "Up"}}
	got := mergeServiceCards(cards)
	if len(got) != 1 || got[0].Key != testMergeKeyMonitor || got[0].Status != "Up" {
		t.Fatalf("mergeServiceCards() = %+v, want the monitor-only card unchanged", got)
	}
}

// TestMergeServiceCardsNeverMergesDifferentEntries verifies two different
// ServiceCard entries (different entryIdx, so different key prefixes) never
// merge into one card, even though both entries share every identity field.
func TestMergeServiceCardsNeverMergesDifferentEntries(t *testing.T) {
	cards := []Card{
		{Key: testMergeKeyW0, Group: testGroup, ServiceName: testServiceName},
		{Key: testMergeKeyEntry1, Group: testGroup, ServiceName: testServiceName},
	}
	got := mergeServiceCards(cards)
	if len(got) != 2 {
		t.Fatalf("mergeServiceCards() = %d cards, want 2 (different entries never merge)", len(got))
	}
}

// TestMergeServiceCardsJoinsErrs verifies each widget's non-empty Err is
// joined with "; " in the merged card, and an empty Err from one widget
// contributes nothing to the join.
func TestMergeServiceCardsJoinsErrs(t *testing.T) {
	cards := []Card{
		{Key: testMergeKeyW0, Group: testGroup, ServiceName: testServiceName, Err: "widget A failed"},
		{Key: testMergeKeyW1, Group: testGroup, ServiceName: testServiceName},
		{Key: testMergeKeyW2, Group: testGroup, ServiceName: testServiceName, Err: "widget C failed"},
	}
	got := mergeServiceCards(cards)
	if len(got) != 1 {
		t.Fatalf("mergeServiceCards() = %d cards, want 1", len(got))
	}
	if want := "widget A failed; widget C failed"; got[0].Err != want {
		t.Errorf("merged.Err = %q, want %q", got[0].Err, want)
	}
}

// TestMergeServiceCardsLeavesDiscoveryCardsUnchanged verifies
// discovery-sourced cards (see discovery.go, Key shaped "discovery/ns/name"
// — not the poller's namespace/crName/entryIdx/widgetIdx shape) never merge
// with each other just because they happen to share a namespace.
func TestMergeServiceCardsLeavesDiscoveryCardsUnchanged(t *testing.T) {
	cards := []Card{
		{Key: "discovery/ns/svc-a", Group: testGroup, ServiceName: testSvcAName},
		{Key: "discovery/ns/svc-b", Group: testGroup, ServiceName: "Svc D"},
	}
	got := mergeServiceCards(cards)
	if len(got) != 2 {
		t.Fatalf("mergeServiceCards() = %d cards, want 2 (discovery cards never merge on namespace alone)", len(got))
	}
}

// TestMergeServiceCardsLeavesDigitNamedDiscoveryCardsUnchanged is the
// regression guard for the discovery-name collision: a discovered resource's
// name is a DNS-1123 label, which can be all-digits or literally "monitor" —
// the exact trailing segments mergeCardGroupKey strips for poller keys. Two
// such discovered services in one namespace must still render as separate
// cards, not collapse into one.
func TestMergeServiceCardsLeavesDigitNamedDiscoveryCardsUnchanged(t *testing.T) {
	cards := []Card{
		{Key: "discovery/ns/123", Group: testGroup, ServiceName: testSvcAName, Fields: []Field{{Label: "A", Value: "1"}}},
		{Key: "discovery/ns/456", Group: testGroup, ServiceName: "Svc D", Fields: []Field{{Label: "B", Value: "2"}}},
		{Key: "discovery/ns/monitor", Group: testGroup, ServiceName: "Svc M", Fields: []Field{{Label: "C", Value: "3"}}},
	}
	got := mergeServiceCards(cards)
	if len(got) != 3 {
		t.Fatalf("mergeServiceCards() = %d cards, want 3 (digit-/monitor-named discovery cards never merge)", len(got))
	}
}

// TestMergeServiceCardsOrdersTenPlusWidgetsNumerically is the regression guard
// for the ≥10-widget field-ordering bug: cards arrive in Store.Snapshot order,
// whose final tiebreaker string-compares the Key and so puts ".../10" before
// ".../2". A ten-plus-widget entry must still concatenate its Fields (and pick
// its identity card) in numeric widget-index order.
func TestMergeServiceCardsOrdersTenPlusWidgetsNumerically(t *testing.T) {
	const n = 11
	// Build cards in the lexicographic Key order Store.Snapshot yields, so a
	// merge that trusts input order would emit them mis-sorted.
	keys := make([]string, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("ns/multi/0/%d", i)
	}
	slices.Sort(keys) // "…/0", "…/1", "…/10", "…/2", …
	cards := make([]Card, 0, n)
	for _, k := range keys {
		idx := k[strings.LastIndex(k, "/")+1:]
		cards = append(cards, Card{Key: k, Group: testGroup, ServiceName: testServiceName, Fields: []Field{{Label: idx, Value: idx}}})
	}

	got := mergeServiceCards(cards)
	if len(got) != 1 {
		t.Fatalf("mergeServiceCards() = %d cards, want 1", len(got))
	}
	merged := got[0]
	if merged.Key != "ns/multi/0/0" {
		t.Errorf("merged.Key = %q, want %q (lowest widget index)", merged.Key, "ns/multi/0/0")
	}
	if len(merged.Fields) != n {
		t.Fatalf("merged.Fields = %d, want %d", len(merged.Fields), n)
	}
	for i := range merged.Fields {
		if want := fmt.Sprintf("%d", i); merged.Fields[i].Label != want {
			t.Errorf("merged.Fields[%d].Label = %q, want %q (numeric widget-index order)", i, merged.Fields[i].Label, want)
		}
	}
}

// TestOrderSubgroupsFollowsLayoutOrder verifies layoutTabs reorders a tab's
// path-named subgroup entries to match their order in the DashboardStyle's
// layout (bug-2 fix), not nestGroups' first-seen/alphabetical order — repro:
// a tab listing "Media/TV" before "Media/Movies" (reverse of first-seen
// order below) renders TV first.
func TestOrderSubgroupsFollowsLayoutOrder(t *testing.T) {
	cards := []Card{
		{Group: testGroupMediaMovies, ServiceName: testNameRadarr},
		{Group: testGroupMediaTV, ServiceName: testNameSonarr},
	}
	layout := []LayoutTab{{Name: testTab1, Groups: []LayoutGroup{
		{Name: testGroupMedia},
		{Name: testGroupMediaTV},
		{Name: testGroupMediaMovies},
	}}}
	tabs := layoutTabs(cards, Site{Layout: layout})
	if len(tabs) != 1 || len(tabs[0].Groups) != 1 {
		t.Fatalf("layoutTabs() = %+v, want 1 tab with Media placed", tabs)
	}
	root := tabs[0].Groups[0]
	if len(root.Subgroups) != 2 || root.Subgroups[0].Path != testGroupMediaTV || root.Subgroups[1].Path != testGroupMediaMovies {
		t.Fatalf("root.Subgroups = %v, want [Media/TV Media/Movies] (layout order)", groupPaths(root.Subgroups))
	}
}

// TestOrderSubgroupsUnlistedSubgroupSortsAfterListed verifies a subgroup with
// no path entry in the tab's layout keeps its place after every listed
// subgroup, rather than disappearing or reordering unpredictably.
func TestOrderSubgroupsUnlistedSubgroupSortsAfterListed(t *testing.T) {
	cards := []Card{
		{Group: testGroupMediaMovies, ServiceName: testNameRadarr},
		{Group: testGroupMediaTV, ServiceName: testNameSonarr},
		{Group: "Media/Music", ServiceName: "Lidarr"},
	}
	layout := []LayoutTab{{Name: testTab1, Groups: []LayoutGroup{
		{Name: testGroupMedia},
		{Name: testGroupMediaTV},
	}}}
	tabs := layoutTabs(cards, Site{Layout: layout})
	root := tabs[0].Groups[0]
	if len(root.Subgroups) != 3 {
		t.Fatalf("root.Subgroups = %+v, want 3 subgroups", root.Subgroups)
	}
	if root.Subgroups[0].Path != testGroupMediaTV {
		t.Errorf("root.Subgroups[0].Path = %q, want %q (listed first)", root.Subgroups[0].Path, testGroupMediaTV)
	}
	// Movies and Music are both unlisted: they keep first-seen order,
	// after TV.
	if root.Subgroups[1].Path != testGroupMediaMovies || root.Subgroups[2].Path != "Media/Music" {
		t.Fatalf("root.Subgroups[1:] = %v, want [Media/Movies Media/Music] (unlisted, first-seen order, after listed)", groupPaths(root.Subgroups[1:]))
	}
}

// TestOrderSubgroupsUntouchedWhenNoLayout verifies the no-layout fallback
// path (layoutTabs' single unnamed tab) never calls orderSubgroups — nested
// groups keep nestGroups' first-seen order exactly as before this fix.
func TestOrderSubgroupsUntouchedWhenNoLayout(t *testing.T) {
	cards := []Card{
		{Group: testGroupMediaTV, ServiceName: testNameSonarr},
		{Group: testGroupMediaMovies, ServiceName: testNameRadarr},
	}
	tabs := layoutTabs(cards, Site{})
	if len(tabs) != 1 || len(tabs[0].Groups) != 1 {
		t.Fatalf("layoutTabs() = %+v, want 1 unnamed tab with Media", tabs)
	}
	root := tabs[0].Groups[0]
	if len(root.Subgroups) != 2 || root.Subgroups[0].Path != testGroupMediaTV || root.Subgroups[1].Path != testGroupMediaMovies {
		t.Fatalf("root.Subgroups = %v, want first-seen order [Media/TV Media/Movies]", groupPaths(root.Subgroups))
	}
}

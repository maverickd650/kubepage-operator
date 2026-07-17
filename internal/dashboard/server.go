package dashboard

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Header InfoWidget types rendered client-side / statically (no poll), as
// opposed to a registered pollable widget like openmeteo.
const (
	headerTypeGreeting = "greeting"
	headerTypeDatetime = "datetime"
	headerTypeLogo     = "logo"
)

// assetFS holds static assets served verbatim under /assets/ — the
// self-hosted Manrope font and a vendored htmx build, embedded so the single
// binary needs no CDN (D11) and the dashboard keeps working air-gapped or if
// a third-party CDN is unreachable/compromised.
//
//go:embed assets/*.woff2 assets/*.js assets/*.svg
var assetFS embed.FS

// pwaIconPath is the app icon's asset path, referenced by handleManifest's
// icons array — an SVG with "sizes": "any" satisfies every installability
// checker (Chrome, Android) without needing multiple raster sizes.
const pwaIconPath = "/assets/icon.svg"

// cardGroup is a display-ready group of cards sharing a ServiceCard Group,
// in the order Store.Snapshot already produced (Order, then name). Columns/
// Style/IconURL/Header/InitiallyCollapsed/UseEqualHeights come from the
// Dashboard's spec.style's Layout, when one places this group (or, for a nested
// subgroup, its full Path) in a tab, already resolved against the Site-wide
// defaults by layoutTabs.
//
// Path is the group's full "/"-separated address (e.g. "Media/Movies"; a
// top-level group's Path has no "/"); Name is just the leaf segment, used
// for the rendered heading. Subgroups holds any nested child groups (see
// nestGroups), rendered inside this group's own block, after its direct
// Cards — homepage parity for nested service groups
// (https://gethomepage.dev/configs/services/#nested-groups), see
// docs/design/nested-groups.md.
type cardGroup struct {
	Path               string
	Name               string
	Cards              []Card
	Subgroups          []cardGroup
	Columns            *int32
	Style              string
	IconURL            string
	Header             bool
	InitiallyCollapsed bool
	UseEqualHeights    bool
}

// layoutTab is one tab's display-ready groups.
type layoutTab struct {
	Name   string
	Groups []cardGroup
}

// otherTabName labels the trailing tab holding Groups not placed by any
// Layout tab, so nothing silently disappears from the dashboard.
const otherTabName = "Other"

// Server serves the card-grid dashboard backed by Store: GET / returns the
// page shell, GET /fragment returns just the card markup for htmx to poll
// into it. Splitting the two means the browser tab never reloads the whole
// page on refresh, only the data.
type Server struct {
	Store          *Store
	Reader         client.Reader
	Namespace      string
	DashboardName  string
	RefreshSeconds int

	// Broadcast, when set, backs GET /events (Server-Sent Events): the page
	// shell subscribes to it and re-fetches /fragment or /header — via a
	// morph swap, so unrelated DOM state (scroll position, open <details>)
	// survives — the moment a poll cycle actually changes their rendered
	// output, instead of waiting up to RefreshSeconds for htmx's own poll.
	// Interval polling stays wired up in index.templ regardless, both as a
	// fallback for a browser with SSE disabled/blocked and to recover from a
	// dropped connection. nil (e.g. in tests that construct a Server
	// directly) makes GET /events answer 501, leaving polling as the only
	// refresh path — the same behavior as before this field existed.
	Broadcast *Broadcaster

	// SecretReader resolves the basic-auth Secret (spec.auth.basicAuthSecretRef),
	// deliberately uncached — see basicAuthMiddleware/loadBasicAuth and
	// poller.go's resolveSecret for the same rule applied to widget secrets.
	SecretReader client.Reader

	// Version/Commit are stamped at build time (see cmd/main.go), shown in
	// the page shell's footer unless Site.HideVersion is set.
	Version string
	Commit  string

	// SampleData is always false for in-cluster dashboard mode; RunPreview
	// sets it from PreviewOptions.SampleData when --sample-data is passed.
	// index.templ renders a visible banner when set, so a screenshot of a
	// sample-data preview can never be mistaken for a live dashboard.
	SampleData bool
}

// indexData is the page shell's template data: site-wide look (theme/
// color/background/search) plus the htmx poll interval. Fragment is the
// initial card grid, server-rendered into the shell so the page never shows
// an empty grid while htmx's first /fragment poll is in flight.
type indexData struct {
	Site           Site
	AccentHex      string
	Ramp           Ramp
	RefreshSeconds int
	Fragment       fragmentData
	Version        string
	Commit         string

	// SampleData mirrors Server.SampleData, rendered as a visible banner —
	// see that field's doc comment.
	SampleData bool

	// AuthEnabled mirrors basicAuthMiddleware's own "is spec.auth.
	// basicAuthSecretRef set" check. index.templ uses it to skip
	// registering the offline-shell service worker (assets/sw.js) entirely
	// on a password-protected Dashboard: the worker's Cache Storage entries
	// aren't themselves gated by HTTP Basic Auth, so caching an
	// authenticated page shell would let anyone with local access to the
	// browser profile read its last-cached contents offline, with no
	// credential check at all — see sw.js's own doc comment for the
	// (unrelated, and fine) reasoning about why the *content* it does cache
	// stays internally consistent.
	AuthEnabled bool
}

// fragmentData is the polled fragment's template data: the live widget
// cards plus the static bookmark cards, both grouped for display.
type fragmentData struct {
	Tabs           []layoutTab
	BookmarkGroups []BookmarkGroup
	// SiteTarget is the default link target a card uses when it has no
	// per-card Target override.
	SiteTarget string

	// DisableCollapse disables the collapsible control on every group
	// header (service and bookmark groups alike).
	DisableCollapse bool
	// BookmarksIconsOnly is the Site-wide bookmark card style; unlike
	// InitiallyCollapsed/Columns/Style/Icon (resolved per bookmark group by
	// groupBookmarks, see BookmarkGroup's doc comment), homepage has no
	// per-group icons-only override to mirror.
	BookmarksIconsOnly bool
}

// headerWidgetView is one rendered header widget: a static definition joined
// with its live polled value (openmeteo) when one exists.
type headerWidgetView struct {
	Type     string
	Greeting string
	Format   string
	IconURL  string
	// Href optionally wraps the "logo" widget type's image in a link.
	Href   string
	Fields []headerFieldView
	Err    string

	// PushRight marks the first widget in the right-aligned slot (once
	// buildHeader has stably partitioned Widgets into left-then-right
	// order): header.templ gives it "margin-left: auto", which — since
	// every widget after it is also right-aligned by construction — pushes
	// it and everything following to the header strip's right edge as one
	// contiguous flex block.
	PushRight bool
}

// headerFieldView is one Field rendered in a header widget's stacked stat
// rows (see headerFields' doc comment): IconURL, when set, replaces Label as
// the row's trailing glyph — matching homepage's own header/info widgets,
// which show a small resource icon (CPU/Memory/Storage) instead of a text
// label to stay compact. Label is "" whenever IconURL is set, or whenever
// the owning widget is a weather type (openmeteo/openweathermap), which
// drops its field labels entirely since the widget's own icon already
// tracks current conditions (see weatherIconURL).
type headerFieldView struct {
	Value     string
	Label     string
	IconURL   string
	Percent   *int
	Highlight string
}

// headerData is the /header fragment's template data.
type headerData struct {
	Widgets []headerWidgetView
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /fragment", s.handleFragment)
	mux.HandleFunc("GET /header", s.handleHeader)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /assets/{file}", handleAsset)
	mux.HandleFunc("GET /manifest.json", s.handleManifest)
	mux.HandleFunc("GET /robots.txt", s.handleRobots)
	mux.HandleFunc("GET /sw.js", handleServiceWorker)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	return securityHeaders(s.basicAuthMiddleware(mux))
}

// contentSecurityPolicy locks the page down to same-origin scripts/styles/
// connections (htmx and every inline <script>/<style> in index.templ are
// first-party; the only cross-origin loads are icons/backgrounds, which
// resolve to operator- or CRD-supplied URLs — see icon.go — so img-src alone
// stays open to https:/data:). script-src/style-src use a per-request nonce
// (see securityHeaders/generateNonce) rather than 'unsafe-inline' for
// *elements*: every inline <style>/<script> in index.templ — including the
// CustomCSS/CustomJS/background-image ones emitted via @templ.Raw
// (render_helpers.go's customStyle/customScript/backgroundStyle) — carries
// the same nonce, so a <script>/<style> tag without it (e.g. one smuggled in
// by a future escaping regression in those @templ.Raw paths) is refused by
// the browser regardless of what CustomCSS/CustomJS/Background.Image's own
// escaping does or doesn't catch.
//
// style-src-attr/script-src-attr are a deliberate, narrower exception: per
// the CSP spec, a nonce only satisfies the check for <script>/<style>
// *elements* — it has no effect on inline attribute values (style="...",
// onclick="..."), which the spec routes through a separate "attribute"
// check that only 'unsafe-inline' (or a hash matching the literal rendered
// value, impractical here since these are computed per request/element) can
// satisfy. This codebase renders several: the page's CSS custom properties
// on <html style=...> (index.templ), grid-template-columns/usage-bar-fill/
// iframe-height styles and the tab switcher's onclick= (cards.templ). Every
// value behind those attributes is server-computed from a fixed lookup
// table, a plain integer, or already-escaped CRD input (see cssStringEscape)
// — never attacker-controlled free text — so scoping 'unsafe-inline' to
// *only* the -attr directives (leaving the element directives nonce-only)
// keeps the actual XSS-relevant surface — a rogue <script>/<style> tag —
// covered, without silently breaking every inline style/onclick in the app.
//
// worker-src is scoped to 'self' explicitly (rather than relying on its
// script-src fallback, per the CSP spec's worker-src -> child-src ->
// script-src chain) so registering GET /sw.js (see handleServiceWorker,
// index.templ's registration script) stays allowed even if script-src's own
// value changes shape later — a same-origin service worker is a fixed,
// deliberate part of this app, not a case that should ride on script-src's
// coincidental coverage.
//
// frame-src mirrors img-src's "https: and nothing else" scope: without it,
// an iframe ServiceWidget's <iframe src="..."> (cards.templ, iframe.go)
// falls back to default-src 'self' and every browser refuses to load it —
// this can't be scoped to just the operator-configured widget URLs without
// threading per-request state through the page shell; iframe.go's own fixed
// sandbox attribute (allow-scripts allow-same-origin, no
// allow-top-navigation) is the actual containment boundary for whatever
// origin an operator points a widget at.
func contentSecurityPolicy(nonce string) string {
	return "default-src 'self'; " +
		"img-src 'self' https: data:; " +
		"frame-src https:; " +
		"style-src 'self' 'nonce-" + nonce + "'; " +
		"style-src-attr 'unsafe-inline'; " +
		"script-src 'self' 'nonce-" + nonce + "'; " +
		"script-src-attr 'unsafe-inline'; " +
		"worker-src 'self'; " +
		"connect-src 'self'; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'"
}

// nonceByteLength is how many random bytes generateNonce reads before
// base64-encoding them — 16 bytes (128 bits) is the commonly recommended
// minimum for a CSP nonce, per https://content-security-policy.com/nonce/.
const nonceByteLength = 16

// generateNonce returns a fresh, cryptographically random, base64
// (URL-safe, unpadded) CSP nonce for one request. URL-safe encoding is used
// so the value needs no further escaping wherever it's interpolated — the
// CSP header value, and every nonce="..." HTML attribute index.templ sets —
// since its alphabet (A-Za-z0-9-_) contains no characters special to either
// context.
func generateNonce() string {
	b := make([]byte, nonceByteLength)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read on a supported platform practically never fails;
		// a panic here surfaces a broken host environment immediately
		// rather than silently serving a predictable/empty nonce, which
		// would defeat the point of nonce-based CSP.
		panic("dashboard: reading random bytes for CSP nonce: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// securityHeaders sets response headers that don't vary per request beyond
// the CSP nonce (see generateNonce) — cheap defense-in-depth against XSS/
// clickjacking/MIME-sniffing for a page that intentionally renders
// CRD-supplied CSS/URLs (background image, custom CSS, icons — see
// cssStringEscape's doc comment on why those are escaped but not blocked
// outright). Threads the nonce into the request context via templ.WithNonce
// so index.templ's inline <style>/<script> tags (and @templ.JSONScript,
// which reads it automatically) can render it — see contentSecurityPolicy's
// doc comment.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce := generateNonce()
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy(nonce))
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r.WithContext(templ.WithNonce(r.Context(), nonce)))
	})
}

// pwaManifest is the GET /manifest.json response shape, letting the
// dashboard be installed as a Progressive Web App (homepage's documented
// PWA support: https://gethomepage.dev/configs/settings/#progressive-web-app-pwa).
type pwaManifest struct {
	Name            string    `json:"name"`
	ShortName       string    `json:"short_name"`
	ThemeColor      string    `json:"theme_color"`
	BackgroundColor string    `json:"background_color"`
	Display         string    `json:"display"`
	StartURL        string    `json:"start_url"`
	Icons           []pwaIcon `json:"icons"`
}

// pwaIcon is one entry of pwaManifest.Icons. "any" for Sizes tells the
// installer the (vector) icon scales to whatever size it needs, so a single
// SVG satisfies Chrome/Android's installability icon requirement without
// shipping multiple raster sizes.
type pwaIcon struct {
	Src   string `json:"src"`
	Sizes string `json:"sizes"`
	Type  string `json:"type"`
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	site, err := LoadSite(r.Context(), s.Reader, s.Namespace, s.DashboardName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	bg := PaletteRamp(site.Color).C900
	if site.Theme == themeLight {
		bg = PaletteRamp(site.Color).C50
	}
	manifest := pwaManifest{
		Name:            site.Title,
		ShortName:       site.Title,
		ThemeColor:      AccentHex(site.Color),
		BackgroundColor: bg,
		Display:         "standalone",
		StartURL:        site.StartURL,
		Icons:           []pwaIcon{{Src: pwaIconPath, Sizes: "any", Type: "image/svg+xml"}},
	}

	w.Header().Set("Content-Type", "application/manifest+json")
	if err := json.NewEncoder(w).Encode(manifest); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleRobots serves a permissive robots.txt by default, or a disallow-all
// one when spec.style sets Indexing: NoIndex — homepage's documented
// "ask search engines not to index" setting.
func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	site, err := LoadSite(r.Context(), s.Reader, s.Namespace, s.DashboardName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if site.DisableIndexing {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
		return
	}
	_, _ = w.Write([]byte("User-agent: *\nAllow: /\n"))
}

// handleAsset serves an embedded static asset by its bare filename. {file} is
// a single path segment (the mux disallows slashes), so it can't traverse out
// of the assets directory. Assets are content-stable, so they're cached hard.
// sw.js is excluded even though the go:embed glob picks it up alongside
// every other asset: it's only ever meant to be reachable at GET /sw.js (see
// handleServiceWorker's doc comment on why scope requires that exact path),
// with its own no-cache header — serving it here too, immutably cached,
// would be a confusing, pointless second route to the same script.
func handleAsset(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("file") == "sw.js" {
		http.NotFound(w, r)
		return
	}
	b, err := assetFS.ReadFile("assets/" + r.PathValue("file"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(r.PathValue("file"), ".woff2"):
		w.Header().Set("Content-Type", "font/woff2")
	case strings.HasSuffix(r.PathValue("file"), ".js"):
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case strings.HasSuffix(r.PathValue("file"), ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(b)
}

// swJS is the offline-shell service worker's script, read once at package
// init rather than per-request: "assets/sw.js" is a fixed path the
// `//go:embed assets/*.js` directive above already guarantees exists at
// compile time (unlike handleAsset's {file}, which comes from the request
// URL and can legitimately name a missing asset), so there's no real
// per-request failure mode to handle — see generateNonce's doc comment for
// the same reasoning applied to a different "should never happen" read.
var swJS = func() []byte {
	b, err := assetFS.ReadFile("assets/sw.js")
	if err != nil {
		panic("dashboard: reading embedded assets/sw.js: " + err.Error())
	}
	return b
}()

// handleServiceWorker serves the offline app-shell service worker
// (assets/sw.js) at the root path rather than under /assets/ — a service
// worker's default registration scope is the directory of its own URL, and
// this one needs to control every same-origin GET (the page shell, static
// assets) to do its job, not just /assets/*. "Cache-Control: no-cache"
// (rather than the long-lived immutable caching handleAsset gives every
// other asset) makes the browser revalidate the script itself on every
// registration check, so a deployed update to its caching logic takes
// effect promptly instead of potentially being stuck behind a stale cached
// copy of the worker script.
func handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(swJS)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	refresh := s.RefreshSeconds
	if refresh <= 0 {
		refresh = 10
	}

	site, err := LoadSite(r.Context(), s.Reader, s.Namespace, s.DashboardName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Already computed once by basicAuthMiddleware for this same request
	// (and cheap to compute again — see authCache) — see indexData.
	// AuthEnabled's doc comment for why index.templ needs it.
	_, authEnabled, err := loadBasicAuth(r.Context(), s.Reader, s.SecretReader, s.Namespace, s.DashboardName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := indexData{
		Site: site, AccentHex: AccentHex(site.Color), Ramp: PaletteRamp(site.Color), RefreshSeconds: refresh,
		Fragment: s.buildFragmentData(site),
		Version:  s.Version, Commit: s.Commit,
		SampleData:  s.SampleData,
		AuthEnabled: authEnabled,
	}
	if err := Index(data).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleFragment(w http.ResponseWriter, r *http.Request) {
	site, err := LoadSite(r.Context(), s.Reader, s.Namespace, s.DashboardName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := s.buildFragmentData(site)
	if err := writeCachedHTML(w, r, func(buf io.Writer) error { return Cards(data).Render(r.Context(), buf) }); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// writeCachedHTML renders an HTML fragment into a buffer and serves it with
// a content-hash ETag: unlike a Store generation counter, a hash of the
// actual rendered bytes stays correct even though /fragment and /header
// depend on more than the Store (Dashboard style/Bookmark/InfoWidget changes,
// read through the cached client, also change the output — see LoadSite).
// "Cache-Control: no-cache" tells the browser to keep revalidating on every
// request rather than caching outright, so it automatically sends
// If-None-Match on htmx's next poll; when that matches, the response is a
// bare 304 with no body — the common case once a dashboard's data has
// settled between polls. When the body does need to go out, it's
// gzip-compressed whenever the client advertises support.
func writeCachedHTML(w http.ResponseWriter, r *http.Request, render func(io.Writer) error) error {
	var buf bytes.Buffer
	if err := render(&buf); err != nil {
		return err
	}

	sum := etagFor(buf.Bytes())
	h := w.Header()
	h.Set("ETag", sum)
	h.Set("Cache-Control", "no-cache")
	// Vary: Accept-Encoding must be sent on every response whose body
	// encoding depends on the request's Accept-Encoding header — including
	// the 304 branch below and the uncompressed branch — so a shared cache
	// sitting in front of the dashboard doesn't serve a gzip response to a
	// client that didn't advertise gzip support, or vice versa.
	h.Set("Vary", "Accept-Encoding")
	if r.Header.Get("If-None-Match") == sum {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	h.Set("Content-Type", "text/html; charset=utf-8")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		if _, err := gz.Write(buf.Bytes()); err != nil {
			_ = gz.Close()
			return err
		}
		// Close (not just Write) flushes gzip's trailer to w; without
		// checking its error, a failed flush would silently truncate the
		// compressed body the client receives.
		return gz.Close()
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// etagFor returns a quoted strong-validator ETag (FNV-1a, not a security
// hash — this only needs to detect byte differences, not resist tampering)
// for writeCachedHTML's If-None-Match comparison.
func etagFor(b []byte) string {
	sum := fnv.New64a()
	sum.Write(b) //nolint:errcheck // hash.Hash.Write never returns an error
	return `"` + strconv.FormatUint(sum.Sum64(), 16) + `"`
}

// buildFragmentData builds the polled fragment's template data from the
// Store's current snapshot; shared by handleIndex (the page shell's
// server-rendered initial fragment) and handleFragment (every subsequent
// htmx poll), so the two never drift apart.
func (s *Server) buildFragmentData(site Site) fragmentData {
	return buildFragmentData(s.Store.Snapshot(), site)
}

// buildFragmentData is the free-function form of Server.buildFragmentData,
// also used by currentHashes (called from Poller.pollOnce, which has no
// Server to hang the method off of).
func buildFragmentData(cards []Card, site Site) fragmentData {
	return fragmentData{
		Tabs:               layoutTabs(mergeServiceCards(serviceCards(cards)), site),
		BookmarkGroups:     site.BookmarkGroups,
		SiteTarget:         site.Target,
		DisableCollapse:    site.DisableCollapse,
		BookmarksIconsOnly: site.BookmarksIconsOnly,
	}
}

// mergeCardGroupKey returns the key mergeServiceCards groups cards by: for a
// poller-produced widget-instance key (namespace/crName/entryIdx/widgetIdx,
// see poller.go's pollOnce, or its .../monitor variant for a monitor-only
// entry with no widgets), it's the key with the trailing widget-index (or
// "monitor") segment stripped — every widget belonging to the same
// ServiceCard entry shares that prefix. Any other key shape (notably a
// discovery-sourced card's "discovery/..." Key, see discovery.go) is returned
// unchanged, so it never accidentally merges with an unrelated card: it's
// only safe to strip the last segment when that segment is recognizably a
// widget index or "monitor", since a discovery Key's last segment is a
// resource name that could coincidentally look like anything.
func mergeCardGroupKey(key string) string {
	// Discovery cards ("discovery/<ns>/<name>" or
	// "discovery/httproute/<ns>/<name>", see discovery.go) are never merged:
	// their trailing segment is a Kubernetes resource name, which is a valid
	// DNS-1123 label and so can be all-digits (e.g. an Ingress named "123")
	// or literally "monitor". Stripping it would collapse two unrelated
	// discovered services into one card, corrupting what renders. The poller's
	// own widget-instance keys never carry this prefix.
	if strings.HasPrefix(key, "discovery/") {
		return key
	}
	i := strings.LastIndex(key, "/")
	if i < 0 {
		return key
	}
	last := key[i+1:]
	if last == "monitor" || isDecimalDigits(last) {
		return key[:i]
	}
	return key
}

// isDecimalDigits reports whether s is non-empty and consists only of ASCII
// digits (a widget index, as formatted by fmt.Sprintf("%d", ...)).
func isDecimalDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// mergeWidgetIndex parses the trailing widget-index segment of a poller key
// ("<namespace>/<crName>/<entryIdx>/<widgetIdx>") as an int, so mergeCards can
// order a merged entry's Fields by real widget index rather than by the
// lexicographic Key order Store.Snapshot leaves them in (which sorts "10"
// before "2"). A key without a numeric trailing segment yields 0.
func mergeWidgetIndex(key string) int {
	i := strings.LastIndex(key, "/")
	if i < 0 {
		return 0
	}
	n, err := strconv.Atoi(key[i+1:])
	if err != nil {
		return 0
	}
	return n
}

// mergeServiceCards merges the poller's one-Card-per-widget storage (see
// poller.go's pollOnce) back into one rendered card per ServiceCard entry —
// homepage parity: a service with several widgets (e.g. three
// prometheusmetric widgets) is one card showing every widget's Fields, not
// one near-identical card per widget. Cards are grouped by
// mergeCardGroupKey, preserving input order (cards arrives already sorted by
// Store.Snapshot) for both the groups themselves and, within a merged card,
// no reordering of the identity fields (only Fields/Err are concatenated). A
// group with exactly one card — the common case for a single-widget entry,
// a monitor-only entry, or any non-poller (e.g. discovery) card — passes
// through unchanged.
func mergeServiceCards(cards []Card) []Card {
	type group struct {
		key   string
		cards []Card
	}
	groups := make(map[string]*group, len(cards))
	var order []*group
	for _, c := range cards {
		k := mergeCardGroupKey(c.Key)
		g, ok := groups[k]
		if !ok {
			g = &group{key: k}
			groups[k] = g
			order = append(order, g)
		}
		g.cards = append(g.cards, c)
	}

	out := make([]Card, 0, len(order))
	for _, g := range order {
		if len(g.cards) == 1 {
			out = append(out, g.cards[0])
			continue
		}
		out = append(out, mergeCards(g.cards))
	}
	return out
}

// mergeCards combines several widget-instance Cards belonging to the same
// ServiceCard entry into the one Card mergeServiceCards renders: identity
// fields (Group, ServiceName, Order, IconURL, Description, Href, Target,
// Status, StatusStyle, Latency, ShowStats, HideErrors, WidgetType, Header) and
// Key come from the lowest-widget-index card, so the DOM id stays stable
// across polls; Fields are concatenated in widget-index order; non-empty Errs
// are joined with "; "; UpdatedAt is the latest of the group.
//
// The cards are (re-)sorted by numeric trailing widget index first: they
// arrive in Store.Snapshot order, whose final tiebreaker is a plain string
// compare of the Key, which puts ".../10" before ".../2" — so an entry with
// ten or more widgets would otherwise concatenate its Fields (and pick its
// identity card) out of order.
func mergeCards(cards []Card) Card {
	slices.SortStableFunc(cards, func(a, b Card) int {
		return mergeWidgetIndex(a.Key) - mergeWidgetIndex(b.Key)
	})
	merged := cards[0]

	var fields []Field
	var errs []string
	latest := merged.UpdatedAt
	for _, c := range cards {
		fields = append(fields, c.Fields...)
		if c.Err != "" {
			errs = append(errs, c.Err)
		}
		if c.UpdatedAt.After(latest) {
			latest = c.UpdatedAt
		}
	}
	merged.Fields = fields
	merged.Err = strings.Join(errs, "; ")
	merged.UpdatedAt = latest
	return merged
}

func (s *Server) handleHeader(w http.ResponseWriter, r *http.Request) {
	site, err := LoadSite(r.Context(), s.Reader, s.Namespace, s.DashboardName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := headerData{Widgets: buildHeader(site.HeaderWidgets, s.Store.Snapshot())}
	if err := writeCachedHTML(w, r, func(buf io.Writer) error { return Header(data).Render(r.Context(), buf) }); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// sseHeartbeatInterval is how often handleEvents writes a comment-only SSE
// line on an otherwise idle connection — without it, an intermediary proxy
// that times out idle connections (or a client library that treats silence
// as a dead stream) could drop the connection long before the next poll
// cycle actually changes anything.
const sseHeartbeatInterval = 25 * time.Second

// sseWriteTimeout bounds each individual write to an SSE connection. The
// connection's overall write deadline is cleared on accept (see below) so
// the stream can sit open indefinitely, but every write still needs its own
// short deadline — otherwise a client that stops reading (a full TCP
// receive window and no more) blocks the write goroutine forever, pinning
// one of the maxSSESubscribers slots for good. A stalled write is treated
// like any other write error: the handler returns and unsubscribes.
const sseWriteTimeout = 10 * time.Second

// handleEvents serves GET /events as a Server-Sent Events stream: one
// "fragmentChanged" or "headerChanged" event whenever a poll cycle actually
// changes what /fragment or /header would render. The content hashes
// (etagFor, the same one writeCachedHTML's ETag uses) are computed once per
// poll cycle by Poller.pollOnce and carried through Broadcast — this handler
// only compares them against its own last-sent baseline, so a cosmetic no-op
// cycle — the common case once a dashboard's data has settled — sends
// nothing, and N open connections cost one render per cycle, not N. The
// event carries no payload; the page shell's own script re-fetches the
// fragment via htmx on receiving it (see index.templ) rather than the
// server pushing HTML down the SSE channel itself, keeping the same
// request/response path — and its gzip/ETag handling — for the actual
// content in both the push and interval-poll cases.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s.Broadcast == nil {
		http.Error(w, "server-sent events not available", http.StatusNotImplemented)
		return
	}
	ch, subscribed := s.Broadcast.Subscribe()
	if !subscribed {
		// maxSSESubscribers already reached: htmx's interval poll (index.templ)
		// still covers this client, so reject rather than growing unbounded.
		http.Error(w, "too many active event streams", http.StatusServiceUnavailable)
		return
	}
	defer s.Broadcast.Unsubscribe(ch)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// The http.Server this handler runs under sets a fixed WriteTimeout for
	// every other route; an SSE connection is expected to sit open far
	// longer than that, so its write deadline is cleared for this response
	// specifically rather than raising the timeout server-wide.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Tells a buffering reverse proxy (nginx) not to hold this response back
	// waiting to fill its buffer — irrelevant for direct/ingress-controller
	// access but harmless to always send.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	lastFragment, lastHeader := s.currentHashes(ctx)

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if err := sseWrite(rc, w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case h := <-ch:
			var wrote bool
			if h.fragment != "" && h.fragment != lastFragment {
				lastFragment = h.fragment
				if err := sseWrite(rc, w, "event: fragmentChanged\ndata: refresh\n\n"); err != nil {
					return
				}
				wrote = true
			}
			if h.header != "" && h.header != lastHeader {
				lastHeader = h.header
				if err := sseWrite(rc, w, "event: headerChanged\ndata: refresh\n\n"); err != nil {
					return
				}
				wrote = true
			}
			if wrote {
				flusher.Flush()
			}
		}
	}
}

// sseWrite sets a short per-write deadline before writing s to an SSE
// connection whose overall write deadline was cleared on accept, then writes
// it. A deadline-exceeded or otherwise failed write is returned like any
// other write error, so the caller's normal "return on error" handling also
// covers a stalled/non-reading client — without this, the earlier
// SetWriteDeadline(time.Time{}) leaves every subsequent write unbounded.
// http.ErrNotSupported (e.g. a ResponseWriter under test that doesn't
// implement deadlines) is not itself a write failure and is ignored.
func sseWrite(rc *http.ResponseController, w io.Writer, s string) error {
	if err := rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	_, err := io.WriteString(w, s)
	return err
}

// currentHashes returns the content-hash (etagFor) of what /fragment and
// /header would currently render, or "" for either on a LoadSite error (a
// transient read failure shouldn't itself look like a content change, and ""
// never matches a real hash so it's simply skipped by handleEvents). A free
// function, not a Server method, so Poller.pollOnce can also call it —
// computing the pair once per poll cycle there and carrying it through
// Broadcast, rather than every handleEvents subscriber recomputing it on
// every broadcast (see Broadcaster's doc comment).
func currentHashes(ctx context.Context, reader client.Reader, namespace, dashboardName string, store *Store) (fragment, header string) {
	site, err := LoadSite(ctx, reader, namespace, dashboardName)
	if err != nil {
		return "", ""
	}
	cards := store.Snapshot()

	var fragBuf bytes.Buffer
	if err := Cards(buildFragmentData(cards, site)).Render(ctx, &fragBuf); err == nil {
		fragment = etagFor(fragBuf.Bytes())
	}

	var headerBuf bytes.Buffer
	hdrData := headerData{Widgets: buildHeader(site.HeaderWidgets, cards)}
	if err := Header(hdrData).Render(ctx, &headerBuf); err == nil {
		header = etagFor(headerBuf.Bytes())
	}
	return fragment, header
}

// currentHashes is the Server-side convenience wrapper handleEvents uses to
// compute a new SSE connection's baseline hashes on connect.
func (s *Server) currentHashes(ctx context.Context) (fragment, header string) {
	return currentHashes(ctx, s.Reader, s.Namespace, s.DashboardName, s.Store)
}

// serviceCards returns only the non-header cards (header InfoWidget cards are
// rendered in the header strip, not the service grid).
func serviceCards(cards []Card) []Card {
	out := make([]Card, 0, len(cards))
	for _, c := range cards {
		if !c.Header {
			out = append(out, c)
		}
	}
	return out
}

// buildHeader joins each header-widget definition with its live polled value
// (matched by Key, the composite header/<InfoWidget name>/<entry index>
// string poller.go's pollInfoWidget stores each Card under — matching by
// object Name alone would collide when a multi-widget InfoWidget's entries
// share one Name), preserving the definitions' order.
func buildHeader(defs []HeaderWidget, cards []Card) []headerWidgetView {
	live := map[string]Card{}
	for _, c := range cards {
		if c.Header {
			live[c.Key] = c
		}
	}

	views := make([]headerWidgetView, 0, len(defs))
	for _, d := range defs {
		v := headerWidgetView{Type: d.Type}
		switch d.Type {
		case headerTypeGreeting:
			v.Greeting = d.Options["text"]
		case headerTypeDatetime:
			v.Format = d.Options["format"]
		case headerTypeLogo:
			v.IconURL = d.IconURL
			v.Href = d.Options["href"]
		default:
			v.IconURL = d.IconURL
			var liveFields []Field
			if c, ok := live[d.Key]; ok {
				liveFields = c.Fields
				v.Err = c.Err
			}
			if v.IconURL == "" {
				v.IconURL = defaultHeaderWidgetIcon(d.Type, liveFields)
			}
			v.Fields = headerFields(d.Type, liveFields)
		}
		views = append(views, v)
	}
	return partitionHeaderAlign(views, defs)
}

// headerIconColor is the fixed color (Tailwind slate-400) baked into every
// Iconify-sourced header default/field icon via IconURL's "-#hexcolor"
// suffix. Header icons render as a plain <img>, which can't pick up a CSS
// currentColor the way homepage's own inline SVG icons do to track the
// active theme, so a neutral mid-gray that reads reasonably against both the
// light and dark theme's header background is the closest static
// approximation.
const headerIconColor = "94a3b8"

// headerWidgetDefaultIcons maps a polled header widget type to the Iconify
// slug (see IconURL) homepage shows for it out of the box, used by
// defaultHeaderWidgetIcon whenever the InfoWidget doesn't set its own Icon —
// verified against homepage's own source (src/components/widgets/…):
// kubemetrics gets the Kubernetes logo (kubernetes/node.jsx's SiKubernetes,
// a Simple Icons brand glyph, recolored to headerIconColor rather than the
// brand's own blue to match homepage's monochrome header icons), and
// longhorn gets the generic disk glyph longhorn/node.jsx draws (FiHardDrive)
// rather than a project logo. glances isn't listed here on purpose:
// glances.jsx gives it no group icon at all, relying solely on each field's
// own icon (see fieldIconSlugs) — CPU/Memory/Storage. openmeteo/
// openweathermap aren't listed either: their icon tracks the current
// weather condition each poll instead of a fixed glyph (see weatherIconURL).
var headerWidgetDefaultIcons = map[string]string{
	"kubemetrics":      "si-kubernetes-#" + headerIconColor,
	widgetTypeLonghorn: "lucide-hard-drive-#" + headerIconColor,
}

// defaultHeaderWidgetIcon returns the built-in icon a polled header widget
// (openmeteo/openweathermap, kubemetrics, glances, longhorn) renders when its
// InfoWidget's Icon is unset — matching homepage's info widgets, which always
// show an icon rather than requiring one to be configured. "" (no icon) for
// any other type, and for a weather widget whose fields don't carry a
// Conditions value yet (e.g. Err is set instead).
func defaultHeaderWidgetIcon(widgetType string, fields []Field) string {
	if widgetType == widgetTypeOpenMeteo || widgetType == widgetTypeOpenWeatherMap {
		return weatherIconURL(fieldValue(fields, labelConditions))
	}
	if slug, ok := headerWidgetDefaultIcons[widgetType]; ok {
		return staticIcon(slug)
	}
	return ""
}

// staticIcon resolves a fixed dashboard-icons/Iconify slug through IconURL,
// which takes a *string (nil meaning "no icon") — slug here is always a
// non-empty literal, so this just adapts the calling convention.
func staticIcon(slug string) string {
	return IconURL(&slug)
}

// weatherIconURL maps an openmeteo/openweathermap Conditions field value to
// an Iconify Weather Icons ("wi") glyph — the exact icon set homepage's own
// weather widget uses (src/utils/weather/{openmeteo,owm}-condition-map.js,
// both keyed off react-icons/wi) — so the header widget's icon visibly
// tracks current conditions rather than a fixed logo. Homepage picks a
// day/night variant per icon and keys off the raw numeric weather code; this
// operator's Field only carries the coarser condition text openmeteo.go's
// weatherCondition()/openweathermap.go's OWM "main" category already reduce
// codes to, so this matches on that text (always the "day" glyph — the day/
// night split isn't worth plumbing a raw code and local sunrise/sunset
// through Field for). Unrecognized/empty conditions (including "Unknown", a
// poll error's fallback) get homepage's own fallback: a plain sun glyph.
func weatherIconURL(condition string) string {
	c := strings.ToLower(condition)
	slug := "day-sunny"
	switch {
	case strings.Contains(c, "clear"):
		slug = "day-sunny"
	case strings.Contains(c, "thunderstorm"):
		slug = "day-thunderstorm"
	case strings.Contains(c, "shower"):
		slug = "day-showers"
	case strings.Contains(c, "drizzle"):
		slug = "day-sprinkle"
	case strings.Contains(c, "rain"):
		slug = "day-rain"
	case strings.Contains(c, "snow"):
		slug = "day-snow"
	case strings.Contains(c, "smoke"):
		slug = "smoke"
	case strings.Contains(c, "haze"):
		slug = "day-haze"
	case strings.Contains(c, "dust"), strings.Contains(c, "ash"):
		slug = "dust"
	case strings.Contains(c, "sand"):
		slug = "sandstorm"
	case strings.Contains(c, "fog"), strings.Contains(c, "mist"):
		slug = "day-fog"
	case strings.Contains(c, "tornado"):
		slug = "tornado"
	case strings.Contains(c, "squall"):
		slug = "strong-wind"
	case strings.Contains(c, "partly"):
		slug = "day-cloudy"
	case strings.Contains(c, "cloud"):
		slug = "day-cloudy"
	}
	return staticIcon("wi-" + slug + "-#" + headerIconColor)
}

// fieldIconSlugs maps a resource-style Field label to the Iconify icon slug
// homepage's own header widgets show in place of the label text (see
// src/components/widgets/kubernetes/node.jsx: FiCpu, FaMemory; longhorn/node.jsx:
// FiHardDrive). Lucide's cpu/hard-drive glyphs are pixel-for-pixel the same
// family homepage draws CPU/Storage from (Feather Icons — Lucide is a
// maintained fork sharing most glyphs 1:1); Memory uses Font Awesome 6's
// solid "memory" glyph, matching homepage's FaMemory more closely than any
// Lucide equivalent.
var fieldIconSlugs = map[string]string{
	labelCPU:     "lucide-cpu",
	labelMemory:  "fa6-solid-memory",
	labelStorage: "lucide-hard-drive",
}

// headerFields converts a header widget's live Fields into render-ready
// headerFieldViews for header.templ's default case. A recognized
// resource-style label (CPU/Memory/Storage — kubemetrics/glances/longhorn's
// only non-error fields) swaps its text label for a small icon; an
// openmeteo/openweathermap field (temperature + Conditions) drops its label
// entirely instead, since that meaning is already carried by the widget's
// own dynamic weather icon (see weatherIconURL). This matches homepage's
// compact, largely label-less header widgets rather than the ServiceCard
// grid's always-labeled stat layout.
func headerFields(widgetType string, fields []Field) []headerFieldView {
	isWeather := widgetType == widgetTypeOpenMeteo || widgetType == widgetTypeOpenWeatherMap
	views := make([]headerFieldView, 0, len(fields))
	for _, f := range fields {
		v := headerFieldView{Value: f.Value, Percent: f.Percent, Highlight: f.Highlight}
		if slug, ok := fieldIconSlugs[f.Label]; ok {
			v.IconURL = staticIcon(slug + "-#" + headerIconColor)
		} else if !isWeather {
			v.Label = f.Label
		}
		views = append(views, v)
	}
	return views
}

// partitionHeaderAlign stably reorders views (built 1:1 with, and in the
// same order as, defs) so every left-aligned widget precedes every
// right-aligned one, regardless of interleaving from Order/name sorting —
// header.templ's CSS-only right-alignment (see headerWidgetView.PushRight's
// doc comment) only works when the right-aligned widgets form one
// contiguous trailing run.
func partitionHeaderAlign(views []headerWidgetView, defs []HeaderWidget) []headerWidgetView {
	left := make([]headerWidgetView, 0, len(views))
	right := make([]headerWidgetView, 0, len(views))
	for i, v := range views {
		if defs[i].Align == alignRight {
			right = append(right, v)
		} else {
			left = append(left, v)
		}
	}
	if len(right) > 0 {
		right[0].PushRight = true
	}
	return append(left, right...)
}

// groupCards buckets an already-ordered card slice into flat display groups
// keyed by the card's full Group path (e.g. "Media/Movies" is one flat
// bucket, not yet split into a tree — see nestGroups), preserving the
// incoming order of both groups and cards within a group. Header defaults to
// true (shown) — only an explicit LayoutGroupSpec.Header false (applied in
// layoutTabs) turns it off; InitiallyCollapsed/UseEqualHeights start at the
// Site-wide defaults, also overridable there.
func groupCards(cards []Card, site Site) []cardGroup {
	var groups []cardGroup
	index := map[string]int{}
	for _, c := range cards {
		i, ok := index[c.Group]
		if !ok {
			i = len(groups)
			index[c.Group] = i
			groups = append(groups, cardGroup{
				Path:               c.Group,
				Name:               leafSegment(c.Group),
				Header:             true,
				InitiallyCollapsed: site.GroupsInitiallyCollapsed,
				UseEqualHeights:    site.UseEqualHeights,
			})
		}
		groups[i].Cards = append(groups[i].Cards, c)
	}
	return groups
}

// leafSegment returns a "/"-separated group path's final segment (e.g.
// "Movies" for "Media/Movies"), used as a nested group's rendered heading —
// or the whole string unchanged when it has no "/" (a top-level group).
func leafSegment(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// nestGroups turns groupCards' flat, path-keyed groups into a tree: each
// group's Path is split on "/" and attached under its parent path's
// cardGroup.Subgroups, materializing any ancestor path that has no direct
// cards of its own (header shown, zero direct Cards, Site-wide defaults) so
// e.g. a card at "Media/Movies" with no card directly in "Media" still gets
// a "Media" parent to render under. Sibling order — both among the returned
// root groups and within each parent's Subgroups — follows first-seen order
// in flat (which is itself Store.Snapshot's Order/name order, see
// groupCards), not alphabetical. Homepage parity for nested service groups,
// see docs/design/nested-groups.md; depth is bounded to 3 by the CRD's
// Group/Name pattern, so recursion here is not depth-limited itself.
func nestGroups(flat []cardGroup, site Site) []cardGroup {
	type node struct {
		group    cardGroup
		children []*node
	}

	nodes := make(map[string]*node, len(flat))
	var roots []*node

	var ensure func(path string) *node
	ensure = func(path string) *node {
		if n, ok := nodes[path]; ok {
			return n
		}
		n := &node{group: cardGroup{
			Path:               path,
			Name:               leafSegment(path),
			Header:             true,
			InitiallyCollapsed: site.GroupsInitiallyCollapsed,
			UseEqualHeights:    site.UseEqualHeights,
		}}
		nodes[path] = n
		if i := strings.LastIndex(path, "/"); i >= 0 {
			parent := ensure(path[:i])
			parent.children = append(parent.children, n)
		} else {
			roots = append(roots, n)
		}
		return n
	}

	for _, g := range flat {
		n := ensure(g.Path)
		n.group.Cards = g.Cards
		n.group.Columns = g.Columns
		n.group.Style = g.Style
		n.group.IconURL = g.IconURL
		n.group.Header = g.Header
		n.group.InitiallyCollapsed = g.InitiallyCollapsed
		n.group.UseEqualHeights = g.UseEqualHeights
	}

	var toValue func(n *node) cardGroup
	toValue = func(n *node) cardGroup {
		out := n.group
		for _, c := range n.children {
			out.Subgroups = append(out.Subgroups, toValue(c))
		}
		return out
	}

	out := make([]cardGroup, 0, len(roots))
	for _, r := range roots {
		out = append(out, toValue(r))
	}
	return out
}

// applyLayoutStyle applies one LayoutGroupSpec's resolved style
// (Columns/Style/IconURL/Header/InitiallyCollapsed/UseEqualHeights) to g,
// falling back to whatever g already carried for anything the spec left
// unset (nil pointer fields on LayoutGroup, see its own doc comment).
func applyLayoutStyle(g cardGroup, lg LayoutGroup) cardGroup {
	g.Columns = lg.Columns
	g.Style = lg.Style
	g.IconURL = lg.IconURL
	if lg.Header != nil {
		g.Header = *lg.Header
	}
	if lg.InitiallyCollapsed != nil {
		g.InitiallyCollapsed = *lg.InitiallyCollapsed
	}
	if lg.UseEqualHeights != nil {
		g.UseEqualHeights = *lg.UseEqualHeights
	}
	return g
}

// applyLayoutStyles walks g and every descendant in Subgroups, applying
// styleByPath's entry for each node's full Path when one exists — how a
// tab's LayoutGroupSpec entries style a placed group and any of its nested
// subgroups (see layoutTabs). A node with no matching entry is returned
// unchanged.
func applyLayoutStyles(g cardGroup, styleByPath map[string]LayoutGroup) cardGroup {
	if lg, ok := styleByPath[g.Path]; ok {
		g = applyLayoutStyle(g, lg)
	}
	for i := range g.Subgroups {
		g.Subgroups[i] = applyLayoutStyles(g.Subgroups[i], styleByPath)
	}
	return g
}

// orderSubgroups recursively reorders g's Subgroups (and each descendant's
// own Subgroups) to follow orderByPath — built by layoutTabs from a tab's
// LayoutGroupSpec entries (t.Groups), keyed by full path — so a path-named
// layout entry (e.g. "Media/Video Services" listed before "Media/E-Book and
// File Management") controls sibling order, not just style
// (applyLayoutStyles). A subgroup whose Path has no entry in orderByPath
// keeps its existing relative order and sorts after every listed subgroup —
// nothing silently reorders or disappears just because a tab's layout
// doesn't mention it. The sort is stable, so unlisted siblings (and equally-
// unlisted or equally-ranked ones) don't get shuffled relative to each
// other.
func orderSubgroups(g cardGroup, orderByPath map[string]int) cardGroup {
	if len(g.Subgroups) > 1 {
		rank := func(path string) int {
			if i, ok := orderByPath[path]; ok {
				return i
			}
			return len(orderByPath)
		}
		slices.SortStableFunc(g.Subgroups, func(a, b cardGroup) int {
			return rank(a.Path) - rank(b.Path)
		})
	}
	for i := range g.Subgroups {
		g.Subgroups[i] = orderSubgroups(g.Subgroups[i], orderByPath)
	}
	return g
}

// layoutTabs arranges groupCards'/nestGroups' output into tabs per the
// Dashboard's spec.style's Layout. An empty layout returns the (nested) groups
// unchanged in a single unnamed tab, so the dashboard renders exactly as it
// did before tabs existed. Placement is root-only: a LayoutGroupSpec whose
// Name has no "/" places that root group — and its whole Subgroups tree —
// into a tab; a path-named entry (e.g. "Media/Movies") never places on its
// own, it only styles the node at that path once its root has already been
// placed (the CRD's per-tab CEL rule requires the root be listed too, see
// LayoutTabSpec). A root Group placed by more than one LayoutTab is shown
// only under the first; any root Group not referenced by any tab is
// appended, with its whole subtree, to a trailing "Other" tab so nothing
// silently disappears from the dashboard.
func layoutTabs(cards []Card, site Site) []layoutTab {
	tree := nestGroups(groupCards(cards, site), site)
	layout := site.Layout
	if len(layout) == 0 {
		return []layoutTab{{Groups: tree}}
	}

	byRootPath := make(map[string]cardGroup, len(tree))
	for _, g := range tree {
		byRootPath[g.Path] = g
	}

	used := make(map[string]bool, len(tree))
	tabs := make([]layoutTab, 0, len(layout)+1)
	for _, t := range layout {
		styleByPath := make(map[string]LayoutGroup, len(t.Groups))
		for _, lg := range t.Groups {
			styleByPath[lg.Name] = lg
		}

		orderByPath := make(map[string]int, len(t.Groups))
		for i, lg := range t.Groups {
			orderByPath[lg.Name] = i
		}

		tab := layoutTab{Name: t.Name}
		for _, lg := range t.Groups {
			if strings.Contains(lg.Name, "/") {
				// A path-named entry styles a subgroup in place; it never
				// places anything on its own (see doc comment above).
				continue
			}
			g, ok := byRootPath[lg.Name]
			if !ok || used[lg.Name] {
				continue
			}
			used[lg.Name] = true
			g = applyLayoutStyles(g, styleByPath)
			g = orderSubgroups(g, orderByPath)
			tab.Groups = append(tab.Groups, g)
		}
		tabs = append(tabs, tab)
	}

	var other []cardGroup
	for _, g := range tree {
		if !used[g.Path] {
			other = append(other, g)
		}
	}
	if len(other) > 0 {
		tabs = append(tabs, layoutTab{Name: otherTabName, Groups: other})
	}
	return tabs
}

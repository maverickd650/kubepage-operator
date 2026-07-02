package dashboard

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"

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
//go:embed assets/*.woff2 assets/*.js
var assetFS embed.FS

// cardGroup is a display-ready group of cards sharing a ServiceEntry Group,
// in the order Store.Snapshot already produced (Order, then name). Columns/
// Style/IconURL/Header/InitiallyCollapsed/UseEqualHeights come from the
// Configuration's Layout, when one places this group in a tab, already
// resolved against the Site-wide defaults by layoutTabs.
type cardGroup struct {
	Name               string
	Cards              []Card
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
	InstanceName   string
	RefreshSeconds int

	// Version/Commit are stamped at build time (see cmd/main.go), shown in
	// the page shell's footer unless Site.HideVersion is set.
	Version string
	Commit  string
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
	Fields []Field
	Err    string

	// PushRight marks the first widget in the right-aligned slot (once
	// buildHeader has stably partitioned Widgets into left-then-right
	// order): header.templ gives it "margin-left: auto", which — since
	// every widget after it is also right-aligned by construction — pushes
	// it and everything following to the header strip's right edge as one
	// contiguous flex block.
	PushRight bool
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
	mux.HandleFunc("GET /assets/{file}", handleAsset)
	mux.HandleFunc("GET /manifest.json", s.handleManifest)
	mux.HandleFunc("GET /robots.txt", s.handleRobots)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	return securityHeaders(mux)
}

// contentSecurityPolicy locks the page down to same-origin scripts/styles/
// connections (htmx and every inline <script>/<style> in index.templ are
// first-party; the only cross-origin loads are icons/backgrounds, which
// resolve to operator- or CRD-supplied URLs — see icon.go — so img-src alone
// stays open to https:/data:). 'unsafe-inline' on script-src/style-src is
// needed because the page shell has no nonce/hash plumbing yet; every value
// interpolated into those inline blocks is either a fixed lookup table
// (AccentHex/PaletteRamp), a plain integer, or escaped via cssStringEscape
// (CustomCSS/Background.Image) rather than free-form script text. frame-src
// mirrors img-src's "https: and nothing else" scope: without it, an iframe
// ServiceWidget's <iframe src="..."> (cards.templ, iframe.go) falls back to
// default-src 'self' and every browser refuses to load it — the CSP is a
// compile-time constant, so this can't be scoped to just the operator-
// configured widget URLs without threading per-request state through the
// page shell; iframe.go's own fixed sandbox attribute (allow-scripts
// allow-same-origin, no allow-top-navigation) is the actual containment
// boundary for whatever origin an operator points a widget at.
const contentSecurityPolicy = "default-src 'self'; " +
	"img-src 'self' https: data:; " +
	"frame-src https:; " +
	"style-src 'self' 'unsafe-inline'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"connect-src 'self'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

// securityHeaders sets response headers that don't vary per request, cheap
// defense-in-depth against XSS/clickjacking/MIME-sniffing for a page that
// intentionally renders CRD-supplied CSS/URLs (background image, custom CSS,
// icons — see cssStringEscape's doc comment on why those are escaped but not
// blocked outright).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// pwaManifest is the GET /manifest.json response shape, letting the
// dashboard be installed as a Progressive Web App (homepage's documented
// PWA support: https://gethomepage.dev/configs/settings/#progressive-web-app-pwa).
type pwaManifest struct {
	Name            string `json:"name"`
	ShortName       string `json:"short_name"`
	ThemeColor      string `json:"theme_color"`
	BackgroundColor string `json:"background_color"`
	Display         string `json:"display"`
	StartURL        string `json:"start_url"`
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	site, err := LoadSite(r.Context(), s.Reader, s.Namespace, s.InstanceName)
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
	}

	w.Header().Set("Content-Type", "application/manifest+json")
	if err := json.NewEncoder(w).Encode(manifest); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleRobots serves a permissive robots.txt by default, or a disallow-all
// one when the Configuration sets DisableIndexing — homepage's documented
// "ask search engines not to index" setting.
func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	site, err := LoadSite(r.Context(), s.Reader, s.Namespace, s.InstanceName)
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
func handleAsset(w http.ResponseWriter, r *http.Request) {
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
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(b)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	refresh := s.RefreshSeconds
	if refresh <= 0 {
		refresh = 10
	}

	site, err := LoadSite(r.Context(), s.Reader, s.Namespace, s.InstanceName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := indexData{
		Site: site, AccentHex: AccentHex(site.Color), Ramp: PaletteRamp(site.Color), RefreshSeconds: refresh,
		Fragment: s.buildFragmentData(site),
		Version:  s.Version, Commit: s.Commit,
	}
	if err := Index(data).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleFragment(w http.ResponseWriter, r *http.Request) {
	site, err := LoadSite(r.Context(), s.Reader, s.Namespace, s.InstanceName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := Cards(s.buildFragmentData(site)).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// buildFragmentData builds the polled fragment's template data from the
// Store's current snapshot; shared by handleIndex (the page shell's
// server-rendered initial fragment) and handleFragment (every subsequent
// htmx poll), so the two never drift apart.
func (s *Server) buildFragmentData(site Site) fragmentData {
	return fragmentData{
		Tabs:               layoutTabs(serviceCards(s.Store.Snapshot()), site),
		BookmarkGroups:     site.BookmarkGroups,
		SiteTarget:         site.Target,
		DisableCollapse:    site.DisableCollapse,
		BookmarksIconsOnly: site.BookmarksIconsOnly,
	}
}

func (s *Server) handleHeader(w http.ResponseWriter, r *http.Request) {
	site, err := LoadSite(r.Context(), s.Reader, s.Namespace, s.InstanceName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := headerData{Widgets: buildHeader(site.HeaderWidgets, s.Store.Snapshot())}
	if err := Header(data).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
// (matched by name to a header Card), preserving the definitions' order.
func buildHeader(defs []HeaderWidget, cards []Card) []headerWidgetView {
	live := map[string]Card{}
	for _, c := range cards {
		if c.Header {
			live[c.ServiceName] = c
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
			if c, ok := live[d.Name]; ok {
				v.Fields = c.Fields
				v.Err = c.Err
			}
		}
		views = append(views, v)
	}
	return partitionHeaderAlign(views, defs)
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

// groupCards buckets an already-ordered card slice into display groups,
// preserving the incoming order of both groups and cards within a group.
// Header defaults to true (shown) — only an explicit LayoutGroupSpec.Header
// false (applied in layoutTabs) turns it off; InitiallyCollapsed/
// UseEqualHeights start at the Site-wide defaults, also overridable there.
func groupCards(cards []Card, site Site) []cardGroup {
	var groups []cardGroup
	index := map[string]int{}
	for _, c := range cards {
		i, ok := index[c.Group]
		if !ok {
			i = len(groups)
			index[c.Group] = i
			groups = append(groups, cardGroup{
				Name:               c.Group,
				Header:             true,
				InitiallyCollapsed: site.GroupsInitiallyCollapsed,
				UseEqualHeights:    site.UseEqualHeights,
			})
		}
		groups[i].Cards = append(groups[i].Cards, c)
	}
	return groups
}

// layoutTabs arranges groupCards' output into tabs per the Configuration's
// Layout. An empty layout returns the groups unchanged in a single unnamed
// tab, so the dashboard renders exactly as it did before tabs existed. A
// Group placed by more than one LayoutTab is shown only under the first;
// any Group not referenced by any tab is appended to a trailing "Other"
// tab so nothing silently disappears from the dashboard.
func layoutTabs(cards []Card, site Site) []layoutTab {
	groups := groupCards(cards, site)
	layout := site.Layout
	if len(layout) == 0 {
		return []layoutTab{{Groups: groups}}
	}

	byName := make(map[string]cardGroup, len(groups))
	for _, g := range groups {
		byName[g.Name] = g
	}

	used := make(map[string]bool, len(groups))
	tabs := make([]layoutTab, 0, len(layout)+1)
	for _, t := range layout {
		tab := layoutTab{Name: t.Name}
		for _, lg := range t.Groups {
			g, ok := byName[lg.Name]
			if !ok || used[lg.Name] {
				continue
			}
			used[lg.Name] = true
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
			tab.Groups = append(tab.Groups, g)
		}
		tabs = append(tabs, tab)
	}

	var other []cardGroup
	for _, g := range groups {
		if !used[g.Name] {
			other = append(other, g)
		}
	}
	if len(other) > 0 {
		tabs = append(tabs, layoutTab{Name: otherTabName, Groups: other})
	}
	return tabs
}

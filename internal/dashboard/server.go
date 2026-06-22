package dashboard

import (
	"embed"
	"html/template"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Header InfoWidget types rendered client-side / statically (no poll), as
// opposed to a registered pollable widget like openmeteo.
const (
	headerTypeGreeting = "greeting"
	headerTypeDatetime = "datetime"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// assetFS holds static assets served verbatim under /assets/ — currently the
// self-hosted Manrope font, embedded so the single binary needs no CDN (D11).
//
//go:embed assets/*.woff2
var assetFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/*.tmpl"))

// cardGroup is a display-ready group of cards sharing a ServiceEntry Group,
// in the order Store.Snapshot already produced (Order, then name). Columns/
// Style/IconURL come from the Configuration's Layout, when one places this
// group in a tab.
type cardGroup struct {
	Name    string
	Cards   []Card
	Columns *int32
	Style   string
	IconURL string
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
}

// indexData is the page shell's template data: site-wide look (theme/
// color/background/search) plus the htmx poll interval.
type indexData struct {
	Site           Site
	AccentHex      string
	Ramp           Ramp
	RefreshSeconds int
}

// fragmentData is the polled fragment's template data: the live widget
// cards plus the static bookmark cards, both grouped for display.
type fragmentData struct {
	Tabs           []layoutTab
	BookmarkGroups []BookmarkGroup
	// SiteTarget is the default link target a card uses when it has no
	// per-card Target override.
	SiteTarget string
}

// headerWidgetView is one rendered header widget: a static definition joined
// with its live polled value (openmeteo) when one exists.
type headerWidgetView struct {
	Type     string
	Greeting string
	Format   string
	Fields   []Field
	Err      string
}

// headerData is the /header fragment's template data.
type headerData struct {
	Widgets []headerWidgetView
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /fragment", s.handleFragment)
	mux.HandleFunc("GET /header", s.handleHeader)
	mux.HandleFunc("GET /assets/{file}", handleAsset)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("GET /metrics", promhttp.Handler())
	return mux
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
	if strings.HasSuffix(r.PathValue("file"), ".woff2") {
		w.Header().Set("Content-Type", "font/woff2")
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
	data := indexData{Site: site, AccentHex: AccentHex(site.Color), Ramp: PaletteRamp(site.Color), RefreshSeconds: refresh}
	if err := templates.ExecuteTemplate(w, "index.html.tmpl", data); err != nil {
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
	data := fragmentData{
		Tabs:           layoutTabs(serviceCards(s.Store.Snapshot()), site.Layout),
		BookmarkGroups: site.BookmarkGroups,
		SiteTarget:     site.Target,
	}
	if err := templates.ExecuteTemplate(w, "cards.html.tmpl", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	if err := templates.ExecuteTemplate(w, "header.html.tmpl", data); err != nil {
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
		default:
			if c, ok := live[d.Name]; ok {
				v.Fields = c.Fields
				v.Err = c.Err
			}
		}
		views = append(views, v)
	}
	return views
}

// groupCards buckets an already-ordered card slice into display groups,
// preserving the incoming order of both groups and cards within a group.
func groupCards(cards []Card) []cardGroup {
	var groups []cardGroup
	index := map[string]int{}
	for _, c := range cards {
		i, ok := index[c.Group]
		if !ok {
			i = len(groups)
			index[c.Group] = i
			groups = append(groups, cardGroup{Name: c.Group})
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
func layoutTabs(cards []Card, layout []LayoutTab) []layoutTab {
	groups := groupCards(cards)
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

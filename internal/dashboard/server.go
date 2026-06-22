package dashboard

import (
	"embed"
	"html/template"
	"net/http"

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

var templates = template.Must(template.ParseFS(templateFS, "templates/*.tmpl"))

// cardGroup is a display-ready group of cards sharing a ServiceEntry Group,
// in the order Store.Snapshot already produced (Order, then name).
type cardGroup struct {
	Name  string
	Cards []Card
}

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
	RefreshSeconds int
}

// fragmentData is the polled fragment's template data: the live widget
// cards plus the static bookmark cards, both grouped for display.
type fragmentData struct {
	Groups         []cardGroup
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
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("GET /metrics", promhttp.Handler())
	return mux
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
	data := indexData{Site: site, AccentHex: AccentHex(site.Color), RefreshSeconds: refresh}
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
		Groups:         groupCards(serviceCards(s.Store.Snapshot())),
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

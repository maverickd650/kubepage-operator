package dashboard

import (
	"embed"
	"html/template"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"
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
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /fragment", s.handleFragment)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
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
	data := fragmentData{Groups: groupCards(s.Store.Snapshot()), BookmarkGroups: site.BookmarkGroups}
	if err := templates.ExecuteTemplate(w, "cards.html.tmpl", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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

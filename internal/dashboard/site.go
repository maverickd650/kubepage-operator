package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

var siteLog = ctrl.Log.WithName("dashboard-site")

// Default look, matching homepage's own out-of-the-box settings.yaml.
const (
	defaultTheme       = "dark"
	defaultColor       = "slate"
	defaultHeaderStyle = "underlined"
	defaultTitle       = "kubepage"
	defaultTarget      = "_blank"
)

// Site is everything the dashboard's look (6.1) needs beyond the polled
// widget cards: the Instance's Configuration (theme/color/background/...)
// and its bound Bookmarks, rendered as static link cards.
type Site struct {
	Theme       string
	Color       string
	HeaderStyle string
	Language    string
	FullWidth   bool

	Title       string
	Description string
	Favicon     string
	// CardBlur is a CSS length (e.g. "12px") already resolved from the
	// Configuration's Tailwind keyword, or "" for no card blur.
	CardBlur string
	// Target is the default link target for cards/bookmarks ("_blank"/"_self").
	Target string

	Background *Background
	Search     Search

	BookmarkGroups []BookmarkGroup
	HeaderWidgets  []HeaderWidget
	Layout         []LayoutTab
}

// LayoutTab mirrors api/v1alpha1.LayoutTabSpec, fully resolved.
type LayoutTab struct {
	Name   string
	Groups []LayoutGroup
}

// LayoutGroup mirrors api/v1alpha1.LayoutGroupSpec, fully resolved (Icon
// resolved to a URL the same way ServiceEntry/Bookmark Icon is).
type LayoutGroup struct {
	Name    string
	Columns *int32
	Style   string
	IconURL string
}

// HeaderWidget is one InfoWidget rendered in the dashboard header strip
// (datetime/greeting are described entirely by this; openmeteo's live value
// is matched in from the Store by Name).
type HeaderWidget struct {
	Name    string
	Type    string
	Order   *int32
	Options map[string]string
}

// Background mirrors api/v1alpha1.BackgroundSpec, resolved to render-ready
// values (nil pointers replaced with homepage's documented defaults where
// one exists, so the template never has to know about absence).
type Background struct {
	Image      string
	Blur       string
	Saturate   *int32
	Brightness *int32
	Opacity    *int32
}

// Search mirrors api/v1alpha1.SearchSpec, fully defaulted.
type Search struct {
	Provider    string
	URL         string
	Target      string
	FilterCards bool
}

// BookmarkGroup is one bookmarks.yaml-style group of static link cards.
type BookmarkGroup struct {
	Name      string
	Bookmarks []BookmarkCard
}

// BookmarkCard is one render-ready bookmark.
type BookmarkCard struct {
	Name        string
	Href        string
	IconURL     string
	Abbr        string
	Description string
}

// LoadSite reads the Instance's bound Configuration and Bookmarks directly
// from reader (expected cache-backed) and returns render-ready data. Unlike
// widget cards, this is read fresh on every request rather than polled on
// an interval: it's a handful of cheap informer-cache List calls, not a
// network round-trip to an external upstream.
func LoadSite(ctx context.Context, reader client.Reader, namespace, instanceName string) (Site, error) {
	site := Site{
		Theme:       defaultTheme,
		Color:       defaultColor,
		HeaderStyle: defaultHeaderStyle,
		Title:       defaultTitle,
		Target:      defaultTarget,
		Search:      Search{Provider: "duckduckgo", Target: "_blank", FilterCards: true},
	}

	var configs pagev1alpha1.ConfigurationList
	if err := reader.List(ctx, &configs, client.InNamespace(namespace)); err != nil {
		return site, fmt.Errorf("listing Configurations: %w", err)
	}

	var bound []pagev1alpha1.Configuration
	for _, c := range configs.Items {
		if c.Spec.InstanceRef.Name == instanceName {
			bound = append(bound, c)
		}
	}
	slices.SortFunc(bound, func(a, b pagev1alpha1.Configuration) int { return strings.Compare(a.Name, b.Name) })
	if len(bound) > 1 {
		siteLog.Info("Multiple Configurations reference this Instance; using the lexicographically first by name",
			"instance", instanceName, "using", bound[0].Name)
	}
	if len(bound) > 0 {
		applyConfiguration(&site, &bound[0].Spec)
	}

	var bookmarks pagev1alpha1.BookmarkList
	if err := reader.List(ctx, &bookmarks, client.InNamespace(namespace)); err != nil {
		return site, fmt.Errorf("listing Bookmarks: %w", err)
	}
	site.BookmarkGroups = groupBookmarks(bookmarks.Items, instanceName)

	var infoWidgets pagev1alpha1.InfoWidgetList
	if err := reader.List(ctx, &infoWidgets, client.InNamespace(namespace)); err != nil {
		return site, fmt.Errorf("listing InfoWidgets: %w", err)
	}
	site.HeaderWidgets = headerWidgets(infoWidgets.Items, instanceName)

	return site, nil
}

// headerWidgets returns the instance's bound InfoWidgets as render-ready
// header widget definitions, ordered by Order (nil last) then object name.
// Options' passthrough JSON is flattened into a string map; nested/array
// values are skipped (header widgets only use scalar options like text,
// latitude, format).
func headerWidgets(items []pagev1alpha1.InfoWidget, instanceName string) []HeaderWidget {
	var bound []pagev1alpha1.InfoWidget
	for _, w := range items {
		if w.Spec.InstanceRef.Name == instanceName {
			bound = append(bound, w)
		}
	}
	slices.SortFunc(bound, func(a, b pagev1alpha1.InfoWidget) int {
		if cmp := compareOrder(a.Spec.Order, b.Spec.Order); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Name, b.Name)
	})

	out := make([]HeaderWidget, 0, len(bound))
	for _, w := range bound {
		out = append(out, HeaderWidget{
			Name:    w.Name,
			Type:    w.Spec.Type,
			Order:   w.Spec.Order,
			Options: scalarOptions(w.Spec.Options),
		})
	}
	return out
}

// scalarOptions flattens an InfoWidget's passthrough Options JSON object into
// a string map, keeping only scalar values (string/number/bool).
func scalarOptions(raw *apiextensionsv1.JSON) map[string]string {
	opts := map[string]string{}
	if raw == nil || len(raw.Raw) == 0 {
		return opts
	}
	var m map[string]any
	if err := json.Unmarshal(raw.Raw, &m); err != nil {
		siteLog.Error(err, "parsing InfoWidget options")
		return opts
	}
	for k, v := range m {
		switch val := v.(type) {
		case string:
			opts[k] = val
		case bool:
			opts[k] = strconv.FormatBool(val)
		case float64:
			opts[k] = strconv.FormatFloat(val, 'f', -1, 64)
		}
	}
	return opts
}

func applyConfiguration(site *Site, spec *pagev1alpha1.ConfigurationSpec) {
	if spec.Title != nil {
		site.Title = *spec.Title
	}
	if spec.Description != nil {
		site.Description = *spec.Description
	}
	if spec.Favicon != nil {
		site.Favicon = *spec.Favicon
	}
	if spec.CardBlur != nil {
		site.CardBlur = blurPx(*spec.CardBlur)
	}
	if spec.Target != nil {
		site.Target = *spec.Target
	}
	if spec.Theme != nil {
		site.Theme = *spec.Theme
	}
	if spec.Color != nil {
		site.Color = *spec.Color
	}
	if spec.HeaderStyle != nil {
		site.HeaderStyle = *spec.HeaderStyle
	}
	if spec.Language != nil {
		site.Language = *spec.Language
	}
	if spec.FullWidth != nil {
		site.FullWidth = *spec.FullWidth
	}
	if spec.Background != nil {
		bg := &Background{Saturate: spec.Background.Saturate, Brightness: spec.Background.Brightness, Opacity: spec.Background.Opacity}
		if spec.Background.Image != nil {
			bg.Image = *spec.Background.Image
		}
		if spec.Background.Blur != nil {
			bg.Blur = *spec.Background.Blur
		}
		site.Background = bg
	}
	if s := spec.Search; s != nil {
		if s.Provider != nil {
			site.Search.Provider = *s.Provider
		}
		if s.URL != nil {
			site.Search.URL = *s.URL
		}
		if s.Target != nil {
			site.Search.Target = *s.Target
		}
		if s.FilterCards != nil {
			site.Search.FilterCards = *s.FilterCards
		}
	}
	if spec.Layout != nil {
		tabs := make([]LayoutTab, 0, len(spec.Layout))
		for _, t := range spec.Layout {
			groups := make([]LayoutGroup, 0, len(t.Groups))
			for _, g := range t.Groups {
				groups = append(groups, LayoutGroup{
					Name:    g.Name,
					Columns: g.Columns,
					Style:   stringOrEmpty(g.Style),
					IconURL: IconURL(g.Icon),
				})
			}
			tabs = append(tabs, LayoutTab{Name: t.Name, Groups: groups})
		}
		site.Layout = tabs
	}
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// blurPx maps a Tailwind backdrop-blur size keyword to its CSS pixel value
// (the Tailwind blur scale). An unknown or empty keyword yields "" (no blur);
// a bare "" keyword from the user means "default blur" → Tailwind's base 8px.
func blurPx(keyword string) string {
	switch keyword {
	case "none":
		return ""
	case "":
		return "8px"
	case "sm":
		return "4px"
	case "md":
		return "12px"
	case "lg":
		return "16px"
	case "xl":
		return "24px"
	case "2xl":
		return "40px"
	case "3xl":
		return "64px"
	default:
		return ""
	}
}

func groupBookmarks(items []pagev1alpha1.Bookmark, instanceName string) []BookmarkGroup {
	var bound []pagev1alpha1.Bookmark
	for _, b := range items {
		if b.Spec.InstanceRef.Name == instanceName {
			bound = append(bound, b)
		}
	}

	var groupNames []string
	groupOrder := map[string]*int32{}
	groupItems := map[string][]pagev1alpha1.Bookmark{}
	for _, b := range bound {
		if _, ok := groupItems[b.Spec.Group]; !ok {
			groupNames = append(groupNames, b.Spec.Group)
			groupOrder[b.Spec.Group] = b.Spec.Order
		} else if compareOrder(b.Spec.Order, groupOrder[b.Spec.Group]) < 0 {
			groupOrder[b.Spec.Group] = b.Spec.Order
		}
		groupItems[b.Spec.Group] = append(groupItems[b.Spec.Group], b)
	}
	slices.SortFunc(groupNames, func(a, c string) int {
		if cmp := compareOrder(groupOrder[a], groupOrder[c]); cmp != 0 {
			return cmp
		}
		return strings.Compare(a, c)
	})

	groups := make([]BookmarkGroup, 0, len(groupNames))
	for _, name := range groupNames {
		bms := groupItems[name]
		slices.SortFunc(bms, func(a, b pagev1alpha1.Bookmark) int {
			if cmp := compareOrder(a.Spec.Order, b.Spec.Order); cmp != 0 {
				return cmp
			}
			return strings.Compare(a.Spec.Name, b.Spec.Name)
		})

		cards := make([]BookmarkCard, 0, len(bms))
		for _, b := range bms {
			card := BookmarkCard{Name: b.Spec.Name, Href: b.Spec.Href, IconURL: IconURL(b.Spec.Icon)}
			if b.Spec.Abbr != nil {
				card.Abbr = *b.Spec.Abbr
			}
			if b.Spec.Description != nil {
				card.Description = *b.Spec.Description
			}
			cards = append(cards, card)
		}
		groups = append(groups, BookmarkGroup{Name: name, Bookmarks: cards})
	}
	return groups
}

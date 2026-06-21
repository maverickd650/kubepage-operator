package dashboard

import (
	"context"
	"fmt"
	"slices"
	"strings"

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

	Background *Background
	Search     Search

	BookmarkGroups []BookmarkGroup
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

	return site, nil
}

func applyConfiguration(site *Site, spec *pagev1alpha1.ConfigurationSpec) {
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

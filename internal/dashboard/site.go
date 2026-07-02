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
	themeLight         = "light"
	defaultColor       = "slate"
	defaultHeaderStyle = "underlined"
	defaultTitle       = "kubepage"
	defaultTarget      = "_blank"
)

// bookmarksStyleIcons is ConfigurationSpec.BookmarksStyle's one valid value,
// matching homepage's `bookmarksStyle: icons`.
const bookmarksStyleIcons = "icons"

// Site is everything the dashboard's look (6.1) needs beyond the polled
// widget cards: the Instance's Configuration (theme/color/background/...)
// and its bound Bookmarks, rendered as static link cards.
type Site struct {
	Theme       string
	Color       string
	HeaderStyle string
	Language    string
	FullWidth   bool

	// ThemeFixed/ColorFixed report whether Configuration.Spec.Theme/Color
	// was set, disabling the corresponding client-side switcher control —
	// homepage's documented "fixed theme/palette" behavior.
	ThemeFixed bool
	ColorFixed bool

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

	// DisableCollapse/GroupsInitiallyCollapsed/UseEqualHeights are the
	// site-wide defaults for collapsible group headers; a LayoutGroup's own
	// fields override these per group.
	DisableCollapse          bool
	GroupsInitiallyCollapsed bool
	UseEqualHeights          bool

	// BookmarksIconsOnly renders every bookmark card icon-only, matching
	// homepage's `bookmarksStyle: icons`.
	BookmarksIconsOnly bool

	// DisableIndexing asks search engines not to index the dashboard.
	DisableIndexing bool

	// StartURL is the PWA manifest's start_url, defaulting to "/".
	StartURL string

	// CustomCSS is raw, operator-supplied CSS appended after the built-in
	// stylesheet.
	CustomCSS string
	// CustomJS is raw, operator-supplied JavaScript run once on page load.
	CustomJS string

	// StatusStyle/HideErrors are the site-wide defaults a ServiceEntry falls
	// back to when it doesn't set its own StatusStyle/HideErrors (see
	// poller.go's siteDefaults).
	StatusStyle string
	HideErrors  bool

	// HideVersion hides the dashboard's version/commit footer.
	HideVersion bool

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
// resolved to a URL the same way ServiceEntry/Bookmark Icon is). Header/
// InitiallyCollapsed/UseEqualHeights stay pointers here (unlike Columns'
// sibling Style) so layoutTabs (server.go) can tell "unset" apart from an
// explicit false/true and fall back to the Site-wide default.
type LayoutGroup struct {
	Name               string
	Columns            *int32
	Style              string
	IconURL            string
	Header             *bool
	InitiallyCollapsed *bool
	UseEqualHeights    *bool
}

// HeaderWidget is one InfoWidget rendered in the dashboard header strip
// (datetime/greeting are described entirely by this; openmeteo's live value
// is matched in from the Store by Name).
type HeaderWidget struct {
	Name    string
	Type    string
	Order   *int32
	IconURL string
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

// Search mirrors api/v1alpha1.SearchSpec, fully defaulted. JSON tags match
// the field names the page shell's client-side searchConfig JS expects
// (read via templ.JSONScript in index.templ).
type Search struct {
	Provider    string `json:"provider"`
	URL         string `json:"url"`
	Target      string `json:"target"`
	FilterCards bool   `json:"filterCards"`
}

// BookmarkGroup is one bookmarks.yaml-style group of static link cards.
// Columns/Style/IconURL/Header/InitiallyCollapsed/UseEqualHeights mirror
// cardGroup's resolved (non-pointer) fields: a LayoutGroupSpec whose Name
// matches this group's Name styles it exactly like a service group's
// (server.go's layoutTabs), already resolved against the Site-wide
// defaults by groupBookmarks. A bookmark group doesn't move into a tab —
// only its look is shared with the Layout config.
type BookmarkGroup struct {
	Name               string
	Bookmarks          []BookmarkCard
	Columns            *int32
	Style              string
	IconURL            string
	Header             bool
	InitiallyCollapsed bool
	UseEqualHeights    bool
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
		StartURL:    "/",
		StatusStyle: statusStyleDot,
		Search:      Search{Provider: "duckduckgo", Target: defaultTarget, FilterCards: true},
	}

	spec, err := boundConfigurationSpec(ctx, reader, namespace, instanceName)
	if err != nil {
		return site, err
	}
	if spec != nil {
		applyConfiguration(&site, spec)
	}

	var bookmarks pagev1alpha1.BookmarkList
	if err := reader.List(ctx, &bookmarks, client.InNamespace(namespace)); err != nil {
		return site, fmt.Errorf("listing Bookmarks: %w", err)
	}
	site.BookmarkGroups = groupBookmarks(bookmarks.Items, instanceName, site)

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
			IconURL: IconURL(w.Spec.Icon),
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

// boundConfigurationSpec returns the Spec of the Configuration bound to
// instanceName (the lexicographically first by name, when more than one
// references the same Instance), or nil if none is bound. Shared by LoadSite
// and Poller.siteDefaults so both read the same "which Configuration wins"
// rule from a single place.
func boundConfigurationSpec(ctx context.Context, reader client.Reader, namespace, instanceName string) (*pagev1alpha1.ConfigurationSpec, error) {
	var configs pagev1alpha1.ConfigurationList
	if err := reader.List(ctx, &configs, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing Configurations: %w", err)
	}

	var bound []pagev1alpha1.Configuration
	for _, c := range configs.Items {
		if c.Spec.InstanceRef.Name == instanceName {
			bound = append(bound, c)
		}
	}
	if len(bound) == 0 {
		return nil, nil
	}
	slices.SortFunc(bound, func(a, b pagev1alpha1.Configuration) int { return strings.Compare(a.Name, b.Name) })
	if len(bound) > 1 {
		siteLog.Info("Multiple Configurations reference this Instance; using the lexicographically first by name",
			"instance", instanceName, "using", bound[0].Name)
	}
	return &bound[0].Spec, nil
}

func applyConfiguration(site *Site, spec *pagev1alpha1.ConfigurationSpec) {
	applyLookFields(site, spec)
	applySearch(site, spec.Search)
	applyLayout(site, spec.Layout)
	applyBehaviorFields(site, spec)
}

// applyLookFields applies the Configuration fields governing the page's
// visual identity: title/description/favicon/card look, theme/color/header
// style, language, layout width, and background image.
func applyLookFields(site *Site, spec *pagev1alpha1.ConfigurationSpec) {
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
		site.ThemeFixed = true
	}
	if spec.Color != nil {
		site.Color = *spec.Color
		site.ColorFixed = true
	}
	if spec.HeaderStyle != nil {
		site.HeaderStyle = *spec.HeaderStyle
	}
	if spec.Language != nil {
		site.Language = *spec.Language
	}
	if spec.FullWidth != nil {
		site.FullWidth = *spec.FullWidth == pagev1alpha1.FullWidthFull
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
}

// applySearch applies the Configuration's header search box settings, if set.
func applySearch(site *Site, s *pagev1alpha1.SearchSpec) {
	if s == nil {
		return
	}
	if s.Provider != nil {
		site.Search.Provider = *s.Provider
	}
	if s.URL != nil && isHTTPURL(*s.URL) {
		// The CRD schema's Pattern marker already rejects non-http(s) URLs at
		// apply time, but re-check here defensively: this value is passed
		// straight into a client-side window.open()/href, so a non-http(s)
		// scheme (e.g. "javascript:") would be a stored script-injection
		// vector, and existing CRs predate the schema change.
		site.Search.URL = *s.URL
	}
	if s.Target != nil {
		site.Search.Target = *s.Target
	}
	if s.FilterCards != nil {
		site.Search.FilterCards = *s.FilterCards == pagev1alpha1.Enabled
	}
}

// applyLayout resolves the Configuration's Layout tabs/groups, if set.
func applyLayout(site *Site, layout []pagev1alpha1.LayoutTabSpec) {
	if layout == nil {
		return
	}
	tabs := make([]LayoutTab, 0, len(layout))
	for _, t := range layout {
		groups := make([]LayoutGroup, 0, len(t.Groups))
		for _, g := range t.Groups {
			groups = append(groups, LayoutGroup{
				Name:               g.Name,
				Columns:            g.Columns,
				Style:              stringOrEmpty(g.Style),
				IconURL:            IconURL(g.Icon),
				Header:             boolFromEnum(g.Header, pagev1alpha1.HeaderShown),
				InitiallyCollapsed: boolFromEnum(g.InitiallyCollapsed, pagev1alpha1.CollapseCollapsed),
				UseEqualHeights:    boolFromEnum(g.UseEqualHeights, pagev1alpha1.HeightsEqual),
			})
		}
		tabs = append(tabs, LayoutTab{Name: t.Name, Groups: groups})
	}
	site.Layout = tabs
}

// applyBehaviorFields applies the Configuration fields governing dashboard
// behavior rather than look: collapsibility, bookmark/indexing/PWA
// settings, injected CSS/JS, and the site-wide status/error/version defaults.
func applyBehaviorFields(site *Site, spec *pagev1alpha1.ConfigurationSpec) {
	if spec.DisableCollapse != nil {
		site.DisableCollapse = *spec.DisableCollapse == pagev1alpha1.Disabled
	}
	if spec.GroupsInitiallyCollapsed != nil {
		site.GroupsInitiallyCollapsed = *spec.GroupsInitiallyCollapsed == pagev1alpha1.CollapseCollapsed
	}
	if spec.UseEqualHeights != nil {
		site.UseEqualHeights = *spec.UseEqualHeights == pagev1alpha1.HeightsEqual
	}
	if spec.BookmarksStyle != nil {
		site.BookmarksIconsOnly = *spec.BookmarksStyle == bookmarksStyleIcons
	}
	if spec.DisableIndexing != nil {
		site.DisableIndexing = *spec.DisableIndexing == pagev1alpha1.IndexingNoIndex
	}
	if spec.StartURL != nil {
		site.StartURL = *spec.StartURL
	}
	if spec.CustomCSS != nil {
		site.CustomCSS = *spec.CustomCSS
	}
	if spec.CustomJS != nil {
		site.CustomJS = *spec.CustomJS
	}
	if spec.StatusStyle != nil {
		site.StatusStyle = *spec.StatusStyle
	}
	if spec.HideErrors != nil {
		site.HideErrors = *spec.HideErrors == pagev1alpha1.StatsHide
	}
	if spec.HideVersion != nil {
		site.HideVersion = *spec.HideVersion == pagev1alpha1.Enabled
	}
}

// boolFromEnum converts an optional two-valued enum pointer (e.g.
// LayoutGroupSpec.Header's "Shown"/"Hidden") into the *bool tri-state used
// internally: nil stays nil (caller falls back to a site-wide default),
// otherwise the pointee reports whether the enum equals trueValue.
func boolFromEnum(s *string, trueValue string) *bool {
	if s == nil {
		return nil
	}
	b := *s == trueValue
	return &b
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

// layoutGroupsByName flattens every LayoutGroup across every tab into a
// lookup by Name, for groupBookmarks to style a bookmark group the same way
// as a service group sharing its name. A name placed in more than one tab
// (already an edge case layoutTabs also just takes the first for) resolves
// to whichever tab's Groups slice was flattened first.
func layoutGroupsByName(layout []LayoutTab) map[string]LayoutGroup {
	byName := map[string]LayoutGroup{}
	for _, t := range layout {
		for _, g := range t.Groups {
			if _, ok := byName[g.Name]; !ok {
				byName[g.Name] = g
			}
		}
	}
	return byName
}

func groupBookmarks(items []pagev1alpha1.Bookmark, instanceName string, site Site) []BookmarkGroup {
	layoutByName := layoutGroupsByName(site.Layout)

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

		bg := BookmarkGroup{
			Name:               name,
			Bookmarks:          cards,
			Header:             true,
			InitiallyCollapsed: site.GroupsInitiallyCollapsed,
			UseEqualHeights:    site.UseEqualHeights,
		}
		if lg, ok := layoutByName[name]; ok {
			bg.Columns = lg.Columns
			bg.Style = lg.Style
			bg.IconURL = lg.IconURL
			if lg.Header != nil {
				bg.Header = *lg.Header
			}
			if lg.InitiallyCollapsed != nil {
				bg.InitiallyCollapsed = *lg.InitiallyCollapsed
			}
			if lg.UseEqualHeights != nil {
				bg.UseEqualHeights = *lg.UseEqualHeights
			}
		}
		groups = append(groups, bg)
	}
	return groups
}

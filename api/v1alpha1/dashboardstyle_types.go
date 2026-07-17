package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// BackgroundSpec specifies a background image and the filters applied over it.
// See https://gethomepage.dev/configs/settings/#background-image
// +kubebuilder:validation:MinProperties=1
type BackgroundSpec struct {
	// image is the full URL or path (relative to /app/public) to the
	// background image.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	Image *string `json:"image,omitempty"`

	// blur is a backdrop blur, a Tailwind backdrop-blur size keyword (e.g.
	// "sm", "md", "xl", ""). An explicit "" is equivalent to leaving it
	// unset (the default 8px blur).
	// +kubebuilder:validation:MaxLength=8
	// +optional
	Blur *string `json:"blur,omitempty"`

	// saturate is a backdrop saturate percentage (Tailwind backdrop-saturate
	// scale, e.g. 0, 50, 100).
	// +optional
	Saturate *int32 `json:"saturate,omitempty"`

	// brightness is a backdrop brightness percentage (Tailwind
	// backdrop-brightness scale, e.g. 0, 50, 75).
	// +optional
	Brightness *int32 `json:"brightness,omitempty"`

	// opacity blends the background image with the background color, 0-100.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	Opacity *int32 `json:"opacity,omitempty"`
}

// SearchSpec configures the native dashboard's header search box (D11 /
// Phase 6.1): as-you-type card filtering plus an Enter-to-search fallthrough
// to a web search provider. This has no homepage settings.yaml equivalent —
// it's specific to the native dashboard renderer, not rendered into
// settings.yaml.
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:XValidation:rule="self.provider != 'custom' || has(self.url)",message="url is required when provider is \"custom\""
type SearchSpec struct {
	// provider is the web search engine Enter submits the query to.
	// "custom" requires URL.
	// +kubebuilder:validation:Enum=duckduckgo;google;bing;custom
	// +default="duckduckgo"
	// +optional
	Provider *string `json:"provider,omitempty"`

	// url is the search endpoint used when Provider is "custom". The query
	// is appended as a URL-encoded "q" parameter. Must be an http(s) URL —
	// the dashboard passes this straight into a client-side window.open()/
	// href, so a non-http(s) scheme (e.g. "javascript:") would be a stored
	// script-injection vector rather than a search endpoint.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	URL *string `json:"url,omitempty"`

	// target controls whether the search results page opens in the same tab
	// or a new one.
	// +kubebuilder:validation:Enum=_blank;_self
	// +default="_blank"
	// +optional
	Target *string `json:"target,omitempty"`

	// filterCards enables as-you-type filtering of service and bookmark
	// cards by name/description, independent of the Enter-to-search
	// fallthrough.
	// +default=true
	// +optional
	FilterCards *bool `json:"filterCards,omitempty"`

	// searchDescriptions includes each card's description, not just its
	// name, when matching the quick-launch (Ctrl/Cmd-K) palette's query
	// against cards. Independent of FilterCards, which controls the
	// inline as-you-type card filter instead.
	// +default=true
	// +optional
	SearchDescriptions *bool `json:"searchDescriptions,omitempty"`

	// internetSearchEntry controls the quick-launch palette's "search the
	// web" fallthrough entry: true (the default) shows it alongside card
	// matches; false removes it, leaving only card matches (and, unless
	// visitURLEntry is also false, the direct-URL entry).
	// +default=true
	// +optional
	InternetSearchEntry *bool `json:"internetSearchEntry,omitempty"`

	// visitURLEntry controls the quick-launch palette's "visit <url>" entry
	// that appears when the typed query itself looks like a URL or domain:
	// true (the default) lets the query jump there directly; false removes
	// the entry, requiring a web search instead.
	// +default=true
	// +optional
	VisitURLEntry *bool `json:"visitURLEntry,omitempty"`
}

// LayoutGroupSpec configures one Group's placement and style within a
// LayoutTabSpec, mirroring homepage's settings.yaml `layout:` per-group
// style keys (style/columns/icon).
// See https://gethomepage.dev/configs/settings/#layout
type LayoutGroupSpec struct {
	// name is the ServiceCard Group this entry places and styles, or a
	// "/"-separated path (e.g. "Media/Movies") naming a nested subgroup to
	// style (see ServiceEntry.Group). A root name (no "/") also places the
	// whole group — and everything nested under it — into this tab; a path
	// entry only styles the subgroup it names and must not place it into a
	// different tab than its root (see LayoutTabSpec's validation rule).
	// +kubebuilder:validation:Pattern=`^[^/]+(/[^/]+){0,2}$`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +required
	Name string `json:"name"`

	// columns is the number of card columns to render this group in,
	// overriding the dashboard's default auto-fill grid.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=6
	// +optional
	Columns *int32 `json:"columns,omitempty"`

	// style lays the group's cards out in a single horizontally-scrolling
	// row instead of a grid. Setting columns alongside "row" follows
	// homepage's layout semantics: the group renders as a normal wrapping
	// grid of that many columns (the single-row scroller applies only when
	// no columns are set).
	// +kubebuilder:validation:Enum=row;column
	// +optional
	Style *string `json:"style,omitempty"`

	// icon overrides the group header's icon. Same resolution rules as
	// ServiceCard/Bookmark Icon: a full URL passes through, homepage's icon
	// prefix syntax ("mdi-X"/"si-X"/"lucide-X"/"wi-X"/"fa6-solid-X"/"sh-X",
	// see ServiceEntry.Icon) resolves to that icon set, and anything else is
	// resolved as a dashboard-icons slug.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Icon *string `json:"icon,omitempty"`

	// header renders this group's header (name + icon) when true (the
	// default). Set false to hide it while still rendering the group's
	// cards.
	// +optional
	Header *bool `json:"header,omitempty"`

	// initiallyCollapsed collapses this group by default on first load,
	// overriding the DashboardStyle's GroupsInitiallyCollapsed. Ignored when
	// Collapse is false.
	// +optional
	InitiallyCollapsed *bool `json:"initiallyCollapsed,omitempty"`

	// useEqualHeights makes every card in this group the same height,
	// overriding the DashboardStyle's UseEqualHeights.
	// +optional
	UseEqualHeights *bool `json:"useEqualHeights,omitempty"`
}

// LayoutTabSpec is one tab: a named, ordered set of Groups shown together.
// Groups not referenced by any tab still render, appended to an implicit
// trailing "Other" tab; a Group referenced by more than one tab is shown
// under the first tab that lists it. Tab placement is root-only: a nested
// subgroup always renders in whatever tab its root group is placed in, so a
// path-named LayoutGroupSpec entry (e.g. "Media/Movies", only styling that
// subgroup) must not appear in a tab unless an ancestor prefix of that path
// ("Media", or "Media/Movies" for a "Media/Movies/4K" entry) is also listed
// in this same tab's groups — applied to every path entry, this makes the
// root itself required transitively; otherwise which tab the subgroup would
// render under is ambiguous. (The rule checks the immediate prefix rather
// than splitting out the root segment to stay within the apiserver's static
// CEL cost budget.)
// +kubebuilder:validation:XValidation:rule="self.groups.all(g, !g.name.contains('/') || self.groups.exists(r, g.name.startsWith(r.name + '/')))",message="a nested group entry's parent group must be listed in the same tab"
type LayoutTabSpec struct {
	// name is the tab's label.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +required
	Name string `json:"name"`

	// groups lists, in display order, the Groups shown under this tab.
	// MaxItems is 32 (down from 64 pre-nesting) to keep the tab's
	// parent-listed CEL rule within the apiserver's static cost budget —
	// the rule's estimated cost is quadratic in this bound.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +listType=atomic
	// +required
	Groups []LayoutGroupSpec `json:"groups"`
}

// DashboardStyleSpec defines the desired state of DashboardStyle: the native
// dashboard's theme/color/background/header-style look and its header search
// box, applied by internal/dashboard's LoadSite.
type DashboardStyleSpec struct {
	// dashboardRef names the Dashboard this DashboardStyle applies to. Must
	// equal this object's own metadata.name (enforced by a CEL rule on the
	// DashboardStyle type), which is what makes at most one DashboardStyle
	// per Dashboard possible to enforce via ordinary object-name uniqueness.
	// +required
	DashboardRef DashboardRef `json:"dashboardRef"`

	// title is the dashboard's browser tab title and header heading.
	// Defaults to "kubepage" when unset.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Title *string `json:"title,omitempty"`

	// description is shown as a header subtitle and the page's meta
	// description.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	Description *string `json:"description,omitempty"`

	// favicon is a URL to the dashboard's favicon.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	Favicon *string `json:"favicon,omitempty"`

	// cardBlur applies a backdrop blur to cards, a Tailwind backdrop-blur
	// size keyword (e.g. "sm", "md", "xl", ""). An explicit "" is equivalent
	// to leaving it unset (the default 8px blur). Most visible over a
	// background image.
	// +kubebuilder:validation:MaxLength=8
	// +optional
	CardBlur *string `json:"cardBlur,omitempty"`

	// target is the default link target for service and bookmark cards.
	// Individual ServiceCards may override it via their own target.
	// +kubebuilder:validation:Enum=_blank;_self
	// +default="_blank"
	// +optional
	Target *string `json:"target,omitempty"`

	// background image and filters, used instead of the solid theme color.
	// +optional
	Background *BackgroundSpec `json:"background,omitempty"`

	// theme is the fixed theme, disabling the theme switcher. One of "light"
	// or "dark".
	// +kubebuilder:validation:Enum=light;dark
	// +optional
	Theme *string `json:"theme,omitempty"`

	// color is the fixed color palette, disabling the palette switcher.
	// +kubebuilder:validation:Enum=slate;gray;zinc;neutral;stone;amber;yellow;lime;green;emerald;teal;cyan;sky;blue;indigo;violet;purple;fuchsia;pink;rose;red;white
	// +optional
	Color *string `json:"color,omitempty"`

	// headerStyle for the header info-widget strip (datetime, greeting,
	// weather, etc.): "underlined" draws a line under it, "boxed" wraps it in
	// a card, "clean" leaves it unstyled, and "boxedWidgets" additionally
	// gives each individual widget its own boxed card.
	// +kubebuilder:validation:Enum=underlined;boxed;clean;boxedWidgets
	// +optional
	HeaderStyle *string `json:"headerStyle,omitempty"`

	// language is the UI language, e.g. "en", "fr", "de".
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=16
	// +optional
	Language *string `json:"language,omitempty"`

	// fullWidth uses the entire window width instead of a centered,
	// constrained layout.
	// +optional
	FullWidth *bool `json:"fullWidth,omitempty"`

	// search configures the native dashboard's header search box (card
	// filtering + web-search fallthrough).
	// +optional
	Search *SearchSpec `json:"search,omitempty"`

	// layout arranges ServiceCard Groups into tabs, mirroring homepage's
	// settings.yaml `layout:` map (group -> style) with tabs added on top.
	// Omitted/empty renders every group flat with no tab UI, as before.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +listType=atomic
	// +optional
	Layout []LayoutTabSpec `json:"layout,omitempty"`

	// collapse controls the collapsible expand/collapse control on service
	// and bookmark group headers. true (the default) shows the control;
	// false renders every group with a plain, non-collapsible header.
	// +default=true
	// +optional
	Collapse *bool `json:"collapse,omitempty"`

	// groupsInitiallyCollapsed collapses every group by default on first
	// load. A LayoutGroupSpec's own InitiallyCollapsed overrides this per
	// group. Ignored when Collapse is false.
	// +optional
	GroupsInitiallyCollapsed *bool `json:"groupsInitiallyCollapsed,omitempty"`

	// useEqualHeights makes every card in a group the same height. A
	// LayoutGroupSpec's own UseEqualHeights overrides this per group.
	// +optional
	UseEqualHeights *bool `json:"useEqualHeights,omitempty"`

	// bookmarksStyle renders every bookmark card icon-only (no name or
	// description), matching homepage's `bookmarksStyle: icons`.
	// +kubebuilder:validation:Enum=icons
	// +optional
	BookmarksStyle *string `json:"bookmarksStyle,omitempty"`

	// indexing controls whether search engines may index the dashboard:
	// true (the default) leaves indexing unrestricted; false disallows all
	// crawlers in robots.txt and adds a noindex meta tag.
	// +default=true
	// +optional
	Indexing *bool `json:"indexing,omitempty"`

	// startUrl is the PWA manifest's start_url, used when the dashboard is
	// installed as an app. Defaults to "/".
	// +kubebuilder:validation:Pattern=`^(https?://|/)`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	StartURL *string `json:"startUrl,omitempty"`

	// customCSS is raw CSS injected into the dashboard's page in a second
	// <style> block appended after the built-in stylesheet, so its rules
	// can override it. Trusted, operator-supplied content — the same trust
	// level as every other DashboardStyle field (e.g. Background.Image).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=10000
	// +optional
	CustomCSS *string `json:"customCSS,omitempty"`

	// customJS is raw JavaScript injected into the dashboard's page in a
	// <script> block, run once on page load. Trusted, operator-supplied
	// content — the same trust level as CustomCSS/Background.Image.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=10000
	// +optional
	CustomJS *string `json:"customJS,omitempty"`

	// statusStyle is the site-wide default for how a ServiceCard's Ping/
	// SiteMonitor/PodSelector status renders ("dot" a colored status dot,
	// "basic" a colored status pill with status word plus latency/
	// ready-count detail), used when a ServiceCard doesn't set its own
	// StatusStyle. Defaults to "dot" when unset here too. Kept as an enum
	// rather than a bool: a third rendering style is plausible here, unlike
	// this file's other converted fields.
	// +kubebuilder:validation:Enum=dot;basic
	// +optional
	StatusStyle *string `json:"statusStyle,omitempty"`

	// errorDisplay is the site-wide default for whether a widget's error
	// text is shown on its card, used when a ServiceCard doesn't set its own
	// ErrorDisplay. Defaults to true when unset here too.
	// +default=true
	// +optional
	ErrorDisplay *bool `json:"errorDisplay,omitempty"`

	// hideVersion hides the dashboard's version/commit footer.
	// +optional
	HideVersion *bool `json:"hideVersion,omitempty"`
}

// DashboardStyleStatus defines the observed state of DashboardStyle.
// +kubebuilder:validation:MinProperties=1
type DashboardStyleStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the DashboardStyle resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pstyle
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Available')].status"
// +kubebuilder:printcolumn:name="Dashboard",type=string,JSONPath=".spec.dashboardRef.name"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// +kubebuilder:validation:XValidation:rule="self.metadata.name == self.spec.dashboardRef.name",message="a DashboardStyle must be named after the Dashboard it styles (metadata.name must equal spec.dashboardRef.name)"

// DashboardStyle is the Schema for the dashboardstyles API. Exactly one
// DashboardStyle may exist per Dashboard: the CEL rule above requires
// metadata.name == spec.dashboardRef.name, so the API server's own name
// uniqueness makes a second DashboardStyle bound to the same Dashboard
// impossible, rather than merely undefined (see docs/crd-architecture-review.md
// finding #4).
type DashboardStyle struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of DashboardStyle
	// +required
	Spec DashboardStyleSpec `json:"spec"`

	// status defines the observed state of DashboardStyle
	// +optional
	Status DashboardStyleStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// DashboardStyleList contains a list of DashboardStyle
type DashboardStyleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []DashboardStyle `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &DashboardStyle{}, &DashboardStyleList{})
		return nil
	})
}

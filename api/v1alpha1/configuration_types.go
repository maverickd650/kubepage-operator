package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// BackgroundSpec specifies a background image and the filters applied over it.
// See https://gethomepage.dev/configs/settings/#background-image
type BackgroundSpec struct {
	// Full URL or path (relative to /app/public) to the background image.
	// +optional
	Image *string `json:"image,omitempty"`

	// Backdrop blur, a Tailwind backdrop-blur size keyword (e.g. "sm", "md", "xl", "").
	// +optional
	Blur *string `json:"blur,omitempty"`

	// Backdrop saturate percentage (Tailwind backdrop-saturate scale, e.g. 0, 50, 100).
	// +optional
	Saturate *int32 `json:"saturate,omitempty"`

	// Backdrop brightness percentage (Tailwind backdrop-brightness scale, e.g. 0, 50, 75).
	// +optional
	Brightness *int32 `json:"brightness,omitempty"`

	// Opacity to blend the background image with the background color, 0-100.
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
type SearchSpec struct {
	// Provider is the web search engine Enter submits the query to.
	// "custom" requires URL.
	// +kubebuilder:validation:Enum=duckduckgo;google;bing;custom
	// +kubebuilder:default=duckduckgo
	// +optional
	Provider *string `json:"provider,omitempty"`

	// URL is the search endpoint used when Provider is "custom". The query
	// is appended as a URL-encoded "q" parameter. Must be an http(s) URL —
	// the dashboard passes this straight into a client-side window.open()/
	// href, so a non-http(s) scheme (e.g. "javascript:") would be a stored
	// script-injection vector rather than a search endpoint.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +optional
	URL *string `json:"url,omitempty"`

	// Target controls whether the search results page opens in the same tab
	// or a new one.
	// +kubebuilder:validation:Enum=_blank;_self
	// +kubebuilder:default=_blank
	// +optional
	Target *string `json:"target,omitempty"`

	// FilterCards enables as-you-type filtering of service and bookmark
	// cards by name/description, independent of the Enter-to-search
	// fallthrough.
	// +kubebuilder:default=true
	// +optional
	FilterCards *bool `json:"filterCards,omitempty"`
}

// LayoutGroupSpec configures one Group's placement and style within a
// LayoutTabSpec, mirroring homepage's settings.yaml `layout:` per-group
// style keys (style/columns/icon).
// See https://gethomepage.dev/configs/settings/#layout
type LayoutGroupSpec struct {
	// Name is the ServiceEntry Group this entry places and styles.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Columns is the number of card columns to render this group in,
	// overriding the dashboard's default auto-fill grid.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=6
	// +optional
	Columns *int32 `json:"columns,omitempty"`

	// Style lays the group's cards out in a single row instead of a grid.
	// +kubebuilder:validation:Enum=row;column
	// +optional
	Style *string `json:"style,omitempty"`

	// Icon overrides the group header's icon. Same resolution rules as
	// ServiceEntry/Bookmark Icon: a full URL passes through, anything else
	// is resolved as a dashboard-icons slug.
	// +optional
	Icon *string `json:"icon,omitempty"`

	// Header renders this group's header (name + icon) when true (the
	// default). Set false to hide it while still rendering the group's
	// cards.
	// +optional
	Header *bool `json:"header,omitempty"`

	// InitiallyCollapsed collapses this group by default on first load,
	// overriding the Configuration's GroupsInitiallyCollapsed. Ignored when
	// DisableCollapse is set.
	// +optional
	InitiallyCollapsed *bool `json:"initiallyCollapsed,omitempty"`

	// UseEqualHeights makes every card in this group the same height,
	// overriding the Configuration's UseEqualHeights.
	// +optional
	UseEqualHeights *bool `json:"useEqualHeights,omitempty"`
}

// LayoutTabSpec is one tab: a named, ordered set of Groups shown together.
// Groups not referenced by any tab still render, appended to an implicit
// trailing "Other" tab; a Group referenced by more than one tab is shown
// under the first tab that lists it.
type LayoutTabSpec struct {
	// Name is the tab's label.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Groups lists, in display order, the Groups shown under this tab.
	// +kubebuilder:validation:MinItems=1
	Groups []LayoutGroupSpec `json:"groups"`
}

// ConfigurationSpec defines the desired state of Configuration: the native
// dashboard's theme/color/background/header-style look and its header search
// box, applied by internal/dashboard's LoadSite.
type ConfigurationSpec struct {
	// InstanceRef names the Instance this Configuration applies to.
	// +required
	InstanceRef InstanceRef `json:"instanceRef"`

	// Title is the dashboard's browser tab title and header heading.
	// Defaults to "kubepage" when unset.
	// +optional
	Title *string `json:"title,omitempty"`

	// Description is shown as a header subtitle and the page's meta
	// description.
	// +optional
	Description *string `json:"description,omitempty"`

	// Favicon is a URL to the dashboard's favicon.
	// +optional
	Favicon *string `json:"favicon,omitempty"`

	// CardBlur applies a backdrop blur to cards, a Tailwind backdrop-blur
	// size keyword (e.g. "sm", "md", "xl"). Most visible over a background
	// image.
	// +optional
	CardBlur *string `json:"cardBlur,omitempty"`

	// Target is the default link target for service and bookmark cards.
	// Individual ServiceEntries may override it via their own target.
	// +kubebuilder:validation:Enum=_blank;_self
	// +kubebuilder:default=_blank
	// +optional
	Target *string `json:"target,omitempty"`

	// Background image and filters, used instead of the solid theme color.
	// +optional
	Background *BackgroundSpec `json:"background,omitempty"`

	// Fixed theme, disabling the theme switcher. One of "light" or "dark".
	// +kubebuilder:validation:Enum=light;dark
	// +optional
	Theme *string `json:"theme,omitempty"`

	// Fixed color palette, disabling the palette switcher.
	// +kubebuilder:validation:Enum=slate;gray;zinc;neutral;stone;amber;yellow;lime;green;emerald;teal;cyan;sky;blue;indigo;violet;purple;fuchsia;pink;rose;red;white
	// +optional
	Color *string `json:"color,omitempty"`

	// Header style for service/bookmark group headers.
	// +kubebuilder:validation:Enum=underlined;boxed;clean;boxedWidgets
	// +optional
	HeaderStyle *string `json:"headerStyle,omitempty"`

	// UI language, e.g. "en", "fr", "de".
	// +optional
	Language *string `json:"language,omitempty"`

	// Use the entire window width instead of a centered, constrained layout.
	// +optional
	FullWidth *bool `json:"fullWidth,omitempty"`

	// Search configures the native dashboard's header search box (card
	// filtering + web-search fallthrough).
	// +optional
	Search *SearchSpec `json:"search,omitempty"`

	// Layout arranges ServiceEntry Groups into tabs, mirroring homepage's
	// settings.yaml `layout:` map (group -> style) with tabs added on top.
	// Omitted/empty renders every group flat with no tab UI, as before.
	// +optional
	Layout []LayoutTabSpec `json:"layout,omitempty"`

	// DisableCollapse disables the collapsible expand/collapse control on
	// service and bookmark group headers. Collapsing is enabled by default.
	// +optional
	DisableCollapse *bool `json:"disableCollapse,omitempty"`

	// GroupsInitiallyCollapsed collapses every group by default on first
	// load. A LayoutGroupSpec's own InitiallyCollapsed overrides this per
	// group. Ignored when DisableCollapse is set.
	// +optional
	GroupsInitiallyCollapsed *bool `json:"groupsInitiallyCollapsed,omitempty"`

	// UseEqualHeights makes every card in a group the same height. A
	// LayoutGroupSpec's own UseEqualHeights overrides this per group.
	// +optional
	UseEqualHeights *bool `json:"useEqualHeights,omitempty"`

	// BookmarksStyle renders every bookmark card icon-only (no name or
	// description), matching homepage's `bookmarksStyle: icons`.
	// +kubebuilder:validation:Enum=icons
	// +optional
	BookmarksStyle *string `json:"bookmarksStyle,omitempty"`

	// DisableIndexing asks search engines not to index the dashboard:
	// disallows all crawlers in robots.txt and adds a noindex meta tag.
	// +optional
	DisableIndexing *bool `json:"disableIndexing,omitempty"`

	// StartURL is the PWA manifest's start_url, used when the dashboard is
	// installed as an app. Defaults to "/".
	// +kubebuilder:validation:Pattern=`^(https?://|/)`
	// +optional
	StartURL *string `json:"startUrl,omitempty"`

	// CustomCSS is raw CSS injected into the dashboard's page in a second
	// <style> block appended after the built-in stylesheet, so its rules
	// can override it. Trusted, operator-supplied content — the same trust
	// level as every other Configuration field (e.g. Background.Image).
	// +kubebuilder:validation:MaxLength=10000
	// +optional
	CustomCSS *string `json:"customCSS,omitempty"`
}

// ConfigurationStatus defines the observed state of Configuration.
type ConfigurationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the Configuration resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pcfg
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Available')].status"
// +kubebuilder:printcolumn:name="Instance",type=string,JSONPath=".spec.instanceRef.name"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// Configuration is the Schema for the configurations API
type Configuration struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Configuration
	// +required
	Spec ConfigurationSpec `json:"spec"`

	// status defines the observed state of Configuration
	// +optional
	Status ConfigurationStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ConfigurationList contains a list of Configuration
type ConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Configuration `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Configuration{}, &ConfigurationList{})
		return nil
	})
}

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Enum values for LayoutGroupSpec.Header/InitiallyCollapsed/UseEqualHeights
// and ConfigurationSpec's site-wide equivalents (FullWidth/DisableCollapse/
// GroupsInitiallyCollapsed/UseEqualHeights/DisableIndexing).
const (
	HeaderShown  = "Shown"
	HeaderHidden = "Hidden"

	CollapseCollapsed = "Collapsed"
	CollapseExpanded  = "Expanded"

	HeightsEqual = "Equal"
	HeightsAuto  = "Auto"

	FullWidthFull      = "Full"
	FullWidthContained = "Contained"

	IndexingIndexed = "Indexed"
	IndexingNoIndex = "NoIndex"
)

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
	// +kubebuilder:validation:MinLength=0
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
	// +kubebuilder:validation:Enum=Enabled;Disabled
	// +default="Enabled"
	// +optional
	FilterCards *string `json:"filterCards,omitempty"`
}

// LayoutGroupSpec configures one Group's placement and style within a
// LayoutTabSpec, mirroring homepage's settings.yaml `layout:` per-group
// style keys (style/columns/icon).
// See https://gethomepage.dev/configs/settings/#layout
type LayoutGroupSpec struct {
	// name is the ServiceEntry Group this entry places and styles.
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

	// style lays the group's cards out in a single row instead of a grid.
	// +kubebuilder:validation:Enum=row;column
	// +optional
	Style *string `json:"style,omitempty"`

	// icon overrides the group header's icon. Same resolution rules as
	// ServiceEntry/Bookmark Icon: a full URL passes through, anything else
	// is resolved as a dashboard-icons slug.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Icon *string `json:"icon,omitempty"`

	// header renders this group's header (name + icon) when "Shown" (the
	// default). Set "Hidden" to hide it while still rendering the group's
	// cards.
	// +kubebuilder:validation:Enum=Shown;Hidden
	// +optional
	Header *string `json:"header,omitempty"`

	// initiallyCollapsed collapses this group by default on first load,
	// overriding the Configuration's GroupsInitiallyCollapsed. Ignored when
	// DisableCollapse is set.
	// +kubebuilder:validation:Enum=Collapsed;Expanded
	// +optional
	InitiallyCollapsed *string `json:"initiallyCollapsed,omitempty"`

	// useEqualHeights makes every card in this group the same height,
	// overriding the Configuration's UseEqualHeights.
	// +kubebuilder:validation:Enum=Equal;Auto
	// +optional
	UseEqualHeights *string `json:"useEqualHeights,omitempty"`
}

// LayoutTabSpec is one tab: a named, ordered set of Groups shown together.
// Groups not referenced by any tab still render, appended to an implicit
// trailing "Other" tab; a Group referenced by more than one tab is shown
// under the first tab that lists it.
type LayoutTabSpec struct {
	// name is the tab's label.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +required
	Name string `json:"name"`

	// groups lists, in display order, the Groups shown under this tab.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	// +required
	Groups []LayoutGroupSpec `json:"groups"`
}

// ConfigurationSpec defines the desired state of Configuration: the native
// dashboard's theme/color/background/header-style look and its header search
// box, applied by internal/dashboard's LoadSite.
type ConfigurationSpec struct {
	// instanceRef names the Instance this Configuration applies to.
	// +required
	InstanceRef InstanceRef `json:"instanceRef"`

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
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	Favicon *string `json:"favicon,omitempty"`

	// cardBlur applies a backdrop blur to cards, a Tailwind backdrop-blur
	// size keyword (e.g. "sm", "md", "xl", ""). An explicit "" is equivalent
	// to leaving it unset (the default 8px blur). Most visible over a
	// background image.
	// +kubebuilder:validation:MinLength=0
	// +kubebuilder:validation:MaxLength=8
	// +optional
	CardBlur *string `json:"cardBlur,omitempty"`

	// target is the default link target for service and bookmark cards.
	// Individual ServiceEntries may override it via their own target.
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

	// headerStyle for service/bookmark group headers.
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
	// +kubebuilder:validation:Enum=Full;Contained
	// +optional
	FullWidth *string `json:"fullWidth,omitempty"`

	// search configures the native dashboard's header search box (card
	// filtering + web-search fallthrough).
	// +optional
	Search *SearchSpec `json:"search,omitempty"`

	// layout arranges ServiceEntry Groups into tabs, mirroring homepage's
	// settings.yaml `layout:` map (group -> style) with tabs added on top.
	// Omitted/empty renders every group flat with no tab UI, as before.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +listType=atomic
	// +optional
	Layout []LayoutTabSpec `json:"layout,omitempty"`

	// disableCollapse disables the collapsible expand/collapse control on
	// service and bookmark group headers. Collapsing is enabled by default.
	// +kubebuilder:validation:Enum=Enabled;Disabled
	// +optional
	DisableCollapse *string `json:"disableCollapse,omitempty"`

	// groupsInitiallyCollapsed collapses every group by default on first
	// load. A LayoutGroupSpec's own InitiallyCollapsed overrides this per
	// group. Ignored when DisableCollapse is set.
	// +kubebuilder:validation:Enum=Collapsed;Expanded
	// +optional
	GroupsInitiallyCollapsed *string `json:"groupsInitiallyCollapsed,omitempty"`

	// useEqualHeights makes every card in a group the same height. A
	// LayoutGroupSpec's own UseEqualHeights overrides this per group.
	// +kubebuilder:validation:Enum=Equal;Auto
	// +optional
	UseEqualHeights *string `json:"useEqualHeights,omitempty"`

	// bookmarksStyle renders every bookmark card icon-only (no name or
	// description), matching homepage's `bookmarksStyle: icons`.
	// +kubebuilder:validation:Enum=icons
	// +optional
	BookmarksStyle *string `json:"bookmarksStyle,omitempty"`

	// disableIndexing asks search engines not to index the dashboard:
	// disallows all crawlers in robots.txt and adds a noindex meta tag.
	// +kubebuilder:validation:Enum=Indexed;NoIndex
	// +optional
	DisableIndexing *string `json:"disableIndexing,omitempty"`

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
	// level as every other Configuration field (e.g. Background.Image).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=10000
	// +optional
	CustomCSS *string `json:"customCSS,omitempty"`
}

// ConfigurationStatus defines the observed state of Configuration.
// +kubebuilder:validation:MinProperties=1
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
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
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

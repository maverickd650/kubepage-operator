package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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
	// is appended as a URL-encoded "q" parameter.
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

// ConfigurationSpec defines the desired state of Configuration, rendered into
// the target Instance's settings.yaml. Common, frequently-set options are
// typed below; anything else supported by homepage's settings.yaml can be
// supplied via Extra (see its doc comment) rather than waiting for it to be
// added here.
type ConfigurationSpec struct {
	// InstanceRef names the Instance this Configuration applies to.
	// +required
	InstanceRef InstanceRef `json:"instanceRef"`

	// Customise the page title.
	// +optional
	Title *string `json:"title,omitempty"`

	// Customise the page description.
	// +optional
	Description *string `json:"description,omitempty"`

	// Customise the start url if required. Default is "/".
	// +optional
	// +kubebuilder:default="/"
	StartUrl *string `json:"startUrl,omitempty"`

	// Background image and filters, used instead of the solid theme color.
	// +optional
	Background *BackgroundSpec `json:"background,omitempty"`

	// Apply a blur filter to the service and bookmark cards. A Tailwind
	// backdrop-blur size keyword (e.g. "xs", "md"). Incompatible with the
	// background blur/saturate/brightness filters.
	// +optional
	CardBlur *string `json:"cardBlur,omitempty"`

	// Specify a custom favicon instead of the included one; a full URL or a
	// path relative to /app/public.
	// +optional
	Favicon *string `json:"favicon,omitempty"`

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

	// Default link target for service/bookmark hrefs.
	// +kubebuilder:validation:Enum=_blank;_self;_top
	// +optional
	Target *string `json:"target,omitempty"`

	// Use the entire window width instead of a centered, constrained layout.
	// +optional
	FullWidth *bool `json:"fullWidth,omitempty"`

	// Hide the homepage release version shown at the bottom of the page.
	// +optional
	HideVersion *bool `json:"hideVersion,omitempty"`

	// Search configures the native dashboard's header search box (card
	// filtering + web-search fallthrough). Has no effect on the
	// homepage-wrapper render path.
	// +optional
	Search *SearchSpec `json:"search,omitempty"`

	// Extra carries any settings.yaml keys not modeled as typed fields above
	// (e.g. providers, pwa, quicklaunch, layout, blockHighlights). Keys here
	// are merged into the rendered settings.yaml; a key also set by a typed
	// field above is overridden by the typed field's value.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Extra *apiextensionsv1.JSON `json:"extra,omitempty"`
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

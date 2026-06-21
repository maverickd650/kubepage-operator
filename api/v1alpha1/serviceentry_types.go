package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ServiceWidget configures a homepage service-card widget. Type and URL are
// typed because nearly every widget has them; everything else (fields,
// headers, highlight, valueOnly, and the long tail of widget-specific knobs)
// goes in Config. Secret-bearing fields (e.g. "key") go in Secrets instead of
// Config: the operator resolves them into a mounted-file placeholder rather
// than storing the value inline.
type ServiceWidget struct {
	// Widget type, e.g. "sonarr", "uptimekuma". See homepage's widget docs
	// for the full list.
	// +kubebuilder:validation:MinLength=1
	// +required
	Type string `json:"type"`

	// Base URL the widget talks to.
	// +optional
	URL *string `json:"url,omitempty"`

	// Secret-bearing widget fields (commonly "key", sometimes "username"/
	// "password"/"token" depending on widget type), resolved via a mounted
	// Secret file rather than stored inline.
	// +optional
	Secrets map[string]SecretValueSource `json:"secrets,omitempty"`

	// Remaining widget fields not covered above (fields, headers, highlight,
	// valueOnly, and any widget-type-specific options).
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Config *apiextensionsv1.JSON `json:"config,omitempty"`
}

// ServiceEntrySpec defines one service card rendered into homepage's
// services.yaml, in the group named by Group.
//
// Nested groups (a group inside another group) are not supported in this
// version of the operator; Group always names a top-level group.
type ServiceEntrySpec struct {
	// InstanceRef names the Instance this ServiceEntry belongs to.
	// +required
	InstanceRef InstanceRef `json:"instanceRef"`

	// Group is the name of the (top-level) services.yaml group this entry
	// belongs to. Entries sharing a Group are rendered together.
	// +kubebuilder:validation:MinLength=1
	// +required
	Group string `json:"group"`

	// Name is the service's display name (the card title).
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// Order controls rendering position: groups and entries are sorted by
	// Order (nil sorts last), ties broken by Name, since CRDs have no
	// inherent ordering but services.yaml's groups/entries are an ordered
	// list. Purely an operator-side rendering concern; not a homepage field.
	// +optional
	Order *int32 `json:"order,omitempty"`

	// +optional
	Href *string `json:"href,omitempty"`

	// +optional
	Icon *string `json:"icon,omitempty"`

	// +optional
	Description *string `json:"description,omitempty"`

	// Ping monitors host availability via ICMP ping.
	// +optional
	Ping *string `json:"ping,omitempty"`

	// SiteMonitor checks a URL's availability via HTTP HEAD (falling back to GET).
	// +optional
	SiteMonitor *string `json:"siteMonitor,omitempty"`

	// Target overrides the link target for this service's href.
	// +kubebuilder:validation:Enum=_blank;_self;_top
	// +optional
	Target *string `json:"target,omitempty"`

	// StatusStyle overrides the global statusStyle for this service's
	// docker/k8s status, site monitor, and ping indicators.
	// +kubebuilder:validation:Enum=dot;basic
	// +optional
	StatusStyle *string `json:"statusStyle,omitempty"`

	// +optional
	ShowStats *bool `json:"showStats,omitempty"`

	// +optional
	HideErrors *bool `json:"hideErrors,omitempty"`

	// Widgets attached to this service. Zero, one, or many are allowed;
	// rendered as homepage's singular widget: key for exactly one, or
	// widgets: for more than one.
	// +optional
	Widgets []ServiceWidget `json:"widgets,omitempty"`
}

// ServiceEntryStatus defines the observed state of ServiceEntry.
type ServiceEntryStatus struct {
	// conditions represent the current state of the ServiceEntry resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=psvc

// ServiceEntry is the Schema for the serviceentries API
type ServiceEntry struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ServiceEntry
	// +required
	Spec ServiceEntrySpec `json:"spec"`

	// status defines the observed state of ServiceEntry
	// +optional
	Status ServiceEntryStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ServiceEntryList contains a list of ServiceEntry
type ServiceEntryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ServiceEntry `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ServiceEntry{}, &ServiceEntryList{})
		return nil
	})
}

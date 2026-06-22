package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ServiceWidget configures one of the native dashboard's pollable widgets
// (internal/dashboard's Widget interface) for a service card. Type and URL
// are typed because nearly every widget has them; everything else
// (widget-type-specific options) goes in Config. Secret-bearing fields (e.g.
// API tokens) go in Secrets instead of Config: the dashboard resolves them
// directly in-process at poll time, so the plaintext value never appears in
// pod env, a ConfigMap, or a projected file.
type ServiceWidget struct {
	// Widget type, e.g. "plex", "grafana", "unifi". See internal/dashboard
	// for the registered set.
	// +kubebuilder:validation:MinLength=1
	// +required
	Type string `json:"type"`

	// Base URL the widget talks to.
	// +optional
	URL *string `json:"url,omitempty"`

	// Secret-bearing widget fields (commonly "token", sometimes "username"/
	// "password" depending on widget type), resolved directly by the
	// dashboard backend rather than stored inline.
	// +optional
	Secrets map[string]SecretValueSource `json:"secrets,omitempty"`

	// Remaining widget-type-specific options (e.g. PrometheusMetric's
	// "query", Cloudflared's "accountId"/"tunnelId").
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Config *apiextensionsv1.JSON `json:"config,omitempty"`
}

// ServiceEntrySpec defines one service card rendered by the native
// dashboard, in the group named by Group.
//
// Nested groups (a group inside another group) are not supported in this
// version of the operator; Group always names a top-level group.
type ServiceEntrySpec struct {
	// InstanceRef names the Instance this ServiceEntry belongs to.
	// +required
	InstanceRef InstanceRef `json:"instanceRef"`

	// Group is the name of the (top-level) group this entry belongs to.
	// Entries sharing a Group are rendered together.
	// +kubebuilder:validation:MinLength=1
	// +required
	Group string `json:"group"`

	// Name is the service's display name (the card title).
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// Order controls rendering position: groups and entries are sorted by
	// Order (nil sorts last), ties broken by Name, since CRDs have no
	// inherent ordering but the dashboard's groups/entries are displayed in a
	// fixed order.
	// +optional
	Order *int32 `json:"order,omitempty"`

	// Href makes the card's title a link to the service.
	// +optional
	Href *string `json:"href,omitempty"`

	// +optional
	Icon *string `json:"icon,omitempty"`

	// +optional
	Description *string `json:"description,omitempty"`

	// Widgets attached to this service. Zero, one, or many are allowed; the
	// dashboard polls each one independently and shows its fields on the
	// card.
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
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Available')].status"
// +kubebuilder:printcolumn:name="Instance",type=string,JSONPath=".spec.instanceRef.name"
// +kubebuilder:printcolumn:name="Group",type=string,JSONPath=".spec.group"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

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

package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// InfoWidgetSpec defines one header/info widget rendered into homepage's
// widgets.yaml (resources, search, datetime, openmeteo, kubernetes, ...).
// Unlike ServiceEntry/Bookmark, widgets.yaml has no group concept — it's a
// flat, ordered list — so there is no Group field here.
type InfoWidgetSpec struct {
	// InstanceRef names the Instance this InfoWidget belongs to.
	// +required
	InstanceRef InstanceRef `json:"instanceRef"`

	// Type is the widget type, e.g. "resources", "search", "datetime",
	// "openmeteo", "kubernetes". See homepage's info-widget docs for the full
	// list.
	// +kubebuilder:validation:MinLength=1
	// +required
	Type string `json:"type"`

	// Order controls rendering position: widgets are sorted by Order (nil
	// sorts last), ties broken by the InfoWidget object's name, since CRDs
	// have no inherent ordering but widgets.yaml's list is ordered. Purely an
	// operator-side rendering concern; not a homepage field.
	// +optional
	Order *int32 `json:"order,omitempty"`

	// Secret-bearing option fields, resolved via a mounted Secret file rather
	// than stored inline. Merged into Options at render time under the same
	// field names.
	// +optional
	Secrets map[string]SecretValueSource `json:"secrets,omitempty"`

	// Options holds every widget-type-specific field (e.g. the kubernetes
	// widget's cluster/nodes blocks, the search widget's provider/target).
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Options *apiextensionsv1.JSON `json:"options,omitempty"`
}

// InfoWidgetStatus defines the observed state of InfoWidget.
type InfoWidgetStatus struct {
	// conditions represent the current state of the InfoWidget resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=piw

// InfoWidget is the Schema for the infowidgets API
type InfoWidget struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of InfoWidget
	// +required
	Spec InfoWidgetSpec `json:"spec"`

	// status defines the observed state of InfoWidget
	// +optional
	Status InfoWidgetStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// InfoWidgetList contains a list of InfoWidget
type InfoWidgetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []InfoWidget `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &InfoWidget{}, &InfoWidgetList{})
		return nil
	})
}

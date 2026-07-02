package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// InfoWidgetSpec defines one header/info widget, rendered by the native
// dashboard in the header strip above the service cards. Supported types:
// "datetime" (client-side clock; Options.format), "greeting" (static text;
// Options.text), "openmeteo" (current weather; Options.latitude/longitude/
// units), and "kubemetrics" (cluster-wide CPU/memory usage from
// metrics-server; optional Options.cpuLabel/memoryLabel). Has no Group field
// since header widgets are a flat, ordered list rather than grouped like
// ServiceEntry/Bookmark.
type InfoWidgetSpec struct {
	// instanceRef names the Instance this InfoWidget belongs to.
	// +required
	InstanceRef InstanceRef `json:"instanceRef"`

	// type is the widget type, e.g. "resources", "search", "datetime".
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +required
	Type string `json:"type"`

	// order controls rendering position: widgets are sorted by Order (nil
	// sorts last), ties broken by the InfoWidget object's name, since CRDs
	// have no inherent ordering of their own.
	// +optional
	Order *int32 `json:"order,omitempty"`

	// icon shown to the left of this widget's value(s) in the header strip,
	// matching homepage's Resource component. Resolved the same way as
	// ServiceEntry/Bookmark Icon: a full URL passes through unchanged,
	// anything else is treated as a dashboard-icons slug. Ignored by the
	// "greeting" and "datetime" widget types, which homepage renders without
	// an icon.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Icon *string `json:"icon,omitempty"`

	// secrets are secret-bearing option fields. Merged into Options under the
	// same field names once a renderer for this CRD exists.
	//
	// RBAC note: the same caveat as ServiceEntry's widgets.secrets applies
	// here — see ServiceWidget.Secrets' doc comment. Anyone who can create
	// an InfoWidget in this namespace can read any Secret in it by
	// referencing it via secretKeyRef and pointing this widget's options at
	// a server they control.
	// +kubebuilder:validation:MinProperties=1
	// +optional
	Secrets map[string]SecretValueSource `json:"secrets,omitempty"`

	// options holds every widget-type-specific field.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Options *apiextensionsv1.JSON `json:"options,omitempty"`

	// pollIntervalSeconds overrides the dashboard's global --poll-interval
	// for this widget only; see ServiceWidget.PollIntervalSeconds. Ignored by
	// "datetime"/"greeting"/"logo", which aren't polled at all.
	// +kubebuilder:validation:Minimum=1
	// +optional
	PollIntervalSeconds *int32 `json:"pollIntervalSeconds,omitempty"`
}

// InfoWidgetStatus defines the observed state of InfoWidget.
// +kubebuilder:validation:MinProperties=1
type InfoWidgetStatus struct {
	// conditions represent the current state of the InfoWidget resource.
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=piw
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Available')].status"
// +kubebuilder:printcolumn:name="Instance",type=string,JSONPath=".spec.instanceRef.name"
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

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

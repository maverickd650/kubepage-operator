package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Enum values for InfoWidgetSpec.Align.
const (
	AlignLeft  = "Left"
	AlignRight = "Right"
)

// InfoWidgetSpec defines one header/info widget, rendered by the native
// dashboard in the header strip above the service cards. Supported types:
// "datetime" (client-side clock; Options.format), "greeting" (static text;
// Options.text), "openmeteo" (current weather; Options.latitude/longitude/
// units), and "kubemetrics" (cluster-wide CPU/memory usage from
// metrics-server; optional Options.cpuLabel/memoryLabel). Has no Group field
// since header widgets are a flat, ordered list rather than grouped like
// ServiceCard/Bookmark.
type InfoWidgetSpec struct {
	// dashboardRef names the Dashboard this InfoWidget belongs to.
	// +required
	DashboardRef DashboardRef `json:"dashboardRef"`

	// type is the widget type: "greeting"/"datetime" render statically
	// (internal/dashboard/server.go); the rest are polled header widgets
	// (openmeteo, openweathermap, glances, longhorn) or cluster-sourced
	// (kubemetrics). internal/controller/widget_type_policy_test.go asserts
	// this enum stays in sync with the internal/dashboard widget registry.
	// +kubebuilder:validation:Enum=greeting;datetime;logo;openmeteo;kubemetrics;glances;longhorn;openweathermap
	// +required
	Type string `json:"type"`

	// order controls rendering position: widgets are sorted by Order (nil
	// sorts last), ties broken by the InfoWidget object's name, since CRDs
	// have no inherent ordering of their own.
	// +optional
	Order *int32 `json:"order,omitempty"`

	// icon shown to the left of this widget's value(s) in the header strip,
	// matching homepage's Resource component. Resolved the same way as
	// ServiceCard/Bookmark Icon: a full URL passes through unchanged,
	// anything else is treated as a dashboard-icons slug. Ignored by the
	// "greeting" and "datetime" widget types, which homepage renders without
	// an icon.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Icon *string `json:"icon,omitempty"`

	// align places this widget in the header strip's left or right slot,
	// matching homepage's right-aligned info widgets. Unset defaults to
	// "Left" for "greeting"/"datetime" and "Right" for every other type
	// (homepage's own default layout: greeting/clock on the left, live
	// stats on the right).
	// +kubebuilder:validation:Enum=Left;Right
	// +optional
	Align *string `json:"align,omitempty"`

	// secrets are secret-bearing option fields. Merged into Options under the
	// same field names once a renderer for this CRD exists.
	//
	// RBAC note: the same caveat as ServiceCard's widgets.secrets applies
	// here — see ServiceWidget.Secrets' doc comment. Anyone who can create
	// an InfoWidget in this namespace can read any Secret in it by
	// referencing it via secretKeyRef and pointing this widget's options at
	// a server they control.
	// +kubebuilder:validation:MinProperties=1
	// +optional
	Secrets map[string]SecretValueSource `json:"secrets,omitempty"`

	// caCert optionally supplies a PEM-encoded CA certificate (or bundle)
	// used, in addition to the system trust store, to verify this widget's
	// upstream. See ServiceWidget.CACert's doc comment for the full
	// rationale.
	// +optional
	CACert *SecretValueSource `json:"caCert,omitempty"`

	// options holds every widget-type-specific field; unrecognized keys are
	// silently ignored rather than rejected (see internal/dashboard's
	// per-widget source for the authoritative shape). Known keys by type:
	//   - greeting: text (the message shown)
	//   - datetime: format (a JSON-encoded Intl.DateTimeFormat options
	//     object, e.g. {"dateStyle":"short","timeStyle":"short"}; defaults
	//     to medium/medium)
	//   - openmeteo, openweathermap: latitude, longitude (both required),
	//     units ("metric"/"imperial"), label
	//   - kubemetrics: cpuLabel, memoryLabel
	//   - glances: apiVersion ("3" or "4"; defaults to "4")
	// logo and longhorn take no options.
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
// +kubebuilder:printcolumn:name="Dashboard",type=string,JSONPath=".spec.dashboardRef.name"
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

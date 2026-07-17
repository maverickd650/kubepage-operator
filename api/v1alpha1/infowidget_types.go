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

// InfoWidgetEntry is one header widget's configuration. An InfoWidgetSpec
// holds a list of these in Widgets, one InfoWidget object per whole
// dashboard.
//
// Unlike BookmarkEntry/ServiceEntry there is no Group field: header widgets
// are a flat, ordered list rather than grouped like ServiceCard/Bookmark, so
// InfoWidgetSpec.Entries() has no shared-default pass to reconcile.
// +kubebuilder:validation:XValidation:rule="!(self.type in ['glances','longhorn']) || has(self.url)",message="url is required when type is \"glances\" or \"longhorn\""
type InfoWidgetEntry struct {
	// type is the widget type: "greeting"/"datetime" render statically
	// (internal/dashboard/server.go); the rest are polled header widgets
	// (openmeteo, openweathermap, glances, longhorn) or cluster-sourced
	// (kubemetrics). internal/controller/widget_type_policy_test.go asserts
	// this enum stays in sync with the internal/dashboard widget registry.
	// +kubebuilder:validation:Enum=greeting;datetime;logo;openmeteo;kubemetrics;glances;longhorn;openweathermap
	// +required
	Type string `json:"type"`

	// url is the base URL this widget polls, for widget types with an HTTP
	// upstream (glances, longhorn); required for those types.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	URL *string `json:"url,omitempty"`

	// order controls rendering position: widgets are sorted by Order (nil
	// sorts last), ties broken by the InfoWidget object's name then this
	// entry's index, since CRDs have no inherent ordering of their own.
	// +optional
	Order *int32 `json:"order,omitempty"`

	// icon shown to the left of this widget's value(s) in the header strip,
	// matching homepage's Resource component. Resolved the same way as
	// ServiceCard/Bookmark Icon: a full URL passes through unchanged,
	// "mdi-X"/"si-X"/"lucide-X"/"wi-X"/"fa6-solid-X"/"sh-X" resolve via
	// homepage's icon prefix syntax (see ServiceEntry.Icon), and anything
	// else is treated as a dashboard-icons slug. Ignored by the
	// "greeting" and "datetime" widget types, which homepage renders without
	// an icon. Every other polled type (openmeteo/openweathermap,
	// kubemetrics, glances, longhorn) already renders a sensible built-in
	// icon when this is unset — openmeteo/openweathermap's tracks the
	// current weather condition each poll — so this field only needs
	// setting to override that default.
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

	// secrets are secret-bearing option fields, resolved in-process at poll
	// time (internal/dashboard/poller.go's pollInfoWidget) into the widget's
	// WidgetConfig.Secrets map, keyed by the same field names used here —
	// distinct from Config, which never carries secret values.
	//
	// RBAC note: the same caveat as ServiceCard's widgets.secrets applies
	// here — see ServiceWidget.Secrets' doc comment. Anyone who can create
	// an InfoWidget in this namespace can read any Secret in it by
	// referencing it via secretKeyRef and pointing this widget's config at
	// a server they control.
	// +kubebuilder:validation:MinProperties=1
	// +kubebuilder:validation:MaxProperties=32
	// +optional
	Secrets map[string]SecretValueSource `json:"secrets,omitempty"`

	// secretRef names a single Secret, in the same namespace, every key of
	// which becomes a resolved secret field for this widget — see
	// ServiceWidget.SecretRef's doc comment for the full rationale and the
	// interaction with Secrets (Secrets always wins per key).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	SecretRef *string `json:"secretRef,omitempty"`

	// caCert optionally supplies a PEM-encoded CA certificate (or bundle)
	// used, in addition to the system trust store, to verify this widget's
	// upstream. See ServiceWidget.CACert's doc comment for the full
	// rationale.
	// +optional
	CACert *SecretValueSource `json:"caCert,omitempty"`

	// config holds every widget-type-specific field; unrecognized keys are
	// silently ignored rather than rejected (see internal/dashboard's
	// per-widget source for the authoritative shape). Known keys by type:
	//   - greeting: text (the message shown)
	//   - datetime: format (a JSON-encoded Intl.DateTimeFormat options
	//     object, e.g. {"dateStyle":"short","timeStyle":"short"}; defaults
	//     to medium date/short time, i.e. no seconds)
	//   - openmeteo, openweathermap: latitude, longitude (both required),
	//     units ("metric"/"imperial"), label
	//   - kubemetrics: cpuLabel, memoryLabel
	//   - glances: apiVersion ("3" or "4"; defaults to "4")
	//   - longhorn: no config keys; url above is required
	// glances and longhorn also require the typed url field above (Glances/
	// Longhorn Manager REST API base URL, e.g.
	// http://longhorn-frontend.longhorn-system:80). logo takes no config.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Config *apiextensionsv1.JSON `json:"config,omitempty"`

	// pollIntervalSeconds overrides the dashboard's global --poll-interval
	// for this widget only; see ServiceWidget.PollIntervalSeconds. Ignored by
	// "datetime"/"greeting"/"logo", which aren't polled at all.
	// +kubebuilder:validation:Minimum=1
	// +optional
	PollIntervalSeconds *int32 `json:"pollIntervalSeconds,omitempty"`
}

// InfoWidgetSpec defines one or more header/info widgets, rendered by the
// native dashboard in the header strip above the service cards. Supported
// types: "datetime" (client-side clock; Config.format), "greeting" (static
// text; Config.text), "openmeteo" (current weather; Config.latitude/
// longitude/units), and "kubemetrics" (cluster-wide CPU/memory usage from
// metrics-server; optional Config.cpuLabel/memoryLabel). Has no Group field
// since header widgets are a flat, ordered list rather than grouped like
// ServiceCard/Bookmark.
type InfoWidgetSpec struct {
	// dashboardRef names the Dashboard this InfoWidget belongs to. Optional:
	// if unset, this InfoWidget binds to the namespace's sole Dashboard: a
	// namespace with zero or more than one Dashboard leaves it unbound (see
	// api/v1alpha1.BoundTo).
	// +optional
	DashboardRef *DashboardRef `json:"dashboardRef,omitempty"`

	// widgets defines one entry per header widget, for an InfoWidget that
	// groups multiple header widgets (a whole dashboard's worth) into one
	// object instead of one InfoWidget per widget. Unlike
	// ServiceCard.Services/Bookmark.Bookmarks there is no shared-default
	// field here (no Group concept for header widgets). A header strip never
	// holds anywhere near 32 widgets, let alone more.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +listType=atomic
	// +required
	Widgets []InfoWidgetEntry `json:"widgets"`
}

// Entries returns the widget entries this InfoWidget defines: a copy of
// Widgets. There is no shared-default pass — header widgets have no Group
// concept.
func (s *InfoWidgetSpec) Entries() []InfoWidgetEntry {
	entries := make([]InfoWidgetEntry, len(s.Widgets))
	copy(entries, s.Widgets)
	return entries
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

	// entries is the number of entries this object defines (len(spec.widgets)).
	// +optional
	Entries int32 `json:"entries,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=piw
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Available')].status"
// +kubebuilder:printcolumn:name="Dashboard",type=string,JSONPath=".spec.dashboardRef.name"
// +kubebuilder:printcolumn:name="Entries",type=integer,JSONPath=".status.entries"
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

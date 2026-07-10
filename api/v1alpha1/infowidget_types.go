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

// InfoWidgetEntry is one header widget's configuration. InfoWidgetSpec
// duplicates InfoWidgetEntry's fields inline for the single-widget form (one
// InfoWidget object per widget, the original wire format), and holds a list
// of them in Widgets for the multi-widget form (one InfoWidget object per
// whole dashboard).
//
// Unlike BookmarkEntry/ServiceEntry there is no Group field: header widgets
// are a flat, ordered list rather than grouped like ServiceCard/Bookmark, so
// InfoWidgetSpec.Entries() has no shared-default pass to reconcile.
type InfoWidgetEntry struct {
	// type is the widget type: "greeting"/"datetime" render statically
	// (internal/dashboard/server.go); the rest are polled header widgets
	// (openmeteo, openweathermap, glances, longhorn) or cluster-sourced
	// (kubemetrics). internal/controller/widget_type_policy_test.go asserts
	// this enum stays in sync with the internal/dashboard widget registry.
	// +kubebuilder:validation:Enum=greeting;datetime;logo;openmeteo;kubemetrics;glances;longhorn;openweathermap
	// +optional
	Type string `json:"type,omitempty"`

	// order controls rendering position: widgets are sorted by Order (nil
	// sorts last), ties broken by the InfoWidget object's name then this
	// entry's index, since CRDs have no inherent ordering of their own.
	// +optional
	Order *int32 `json:"order,omitempty"`

	// icon shown to the left of this widget's value(s) in the header strip,
	// matching homepage's Resource component. Resolved the same way as
	// ServiceCard/Bookmark Icon: a full URL passes through unchanged,
	// anything else is treated as a dashboard-icons slug. Ignored by the
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
	//     to medium date/short time, i.e. no seconds)
	//   - openmeteo, openweathermap: latitude, longitude (both required),
	//     units ("metric"/"imperial"), label
	//   - kubemetrics: cpuLabel, memoryLabel
	//   - glances: url (required; Glances REST API base URL), apiVersion
	//     ("3" or "4"; defaults to "4")
	//   - longhorn: url (required; Longhorn Manager REST API base URL, e.g.
	//     http://longhorn-frontend.longhorn-system:80)
	// Unlike ServiceCard's widgets, an InfoWidget has no dedicated url field,
	// so any header widget type that polls an HTTP upstream takes its base
	// URL via this options block's "url" key instead.
	// logo takes no options.
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

// InfoWidgetSpec defines one header/info widget, rendered by the native
// dashboard in the header strip above the service cards. Supported types:
// "datetime" (client-side clock; Options.format), "greeting" (static text;
// Options.text), "openmeteo" (current weather; Options.latitude/longitude/
// units), and "kubemetrics" (cluster-wide CPU/memory usage from
// metrics-server; optional Options.cpuLabel/memoryLabel). Has no Group field
// since header widgets are a flat, ordered list rather than grouped like
// ServiceCard/Bookmark.
//
// Choose exactly one form: the inline single-widget form (set type and the
// other widget fields directly on spec, unchanged from earlier versions of
// this API), or the multi-widget form (set widgets, a list of one or more
// widget entries) — never both.
//
// The single-widget fields below duplicate InfoWidgetEntry's rather than
// embedding it, so that existing Go code building an InfoWidgetSpec{Type:
// ...} literal (the single-widget form predates Widgets) keeps compiling
// unchanged — an embedded field can't be set by name in a keyed struct
// literal of the embedding type. Entries() is the single place that
// reconciles the two representations. Unlike BookmarkSpec/ServiceCardSpec
// there is no shared-default field (no Group concept here), so Entries()
// for the multi-widget form is a straight passthrough of Widgets.
// +kubebuilder:validation:XValidation:rule="has(self.type) != has(self.widgets)",message="exactly one of type (single-widget form) or widgets (multi-widget form) must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.widgets) || self.widgets.all(w, has(w.type))",message="every widgets entry must set type"
// +kubebuilder:validation:XValidation:rule="!has(self.widgets) || (!has(self.order) && !has(self.icon) && !has(self.align) && !has(self.secrets) && !has(self.caCert) && !has(self.options) && !has(self.pollIntervalSeconds))",message="when widgets is set, the single-widget inline fields (order, icon, align, secrets, caCert, options, pollIntervalSeconds) must be absent"
type InfoWidgetSpec struct {
	// dashboardRef names the Dashboard this InfoWidget belongs to.
	// +required
	DashboardRef DashboardRef `json:"dashboardRef"`

	// type is the widget type: "greeting"/"datetime" render statically
	// (internal/dashboard/server.go); the rest are polled header widgets
	// (openmeteo, openweathermap, glances, longhorn) or cluster-sourced
	// (kubemetrics). internal/controller/widget_type_policy_test.go asserts
	// this enum stays in sync with the internal/dashboard widget registry.
	// Set only for the single-widget form; mutually exclusive with Widgets.
	// +kubebuilder:validation:Enum=greeting;datetime;logo;openmeteo;kubemetrics;glances;longhorn;openweathermap
	// +optional
	Type string `json:"type,omitempty"`

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
	//     to medium date/short time, i.e. no seconds)
	//   - openmeteo, openweathermap: latitude, longitude (both required),
	//     units ("metric"/"imperial"), label
	//   - kubemetrics: cpuLabel, memoryLabel
	//   - glances: url (required; Glances REST API base URL), apiVersion
	//     ("3" or "4"; defaults to "4")
	//   - longhorn: url (required; Longhorn Manager REST API base URL, e.g.
	//     http://longhorn-frontend.longhorn-system:80)
	// Unlike ServiceCard's widgets, an InfoWidget has no dedicated url field,
	// so any header widget type that polls an HTTP upstream takes its base
	// URL via this options block's "url" key instead.
	// logo takes no options.
	//
	// Type=object is set (in addition to PreserveUnknownFields) purely so the
	// mutual-exclusion CEL rule above can call has(self.options): a
	// preserve-unknown-fields field with no declared type isn't structural
	// enough for CEL to reference at all ("undefined field"), even just to
	// test presence.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	// +optional
	Options *apiextensionsv1.JSON `json:"options,omitempty"`

	// pollIntervalSeconds overrides the dashboard's global --poll-interval
	// for this widget only; see ServiceWidget.PollIntervalSeconds. Ignored by
	// "datetime"/"greeting"/"logo", which aren't polled at all.
	// +kubebuilder:validation:Minimum=1
	// +optional
	PollIntervalSeconds *int32 `json:"pollIntervalSeconds,omitempty"`

	// widgets defines one entry per header widget, for an InfoWidget that
	// groups multiple header widgets (a whole dashboard's worth) into one
	// object instead of one InfoWidget per widget. Mutually exclusive with
	// the inline single-widget fields above (type, order, icon, align,
	// secrets, caCert, options, pollIntervalSeconds). Unlike
	// ServiceCard.Services/Bookmark.Bookmarks there is no shared-default
	// field here (no Group concept for header widgets).
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=128
	// +listType=atomic
	// +optional
	Widgets []InfoWidgetEntry `json:"widgets,omitempty"`
}

// Entries returns the widget entries this InfoWidget defines, normalized to
// the multi-widget form: for the single-widget form it returns spec's own
// inline fields as a one-element slice; for the multi-widget form it returns
// a copy of Widgets as-is. Unlike BookmarkSpec/ServiceCardSpec's Entries,
// there is no shared-default pass — header widgets have no Group concept.
func (s *InfoWidgetSpec) Entries() []InfoWidgetEntry {
	if len(s.Widgets) == 0 {
		return []InfoWidgetEntry{{
			Type:                s.Type,
			Order:               s.Order,
			Icon:                s.Icon,
			Align:               s.Align,
			Secrets:             s.Secrets,
			CACert:              s.CACert,
			Options:             s.Options,
			PollIntervalSeconds: s.PollIntervalSeconds,
		}}
	}
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

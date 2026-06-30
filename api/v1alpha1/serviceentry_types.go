package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// HighlightRuleSpec is one rule in a FieldHighlight's evaluation list. Rules
// are evaluated in order; the first match sets the field's highlight level.
// Mirrors homepage's per-field highlight rule, see
// https://gethomepage.dev/configs/services/#block-highlighting.
type HighlightRuleSpec struct {
	// Level this rule sets when it matches.
	// +kubebuilder:validation:Enum=good;warn;danger
	// +required
	Level string `json:"level"`

	// When is the comparison operator. Numeric fields use gt/gte/lt/lte/eq/
	// ne/between/outside (Value/Value2 parsed as numbers); string fields use
	// equals/includes/startsWith/endsWith/regex (Value compared as text).
	// +kubebuilder:validation:Enum=gt;gte;lt;lte;eq;ne;between;outside;equals;includes;startsWith;endsWith;regex
	// +required
	When string `json:"when"`

	// Value is the rule's comparison value: a number for a numeric
	// operator, text for a string operator. For "between"/"outside" this is
	// the lower bound.
	// +kubebuilder:validation:MinLength=1
	// +required
	Value string `json:"value"`

	// Value2 is the upper bound for the "between"/"outside" operators;
	// ignored by every other operator.
	// +optional
	Value2 *string `json:"value2,omitempty"`

	// Negate inverts the rule's match.
	// +optional
	Negate *bool `json:"negate,omitempty"`

	// CaseSensitive makes a string operator's comparison case-sensitive.
	// Defaults to false (case-insensitive); ignored by numeric operators.
	// +optional
	CaseSensitive *bool `json:"caseSensitive,omitempty"`
}

// FieldHighlight configures highlight rules for one widget field, keyed by
// the field's label in ServiceWidget.Highlight.
type FieldHighlight struct {
	// Rules are evaluated in order; the first match sets the field's
	// highlight level. No match leaves the field unhighlighted.
	// +kubebuilder:validation:MinItems=1
	// +required
	Rules []HighlightRuleSpec `json:"rules"`

	// ValueOnly highlights only the field's value rather than its whole
	// stat chip (label included) when a rule matches.
	// +optional
	ValueOnly *bool `json:"valueOnly,omitempty"`
}

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
	// +kubebuilder:validation:Pattern=`^https?://`
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

	// Fields restricts which of the widget's returned fields are rendered,
	// by label (e.g. "queued", "wanted"). Unset (the default) renders every
	// field the widget returns.
	// +optional
	Fields []string `json:"fields,omitempty"`

	// Highlight tints a field's stat chip by severity (good/warn/danger),
	// keyed by the field's label. See FieldHighlight.
	// +optional
	Highlight map[string]FieldHighlight `json:"highlight,omitempty"`
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

	// Target overrides the Configuration's default link target for this
	// card's Href ("_blank" opens a new tab, "_self" the same tab).
	// +kubebuilder:validation:Enum=_blank;_self
	// +optional
	Target *string `json:"target,omitempty"`

	// +optional
	Icon *string `json:"icon,omitempty"`

	// +optional
	Description *string `json:"description,omitempty"`

	// ShowStats controls whether the polled widget fields are displayed on
	// the card. Defaults to true; set false to show only the title/icon/
	// description (and any monitor status).
	// +optional
	ShowStats *bool `json:"showStats,omitempty"`

	// HideErrors suppresses a widget's error text on the card (e.g. for a
	// service that is expected to be intermittently unreachable). Defaults
	// to false.
	// +optional
	HideErrors *bool `json:"hideErrors,omitempty"`

	// Ping is a URL probed over HTTP for reachability and latency, shown as
	// an up/down status on the card. (Raw ICMP is not used, so a pod needs
	// no elevated capabilities.)
	// +kubebuilder:validation:Pattern=`^https?://`
	// +optional
	Ping *string `json:"ping,omitempty"`

	// SiteMonitor is a URL probed over HTTP, shown as an up/down status with
	// response latency on the card.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +optional
	SiteMonitor *string `json:"siteMonitor,omitempty"`

	// PodSelector selects pods in this ServiceEntry's namespace whose
	// readiness determines this service's up/down status — a
	// Kubernetes-native alternative to Ping/SiteMonitor for services that
	// run as pods in this cluster, needing no externally reachable URL.
	// With multiple matches, any Ready pod renders "Up"; the card shows
	// "<ready>/<total> ready" in place of Ping/SiteMonitor's latency.
	// Mutually exclusive with Ping and SiteMonitor (enforced by the
	// serviceentry-monitor-source ValidatingAdmissionPolicy).
	// +optional
	PodSelector *metav1.LabelSelector `json:"podSelector,omitempty"`

	// StatusStyle controls how the Ping/SiteMonitor/PodSelector status
	// renders: "dot" a colored status dot, "basic" up/down text. Ignored
	// unless one of Ping, SiteMonitor, or PodSelector is set.
	// +kubebuilder:validation:Enum=dot;basic
	// +optional
	StatusStyle *string `json:"statusStyle,omitempty"`

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

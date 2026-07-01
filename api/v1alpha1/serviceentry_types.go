package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Enum values for HighlightRuleSpec.Negate/CaseSensitive,
// FieldHighlight.ValueOnly, and ServiceEntrySpec.ShowStats/HideErrors.
const (
	NegateMatch  = "Match"
	NegateNegate = "Negate"

	CaseSensitiveOn  = "CaseSensitive"
	CaseSensitiveOff = "CaseInsensitive"

	HighlightWholeField = "WholeField"
	HighlightValueOnly  = "ValueOnly"

	StatsShow = "Show"
	StatsHide = "Hide"
)

// HighlightRuleSpec is one rule in a FieldHighlight's evaluation list. Rules
// are evaluated in order; the first match sets the field's highlight level.
// Mirrors homepage's per-field highlight rule, see
// https://gethomepage.dev/configs/services/#block-highlighting.
type HighlightRuleSpec struct {
	// level this rule sets when it matches.
	// +kubebuilder:validation:Enum=good;warn;danger
	// +required
	Level string `json:"level"`

	// when is the comparison operator. Numeric fields use gt/gte/lt/lte/eq/
	// ne/between/outside (Value/Value2 parsed as numbers); string fields use
	// equals/includes/startsWith/endsWith/regex (Value compared as text).
	// +kubebuilder:validation:Enum=gt;gte;lt;lte;eq;ne;between;outside;equals;includes;startsWith;endsWith;regex
	// +required
	When string `json:"when"`

	// value is the rule's comparison value: a number for a numeric
	// operator, text for a string operator. For "between"/"outside" this is
	// the lower bound.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +required
	Value string `json:"value"`

	// value2 is the upper bound for the "between"/"outside" operators;
	// ignored by every other operator.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Value2 *string `json:"value2,omitempty"`

	// negate inverts the rule's match ("Negate") instead of the default
	// ("Match").
	// +kubebuilder:validation:Enum=Match;Negate
	// +optional
	Negate *string `json:"negate,omitempty"`

	// caseSensitive makes a string operator's comparison case-sensitive.
	// Defaults to "CaseInsensitive"; ignored by numeric operators.
	// +kubebuilder:validation:Enum=CaseSensitive;CaseInsensitive
	// +optional
	CaseSensitive *string `json:"caseSensitive,omitempty"`
}

// FieldHighlight configures highlight rules for one widget field, keyed by
// the field's label in ServiceWidget.Highlight.
type FieldHighlight struct {
	// rules are evaluated in order; the first match sets the field's
	// highlight level. No match leaves the field unhighlighted.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +listType=atomic
	// +required
	Rules []HighlightRuleSpec `json:"rules"`

	// valueOnly highlights only the field's value ("ValueOnly") rather than
	// its whole stat chip (label included, the default "WholeField") when a
	// rule matches.
	// +kubebuilder:validation:Enum=WholeField;ValueOnly
	// +optional
	ValueOnly *string `json:"valueOnly,omitempty"`
}

// ServiceWidget configures one of the native dashboard's pollable widgets
// (internal/dashboard's Widget interface) for a service card. Type and URL
// are typed because nearly every widget has them; everything else
// (widget-type-specific options) goes in Config. Secret-bearing fields (e.g.
// API tokens) go in Secrets instead of Config: the dashboard resolves them
// directly in-process at poll time, so the plaintext value never appears in
// pod env, a ConfigMap, or a projected file.
type ServiceWidget struct {
	// type is the widget type, e.g. "plex", "grafana", "unifi". See
	// internal/dashboard for the registered set.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +required
	Type string `json:"type"`

	// url is the base URL the widget talks to.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	URL *string `json:"url,omitempty"`

	// secrets are secret-bearing widget fields (commonly "token", sometimes
	// "username"/"password" depending on widget type), resolved directly by
	// the dashboard backend rather than stored inline.
	// +kubebuilder:validation:MinProperties=1
	// +optional
	Secrets map[string]SecretValueSource `json:"secrets,omitempty"`

	// config holds the remaining widget-type-specific options (e.g.
	// PrometheusMetric's "query", Cloudflared's "accountId"/"tunnelId").
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Config *apiextensionsv1.JSON `json:"config,omitempty"`

	// fields restricts which of the widget's returned fields are rendered,
	// by label (e.g. "queued", "wanted"). Unset (the default) renders every
	// field the widget returns.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=64
	// +listType=set
	// +optional
	Fields []string `json:"fields,omitempty"`

	// highlight tints a field's stat chip by severity (good/warn/danger),
	// keyed by the field's label. See FieldHighlight.
	// +kubebuilder:validation:MinProperties=1
	// +optional
	Highlight map[string]FieldHighlight `json:"highlight,omitempty"`
}

// ServiceEntrySpec defines one service card rendered by the native
// dashboard, in the group named by Group.
//
// Nested groups (a group inside another group) are not supported in this
// version of the operator; Group always names a top-level group.
type ServiceEntrySpec struct {
	// instanceRef names the Instance this ServiceEntry belongs to.
	// +required
	InstanceRef InstanceRef `json:"instanceRef"`

	// group is the name of the (top-level) group this entry belongs to.
	// Entries sharing a Group are rendered together.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +required
	Group string `json:"group"`

	// name is the service's display name (the card title).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +required
	Name string `json:"name"`

	// order controls rendering position: groups and entries are sorted by
	// Order (nil sorts last), ties broken by Name, since CRDs have no
	// inherent ordering but the dashboard's groups/entries are displayed in a
	// fixed order.
	// +optional
	Order *int32 `json:"order,omitempty"`

	// href makes the card's title a link to the service.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	Href *string `json:"href,omitempty"`

	// target overrides the Configuration's default link target for this
	// card's Href ("_blank" opens a new tab, "_self" the same tab).
	// +kubebuilder:validation:Enum=_blank;_self
	// +optional
	Target *string `json:"target,omitempty"`

	// icon resolves as a dashboard-icons slug, or passes through as-is if
	// it's already a full URL.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Icon *string `json:"icon,omitempty"`

	// description overrides the default description shown on the card.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	Description *string `json:"description,omitempty"`

	// showStats controls whether the polled widget fields are displayed on
	// the card. Defaults to "Show"; set "Hide" to show only the title/icon/
	// description (and any monitor status).
	// +kubebuilder:validation:Enum=Show;Hide
	// +optional
	ShowStats *string `json:"showStats,omitempty"`

	// hideErrors suppresses a widget's error text on the card (e.g. for a
	// service that is expected to be intermittently unreachable). Defaults
	// to "Show".
	// +kubebuilder:validation:Enum=Show;Hide
	// +optional
	HideErrors *string `json:"hideErrors,omitempty"`

	// ping is a URL probed over HTTP for reachability and latency, shown as
	// an up/down status on the card. (Raw ICMP is not used, so a pod needs
	// no elevated capabilities.)
	// +kubebuilder:validation:Pattern=`^https?://`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	Ping *string `json:"ping,omitempty"`

	// siteMonitor is a URL probed over HTTP, shown as an up/down status with
	// response latency on the card.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	SiteMonitor *string `json:"siteMonitor,omitempty"`

	// podSelector selects pods in this ServiceEntry's namespace whose
	// readiness determines this service's up/down status — a
	// Kubernetes-native alternative to Ping/SiteMonitor for services that
	// run as pods in this cluster, needing no externally reachable URL.
	// With multiple matches, any Ready pod renders "Up"; the card shows
	// "<ready>/<total> ready" in place of Ping/SiteMonitor's latency.
	// Mutually exclusive with Ping and SiteMonitor (enforced by the
	// serviceentry-monitor-source ValidatingAdmissionPolicy).
	// +optional
	PodSelector *metav1.LabelSelector `json:"podSelector,omitempty"`

	// statusStyle controls how the Ping/SiteMonitor/PodSelector status
	// renders: "dot" a colored status dot, "basic" up/down text. Ignored
	// unless one of Ping, SiteMonitor, or PodSelector is set.
	// +kubebuilder:validation:Enum=dot;basic
	// +optional
	StatusStyle *string `json:"statusStyle,omitempty"`

	// widgets attached to this service. Zero, one, or many are allowed; the
	// dashboard polls each one independently and shows its fields on the
	// card.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	// +listType=atomic
	// +optional
	Widgets []ServiceWidget `json:"widgets,omitempty"`
}

// ServiceEntryStatus defines the observed state of ServiceEntry.
// +kubebuilder:validation:MinProperties=1
type ServiceEntryStatus struct {
	// conditions represent the current state of the ServiceEntry resource.
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
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

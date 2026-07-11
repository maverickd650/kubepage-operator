package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Enum values for HighlightRuleSpec.Negate/CaseSensitive,
// FieldHighlight.Scope, and ServiceCardSpec.ShowStats.
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

// Enum values for ServiceCardSpec.ErrorDisplay and
// DashboardStyleSpec.ErrorDisplay.
const (
	ErrorDisplayShown  = "Shown"
	ErrorDisplayHidden = "Hidden"
)

// HighlightRuleSpec is one rule in a FieldHighlight's evaluation list. Rules
// are evaluated in order; the first match sets the field's highlight level.
// Mirrors homepage's per-field highlight rule, see
// https://gethomepage.dev/configs/services/#block-highlighting.
// +kubebuilder:validation:XValidation:rule="!(self.when in ['between','outside']) || has(self.value2)",message="value2 is required when when is \"between\" or \"outside\""
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

	// scope controls how much of the stat chip a matching rule highlights:
	// "WholeField" (the default) highlights the whole chip, label included;
	// "ValueOnly" highlights just the value.
	// +kubebuilder:validation:Enum=WholeField;ValueOnly
	// +optional
	Scope *string `json:"scope,omitempty"`
}

// ServiceWidget configures one of the native dashboard's pollable widgets
// (internal/dashboard's Widget interface) for a service card. Type and URL
// are typed because nearly every widget has them; everything else
// (widget-type-specific options) goes in Config. Secret-bearing fields (e.g.
// API tokens) go in Secrets instead of Config: the dashboard resolves them
// directly in-process at poll time, so the plaintext value never appears in
// pod env, a ConfigMap, or a projected file.
type ServiceWidget struct {
	// type is the widget type. Must be one of the service widget types
	// registered in internal/dashboard (each widget's init() Register call);
	// internal/controller/widget_type_policy_test.go asserts this enum stays
	// in sync with the registry.
	// +kubebuilder:validation:Enum=plex;stash;paperlessngx;grafana;prometheus;prometheusmetric;unifi;truenas;cloudflared;linkwarden;homeassistant;mealie;customapi;iframe;sonarr;radarr;jellyfin;jellyseerr;immich;adguard;pihole;uptime-kuma;portainer;argocd;gitea;tautulli
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
	//
	// RBAC note: creating a ServiceCard that references a secretKeyRef
	// grants no RBAC to the creator, but it does let the dashboard pod read
	// that Secret and send its plaintext value to this widget's own url —
	// so anyone who can create a ServiceCard in this namespace can read any
	// Secret in it by pointing url at a server they control, without ever
	// needing "get secrets" themselves. Only grant ServiceCard create
	// access to principals you'd also trust with every Secret in the
	// namespace.
	// +kubebuilder:validation:MinProperties=1
	// +optional
	Secrets map[string]SecretValueSource `json:"secrets,omitempty"`

	// caCert optionally supplies a PEM-encoded CA certificate (or bundle)
	// used, in addition to the system trust store, to verify this widget's
	// url. An alternative to Config's per-widget "insecureTLS" escape hatch
	// (e.g. unifi.go) for self-hosted upstreams with a private CA: this lets
	// the connection stay verified instead of skipping verification
	// entirely. Resolved the same way as Secrets (never stored outside this
	// object beyond the dashboard pod's in-memory TLS config).
	// +optional
	CACert *SecretValueSource `json:"caCert,omitempty"`

	// config holds the remaining widget-type-specific options; unrecognized
	// keys are silently ignored rather than rejected (see internal/dashboard's
	// per-widget source for the authoritative shape). Known keys by type:
	//   - cloudflared: accountId, tunnelId (both required)
	//   - customapi: mappings (required) — a list of {label, jsonpath, suffix}
	//   - prometheusmetric: query (required), label (defaults to "Value")
	//   - unifi: site (defaults to "default"), insecureTLS (bool)
	//   - iframe: height (a CSS length, e.g. "300px"; defaults to "300px")
	//   - glances: apiVersion ("3" or "4"; defaults to "4")
	// Every other registered type takes no Config options.
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

	// pollIntervalSeconds overrides the dashboard's global --poll-interval
	// for this widget only, letting a slow upstream (e.g. weather) poll less
	// often than a fast one (e.g. Prometheus) without slowing every other
	// widget down to match. Unset polls at the global interval every cycle.
	// Floor-clamped to the global interval: a value smaller than it would
	// have no effect anyway, since the poller only ever runs once per cycle.
	// +kubebuilder:validation:Minimum=1
	// +optional
	PollIntervalSeconds *int32 `json:"pollIntervalSeconds,omitempty"`
}

// ServiceEntry is one service card's configuration: display fields, monitor
// source, and widgets. A ServiceCardSpec holds a list of these in Services,
// one ServiceCard object per group, or per whole dashboard.
//
// Nested groups (a group inside another group) are not supported in this
// version of the operator; Group always names a top-level group.
// +kubebuilder:validation:XValidation:rule="(has(self.ping) ? 1 : 0) + (has(self.siteMonitor) ? 1 : 0) + (has(self.podSelector) ? 1 : 0) <= 1",message="at most one of ping, siteMonitor, or podSelector may be set"
type ServiceEntry struct {
	// group is the name of the (top-level) group this entry belongs to.
	// Entries sharing a Group are rendered together. An entry that omits
	// group falls back to ServiceCardSpec.Group as a shared default (see
	// ServiceCardSpec.Entries).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Group string `json:"group,omitempty"`

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
	// +kubebuilder:validation:Pattern=`^https?://`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	Href *string `json:"href,omitempty"`

	// target overrides the DashboardStyle's default link target for this
	// card's Href ("_blank" opens a new tab, "_self" the same tab).
	// +kubebuilder:validation:Enum=_blank;_self
	// +optional
	Target *string `json:"target,omitempty"`

	// icon resolves as a dashboard-icons slug (e.g. "grafana"), passes
	// through as-is if it's already a full URL, or accepts homepage's icon
	// prefix syntax: "mdi-X"/"si-X"/"lucide-X"/"wi-X"/"fa6-solid-X" for a
	// generic icon glyph (Material Design Icons/Simple Icons/Lucide/Weather
	// Icons/Font Awesome 6 Solid, resolved via Iconify — X may end in
	// "-#hexcolor" to recolor it), or "sh-X" for a selfh.st/icons glyph (X
	// may end in .svg/.png/.webp, defaulting to .png).
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

	// errorDisplay controls whether a widget's error text is shown on the
	// card (e.g. set "Hidden" for a service that is expected to be
	// intermittently unreachable). Defaults to "Shown".
	// +kubebuilder:validation:Enum=Shown;Hidden
	// +optional
	ErrorDisplay *string `json:"errorDisplay,omitempty"`

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

	// podSelector selects pods in this ServiceCard's namespace whose
	// readiness determines this service's up/down status — a
	// Kubernetes-native alternative to Ping/SiteMonitor for services that
	// run as pods in this cluster, needing no externally reachable URL.
	// With multiple matches, any Ready pod renders "Up"; the card shows
	// "<ready>/<total> ready" in place of Ping/SiteMonitor's latency.
	// Mutually exclusive with Ping and SiteMonitor (enforced by this type's
	// CEL validation rule).
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

// ServiceCardSpec defines the service card(s) rendered by the native
// dashboard: a list of one or more card entries (a whole group's, or a whole
// dashboard's, worth in one object). group is the default group for any
// entry that doesn't set its own.
// +kubebuilder:validation:XValidation:rule="has(self.group) || self.services.all(s, has(s.group))",message="every services entry must resolve a group: set spec.group as a default, or set group on every entry"
type ServiceCardSpec struct {
	// dashboardRef names the Dashboard this ServiceCard belongs to.
	// +required
	DashboardRef DashboardRef `json:"dashboardRef"`

	// group is the name of the (top-level) group this ServiceCard belongs
	// to, used as the default group for any Services entry that doesn't set
	// its own group.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Group string `json:"group,omitempty"`

	// services defines one entry per service card, for a ServiceCard that
	// groups multiple cards (a whole group, or a whole dashboard) into one
	// object instead of one ServiceCard per card. group is optional on each
	// entry: an entry without its own group falls back to spec.group as a
	// shared default.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	// +required
	Services []ServiceEntry `json:"services"`
}

// Entries returns the card entries this ServiceCard defines: a copy of
// Services with each entry's empty Group replaced by spec.group (the shared
// default).
func (s *ServiceCardSpec) Entries() []ServiceEntry {
	entries := make([]ServiceEntry, len(s.Services))
	copy(entries, s.Services)
	for i := range entries {
		if entries[i].Group == "" {
			entries[i].Group = s.Group
		}
	}
	return entries
}

// ServiceCardStatus defines the observed state of ServiceCard.
// +kubebuilder:validation:MinProperties=1
type ServiceCardStatus struct {
	// conditions represent the current state of the ServiceCard resource.
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// entries is the number of entries this object defines (len(spec.services)).
	// +optional
	Entries int32 `json:"entries,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pcard
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Available')].status"
// +kubebuilder:printcolumn:name="Dashboard",type=string,JSONPath=".spec.dashboardRef.name"
// +kubebuilder:printcolumn:name="Group",type=string,JSONPath=".spec.group"
// +kubebuilder:printcolumn:name="Entries",type=integer,JSONPath=".status.entries"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ServiceCard is the Schema for the servicecards API
type ServiceCard struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ServiceCard
	// +required
	Spec ServiceCardSpec `json:"spec"`

	// status defines the observed state of ServiceCard
	// +optional
	Status ServiceCardStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ServiceCardList contains a list of ServiceCard
type ServiceCardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ServiceCard `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ServiceCard{}, &ServiceCardList{})
		return nil
	})
}

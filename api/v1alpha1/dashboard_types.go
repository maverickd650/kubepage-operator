package v1alpha1

import (
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// DashboardSpec defines the desired state of Dashboard
type DashboardSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// replicas defines the number of Dashboard instances
	//
	// Each dashboard replica polls every bound ServiceCard/InfoWidget
	// independently (internal/dashboard's Poller keeps its own in-memory
	// Store per process) and serves whichever replica an incoming request
	// happens to land on. More than 1 replica therefore multiplies the
	// load placed on every polled upstream by that factor — noticeable for
	// upstreams that rate-limit logins (e.g. UniFi, see
	// internal/dashboard/unifi.go) — and can show a card flapping between
	// replicas' independently-timed poll results if the Service has no
	// session affinity. replicas: 1 (the default) is the intended mode
	// unless you've verified your upstreams tolerate the extra load. This
	// is deliberately a plain field, not the +kubebuilder:subresource:scale
	// marker (kubectl scale, HPA): scaling this workload isn't a supported
	// operation given the per-replica polling behavior above. If you run
	// more than one replica for pod-availability reasons rather than
	// throughput, pair it with your own PodDisruptionBudget targeting the
	// dashboard pod's labels — the operator does not create one.
	// +default=1
	// +kubebuilder:validation:Minimum=0
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// containerPort defines the port the dashboard HTTP server listens on
	// +default=8080
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	ContainerPort int32 `json:"containerPort,omitempty"`

	// pollIntervalSeconds is how often the dashboard polls each widget's
	// upstream and re-probes each ServiceCard's monitor. Lowering it makes
	// cards fresher at the cost of more load on upstream services; raising
	// it is useful for slow/rate-limited upstreams (e.g. UniFi's login
	// rate-limiting, see internal/dashboard/unifi.go).
	// +kubebuilder:validation:Minimum=5
	// +kubebuilder:validation:Maximum=3600
	// +default=15
	// +optional
	PollIntervalSeconds *int32 `json:"pollIntervalSeconds,omitempty"`

	// env is the additional environment variables to set. Uses k8s env var
	// syntax (includes secretKeyRef, configMapKeyRef, etc.)
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +listType=map
	// +listMapKey=name
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// podSecurityContext is the pod security context
	// +optional
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// containerSecurityContext is the container security context. Defaults
	// include readOnlyRootFilesystem: true (the dashboard is a distroless
	// static binary that writes nothing to disk); set any field here to
	// override the corresponding default.
	// +optional
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`

	// hostUsers controls whether the pod uses the host's user namespace.
	// Defaults to "Enabled" (the pod runs in the host's user namespace,
	// matching the Kubernetes default); "Disabled" requests a separate,
	// isolated user namespace for the pod.
	// +kubebuilder:validation:Enum=Enabled;Disabled
	// +default="Enabled"
	// +optional
	HostUsers *string `json:"hostUsers,omitempty"`

	// nodeSelector constrains the pod to nodes matching every label here,
	// passed straight through to the pod template.
	// +kubebuilder:validation:MinProperties=1
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// tolerations let the pod schedule onto nodes with matching taints (e.g.
	// a tainted Raspberry Pi or single-node control plane), passed straight
	// through to the pod template.
	// +kubebuilder:validation:MaxItems=32
	// +listType=atomic
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// affinity expresses node/pod (anti-)affinity rules, passed straight
	// through to the pod template.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// topologySpreadConstraints spread pods across failure domains (zones,
	// nodes, ...), passed straight through to the pod template. Only takes
	// effect when replicas is greater than 1.
	// +kubebuilder:validation:MaxItems=16
	// +listType=atomic
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// imagePullSecrets names Secrets in this namespace holding registry
	// credentials for pulling the dashboard image, passed straight through
	// to the pod template.
	// +kubebuilder:validation:MaxItems=16
	// +listType=atomic
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// volumes are additional pod volumes, passed straight through to the pod
	// template (e.g. a ConfigMap holding a CA bundle to mount cluster-wide,
	// or an emptyDir shared with a sidecar). Only takes effect on a volume
	// actually referenced by volumeMounts; the dashboard container's own
	// working directory and TLS assets are unaffected either way.
	// +kubebuilder:validation:MaxItems=32
	// +listType=map
	// +listMapKey=name
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// volumeMounts mounts entries from volumes into the dashboard container,
	// passed straight through to the pod template (e.g. mounting a CA bundle
	// volume at a path the dashboard process's HTTP client trusts, or custom
	// background/logo assets served from internal/dashboard's static asset
	// path). A mount naming a volume not present in volumes is rejected by
	// the API server's own pod validation at admission time, same as any Pod.
	// +kubebuilder:validation:MaxItems=32
	// +listType=map
	// +listMapKey=mountPath
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// priorityClassName names a PriorityClass for the pod, passed straight
	// through to the pod template.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	PriorityClassName *string `json:"priorityClassName,omitempty"`

	// labels are the additional labels to add to the workload and pod
	// +kubebuilder:validation:MinProperties=1
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// annotations are the additional annotations to add to the workload and pod
	// +kubebuilder:validation:MinProperties=1
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// readinessProbe is the readiness probe configuration. Defaults to an
	// httpGet probe against /healthz on containerPort when unset; set this
	// field to fully override the default rather than merge with it.
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`

	// livenessProbe is the liveness probe configuration. Defaults to an
	// httpGet probe against /healthz on containerPort when unset; set this
	// field to fully override the default rather than merge with it.
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`

	// resources are the resource requests and limits for the container.
	// Defaults to modest limits/requests (mirroring the manager's own
	// defaults) so a Dashboard doesn't run unbounded; set limits and/or
	// requests here to override the corresponding default.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// service customizes the dashboard Service beyond the default
	// ClusterIP (e.g. LoadBalancer for MetalLB, or annotations for an
	// external-dns/Tailscale integration).
	// +optional
	Service *ServiceSpec `json:"service,omitempty"`

	// ingress optionally exposes the dashboard Service via an Ingress. Off by
	// default: most users reach the dashboard through a Service (port-forward,
	// LoadBalancer, existing Ingress/Gateway managed outside this operator,
	// etc.), so this operator shouldn't assume an IngressClass / external-DNS
	// / cert-manager setup is present.
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// gateway optionally exposes the dashboard Service via a Gateway API
	// HTTPRoute instead of (or alongside) Ingress. Off by default, and only
	// takes effect if the cluster has Gateway API CRDs installed — the
	// Dashboard controller checks this once at startup and surfaces a clear
	// status condition if Gateway is set but the CRDs aren't present, rather
	// than crashing the manager trying to watch a Kind that doesn't exist.
	// +optional
	Gateway *GatewaySpec `json:"gateway,omitempty"`

	// discovery opts this Dashboard into synthesizing service cards from
	// annotated Ingresses in its own namespace, in addition to explicit
	// ServiceCard cards. There is no de-duplication: an explicit ServiceCard
	// and a discovered Ingress/HTTPRoute that would render under the same
	// Group+Name both show up as separate cards. Off by default. See
	// DiscoverySpec for the annotation contract.
	// +optional
	Discovery *DiscoverySpec `json:"discovery,omitempty"`

	// metrics controls whether the dashboard's /metrics endpoint (per-widget
	// poll counts/latencies, per-service up/down gauges) is exposed on the
	// dashboard Service, in addition to the pod itself always serving it
	// directly (reachable via a PodMonitor or port-forward regardless of
	// this setting). Off by default: unlike the manager's own /metrics
	// (which requires authn/authz), the dashboard's has none, so any pod
	// able to reach the Service port could otherwise read which internal
	// services this dashboard names and their live status with no RBAC
	// check at all.
	// +optional
	Metrics *MetricsSpec `json:"metrics,omitempty"`

	// auth optionally gates every dashboard route (except /healthz) behind
	// HTTP Basic authentication. Unset (the default) means the dashboard has
	// no authentication of its own — see SECURITY.md's trust-model section
	// for why, and for the recommended alternative of an authenticating
	// reverse proxy.
	// +optional
	Auth *AuthSpec `json:"auth,omitempty"`

	// networkPolicy optionally creates an owner-referenced NetworkPolicy
	// scoping which pods may reach this Dashboard's dashboard/metrics ports
	// and, when egressCIDRs is set, which addresses its pods may reach. Off
	// by default: dashboard pods otherwise accept ingress from anywhere in
	// the cluster and have unrestricted egress, same as any Pod with no
	// NetworkPolicy selecting it.
	// +optional
	NetworkPolicy *NetworkPolicySpec `json:"networkPolicy,omitempty"`

	// secretPolicy controls which Secrets a bound ServiceCard/InfoWidget
	// widget may reference via secretKeyRef (or caCert):
	//   - "Unrestricted" (default): any Secret in this namespace — the
	//     documented trust model (see SECURITY.md): anyone who can author
	//     these CRDs in a namespace is trusted with every Secret in it.
	//   - "Labeled": only Secrets carrying the
	//     page.kubepage.dev/allow-widgets: "true" label are granted to the
	//     dashboard pod's RBAC; a widget referencing an unlabeled (or
	//     nonexistent) Secret surfaces a clear card error instead.
	// +kubebuilder:validation:Enum=Unrestricted;Labeled
	// +default="Unrestricted"
	// +optional
	SecretPolicy *string `json:"secretPolicy,omitempty"`

	// widgetDefaults supplies per-widget-type default values for
	// secret-bearing fields, keyed by widget type then field name (e.g.
	// openweathermap: {key: {secretKeyRef: ...}}). A ServiceCard/InfoWidget
	// widget of that type that does not set the field in its own secrets
	// inherits the default; a widget's own secrets always win. The
	// equivalent of homepage's settings.yaml "providers" block: one shared
	// API key can serve every widget of a given type instead of each widget
	// repeating its own secrets stanza. Resolved under the same
	// secretPolicy rules and the same dashboard-pod RBAC as a widget's own
	// secretKeyRef (see internal/controller/dashboard_rbac.go's
	// referencedSecretNames) — this field widens convenience, not the trust
	// model documented in SECURITY.md.
	//
	// The map key (widget type) is deliberately not validated against the
	// ServiceWidget.Type/InfoWidgetEntry.Type enums here, to avoid two enums
	// drifting apart; an unknown type here is simply never matched by any
	// widget.
	// +kubebuilder:validation:MinProperties=1
	// +optional
	WidgetDefaults map[string]WidgetDefaultsEntry `json:"widgetDefaults,omitempty"`
}

// WidgetDefaultsEntry supplies default secret-bearing values for one widget
// type, used to fill gaps in a widget's own Secrets/CACert rather than
// override them — see DashboardSpec.WidgetDefaults' doc comment. At least
// one of Secrets or CACert must be set.
// +kubebuilder:validation:XValidation:rule="has(self.secrets) || has(self.caCert)",message="at least one of secrets or caCert must be set"
type WidgetDefaultsEntry struct {
	// secrets are default secret-bearing widget fields, keyed the same way as
	// ServiceWidget.Secrets/InfoWidgetEntry.Secrets (e.g. "token", "apiKey").
	// A widget of this type that doesn't set a given key in its own secrets
	// inherits the default here, per key; a widget's own secrets always win.
	// +kubebuilder:validation:MinProperties=1
	// +kubebuilder:validation:MaxProperties=32
	// +optional
	Secrets map[string]SecretValueSource `json:"secrets,omitempty"`

	// caCert is the default CA certificate for widgets of this type that
	// don't set their own caCert. See ServiceWidget.CACert's doc comment for
	// the full rationale.
	// +optional
	CACert *SecretValueSource `json:"caCert,omitempty"`
}

// MetricsSpec controls whether the dashboard's /metrics port is exposed on
// the dashboard Service.
type MetricsSpec struct {
	// enabled exposes port 9090 on the dashboard Service when "Enabled".
	// +kubebuilder:validation:Enum=Enabled;Disabled
	// +default="Disabled"
	// +optional
	Enabled string `json:"enabled,omitempty"`
}

// AuthSpec configures the dashboard's optional built-in HTTP Basic
// authentication. See SECURITY.md's "Optional built-in authentication"
// section for the trade-offs versus fronting the dashboard with a real
// authenticating reverse proxy (oauth2-proxy, Authelia, ...).
//
// basicAuthSecretRef is AuthSpec's only field, so without the CEL rule below
// an empty `spec.auth: {}` would be schema-valid and silently serve the
// dashboard UNAUTHENTICATED — internal/dashboard/auth.go treats a nil ref
// the same as no auth configured at all. Requiring the field fails closed:
// setting spec.auth at all now requires actually naming the htpasswd Secret.
// +kubebuilder:validation:XValidation:rule="has(self.basicAuthSecretRef)",message="basicAuthSecretRef is required when auth is set"
type AuthSpec struct {
	// basicAuthSecretRef names a Secret, in the same namespace as this
	// Dashboard, holding an htpasswd-format file (bcrypt-hashed entries,
	// e.g. produced by `htpasswd -B`) under its ".htpasswd" key. When set,
	// every dashboard route except /healthz requires HTTP Basic credentials
	// matching an entry in that file, checked with a constant-time compare.
	// +optional
	BasicAuthSecretRef *corev1.LocalObjectReference `json:"basicAuthSecretRef,omitempty"`
}

// NetworkPolicySpec configures an operator-managed NetworkPolicy for this
// Dashboard's dashboard pods.
type NetworkPolicySpec struct {
	// enabled creates and manages a NetworkPolicy for this Dashboard's
	// dashboard pods when "Enabled".
	// +kubebuilder:validation:Enum=Enabled;Disabled
	// +default="Disabled"
	// +optional
	Enabled string `json:"enabled,omitempty"`

	// ingressNamespaceSelector selects which namespaces' pods may reach the
	// dashboard's containerPort. Leave unset to allow ingress from any
	// namespace (matching the default, unrestricted Service behavior) while
	// still scoping the metrics port/egress below.
	// +optional
	IngressNamespaceSelector *metav1.LabelSelector `json:"ingressNamespaceSelector,omitempty"`

	// metricsNamespaceSelector selects which namespaces' pods may reach the
	// metrics port (9090). Only meaningful when spec.metrics.enabled; leave
	// unset to allow from any namespace.
	// +optional
	MetricsNamespaceSelector *metav1.LabelSelector `json:"metricsNamespaceSelector,omitempty"`

	// egressCIDRs additionally allows egress to exactly these CIDR blocks
	// (e.g. widget upstreams), plus DNS (port 53) and the Kubernetes API
	// server (port 443) which are always allowed once egress is restricted.
	// Leave empty to leave egress unrestricted (the non-breaking default)
	// even with networkPolicy enabled for ingress — widget URLs are
	// CRD-supplied by design (see SECURITY.md's explicit non-goals), so this
	// is an opt-in positive scope, not a default deny.
	//
	// Each entry must look like a CIDR block (IPv4 or IPv6, with a mask), so
	// a malformed entry is rejected at admission instead of passing schema
	// validation and only surfacing as an opaque reconcile error once it's
	// placed into a NetworkPolicy IPBlock. This uses a regex Pattern rather
	// than CEL's isCIDR() extension function: isCIDR() requires Kubernetes
	// 1.31+ (see https://kubernetes.io/docs/reference/using-api/cel/), newer
	// than this project's documented CEL floor of 1.29/1.30 (see CLAUDE.md),
	// so a regex keeps the same floor. The pattern is a shape check, not a
	// full validator (e.g. it doesn't enforce octet/hextet range or IPv6
	// compression rules) — it exists to catch obviously malformed input, not
	// to replace the apiserver's own CIDR parsing.
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=43
	// +kubebuilder:validation:items:Pattern=`^([0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}/[0-9]{1,2}|[0-9A-Fa-f:]+:[0-9A-Fa-f:]*/[0-9]{1,3})$`
	// +listType=set
	// +optional
	EgressCIDRs []string `json:"egressCIDRs,omitempty"`
}

// DiscoverySpec opts a Dashboard into Kubernetes-native service discovery:
// the dashboard process lists resources of each kind named in Sources in its
// own namespace and renders one card per resource carrying the enable
// annotation, without requiring an explicit ServiceCard. Mirrors (a curated
// subset of) homepage's Kubernetes discovery
// (https://gethomepage.dev/configs/kubernetes/), scoped to what an
// annotation — world-readable to anyone who can read the source resource —
// can safely carry: no secrets, so no widget config, only href/icon/
// description/group/ping.
//
// The scanned namespace is single (the Dashboard's own) by default: this
// keeps a Dashboard's blast radius equal to its own RBAC, matching every
// other config CRD in this project (DashboardStyle/ServiceCard/Bookmark/
// InfoWidget all require dashboardRef's Dashboard to be in the same
// namespace — see CLAUDE.md). Namespaces opts a specific Dashboard into
// scanning additional namespaces beyond its own, for the common homelab
// shape of one dashboard namespace and apps spread across several others;
// see its own doc comment for the RBAC this widens.
type DiscoverySpec struct {
	// enabled turns on annotation discovery for this Dashboard.
	// +kubebuilder:validation:Enum=Enabled;Disabled
	// +default="Disabled"
	// +optional
	Enabled string `json:"enabled,omitempty"`

	// sources selects which resource kinds are scanned for the discovery
	// annotations. Defaults to ["Ingress"] (the pre-existing behavior).
	// "HTTPRoute" requires Gateway API CRDs on the cluster; without them a
	// Dashboard requesting it gets a clear Available=False condition, the
	// same behavior as spec.gateway.
	// +kubebuilder:validation:items:Enum=Ingress;HTTPRoute
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=2
	// +listType=set
	// +optional
	Sources []string `json:"sources,omitempty"`

	// annotationPrefix is the annotation key prefix a source resource must
	// carry to be discovered, e.g. "<prefix>enabled", "<prefix>name",
	// "<prefix>group". Defaults to "kubepage.io/".
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	AnnotationPrefix *string `json:"annotationPrefix,omitempty"`

	// homepageCompat additionally honors "gethomepage.dev/*" annotations
	// (homepage's own discovery convention) on any source resource that
	// doesn't carry AnnotationPrefix's own enable annotation, so a cluster
	// migrating from homepage doesn't need to relabel every resource.
	// +kubebuilder:validation:Enum=Enabled;Disabled
	// +optional
	HomepageCompat *string `json:"homepageCompat,omitempty"`

	// namespaces additionally scans these namespaces (beyond the Dashboard's
	// own, which is always scanned) for the same discovery annotations. A
	// static, explicit list rather than a label selector — deliberately, to
	// keep exactly which namespaces a Dashboard can read a reviewable,
	// one-time grant rather than something that silently grows as
	// unrelated namespaces pick up a matching label.
	//
	// Setting this widens the dashboard pod's RBAC beyond its own namespace:
	// for each namespace listed here, the controller creates a RoleBinding
	// *in that namespace* granting the dashboard pod's ServiceAccount
	// get/list/watch on Ingresses (and, when Sources includes "HTTPRoute",
	// HTTPRoutes) — never a ClusterRoleBinding, so access never extends
	// beyond the namespaces actually listed here. This mirrors
	// dashboardIngressRule/dashboardHTTPRouteRule's existing scope
	// (internal/controller/dashboard_rbac.go), just bound in more than one
	// namespace. Whoever can set this field on a Dashboard can therefore
	// make its dashboard pod read every Ingress/HTTPRoute's hostnames/paths
	// (not their annotations' hidden fields — annotation discovery only
	// ever reads what an Ingress author already opted into via the
	// discovery annotations) in the namespaces named here — see
	// SECURITY.md's trust model.
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=63
	// +kubebuilder:validation:MaxItems=32
	// +listType=set
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`
}

// DiscoverySourceIngress and DiscoverySourceHTTPRoute are Sources' valid
// values, shared between the CRD validation marker above and the code that
// reads Sources (internal/dashboard/discovery.go's poller wiring,
// internal/controller's RBAC/availability gating) so the two can't drift.
const (
	DiscoverySourceIngress   = "Ingress"
	DiscoverySourceHTTPRoute = "HTTPRoute"
)

// HasSource reports whether source (one of DiscoverySourceIngress/
// DiscoverySourceHTTPRoute) is among the resource kinds this DiscoverySpec
// scans. An unset/empty Sources defaults to Ingress only, matching the
// pre-existing (Sources-less) behavior byte-for-byte.
func (d *DiscoverySpec) HasSource(source string) bool {
	if len(d.Sources) == 0 {
		return source == DiscoverySourceIngress
	}
	return slices.Contains(d.Sources, source)
}

// ServiceSpec customizes the dashboard Service beyond the default ClusterIP.
type ServiceSpec struct {
	// type is the Service type. Defaults to "ClusterIP".
	// +kubebuilder:validation:Enum=ClusterIP;LoadBalancer;NodePort
	// +default="ClusterIP"
	// +optional
	Type string `json:"type,omitempty"`

	// annotations to set on the generated Service (e.g. a MetalLB IP pool,
	// an external-dns hostname, a Tailscale annotation).
	// +kubebuilder:validation:MinProperties=1
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// IngressSpec configures an Ingress exposing the dashboard Service.
type IngressSpec struct {
	// enabled creates and manages an Ingress for this Dashboard when
	// "Enabled".
	// +kubebuilder:validation:Enum=Enabled;Disabled
	// +default="Disabled"
	// +optional
	Enabled string `json:"enabled,omitempty"`

	// host is the hostname routed to the dashboard Service.
	// +kubebuilder:validation:Pattern=`^(\*\.)?([a-z0-9]([-a-z0-9]*[a-z0-9])?\.)+[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Host string `json:"host"`

	// ingressClassName selects the IngressClass that implements this
	// Ingress. Leave unset to use the cluster's default IngressClass.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	IngressClassName *string `json:"ingressClassName,omitempty"`

	// annotations to set on the generated Ingress (e.g. a cert-manager
	// issuer, nginx rewrite rules).
	// +kubebuilder:validation:MinProperties=1
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// tls terminates TLS for Host using the named Secret. Leave unset to
	// serve plain HTTP.
	// +optional
	TLS *IngressTLSSpec `json:"tls,omitempty"`
}

// IngressTLSSpec names the Secret holding a TLS certificate/key for an
// Ingress host.
type IngressTLSSpec struct {
	// secretName is the Secret holding the TLS certificate/key for Host.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	SecretName string `json:"secretName"`
}

// GatewaySpec configures a Gateway API HTTPRoute exposing the dashboard
// Service, attached to an existing Gateway (TLS termination, listener
// config, etc. are the Gateway's concern, not this operator's — mirroring
// how IngressSpec leaves cert-manager/IngressClass setup to the cluster).
type GatewaySpec struct {
	// enabled creates and manages an HTTPRoute for this Dashboard when
	// "Enabled".
	// +kubebuilder:validation:Enum=Enabled;Disabled
	// +default="Disabled"
	// +optional
	Enabled string `json:"enabled,omitempty"`

	// hostnames the HTTPRoute matches. At least one is required.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:items:Pattern=`^(\*\.)?([a-z0-9]([-a-z0-9]*[a-z0-9])?\.)+[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=253
	// +listType=set
	// +required
	Hostnames []string `json:"hostnames"`

	// parentRef names the Gateway this HTTPRoute attaches to.
	// +required
	ParentRef GatewayParentRef `json:"parentRef"`

	// annotations to set on the generated HTTPRoute.
	// +kubebuilder:validation:MinProperties=1
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// GatewayParentRef names the Gateway (and optionally one of its listeners)
// an HTTPRoute attaches to.
type GatewayParentRef struct {
	// name of the Gateway.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name"`

	// namespace of the Gateway. Defaults to the Dashboard's own namespace if
	// unset.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	Namespace *string `json:"namespace,omitempty"`

	// sectionName names a specific listener on the Gateway to attach to.
	// Leave unset to attach to every listener that allows it.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	SectionName *string `json:"sectionName,omitempty"`
}

// DashboardStatus defines the observed state of Dashboard
// +kubebuilder:validation:MinProperties=1
type DashboardStatus struct {
	// conditions represent the current status of the Dashboard
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// observedGeneration is the most recent generation this status reflects.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// boundDashboardStyles is the number of DashboardStyle objects currently
	// bound to (dashboardRef-ing) this Dashboard.
	// +optional
	BoundDashboardStyles int32 `json:"boundDashboardStyles,omitempty"`

	// boundServiceCards is the number of ServiceCard objects currently
	// bound to this Dashboard.
	// +optional
	BoundServiceCards int32 `json:"boundServiceCards,omitempty"`

	// boundBookmarks is the number of Bookmark objects currently bound to
	// this Dashboard.
	// +optional
	BoundBookmarks int32 `json:"boundBookmarks,omitempty"`

	// boundInfoWidgets is the number of InfoWidget objects currently bound to
	// this Dashboard.
	// +optional
	BoundInfoWidgets int32 `json:"boundInfoWidgets,omitempty"`

	// url is where this Dashboard is reachable, derived in priority order:
	// spec.ingress's host (scheme "https" if spec.ingress.tls is set,
	// otherwise "http"); else spec.gateway's first hostname (always
	// "https", since TLS termination is the attaching Gateway's listener
	// config, not visible to this controller); else the dashboard Service's
	// cluster-internal DNS name. A Service of type LoadBalancer's external
	// IP is deliberately not resolved here — that lives on the Service's
	// own status and would need an additional watch for a field most
	// clusters don't populate synchronously; use `kubectl get svc` for that
	// case in the meantime.
	// +optional
	URL string `json:"url,omitempty"`

	// discoveryNamespaces is the set of namespaces (beyond this Dashboard's
	// own) discovery RBAC currently applies to, i.e. the last value of
	// spec.discovery.namespaces the controller successfully reconciled a
	// RoleBinding for in each named namespace. Tracked so the controller can
	// clean up a RoleBinding in a namespace that's since been removed from
	// spec.discovery.namespaces without needing cluster-wide list/watch on
	// RoleBindings (which this operator deliberately never requests — see
	// SECURITY.md).
	// +optional
	DiscoveryNamespaces []string `json:"discoveryNamespaces,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pdash
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Available')].status"
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Services",type=integer,JSONPath=".status.boundServiceCards"
// +kubebuilder:printcolumn:name="Bookmarks",type=integer,JSONPath=".status.boundBookmarks"
// +kubebuilder:printcolumn:name="Widgets",type=integer,JSONPath=".status.boundInfoWidgets"
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=".status.url"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// Dashboard is the Schema for the dashboards API
type Dashboard struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Dashboard
	// +required
	Spec DashboardSpec `json:"spec"`

	// status defines the observed state of Dashboard
	// +optional
	Status DashboardStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// DashboardList contains a list of Dashboard
type DashboardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Dashboard `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Dashboard{}, &DashboardList{})
		return nil
	})
}

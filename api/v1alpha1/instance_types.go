package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// InstanceSpec defines the desired state of Instance
type InstanceSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// size defines the number of Instance instances
	//
	// Each dashboard replica polls every bound ServiceEntry/InfoWidget
	// independently (internal/dashboard's Poller keeps its own in-memory
	// Store per process) and serves whichever replica an incoming request
	// happens to land on. A size greater than 1 therefore multiplies the
	// load placed on every polled upstream by that factor — noticeable for
	// upstreams that rate-limit logins (e.g. UniFi, see
	// internal/dashboard/unifi.go) — and can show a card flapping between
	// replicas' independently-timed poll results if the Service has no
	// session affinity. size: 1 is the intended mode unless you've verified
	// your upstreams tolerate the extra load.
	// +default=1
	// +kubebuilder:validation:Minimum=0
	// +optional
	Size *int32 `json:"size,omitempty"`

	// containerPort defines the port the dashboard HTTP server listens on
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +required
	ContainerPort int32 `json:"containerPort"`

	// pollIntervalSeconds is how often the dashboard polls each widget's
	// upstream and re-probes each ServiceEntry's monitor. Lowering it makes
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

	// containerSecurityContext is the container security context
	// +optional
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`

	// hostUsers controls whether the container's user namespace is separate
	// from the host. Defaults to "Enabled" (separate namespace).
	// +kubebuilder:validation:Enum=Enabled;Disabled
	// +default="Enabled"
	// +optional
	HostUsers *string `json:"hostUsers,omitempty"`

	// labels are the additional labels to add to the workload and pod
	// +kubebuilder:validation:MinProperties=1
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// annotations are the additional annotations to add to the workload and pod
	// +kubebuilder:validation:MinProperties=1
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// readinessProbe is the readiness probe configuration
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`

	// livenessProbe is the liveness probe configuration
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`

	// resources are the resource requests and limits for the container
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

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
	// Instance controller checks this once at startup and surfaces a clear
	// status condition if Gateway is set but the CRDs aren't present, rather
	// than crashing the manager trying to watch a Kind that doesn't exist.
	// +optional
	Gateway *GatewaySpec `json:"gateway,omitempty"`

	// discovery opts this Instance into synthesizing service cards from
	// annotated Ingresses in its own namespace, in addition to explicit
	// ServiceEntry cards (an explicit ServiceEntry wins on a Group+Name
	// collision). Off by default. See DiscoverySpec for the annotation
	// contract.
	// +optional
	Discovery *DiscoverySpec `json:"discovery,omitempty"`
}

// DiscoverySpec opts an Instance into Kubernetes-native service discovery:
// the dashboard process lists Ingresses in its own namespace and renders one
// card per Ingress carrying the enable annotation, without requiring an
// explicit ServiceEntry. Mirrors (a curated subset of) homepage's Ingress
// annotation discovery (https://gethomepage.dev/configs/kubernetes/), scoped
// to what an annotation — world-readable to anyone who can read the Ingress —
// can safely carry: no secrets, so no widget config, only href/icon/
// description/group/ping.
type DiscoverySpec struct {
	// enabled turns on Ingress annotation discovery for this Instance.
	// +kubebuilder:validation:Enum=Enabled;Disabled
	// +default="Disabled"
	// +optional
	Enabled string `json:"enabled,omitempty"`

	// annotationPrefix is the annotation key prefix an Ingress must carry to
	// be discovered, e.g. "<prefix>enabled", "<prefix>name", "<prefix>group".
	// Defaults to "kubepage.io/".
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	AnnotationPrefix *string `json:"annotationPrefix,omitempty"`

	// homepageCompat additionally honors "gethomepage.dev/*" annotations
	// (homepage's own discovery convention) on any Ingress that doesn't carry
	// AnnotationPrefix's own enable annotation, so a cluster migrating from
	// homepage doesn't need to relabel every Ingress.
	// +kubebuilder:validation:Enum=Enabled;Disabled
	// +optional
	HomepageCompat *string `json:"homepageCompat,omitempty"`
}

// IngressSpec configures an Ingress exposing the dashboard Service.
type IngressSpec struct {
	// enabled creates and manages an Ingress for this Instance when
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
	// enabled creates and manages an HTTPRoute for this Instance when
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

	// namespace of the Gateway. Defaults to the Instance's own namespace if
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

// InstanceStatus defines the observed state of Instance
// +kubebuilder:validation:MinProperties=1
type InstanceStatus struct {
	// conditions represent the current status of the Instance
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// observedGeneration is the most recent generation this status reflects.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// boundConfigurations is the number of Configuration objects currently
	// bound to (instanceRef-ing) this Instance.
	// +optional
	BoundConfigurations int32 `json:"boundConfigurations,omitempty"`

	// boundServiceEntries is the number of ServiceEntry objects currently
	// bound to this Instance.
	// +optional
	BoundServiceEntries int32 `json:"boundServiceEntries,omitempty"`

	// boundBookmarks is the number of Bookmark objects currently bound to
	// this Instance.
	// +optional
	BoundBookmarks int32 `json:"boundBookmarks,omitempty"`

	// boundInfoWidgets is the number of InfoWidget objects currently bound to
	// this Instance.
	// +optional
	BoundInfoWidgets int32 `json:"boundInfoWidgets,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pageinst
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Available')].status"
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=".spec.size"
// +kubebuilder:printcolumn:name="Services",type=integer,JSONPath=".status.boundServiceEntries"
// +kubebuilder:printcolumn:name="Bookmarks",type=integer,JSONPath=".status.boundBookmarks"
// +kubebuilder:printcolumn:name="Widgets",type=integer,JSONPath=".status.boundInfoWidgets"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// Instance is the Schema for the instances API
type Instance struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Instance
	// +required
	Spec InstanceSpec `json:"spec"`

	// status defines the observed state of Instance
	// +optional
	Status InstanceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// InstanceList contains a list of Instance
type InstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Instance `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Instance{}, &InstanceList{})
		return nil
	})
}

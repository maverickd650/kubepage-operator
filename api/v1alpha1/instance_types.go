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
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +optional
	Size *int32 `json:"size,omitempty"`

	// containerPort defines the port the dashboard HTTP server listens on
	// +required
	ContainerPort int32 `json:"containerPort"`

	// Additional environment variables to set
	// Uses k8s env var syntax (includes secretKeyRef, configMapKeyRef, etc.)
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Pod security context
	// +optional
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// Container security context
	// +optional
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`

	// HostUsers controls whether the container's user namespace is separate from the host
	// Defaults to true
	// +kubebuilder:default=true
	HostUsers *bool `json:"hostUsers,omitempty"`

	// Additional labels to add to the workload and pod
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Additional annotations to add to the workload and pod
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// Readiness probe configuration
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`

	// Liveness probe configuration
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`

	// Resource requests and limits for the container
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Ingress optionally exposes the dashboard Service via an Ingress. Off by
	// default: most users reach the dashboard through a Service (port-forward,
	// LoadBalancer, existing Ingress/Gateway managed outside this operator,
	// etc.), so this operator shouldn't assume an IngressClass / external-DNS
	// / cert-manager setup is present.
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// Gateway optionally exposes the dashboard Service via a Gateway API
	// HTTPRoute instead of (or alongside) Ingress. Off by default, and only
	// takes effect if the cluster has Gateway API CRDs installed — the
	// Instance controller checks this once at startup and surfaces a clear
	// status condition if Gateway is set but the CRDs aren't present, rather
	// than crashing the manager trying to watch a Kind that doesn't exist.
	// +optional
	Gateway *GatewaySpec `json:"gateway,omitempty"`
}

// IngressSpec configures an Ingress exposing the dashboard Service.
type IngressSpec struct {
	// Enabled creates and manages an Ingress for this Instance when true.
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Host is the hostname routed to the dashboard Service.
	// +required
	Host string `json:"host"`

	// IngressClassName selects the IngressClass that implements this
	// Ingress. Leave unset to use the cluster's default IngressClass.
	// +optional
	IngressClassName *string `json:"ingressClassName,omitempty"`

	// Annotations to set on the generated Ingress (e.g. a cert-manager
	// issuer, nginx rewrite rules).
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// TLS terminates TLS for Host using the named Secret. Leave unset to
	// serve plain HTTP.
	// +optional
	TLS *IngressTLSSpec `json:"tls,omitempty"`
}

// IngressTLSSpec names the Secret holding a TLS certificate/key for an
// Ingress host.
type IngressTLSSpec struct {
	// SecretName is the Secret holding the TLS certificate/key for Host.
	// +kubebuilder:validation:MinLength=1
	// +required
	SecretName string `json:"secretName"`
}

// GatewaySpec configures a Gateway API HTTPRoute exposing the dashboard
// Service, attached to an existing Gateway (TLS termination, listener
// config, etc. are the Gateway's concern, not this operator's — mirroring
// how IngressSpec leaves cert-manager/IngressClass setup to the cluster).
type GatewaySpec struct {
	// Enabled creates and manages an HTTPRoute for this Instance when true.
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Hostnames the HTTPRoute matches. At least one is required.
	// +kubebuilder:validation:MinItems=1
	// +required
	Hostnames []string `json:"hostnames"`

	// ParentRef names the Gateway this HTTPRoute attaches to.
	// +required
	ParentRef GatewayParentRef `json:"parentRef"`

	// Annotations to set on the generated HTTPRoute.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// GatewayParentRef names the Gateway (and optionally one of its listeners)
// an HTTPRoute attaches to.
type GatewayParentRef struct {
	// Name of the Gateway.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// Namespace of the Gateway. Defaults to the Instance's own namespace if
	// unset.
	// +optional
	Namespace *string `json:"namespace,omitempty"`

	// SectionName names a specific listener on the Gateway to attach to.
	// Leave unset to attach to every listener that allows it.
	// +optional
	SectionName *string `json:"sectionName,omitempty"`
}

// InstanceStatus defines the observed state of Instance
type InstanceStatus struct {
	// Conditions represent the current status of the Instance
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the most recent generation this status reflects.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// BoundConfigurations is the number of Configuration objects currently
	// bound to (instanceRef-ing) this Instance.
	// +optional
	BoundConfigurations int32 `json:"boundConfigurations,omitempty"`

	// BoundServiceEntries is the number of ServiceEntry objects currently
	// bound to this Instance.
	// +optional
	BoundServiceEntries int32 `json:"boundServiceEntries,omitempty"`

	// BoundBookmarks is the number of Bookmark objects currently bound to
	// this Instance.
	// +optional
	BoundBookmarks int32 `json:"boundBookmarks,omitempty"`

	// BoundInfoWidgets is the number of InfoWidget objects currently bound to
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

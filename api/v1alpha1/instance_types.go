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

	// containerPort defines the port that will be used to init the container with the image
	// +required
	ContainerPort int32 `json:"containerPort"`

	// External URL homepage can be reached at
	// See the official documentation for APP_URL
	// +optional
	AppURL string `json:"appUrl,omitempty"`

	// AllowedHosts is a comma-separated list of hostnames homepage will accept
	// requests for (maps to HOMEPAGE_ALLOWED_HOSTS). Homepage rejects requests
	// for hosts not in this list, so this must include every hostname/IP used
	// to reach the dashboard (Service DNS name, Ingress host, port-forward
	// address, etc).
	// +optional
	AllowedHosts string `json:"allowedHosts,omitempty"`

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

	// Ingress optionally exposes the homepage Service via an Ingress. Off by
	// default: most users reach homepage through a Service (port-forward,
	// LoadBalancer, existing Ingress/Gateway managed outside this operator,
	// etc.), so this operator shouldn't assume an IngressClass / external-DNS
	// / cert-manager setup is present.
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`
}

// IngressSpec configures an Ingress exposing the homepage Service.
type IngressSpec struct {
	// Enabled creates and manages an Ingress for this Instance when true.
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Host is the hostname routed to the homepage Service.
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

	// RenderHash is the hash of the most recently rendered homepage config,
	// also set as the pod template's page.kubepage.dev/config-hash
	// annotation. Useful to confirm a Deployment rollout actually picked up
	// a config change.
	// +optional
	RenderHash string `json:"renderHash,omitempty"`
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

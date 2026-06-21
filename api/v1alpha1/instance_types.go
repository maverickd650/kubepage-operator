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
}

// InstanceStatus defines the observed state of Instance
type InstanceStatus struct {
	// Conditions represent the current status of the Instance
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pageinst

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

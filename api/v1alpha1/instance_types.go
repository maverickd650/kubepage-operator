/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type EnvValue struct {
	// Plain text value
	// +optional
	Value *string `json:"value,omitempty"`

	// ValueFrom defines the source of the value
	// +optional
	ValueFrom *corev1.EnvVarSource `json:"valueFrom,omitempty"`
}
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

	// External URL Pocket-id can be reached at
	// See the official documentation for APP_URL
	// +optional
	AppURL string `json:"appUrl,omitempty"`

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
	SchemeBuilder.Register(&Instance{}, &InstanceList{})
}

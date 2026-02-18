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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Background specifies specific background attributes
type BackgroundSpec struct {
	// For a custom image instead of plain colour background provide a full URL to the image or a path to the image e.g. /app/public/images
	// +optional
	image *string `json:"image,omitempty"`

	// Apply a backdrop blur
	// +optional
	blur *int32 `json:"blur,omitempty"`

	// Apply a saturation
	// +optional
	saturate *int32 `json:"saturate,omitempty"`

	// Modify brightness
	// +optional
	brightness *int32 `json:"brightness,omitempty"`

	// Modify opacity of the background
	// +optional
	opacity *int32 `json:"opacity,omitempty"`
}

// ConfigurationSpec defines the desired state of Configuration
type ConfigurationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// Customise the page title
	// +optional
	title *string `json:"title,omitempty"`

	// Customise the page description
	// +optional
	description *string `json:"description,omitempty"`

	// Customise the start url if required. Default is "/".
	// +optional
	// +kubebuilder:default="/"
	startUrl *string `json:"startUrl,omitempty"`

	// Background specifies specific background attributes
	// +optional
	background []BackgroundSpec `json:"background,omitempty"`

	// Apply a blur to the service and bookmark cards, this is compatible with the background filters.
	// +optional
	cardBlur *string `json:"cardBlur,omitempty"`

	// Specify a custom favicon instead of the included one, this can be a full URL or path to the file e.g. /app/images
	// +optional
	favicon *string `json:"favicon,omitempty"`
}

// ConfigurationStatus defines the observed state of Configuration.
type ConfigurationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the Configuration resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Configuration is the Schema for the configurations API
type Configuration struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Configuration
	// +required
	Spec ConfigurationSpec `json:"spec"`

	// status defines the observed state of Configuration
	// +optional
	Status ConfigurationStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ConfigurationList contains a list of Configuration
type ConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Configuration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Configuration{}, &ConfigurationList{})
}

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// InstanceRef binds a config object (Configuration, ServiceEntry, Bookmark,
// InfoWidget) to the Instance it should be rendered into. The referenced
// Instance must exist in the same namespace as the object carrying this ref.
type InstanceRef struct {
	// Name of the Instance this object belongs to.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// SecretValueSource is an inline value or a reference to a key in a Secret,
// used for any config field that may hold a credential (e.g. a widget API
// key). Exactly one of Value or SecretKeyRef must be set; this is enforced at
// admission by a ValidatingAdmissionPolicy (config/admission), so neither a
// secret that sets both nor one that sets neither reaches the dashboard, which
// resolves whichever is set in-process at poll time.
type SecretValueSource struct {
	// Value is a literal value. Avoid this for real credentials; prefer
	// SecretKeyRef so the value isn't stored in the CR.
	// +optional
	Value *string `json:"value,omitempty"`

	// SecretKeyRef selects a key of a Secret in the same namespace.
	// +optional
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
}

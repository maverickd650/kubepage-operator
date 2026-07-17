package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// SecretPolicySpec.SecretPolicy enum values; see that field's doc comment.
const (
	SecretPolicyUnrestricted = "Unrestricted"
	SecretPolicyLabeled      = "Labeled"
)

// SecretAllowWidgetsLabel is the label a Secret must carry (value "true") to
// be readable by a ServiceCard/InfoWidget widget when the owning Dashboard
// sets spec.secretPolicy: Labeled. Ignored under the default "Unrestricted"
// policy. See dashboard_types.go's DashboardSpec.SecretPolicy doc comment for
// the full trust-model rationale.
const SecretAllowWidgetsLabel = "page.kubepage.dev/allow-widgets"

// DashboardRef binds a config object (ServiceCard, Bookmark, InfoWidget) to
// the Dashboard it should be rendered into. The referenced Dashboard must
// exist in the same namespace as the object carrying this ref.
type DashboardRef struct {
	// name of the Dashboard this object belongs to.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name"`
}

// SecretValueSource is an inline value or a reference to a key in a Secret,
// used for any config field that may hold a credential (e.g. a widget API
// key). Exactly one of Value or SecretKeyRef must be set; this is enforced by
// the CRD schema's CEL rule below, so neither a secret that sets both nor one
// that sets neither reaches the dashboard, which resolves whichever is set
// in-process at poll time.
// +kubebuilder:validation:XValidation:rule="(has(self.value) ? 1 : 0) + (has(self.secretKeyRef) ? 1 : 0) == 1",message="exactly one of value or secretKeyRef must be set"
type SecretValueSource struct {
	// value is a literal value. Avoid this for real credentials; prefer
	// SecretKeyRef so the value isn't stored in the CR.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	// +optional
	Value *string `json:"value,omitempty"`

	// secretKeyRef selects a key of a Secret in the same namespace.
	// +optional
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
}

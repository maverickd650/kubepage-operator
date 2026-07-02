package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// BookmarkSpec defines one static link card rendered by the native
// dashboard, in the group named by Group. Bookmarks are much simpler than
// ServiceCard's service cards: just a link, with an optional
// abbreviation/icon and override description.
//
// Nested groups (a group inside another group) are not supported in this
// version of the operator; Group always names a top-level group.
type BookmarkSpec struct {
	// dashboardRef names the Dashboard this Bookmark belongs to.
	// +required
	DashboardRef DashboardRef `json:"dashboardRef"`

	// group is the name of the (top-level) bookmarks.yaml group this entry
	// belongs to. Entries sharing a Group are rendered together.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +required
	Group string `json:"group"`

	// name is the bookmark's display name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +required
	Name string `json:"name"`

	// href is the bookmark's link target.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +required
	Href string `json:"href"`

	// order controls rendering position: groups and entries are sorted by
	// Order (nil sorts last), ties broken by Name, since CRDs have no
	// inherent ordering but bookmarks.yaml's groups/entries are an ordered
	// list. Purely an operator-side rendering concern; not a homepage field.
	// +optional
	Order *int32 `json:"order,omitempty"`

	// abbr is a two-letter abbreviation shown when Icon is not set. If both
	// are set, Icon takes precedence.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=8
	// +optional
	Abbr *string `json:"abbr,omitempty"`

	// icon overrides the group header's icon, resolved as a dashboard-icons
	// slug (or passed through as-is if it's already a full URL).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Icon *string `json:"icon,omitempty"`

	// description overrides the default hostname-derived description.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	Description *string `json:"description,omitempty"`
}

// BookmarkStatus defines the observed state of Bookmark.
// +kubebuilder:validation:MinProperties=1
type BookmarkStatus struct {
	// conditions represent the current state of the Bookmark resource.
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pbmk
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Available')].status"
// +kubebuilder:printcolumn:name="Dashboard",type=string,JSONPath=".spec.dashboardRef.name"
// +kubebuilder:printcolumn:name="Group",type=string,JSONPath=".spec.group"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// Bookmark is the Schema for the bookmarks API
type Bookmark struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Bookmark
	// +required
	Spec BookmarkSpec `json:"spec"`

	// status defines the observed state of Bookmark
	// +optional
	Status BookmarkStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// BookmarkList contains a list of Bookmark
type BookmarkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Bookmark `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Bookmark{}, &BookmarkList{})
		return nil
	})
}

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// BookmarkEntry is one bookmark's configuration: display fields and link
// target. BookmarkSpec duplicates BookmarkEntry's fields inline for the
// single-bookmark form (one Bookmark object per bookmark, the original wire
// format), and holds a list of them in Bookmarks for the multi-bookmark form
// (one Bookmark object per group, or per whole dashboard).
//
// Nested groups (a group inside another group) are not supported in this
// version of the operator; Group always names a top-level group.
type BookmarkEntry struct {
	// group is the name of the (top-level) group this entry belongs to.
	// Entries sharing a Group are rendered together. In the multi-bookmark
	// form (BookmarkSpec.Bookmarks), an entry that omits group falls back to
	// BookmarkSpec.Group as a shared default (see BookmarkSpec.Entries).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Group string `json:"group,omitempty"`

	// name is the bookmark's display name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Name string `json:"name,omitempty"`

	// href is the bookmark's link target.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	Href string `json:"href,omitempty"`

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

// BookmarkSpec defines the bookmark(s) rendered by the native dashboard.
// Choose exactly one form: the inline single-bookmark form (set name, href,
// and the other bookmark fields directly on spec, unchanged from earlier
// versions of this API), or the multi-bookmark form (set bookmarks, a list
// of one or more bookmark entries) — never both. group is the one inline
// field allowed alongside bookmarks: it becomes the default group for any
// entry that doesn't set its own.
//
// The single-bookmark fields below duplicate BookmarkEntry's rather than
// embedding it, so that existing Go code building a BookmarkSpec{Group: ...,
// Name: ...} literal (the single-bookmark form predates Bookmarks) keeps
// compiling unchanged — an embedded field can't be set by name in a keyed
// struct literal of the embedding type. Entries() is the single place that
// reconciles the two representations.
// +kubebuilder:validation:XValidation:rule="has(self.name) != has(self.bookmarks)",message="exactly one of name (single-bookmark form) or bookmarks (multi-bookmark form) must be set"
// +kubebuilder:validation:XValidation:rule="has(self.bookmarks) || (has(self.name) && has(self.href) && has(self.group))",message="the single-bookmark form requires name, href, and group"
// +kubebuilder:validation:XValidation:rule="!has(self.bookmarks) || self.bookmarks.all(b, has(b.name) && has(b.href))",message="every bookmarks entry must set name and href"
// +kubebuilder:validation:XValidation:rule="!has(self.bookmarks) || has(self.group) || self.bookmarks.all(b, has(b.group))",message="every bookmarks entry must resolve a group: set spec.group as a default, or set group on every entry"
// +kubebuilder:validation:XValidation:rule="!has(self.bookmarks) || (!has(self.href) && !has(self.abbr) && !has(self.icon) && !has(self.description) && !has(self.order))",message="when bookmarks is set, the single-bookmark inline fields (href, abbr, icon, description, order) must be absent; group is still allowed as the default group for entries"
type BookmarkSpec struct {
	// dashboardRef names the Dashboard this Bookmark belongs to.
	// +required
	DashboardRef DashboardRef `json:"dashboardRef"`

	// group is the name of the (top-level) bookmarks.yaml group this entry
	// belongs to (the single-bookmark form), or the default group for any
	// Bookmarks entry that doesn't set its own group (the multi-bookmark
	// form). Entries sharing a Group are rendered together.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Group string `json:"group,omitempty"`

	// name is the bookmark's display name. Set only for the single-bookmark
	// form; mutually exclusive with Bookmarks.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Name string `json:"name,omitempty"`

	// href is the bookmark's link target.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	Href string `json:"href,omitempty"`

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

	// bookmarks defines one entry per bookmark, for a Bookmark object that
	// groups multiple bookmarks (a whole group, or a whole dashboard) into
	// one object instead of one Bookmark per bookmark. group is optional on
	// each entry: an entry without its own group falls back to spec.group as
	// a shared default. Mutually exclusive with the inline single-bookmark
	// fields above (name, href, order, abbr, icon, description).
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=128
	// +listType=atomic
	// +optional
	Bookmarks []BookmarkEntry `json:"bookmarks,omitempty"`
}

// Entries returns the bookmark entries this Bookmark defines, normalized to
// the multi-bookmark form: for the single-bookmark form it returns spec's
// own inline fields as a one-element slice; for the multi-bookmark form it
// returns a copy of Bookmarks with each entry's empty Group replaced by
// spec.group (the shared default).
func (s *BookmarkSpec) Entries() []BookmarkEntry {
	if len(s.Bookmarks) == 0 {
		return []BookmarkEntry{{
			Group:       s.Group,
			Name:        s.Name,
			Href:        s.Href,
			Order:       s.Order,
			Abbr:        s.Abbr,
			Icon:        s.Icon,
			Description: s.Description,
		}}
	}
	entries := make([]BookmarkEntry, len(s.Bookmarks))
	copy(entries, s.Bookmarks)
	for i := range entries {
		if entries[i].Group == "" {
			entries[i].Group = s.Group
		}
	}
	return entries
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

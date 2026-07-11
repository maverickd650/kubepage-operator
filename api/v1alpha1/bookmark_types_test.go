package v1alpha1

import (
	"reflect"
	"testing"
)

const (
	testGroupDeveloper = "Developer"
	testNameGithub     = "Github"
	testHrefGithub     = "https://github.com/"
)

// TestBookmarkSpecEntriesGroupDefaulting verifies that an entry with its own
// Group keeps it, and an entry without one inherits spec.Group.
func TestBookmarkSpecEntriesGroupDefaulting(t *testing.T) {
	spec := &BookmarkSpec{
		Group: testGroupDeveloper,
		Bookmarks: []BookmarkEntry{
			{Name: testNameGithub, Href: testHrefGithub},                            // inherits Group from spec
			{Name: "Wikipedia", Href: "https://wikipedia.org/", Group: "Reference"}, // keeps its own Group
		},
	}

	entries := spec.Entries()
	if len(entries) != 2 {
		t.Fatalf("Entries() = %d entries, want 2", len(entries))
	}
	if entries[0].Name != testNameGithub || entries[0].Group != testGroupDeveloper {
		t.Errorf("entries[0] = %+v, want Name=Github Group=Developer (inherited)", entries[0])
	}
	if entries[1].Name != "Wikipedia" || entries[1].Group != "Reference" {
		t.Errorf("entries[1] = %+v, want Name=Wikipedia Group=Reference (own)", entries[1])
	}

	// Entries() must not mutate spec.Bookmarks itself.
	if spec.Bookmarks[0].Group != "" {
		t.Errorf("spec.Bookmarks[0].Group = %q, want unchanged empty string (Entries() must return a copy)", spec.Bookmarks[0].Group)
	}
}

// TestBookmarkSpecEntriesDeepCopyIsolation guards against a future change
// accidentally aliasing the returned slice's backing array with
// spec.Bookmarks.
func TestBookmarkSpecEntriesDeepCopyIsolation(t *testing.T) {
	spec := &BookmarkSpec{
		Group:     testGroupDeveloper,
		Bookmarks: []BookmarkEntry{{Name: testNameGithub, Href: testHrefGithub}},
	}

	entries := spec.Entries()
	entries[0].Name = "Mutated"

	if spec.Bookmarks[0].Name != testNameGithub {
		t.Errorf("spec.Bookmarks[0].Name = %q, want unchanged %q after mutating Entries() result", spec.Bookmarks[0].Name, testNameGithub)
	}
	if reflect.ValueOf(entries).Pointer() == reflect.ValueOf(spec.Bookmarks).Pointer() {
		t.Error("Entries() returned a slice sharing spec.Bookmarks' backing array")
	}
}

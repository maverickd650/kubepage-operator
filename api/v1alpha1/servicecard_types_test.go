package v1alpha1

import (
	"reflect"
	"testing"
)

const (
	testGroupMedia = "Media"
	testNamePlex   = "Plex"
)

// TestServiceCardSpecEntriesSingleForm verifies the single-card form (no
// Services set) normalizes to a one-element slice built from spec's own
// inline fields.
func TestServiceCardSpecEntriesSingleForm(t *testing.T) {
	href := "https://example.invalid"
	spec := &ServiceCardSpec{
		Group: testGroupMedia,
		Name:  testNamePlex,
		Href:  &href,
	}

	entries := spec.Entries()
	if len(entries) != 1 {
		t.Fatalf("Entries() = %d entries, want 1", len(entries))
	}
	if entries[0].Group != testGroupMedia || entries[0].Name != testNamePlex {
		t.Errorf("entries[0] = %+v, want Group=Media Name=Plex", entries[0])
	}
	if entries[0].Href != &href && (entries[0].Href == nil || *entries[0].Href != href) {
		t.Errorf("entries[0].Href = %v, want %q", entries[0].Href, href)
	}
}

// TestServiceCardSpecEntriesMultiFormGroupDefaulting verifies the multi-card
// form (Services set): an entry with its own Group keeps it, and an entry
// without one inherits spec.Group.
func TestServiceCardSpecEntriesMultiFormGroupDefaulting(t *testing.T) {
	spec := &ServiceCardSpec{
		Group: testGroupMedia,
		Services: []ServiceEntry{
			{Name: testNamePlex},                      // inherits Group from spec
			{Name: "Grafana", Group: "Observability"}, // keeps its own Group
		},
	}

	entries := spec.Entries()
	if len(entries) != 2 {
		t.Fatalf("Entries() = %d entries, want 2", len(entries))
	}
	if entries[0].Name != testNamePlex || entries[0].Group != testGroupMedia {
		t.Errorf("entries[0] = %+v, want Name=Plex Group=Media (inherited)", entries[0])
	}
	if entries[1].Name != "Grafana" || entries[1].Group != "Observability" {
		t.Errorf("entries[1] = %+v, want Name=Grafana Group=Observability (own)", entries[1])
	}

	// Entries() must not mutate spec.Services itself.
	if spec.Services[0].Group != "" {
		t.Errorf("spec.Services[0].Group = %q, want unchanged empty string (Entries() must return a copy)", spec.Services[0].Group)
	}
}

// TestServiceCardSpecEntriesMultiFormDeepCopyIsolation guards against a
// future change accidentally aliasing the returned slice's backing array
// with spec.Services.
func TestServiceCardSpecEntriesMultiFormDeepCopyIsolation(t *testing.T) {
	spec := &ServiceCardSpec{
		Group:    testGroupMedia,
		Services: []ServiceEntry{{Name: testNamePlex}},
	}

	entries := spec.Entries()
	entries[0].Name = "Mutated"

	if spec.Services[0].Name != testNamePlex {
		t.Errorf("spec.Services[0].Name = %q, want unchanged %q after mutating Entries() result", spec.Services[0].Name, testNamePlex)
	}
	if reflect.ValueOf(entries).Pointer() == reflect.ValueOf(spec.Services).Pointer() {
		t.Error("Entries() returned a slice sharing spec.Services' backing array")
	}
}

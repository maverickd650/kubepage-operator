package v1alpha1

import (
	"reflect"
	"testing"
)

const (
	testGroupMedia = "Media"
	testNamePlex   = "Plex"
)

// TestServiceCardSpecEntriesGroupDefaulting verifies that an entry with its
// own Group keeps it, and an entry without one inherits spec.Group.
func TestServiceCardSpecEntriesGroupDefaulting(t *testing.T) {
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

// TestServiceCardSpecEntriesDeepCopyIsolation guards against a future change
// accidentally aliasing the returned slice's backing array with
// spec.Services.
func TestServiceCardSpecEntriesDeepCopyIsolation(t *testing.T) {
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

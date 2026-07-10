package v1alpha1

import (
	"reflect"
	"testing"
)

const (
	testInfoWidgetTypeDatetime  = "datetime"
	testInfoWidgetTypeOpenMeteo = "openmeteo"
)

// TestInfoWidgetSpecEntriesSingleForm verifies the single-widget form (no
// Widgets set) normalizes to a one-element slice built from spec's own
// inline fields.
func TestInfoWidgetSpecEntriesSingleForm(t *testing.T) {
	order := int32(5)
	spec := &InfoWidgetSpec{
		Type:  testInfoWidgetTypeDatetime,
		Order: &order,
	}

	entries := spec.Entries()
	if len(entries) != 1 {
		t.Fatalf("Entries() = %d entries, want 1", len(entries))
	}
	if entries[0].Type != testInfoWidgetTypeDatetime {
		t.Errorf("entries[0].Type = %q, want %q", entries[0].Type, testInfoWidgetTypeDatetime)
	}
	if entries[0].Order != &order {
		t.Errorf("entries[0].Order = %v, want the same pointer as spec.Order", entries[0].Order)
	}
}

// TestInfoWidgetSpecEntriesMultiForm verifies the multi-widget form (Widgets
// set) returns the entries as-is, unlike BookmarkSpec/ServiceCardSpec there
// is no shared-default (Group) field to reconcile — header widgets are a
// flat list.
func TestInfoWidgetSpecEntriesMultiForm(t *testing.T) {
	spec := &InfoWidgetSpec{
		Widgets: []InfoWidgetEntry{
			{Type: testInfoWidgetTypeDatetime},
			{Type: testInfoWidgetTypeOpenMeteo},
		},
	}

	entries := spec.Entries()
	if len(entries) != 2 {
		t.Fatalf("Entries() = %d entries, want 2", len(entries))
	}
	if entries[0].Type != testInfoWidgetTypeDatetime {
		t.Errorf("entries[0].Type = %q, want %q", entries[0].Type, testInfoWidgetTypeDatetime)
	}
	if entries[1].Type != testInfoWidgetTypeOpenMeteo {
		t.Errorf("entries[1].Type = %q, want %q", entries[1].Type, testInfoWidgetTypeOpenMeteo)
	}
}

// TestInfoWidgetSpecEntriesMultiFormDeepCopyIsolation guards against a future
// change accidentally aliasing the returned slice's backing array with
// spec.Widgets.
func TestInfoWidgetSpecEntriesMultiFormDeepCopyIsolation(t *testing.T) {
	spec := &InfoWidgetSpec{
		Widgets: []InfoWidgetEntry{{Type: testInfoWidgetTypeDatetime}},
	}

	entries := spec.Entries()
	entries[0].Type = "mutated"

	if spec.Widgets[0].Type != testInfoWidgetTypeDatetime {
		t.Errorf("spec.Widgets[0].Type = %q, want unchanged %q after mutating Entries() result", spec.Widgets[0].Type, testInfoWidgetTypeDatetime)
	}
	if reflect.ValueOf(entries).Pointer() == reflect.ValueOf(spec.Widgets).Pointer() {
		t.Error("Entries() returned a slice sharing spec.Widgets' backing array")
	}
}

// TestInfoWidgetSpecEntriesEmptyWidgetsFallsBackToSingleForm verifies that an
// explicitly empty (but non-nil in JSON terms, nil in Go terms since
// omitempty) Widgets slice falls back to the single-widget form rather than
// returning zero entries, mirroring len(s.Widgets) == 0's check.
func TestInfoWidgetSpecEntriesEmptyWidgetsFallsBackToSingleForm(t *testing.T) {
	spec := &InfoWidgetSpec{
		Type:    testInfoWidgetTypeDatetime,
		Widgets: nil,
	}

	entries := spec.Entries()
	if len(entries) != 1 {
		t.Fatalf("Entries() = %d entries, want 1 (single-widget form fallback)", len(entries))
	}
	if entries[0].Type != testInfoWidgetTypeDatetime {
		t.Errorf("entries[0].Type = %q, want %q", entries[0].Type, testInfoWidgetTypeDatetime)
	}
}

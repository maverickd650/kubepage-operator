package v1alpha1

import (
	"reflect"
	"testing"
)

const (
	testInfoWidgetTypeDatetime  = "datetime"
	testInfoWidgetTypeOpenMeteo = "openmeteo"
)

// TestInfoWidgetSpecEntries verifies Entries() returns the widgets as-is,
// unlike BookmarkSpec/ServiceCardSpec there is no shared-default (Group)
// field to reconcile — header widgets are a flat list.
func TestInfoWidgetSpecEntries(t *testing.T) {
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

// TestInfoWidgetSpecEntriesDeepCopyIsolation guards against a future change
// accidentally aliasing the returned slice's backing array with
// spec.Widgets.
func TestInfoWidgetSpecEntriesDeepCopyIsolation(t *testing.T) {
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

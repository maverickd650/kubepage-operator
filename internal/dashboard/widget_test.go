package dashboard

import (
	"context"
	"net/http"
	"slices"
	"testing"
)

// stubWidget is a minimal Widget for exercising Register/Lookup directly,
// independent of any real widget's init() registration.
type stubWidget struct{}

func (stubWidget) Poll(context.Context, *http.Client, WidgetConfig) ([]Field, error) {
	return nil, nil
}

// TestRegisterLookupRegisteredTypes exercises the registry directly: every
// real widget type is already registered via init() by the time any test in
// this package runs, so this uses a type name guaranteed not to collide with
// a real one instead of trying to isolate a fresh registry.
func TestRegisterLookupRegisteredTypes(t *testing.T) {
	const testType = "test-stub-widget-type"
	w := stubWidget{}

	if _, ok := Lookup(testType); ok {
		t.Fatalf("Lookup(%q) found a widget before Register was called", testType)
	}

	Register(testType, w)
	t.Cleanup(func() { delete(registry, testType) })

	got, ok := Lookup(testType)
	if !ok {
		t.Fatalf("Lookup(%q) = (_, false), want the registered widget", testType)
	}
	if got != Widget(w) {
		t.Errorf("Lookup(%q) = %#v, want %#v", testType, got, w)
	}

	if !slices.Contains(RegisteredTypes(), testType) {
		t.Errorf("RegisteredTypes() = %v, want it to contain %q", RegisteredTypes(), testType)
	}
	if !slices.IsSorted(RegisteredTypes()) {
		t.Errorf("RegisteredTypes() = %v, want sorted", RegisteredTypes())
	}
}

// TestRegisterPanicsOnDuplicateType protects Register's documented contract:
// a duplicate registration is a programming error (e.g. two widgets'
// init() funcs colliding on the same type string) and must panic loudly at
// startup rather than silently overwriting the first registration, which
// would make the dashboard poll the wrong implementation for that type.
func TestRegisterPanicsOnDuplicateType(t *testing.T) {
	const testType = "test-stub-widget-type-dup"
	Register(testType, stubWidget{})
	t.Cleanup(func() { delete(registry, testType) })

	defer func() {
		if recover() == nil {
			t.Error("Register() did not panic on duplicate registration")
		}
	}()
	Register(testType, stubWidget{})
}

func TestLookupUnknownType(t *testing.T) {
	if _, ok := Lookup("definitely-not-a-registered-widget-type"); ok {
		t.Error("Lookup() of an unregistered type = (_, true), want false")
	}
}

// TestEveryRegisteredWidgetHasASample guards preview mode's --sample-data
// feature against silent drift: every widget registered via Register (i.e.
// every real widget type in internal/dashboard) must also implement Sampler
// and return at least one Field from a zero-value WidgetConfig, or a future
// widget addition would render an empty/error card under --sample-data
// instead of a populated preview. Mirrors
// internal/controller/widget_type_policy_test.go's
// TestRegisteredWidgetTypesCoveredByPolicy, which guards the same registry
// against a different kind of drift (the CRD schema's Enum allow-list).
func TestEveryRegisteredWidgetHasASample(t *testing.T) {
	for _, widgetType := range RegisteredTypes() {
		impl, ok := Lookup(widgetType)
		if !ok {
			t.Fatalf("Lookup(%q) = false right after RegisteredTypes() reported it", widgetType)
		}
		sampler, ok := impl.(Sampler)
		if !ok {
			t.Errorf("widget type %q does not implement Sampler; add a Sample method so --sample-data can preview it", widgetType)
			continue
		}
		if len(sampler.Sample(WidgetConfig{})) == 0 {
			t.Errorf("widget type %q's Sample(WidgetConfig{}) returned no Fields, want at least one", widgetType)
		}
	}
}

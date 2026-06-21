package render

import "testing"

func TestWidgets_Single(t *testing.T) {
	got, err := Widgets([]WidgetInput{
		{Name: "res", Type: "resources", Options: map[string]any{"cpu": true, "memory": true}},
	})
	if err != nil {
		t.Fatalf("Widgets: %v", err)
	}
	assertGolden(t, "widgets_single", got)
}

func TestWidgets_NoOptions(t *testing.T) {
	got, err := Widgets([]WidgetInput{
		{Name: "dt", Type: "datetime"},
	})
	if err != nil {
		t.Fatalf("Widgets: %v", err)
	}
	assertGolden(t, "widgets_no_options", got)
}

// TestWidgets_Ordering covers: widgets are sorted by Order (nil sorts last),
// ties broken by Name, since widgets.yaml's list is ordered but the
// underlying InfoWidget objects aren't.
func TestWidgets_Ordering(t *testing.T) {
	got, err := Widgets([]WidgetInput{
		{Name: "search", Type: "search", Options: map[string]any{"provider": "duckduckgo"}},
		{Name: "res", Type: "resources", Order: ptr(int32(1)), Options: map[string]any{"cpu": true}},
		{Name: "dt", Type: "datetime", Order: ptr(int32(2))},
	})
	if err != nil {
		t.Fatalf("Widgets: %v", err)
	}
	assertGolden(t, "widgets_ordering", got)
}

func TestWidgets_SecretPlaceholderPassthrough(t *testing.T) {
	got, err := Widgets([]WidgetInput{
		{
			Name: "weather",
			Type: "openmeteo",
			Options: map[string]any{
				"key":   "{{HOMEPAGE_FILE_ABC123}}",
				"label": "Home",
			},
		},
	})
	if err != nil {
		t.Fatalf("Widgets: %v", err)
	}
	assertGolden(t, "widgets_secret_placeholder", got)
}

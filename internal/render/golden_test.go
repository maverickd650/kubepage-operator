package render

import (
	"os"
	"path/filepath"
	"testing"
)

// assertGolden compares got against testdata/<name>.golden.yaml. Run with
// UPDATE_GOLDEN=1 to (re)write the golden file from got instead of comparing,
// e.g.: UPDATE_GOLDEN=1 go test ./internal/render/...
func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden.yaml")

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("writing golden file %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file %s (run with UPDATE_GOLDEN=1 to create it): %v", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("rendered output does not match %s (run with UPDATE_GOLDEN=1 to update it)\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

// TestToYAML_Trivial proves the golden-file harness itself works end to end.
// Per-domain renderers (Settings, Services, Bookmarks, Widgets) get their own
// golden tests as each is added in later phases.
func TestToYAML_Trivial(t *testing.T) {
	type fixture struct {
		Title *string `json:"title,omitempty"`
		Order *int32  `json:"order,omitempty"`
	}

	title := "My Dashboard"
	order := int32(1)

	got, err := ToYAML(fixture{Title: &title, Order: &order})
	if err != nil {
		t.Fatalf("ToYAML: %v", err)
	}

	assertGolden(t, "trivial", got)
}

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// TestRunGeneratesJSONSchemasForEveryCRDVersion runs the converter against
// this repo's real config/crd/bases and checks that every generated file is
// valid JSON, is laid out in the CRDs-catalog path convention
// (schemas/<group>/<lowercase-kind>_<version>.json), and carries a
// "$schema" marker.
func TestRunGeneratesJSONSchemasForEveryCRDVersion(t *testing.T) {
	crdDir := filepath.Join("..", "..", "config", "crd", "bases")
	entries, err := os.ReadDir(crdDir)
	if err != nil {
		t.Fatalf("reading %q: %v", crdDir, err)
	}
	if len(entries) == 0 {
		t.Fatalf("no CRD manifests found under %q", crdDir)
	}

	outDir := t.TempDir()
	if err := run(crdDir, outDir); err != nil {
		t.Fatalf("run: %v", err)
	}

	groupDir := filepath.Join(outDir, "page.kubepage.dev")
	files, err := os.ReadDir(groupDir)
	if err != nil {
		t.Fatalf("reading generated group directory %q: %v", groupDir, err)
	}
	if len(files) == 0 {
		t.Fatalf("no schema files generated under %q", groupDir)
	}

	wantKinds := map[string]bool{
		"bookmark_v1alpha1.json":       false,
		"dashboard_v1alpha1.json":      false,
		"dashboardstyle_v1alpha1.json": false,
		"infowidget_v1alpha1.json":     false,
		"servicecard_v1alpha1.json":    false,
	}

	for _, f := range files {
		if _, ok := wantKinds[f.Name()]; ok {
			wantKinds[f.Name()] = true
		}

		raw, err := os.ReadFile(filepath.Join(groupDir, f.Name()))
		if err != nil {
			t.Fatalf("reading %q: %v", f.Name(), err)
		}

		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%q is not valid JSON: %v", f.Name(), err)
		}
		if doc["$schema"] != "http://json-schema.org/draft-07/schema#" {
			t.Errorf("%q missing draft-07 $schema marker, got %v", f.Name(), doc["$schema"])
		}
		if doc["type"] != "object" {
			t.Errorf("%q: expected top-level type \"object\", got %v", f.Name(), doc["type"])
		}
	}

	for name, found := range wantKinds {
		if !found {
			t.Errorf("expected generated schema %q not found in %q", name, groupDir)
		}
	}
}

// minimalCRD is a syntactically complete CRD with one schema-bearing version
// (v1) and one version without a schema (v2legacy), for exercising the
// skip-and-convert paths without depending on the real manifests.
const minimalCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.test
spec:
  group: example.test
  names:
    kind: Widget
    plural: widgets
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
    - name: v2legacy
      served: false
      storage: false
`

func TestRunSkipsNonYAMLEntriesAndSchemalessVersions(t *testing.T) {
	crdDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(crdDir, "widget.yaml"), []byte(minimalCRD), 0o644); err != nil {
		t.Fatal(err)
	}
	// Neither of these is a *.yaml file, so both must be skipped, not parsed.
	if err := os.WriteFile(filepath.Join(crdDir, "notes.txt"), []byte("not yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(crdDir, "subdir.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	if err := run(crdDir, outDir); err != nil {
		t.Fatalf("run: %v", err)
	}

	files, err := os.ReadDir(filepath.Join(outDir, "example.test"))
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if len(files) != 1 || files[0].Name() != "widget_v1.json" {
		t.Fatalf("expected exactly widget_v1.json (v2legacy has no schema), got %v", files)
	}
}

func TestRunFailsWhenOutputDirectoryCannotBeCreated(t *testing.T) {
	crdDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(crdDir, "widget.yaml"), []byte(minimalCRD), 0o644); err != nil {
		t.Fatal(err)
	}
	// A regular file where the output directory should go makes MkdirAll fail.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(crdDir, filepath.Join(blocker, "schemas")); err == nil {
		t.Fatal("expected an error when the output directory cannot be created, got nil")
	}
}

func TestRunFailsWhenSchemaFileCannotBeWritten(t *testing.T) {
	crdDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(crdDir, "widget.yaml"), []byte(minimalCRD), 0o644); err != nil {
		t.Fatal(err)
	}
	// A directory squatting on the target file path makes WriteFile fail
	// while MkdirAll on the group directory still succeeds.
	outDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outDir, "example.test", "widget_v1.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := run(crdDir, outDir); err == nil {
		t.Fatal("expected an error when the schema file cannot be written, got nil")
	}
}

func TestRealMain(t *testing.T) {
	crdDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(crdDir, "widget.yaml"), []byte(minimalCRD), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("success", func(t *testing.T) {
		var stderr strings.Builder
		if code := realMain([]string{"-crd-dir", crdDir, "-out", t.TempDir()}, &stderr); code != 0 {
			t.Fatalf("exit code %d, want 0; stderr: %s", code, stderr.String())
		}
	})

	t.Run("run error", func(t *testing.T) {
		var stderr strings.Builder
		if code := realMain([]string{"-crd-dir", filepath.Join(t.TempDir(), "missing")}, &stderr); code != 1 {
			t.Fatalf("exit code %d, want 1", code)
		}
		if !strings.Contains(stderr.String(), "crd2jsonschema:") {
			t.Errorf("stderr missing error prefix: %q", stderr.String())
		}
	})

	t.Run("bad flag", func(t *testing.T) {
		var stderr strings.Builder
		if code := realMain([]string{"-no-such-flag"}, &stderr); code != 2 {
			t.Fatalf("exit code %d, want 2", code)
		}
	})
}

func TestToJSONSchemaFailsOnUnmarshalableProps(t *testing.T) {
	// apiextensionsv1.JSON marshals its Raw bytes verbatim, so invalid raw
	// JSON in a default value makes json.Marshal fail — the only reachable
	// error path in toJSONSchema.
	props := &apiextensionsv1.JSONSchemaProps{
		Default: &apiextensionsv1.JSON{Raw: []byte("{not json")},
	}
	if _, err := toJSONSchema(props); err == nil {
		t.Fatal("expected an error for unmarshalable schema props, got nil")
	}
}

func TestRunFailsOnMissingCRDDirectory(t *testing.T) {
	if err := run(filepath.Join(t.TempDir(), "does-not-exist"), t.TempDir()); err == nil {
		t.Fatal("expected an error for a nonexistent CRD directory, got nil")
	}
}

func TestRunFailsOnNonCRDYAML(t *testing.T) {
	crdDir := t.TempDir()
	// Valid YAML, but not a CustomResourceDefinition — spec.group and
	// spec.names.kind are absent, which run treats as a hard error rather
	// than silently emitting a bogus schema path.
	if err := os.WriteFile(filepath.Join(crdDir, "not-a-crd.yaml"), []byte("kind: ConfigMap\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(crdDir, t.TempDir()); err == nil {
		t.Fatal("expected an error for a non-CRD manifest, got nil")
	}
}

func TestRunFailsOnMalformedYAML(t *testing.T) {
	crdDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(crdDir, "broken.yaml"), []byte("{not yaml: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(crdDir, t.TempDir()); err == nil {
		t.Fatal("expected an error for malformed YAML, got nil")
	}
}

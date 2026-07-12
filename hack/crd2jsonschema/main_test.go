package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRunGeneratesJSONSchemasForEveryCRDVersion runs the converter against
// this repo's real config/crd/bases and checks that every generated file is
// valid JSON, is laid out in the CRDs-catalog path convention
// (schemas/<group>/<lowercase-kind>_<version>.json), and carries a
// "$schema" marker.
func TestRunGeneratesJSONSchemasForEveryCRDVersion(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot = filepath.Join(repoRoot, "..", "..")

	crdDir := filepath.Join(repoRoot, "config", "crd", "bases")
	entries, err := os.ReadDir(crdDir)
	if err != nil {
		t.Fatalf("reading %q: %v", crdDir, err)
	}
	if len(entries) == 0 {
		t.Fatalf("no CRD manifests found under %q", crdDir)
	}

	outDir := t.TempDir()

	cmd := exec.Command("go", "run", ".", "-crd-dir", crdDir, "-out", outDir)
	cmd.Dir = filepath.Join(repoRoot, "hack", "crd2jsonschema")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running converter: %v\n%s", err, out)
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

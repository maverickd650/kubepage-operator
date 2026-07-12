// Command crd2jsonschema converts the CustomResourceDefinition manifests
// under config/crd/bases into standalone JSON Schema files, one per CRD
// version, laid out the way datreeio/CRDs-catalog and kubeconform's
// -schema-location expect: schemas/<group>/<lowercase-kind>_<version>.json.
//
// The conversion is a passthrough of each version's
// spec.versions[].schema.openAPIV3Schema (already OpenAPI v3 / JSON-Schema-ish,
// since that's what controller-gen emits) with a top-level "$schema" draft-07
// marker added so validators like kubeconform know how to interpret it.
//
// Output defaults to schemas/ at the repo root; see `mise run schemas`.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

func main() {
	crdDir := flag.String("crd-dir", "config/crd/bases", "directory of CustomResourceDefinition YAML manifests to convert")
	outDir := flag.String("out", "schemas", "output directory for the generated JSON Schema files")
	flag.Parse()

	if err := run(*crdDir, *outDir); err != nil {
		fmt.Fprintln(os.Stderr, "crd2jsonschema:", err)
		os.Exit(1)
	}
}

func run(crdDir, outDir string) error {
	entries, err := os.ReadDir(crdDir)
	if err != nil {
		return fmt.Errorf("reading CRD directory %q: %w", crdDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		path := filepath.Join(crdDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %q: %w", path, err)
		}

		var crd apiextensionsv1.CustomResourceDefinition
		if err := yaml.Unmarshal(raw, &crd); err != nil {
			return fmt.Errorf("unmarshalling %q: %w", path, err)
		}

		if crd.Spec.Group == "" || crd.Spec.Names.Kind == "" {
			return fmt.Errorf("%q: missing spec.group or spec.names.kind, not a CustomResourceDefinition?", path)
		}

		if err := os.MkdirAll(filepath.Join(outDir, crd.Spec.Group), 0o755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}

		for _, version := range crd.Spec.Versions {
			if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
				continue
			}

			schema, err := toJSONSchema(version.Schema.OpenAPIV3Schema)
			if err != nil {
				return fmt.Errorf("%s/%s: %w", crd.Spec.Names.Kind, version.Name, err)
			}

			outPath := filepath.Join(outDir, crd.Spec.Group,
				fmt.Sprintf("%s_%s.json", strings.ToLower(crd.Spec.Names.Kind), version.Name))
			if err := os.WriteFile(outPath, schema, 0o644); err != nil {
				return fmt.Errorf("writing %q: %w", outPath, err)
			}
			fmt.Println(outPath)
		}
	}

	return nil
}

// toJSONSchema marshals a CRD version's openAPIV3Schema to formatted JSON,
// injecting a "$schema" draft-07 marker so validators know how to interpret
// it. The rest of the schema is passed through unmodified.
func toJSONSchema(props *apiextensionsv1.JSONSchemaProps) ([]byte, error) {
	body, err := json.Marshal(props)
	if err != nil {
		return nil, fmt.Errorf("marshalling schema to JSON: %w", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("re-decoding schema JSON: %w", err)
	}
	doc["$schema"] = "http://json-schema.org/draft-07/schema#"

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("formatting schema JSON: %w", err)
	}
	return append(out, '\n'), nil
}

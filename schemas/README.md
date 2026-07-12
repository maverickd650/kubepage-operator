# Generated JSON Schemas

**Generated. Do not edit by hand.**

The files under this directory are produced from `config/crd/bases/*.yaml`
by `hack/crd2jsonschema` — regenerate with:

```sh
mise run schemas
```

Any manual edit will be silently overwritten on the next regeneration, and
CI's drift check (`.github/workflows/test.yml`) fails a PR that edits a CRD
type without also committing the regenerated output here.

## Layout

One JSON Schema per CRD version, following the
[datreeio/CRDs-catalog](https://github.com/datreeio/CRDs-catalog) /
kubeconform convention:

```
schemas/<group>/<lowercase-kind>_<version>.json
```

For example, `ServiceCard`'s `v1alpha1` schema lives at
`schemas/page.kubepage.dev/servicecard_v1alpha1.json`.

Each file is a passthrough of the corresponding CRD version's
`spec.versions[].schema.openAPIV3Schema`, with a top-level
`"$schema": "http://json-schema.org/draft-07/schema#"` marker added so
validators know how to interpret it.

## Usage

Point a validator's schema-location template at this layout, e.g.
[kubeconform](https://github.com/yannh/kubeconform):

```sh
kubeconform -strict -schema-location 'schemas/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' my-manifest.yaml
```

or reference a single file directly from a manifest via the
[yaml-language-server](https://github.com/redhat-developer/yaml-language-server)
modeline for editor validation/completion:

```yaml
# yaml-language-server: $schema=schemas/page.kubepage.dev/servicecard_v1alpha1.json
```

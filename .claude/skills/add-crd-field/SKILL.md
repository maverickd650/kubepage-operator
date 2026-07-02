---
name: add-crd-field
description: Change a CRD Go type safely — codegen, CEL cross-field validation, dashboard Deployment wiring if relevant, samples, Helm chart refresh. Use when editing any api/v1alpha1/*_types.go file or its +kubebuilder markers.
user-invocable: true
---

Encodes the codegen dance around any `api/v1alpha1/*_types.go` change (see
`CLAUDE.md`'s CRD table and "never touch" list — generated files are never
hand-edited).

1. Edit `api/v1alpha1/*_types.go` only. Cross-field invariants (e.g. "exactly
   one of X/Y set") go on the type as
   `+kubebuilder:validation:XValidation` CEL markers, not a separate
   `ValidatingAdmissionPolicy` — those are reserved for heuristics that
   can't be expressed as hard schema rules (see
   `config/admission/credential_shaped_value_policy.yaml` for the one
   exception). Follow the enum-naming convention from `CLAUDE.md`:
   affirmative noun field names, never negated verbs (`collapse` not
   `disableCollapse`); PascalCase state values for operator-native toggles;
   a value never repeats the field name.
2. `mise run manifests ::: generate` (mise's multi-task syntax — plain
   `mise run manifests generate` passes `generate` as an argument to the
   `manifests` task and errors) — this regenerates
   `config/crd/bases/*.yaml` and `zz_generated.deepcopy.go`. Commit both;
   never hand-edit either.
3. If the field influences the dashboard Deployment
   (`internal/controller/dashboard_controller.go`), update **both**:
   - `deploymentForDashboard` (builds the desired Deployment spec from the
     `Dashboard`/related CRDs), and
   - the explicit field-by-field comparison in `reconcileDeployment` (it
     deliberately avoids `reflect.DeepEqual` against the live object, since
     API-server defaulting on the stored object would otherwise look like
     permanent drift). A field written in the first but not compared in the
     second means the controller never heals drift on that field; compared
     without matching API-server defaulting means it flaps forever — get
     both or neither.
4. Update `config/samples/` for the changed CRD and any README section that
   documents the field.
5. `mise run helm-chart-refresh`. It prints a
   "upstream drift in preserved templates" diff for
   `config/admission/`-derived Helm templates — review that diff before
   committing `dist/`; it should normally be empty unless kubebuilder/the
   helm plugin itself was upgraded.
6. Run `/preflight` to close out.
7. Commit as `feat(api): ...` (or `fix(api): ...`), adding `!` after the type
   if the schema change is breaking (a required field addition, a removed/
   renamed field, a narrowed enum) — release-please derives a major bump
   from the `!`.

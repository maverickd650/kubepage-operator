# Handoff: security-review.md implementation (PR #64)

This branch (`claude/security-review-recommendations-y1iho1`) implements the
recommendations in `docs/security-review.md`. It was developed in a sandbox
with **network access scoped to only this GitHub repo** — no module proxy,
no container registry, no `kind`/`helm`/`kustomize`/`controller-gen`
binaries. That constrained how the work could be verified locally, and left
some codegen steps for whoever picks this up next to run. This document is
that handoff.

## mise tasks not run, and why

| Task | Status | Why not run here | What's needed |
|---|---|---|---|
| `mise run manifests` | **Not run** | `controller-gen` binary unavailable | Regenerates `config/crd/bases/*.yaml` and `config/rbac/role.yaml` from the `+kubebuilder:*` markers below |
| `mise run generate` | **Not run** | `controller-gen` binary unavailable | Regenerates `**/zz_generated.deepcopy.go` for the new API types below |
| `mise run templ-generate` | **Done** | `go tool templ generate ./...` ran directly (the `templ` tool is vendored via `go tool`, no extra network needed) | Nothing — `internal/dashboard/index_templ.go` is current with `index.templ` |
| `mise run lint` / `lint-fix` | **Not run** | The system has a stock `golangci-lint` binary, but it's a different (older) version than the one `mise` pins, and doesn't support the `custom` subcommand flags this project's `.custom-gcl.yml` (logcheck plugin) needs. Building the pinned+plugin binary needs network. | `go vet ./...`, `gofmt -l`, and `go build ./...` were used as a proxy — all clean — but this is not a substitute for the real lint pass |
| `mise run test` | **Partially run** | Ran everything except the two envtest-backed suites: `internal/controller`'s Ginkgo suite (`TestControllers`) and `internal/dashboard`'s `TestRunStopsAllGoroutinesOnContextCancel`. Both need `KUBEBUILDER_ASSETS` (a real `etcd`/`kube-apiserver` binary pair via `setup-envtest`), which isn't installed and can't be fetched here (`fork/exec /usr/local/kubebuilder/bin/etcd: no such file or directory`). | Run `mise run test` in full once envtest binaries are available |
| `mise run test-e2e` | **Not run locally** | Needs `kind` + Docker, neither available | Verified instead via GitHub Actions CI on pushes to PR #64 — this caught two real bugs, see below |
| `mise run build-installer` / `helm-chart-refresh` | **Not run** | Both depend on `manifests`/`generate` | `dist/install.yaml` and `dist/chart/templates/crd/*.yaml` + `dist/chart/templates/rbac/*.yaml` are stale relative to the API/RBAC changes below |

**Everything that could be checked without those tools was**: `go build ./...`,
`go vet ./...`, `gofmt -l`, and `go test` for every package/test that doesn't
require envtest. All clean as of the last commit on this branch.

## What actually needs regenerating, and why

New API fields were added to `api/v1alpha1/*_types.go` (all hand-written
source, not generated — these are fine as committed):

- `InstanceSpec.Metrics *MetricsSpec`, `.Auth *AuthSpec`,
  `.NetworkPolicy *NetworkPolicySpec`, `.SecretPolicy *string`
  (`api/v1alpha1/instance_types.go`)
- `ServiceWidget.CACert *SecretValueSource` (`api/v1alpha1/serviceentry_types.go`)
- `InfoWidgetSpec.CACert *SecretValueSource` (`api/v1alpha1/infowidget_types.go`)
- `SecretPolicyUnrestricted`/`SecretPolicyLabeled`/`SecretAllowWidgetsLabel`
  constants (`api/v1alpha1/common_types.go`)

These need `mise run generate` (deepcopy) and `mise run manifests` (CRD
YAML) before the new fields are usable via `kubectl apply` against a real
CRD, and before `dist/chart`'s CRD templates match.

A new RBAC marker was added to `internal/controller/instance_controller.go`:

```go
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
```

This needs `mise run manifests` before `config/rbac/role.yaml` (and
`dist/chart/templates/rbac/*.yaml`) actually grants it. Until that's run,
`spec.networkPolicy` on a real cluster will 403 the moment it's used —
`reconcileNetworkPolicy` (`internal/controller/instance_network.go`) already
guards against this by never touching the NetworkPolicy API when
`spec.networkPolicy` is unset, so this only affects Instances that opt into
the field, not every Instance.

### Suggested regeneration checklist

1. `mise run manifests generate` — review the diff to
   `config/crd/bases/page.kubepage.dev_instances.yaml` (new
   metrics/auth/networkPolicy/secretPolicy fields),
   `page.kubepage.dev_serviceentries.yaml` and
   `page.kubepage.dev_infowidgets.yaml` (new `caCert` field),
   `config/rbac/role.yaml` (new `networkpolicies` verbs), and
   `**/zz_generated.deepcopy.go`.
2. `mise run helm-chart-refresh` — review the `dist/chart/` diff (should
   mirror the same CRD/RBAC changes).
3. `mise run lint-fix` — this branch was never run through the real,
   plugin-augmented `golangci-lint`; only `go vet`/`gofmt` stood in for it.
4. `mise run test` and `mise run test-e2e` in full, now that generated
   files are in place.
5. Commit the regenerated files as their own commit for a clean diff before
   merging.

### Not generated, and doesn't need to be

`config/admission/credential_shaped_value_policy.yaml` and everything under
`config/namespace-scoped/` and `config/network-policy/` are hand-written
(not `controller-gen` output), so they're already complete as committed.
One caveat: `config/namespace-scoped/`'s `kustomize build` output was never
run locally (no `kustomize` binary in this sandbox) — its own header comment
already flags this ("verify the output before applying"); worth an explicit
`kustomize build config/namespace-scoped` smoke test before anyone relies on
it.

## Bugs found via CI iteration (already fixed, for context)

Because full local `test`/`test-e2e` wasn't possible, two real bugs only
surfaced once GitHub Actions ran the actual E2E suite against a kind
cluster. Both are already fixed and pushed; noted here so whoever
regenerates/re-verifies doesn't need to rediscover them:

- **`labelsForInstance` digest-length bug** (`47c7a7a`): a digest-pinned
  image reference (`repo@sha256:<64-hex>`) was mis-split by
  `strings.SplitN(image, ":", 2)`, producing a 64-byte
  `app.kubernetes.io/version` label value — over Kubernetes' 63-byte limit,
  failing every dashboard Deployment's admission. Fixed by
  `imageVersionLabel` in `internal/controller/instance_controller.go`.
- **`digestPinnedImage` repo-mismatch bug** (`0b2d872`): digest-pinning
  combined the digest from the manager's own `ContainerStatus.ImageID` with
  the repository from `spec.image` unconditionally. When a locally-loaded
  image (`kind load docker-image`, used by this project's own e2e tests, and
  plausible for anyone self-building/self-hosting without a real registry)
  reports a synthetic import repository in `ImageID`, the combined
  `repo@digest` reference doesn't resolve, and the dashboard pod is stuck
  forever. Fixed in `cmd/main.go` to only digest-pin when the two
  repositories match, falling back to the tag reference otherwise.

This is a good argument for actually running `mise run test-e2e` locally (or
at minimum watching CI closely) after any further changes to the
digest-pinning or labeling code — both bugs were invisible to `go vet`/unit
tests and only showed up against a real cluster.

## Comment cleanup done in this session

Two doc comments over-indexed on the *temporary* "RBAC marker hasn't been
regenerated yet" framing as if that were the permanent justification for a
design choice, which would read as stale/confusing once `mise run manifests`
is actually run. Reworded to describe the real, permanent rationale instead
(least-privilege: an Instance that never opts into `spec.networkPolicy`
should never cause the reconciler to touch the NetworkPolicy API at all,
independent of whether the RBAC grant exists):

- `internal/controller/instance_network.go`, `reconcileNetworkPolicy`'s doc
  comment
- `internal/controller/instance_network_policy_test.go`,
  `TestReconcileNetworkPolicyDisabledSkipsClientEntirely`'s doc comment

No other workaround comments, TODOs, or session-referencing language were
found in the diff (checked via `git diff origin/main...HEAD` for phrases
like "hack", "for now", "couldn't run", "this session", etc.).

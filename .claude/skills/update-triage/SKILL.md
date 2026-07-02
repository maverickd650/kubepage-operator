---
name: update-triage
description: Triage a Renovate major-version PR or a weekly kubebuilder-update-* scaffold branch using this repo's hidden version-coupling knowledge. Use when reviewing a Renovate PR labeled type/major, or a branch named kubebuilder-update-from-*-to-*.
user-invocable: true
---

The value here is the coupling table below — currently scattered across
comments in `.mise/config.toml`, `.custom-gcl.yml`, and workflow files. Check
it before approving any version bump in the left column.

| When this moves | Must also move / re-check |
|---|---|
| `[tools].golangci-lint` (`.mise/config.toml`) | `version:` in `.custom-gcl.yml` must match (CI's lockstep check enforces this); rebuild via `mise run lint` |
| `[tools].go` (`.mise/config.toml`) | `GO_VERSION` in `[env]` (feeds the Dockerfile `GO_VERSION` build-arg) and `go.mod`'s `toolchain` directive |
| `sigs.k8s.io/controller-runtime` (`go.mod`) | `[tools].setup-envtest` tracks it |
| `k8s.io/api` minor (`go.mod`) | the `test`/`test-flaky-check` tasks derive the envtest K8s version from it — confirm `setup-envtest use` can actually serve the new minor before merging |
| `kube-controller-tools` (controller-gen, `.mise/config.toml`) | regenerate: `mise run manifests ::: generate` (NOT `mise run manifests generate` — that passes `generate` as an arg to `manifests` and errors), then `mise run helm-chart-refresh`; commit the regenerated diff |
| `kubebuilder` / helm plugin (`.mise/config.toml`) | after `mise run helm-chart-refresh`, scrutinize the printed preserved-template drift diff, and confirm the task's "never keep" deletions (`dist/chart/templates/extras/`, `.github/workflows/test-chart.yml`) still apply — a plugin upgrade can reintroduce either |

## Procedure — Renovate `type/major` PR

1. Read the upstream release notes for the bumped dependency.
2. Fix any deprecations the major introduces.
3. Cross-check the coupling table above for anything else that needs to move
   in lockstep.
4. Run `/preflight`.
5. Push fixes to the existing Renovate branch (don't open a new PR).

## Procedure — `kubebuilder-update-from-<x>-to-<y>` branch (from `auto_update.yml`)

1. Diff the branch against this repo's known intentional divergences from
   stock kubebuilder scaffolding: mise tasks instead of a Makefile, no
   webhook server, hand-added `config/admission/` (see `CLAUDE.md`).
2. Confirm none of those divergences were silently reverted by the scaffold
   update.
3. Cross-check the coupling table above (a kubebuilder bump commonly drags
   `kube-controller-tools`/the helm plugin with it).
4. Run `/preflight`, including `mise run helm-chart-refresh`.

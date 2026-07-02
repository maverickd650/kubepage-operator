# Project automation plan

Status: **proposal — nothing in this document is implemented yet.**

Goal: reduce the ongoing maintenance cost of this repository by encoding the
recurring, error-prone workflows as automation — primarily [Claude Code
project skills](https://code.claude.com/docs/en/skills) (`.claude/skills/`),
plus a session-start hook and two small CI guards. Each item below states the
manual cost it removes so the list can be prioritized and trimmed.

## 1. What is already automated (don't duplicate)

The repo's CI/dependency automation is mature; the plan builds on it rather
than adding to it:

| Area | Mechanism |
|------|-----------|
| Lint / unit tests / flake-check / e2e (Kind) | `lint.yml`, `test.yml`, `test-e2e.yml`, all via `mise run <task>` (`.mise/config.toml` is the single source of truth, local == CI) |
| Security scanning | `codeql.yaml` (weekly + per-PR), `govulncheck.yaml` |
| Commit hygiene | `commitlint.yml` enforces Conventional Commits on PR titles and every non-merge commit |
| Releases | release-please (`release.yaml`) — changelog/version derived from commit types |
| Dependency updates | Renovate (`.renovaterc.json5`): automerges trusted minors/patches/digests, maps updates to semantic commit types, manages `[tools]` in `.mise/config.toml` via its native mise manager |
| Kubebuilder scaffold updates | `auto_update.yml` runs `kubebuilder alpha update` weekly, pushing a `kubebuilder-update-from-<x>-to-<y>` branch + issue |
| Helm chart regeneration | `mise run helm-chart-refresh` already encodes the tricky parts (Makefile shim, preserved admission templates, deleting the plugin's broken `templates/extras/` and `test-chart.yml`) |

## 2. Where the manual cost actually is (the gaps)

1. **Adding a widget type** — the most common feature request — touches at
   least six places: a new `internal/dashboard/<type>.go` (+ `init()`
   registration), its `_test.go`, the `Enum` marker on `ServiceWidget.Type`
   (`api/v1alpha1/servicecard_types.go`) or `InfoWidgetSpec.Type`
   (`infowidget_types.go`), the mirror lists in
   `internal/controller/widget_type_policy_test.go`, regenerated CRDs
   (`mise run manifests`), regenerated `dist/` artifacts
   (`mise run helm-chart-refresh`), and README docs. Miss one and either
   `TestRegisteredWidgetTypesCoveredByPolicy` fails or — worse — `dist/`
   silently drifts (see gap 5).
2. **CRD schema changes** — every `*_types.go` edit requires the codegen
   dance (`manifests` + `generate`), CEL `XValidation` for cross-field
   invariants, the enum-naming convention from `CLAUDE.md`, sample updates
   in `config/samples/`, a `helm-chart-refresh` with manual review of the
   printed upstream-drift diff, and — if the field feeds the dashboard
   Deployment — matching updates to **both** `deploymentForDashboard` and
   the field-by-field drift comparison in `reconcileDeployment`.
3. **Pre-PR verification** — the PR template's checklist is applied by hand
   every time; the common failure mode (stale generated files) is only
   caught at review, if at all.
4. **Dependency majors and scaffold-update branches** — Renovate automerges
   the safe stream, but majors (`type/major`, no automerge) and the weekly
   `kubebuilder-update-*` branches need a human who knows the repo's hidden
   version couplings (§3.4). That knowledge currently lives in scattered
   comments.
5. **CI does not catch generated-file drift.** `mise run test` *regenerates*
   manifests/deepcopy/templ output before testing, but no workflow runs
   `git diff --exit-code` over the generated paths afterwards (only
   `go.mod`/`go.sum` tidiness is checked). A PR that edits `*_types.go` or a
   `.templ` file without committing the regenerated output passes CI green
   and leaves `config/crd/bases/`, `zz_generated.*.go`, `*_templ.go`, or
   `dist/` stale on `main`.
6. **Claude Code web sessions start cold** — the remote container has no
   `mise`, so an agent session can't run `mise run lint|test` (the only
   supported entry points) without manually bootstrapping the toolchain
   first, every session.

## 3. Proposed skills (`.claude/skills/<name>/SKILL.md`)

Four skills, one per recurring workflow. Skills should stay short —
checklists with exact file paths and task invocations, referencing
`CLAUDE.md`/`AGENTS.md` for background rather than duplicating them (those
files are already loaded; the skill's job is the *ordering* and the
*easy-to-forget* steps).

### 3.1 `/add-widget` — add a new dashboard widget type

Encodes gap 1 as an ordered checklist:

1. Create `internal/dashboard/<type>.go` implementing `Widget`
   (`Poll(ctx, httpClient, cfg)`); model on `grafana.go` for HTTP/JSON
   upstreams. If it reads the Kubernetes API instead, also implement
   `ClusterWidget.PollCluster` (model on `kubemetrics.go`). Self-register
   via `Register("<type>", ...)` in `init()`.
2. Add `<type>_test.go` table tests (helpers in
   `widget_test_common_test.go`).
3. Add the type to the `+kubebuilder:validation:Enum` marker on
   `ServiceWidget.Type` (card widgets) or `InfoWidgetSpec.Type` (header
   widgets).
4. Add it to the matching list in
   `internal/controller/widget_type_policy_test.go`
   (`serviceEntryWidgetTypes` / `infoWidgetPollableTypes`) — the drift test
   fails otherwise.
5. `mise run manifests` (CRD enum), then `mise run helm-chart-refresh`
   (`dist/chart` + `dist/install.yaml` embed CRD copies).
6. Update README's supported-services table; extend
   `config/samples/page_v1alpha1_servicecard.yaml` if it aids review.
7. Finish with `/preflight` (§3.3). Commit as
   `feat(dashboard): add <type> widget`.

### 3.2 `/add-crd-field` — change an API type safely

Encodes gap 2:

1. Edit `api/v1alpha1/*_types.go` only (never generated files). Cross-field
   invariants go on the type as CEL `XValidation` markers, not admission
   policies; follow the enum-naming convention (affirmative noun fields,
   PascalCase state values, no negated verbs).
2. `mise run manifests generate` — commit the regenerated
   `config/crd/bases/*.yaml` and `zz_generated.deepcopy.go`.
3. If the field influences the dashboard Deployment: update
   `deploymentForDashboard` **and** the explicit field list compared in
   `reconcileDeployment` (it deliberately avoids `reflect.DeepEqual`; a
   field written but not compared silently never heals drift, a field
   compared against API-server defaulting flaps forever).
4. Update `config/samples/` and README.
5. `mise run helm-chart-refresh`; review the printed
   "upstream drift in preserved templates" diff before committing `dist/`.
6. `/preflight`; commit `feat(api): ...`, with `!` if the schema change is
   breaking (release-please majors from it).

### 3.3 `/preflight` — run the CI gate locally before pushing

Encodes gap 3; also what the other skills end with:

1. `mise run manifests generate templ-generate`, then check
   `git status --porcelain` — any resulting dirt is uncommitted generated
   output (the drift CI currently misses, gap 5).
2. `go mod tidy` + `git diff --exit-code go.mod go.sum` (mirrors `test.yml`).
3. `mise run lint` (or `lint-fix`), `mise run test`.
4. If `internal/dashboard` concurrency (poller/store) was touched:
   `mise run test-flaky-check`. If controller/Deployment/RBAC/networking
   logic was touched and Docker is available: `mise run test-e2e`.
5. If `config/`, CRDs, or RBAC changed: confirm `dist/` was refreshed
   (`mise run helm-chart-refresh` produces no diff) and `helm lint
   dist/chart` passes.
6. Verify every commit subject matches the commitlint pattern
   (`type(scope)!: description`, allowed types per `CLAUDE.md`).

### 3.4 `/update-triage` — dependency majors & scaffold-update branches

Encodes gap 4. The skill's core value is the **coupling table**, currently
spread across comments in `.mise/config.toml`, `.custom-gcl.yml`, and
workflow files:

| When this moves | Must also move / re-check |
|---|---|
| `[tools].golangci-lint` (mise) | `version:` in `.custom-gcl.yml` must match; rebuild via `mise run lint` |
| `[tools].go` (mise) | `GO_VERSION` in `[env]` (feeds the Dockerfile build-arg) and `go.mod`'s toolchain directive |
| `sigs.k8s.io/controller-runtime` (go.mod) | `[tools].setup-envtest` tracks it |
| `k8s.io/api` minor (go.mod) | envtest binary version is derived from it in the `test` task — confirm `setup-envtest use` can serve the new minor |
| `kube-controller-tools` (controller-gen) | regenerate: `mise run manifests generate`, then `helm-chart-refresh`; commit the regenerated diff |
| `kubebuilder` / helm plugin | after `helm-chart-refresh`, scrutinize the preserved-template drift diff and confirm the task's "never keep" deletions (`templates/extras/`, `test-chart.yml`) still hold |

Procedure: for a `type/major` Renovate PR — read upstream release notes, fix
deprecations, run `/preflight`, push to the Renovate branch. For a
`kubebuilder-update-*` branch — diff against the repo's known intentional
divergences (mise tasks instead of a Makefile, no webhook server, hand-added
`config/admission/`) before accepting scaffold changes.

## 4. Proposed session-start hook (Claude Code on the web)

Encodes gap 6. A `SessionStart` hook (`.claude/hooks/session-start.sh`,
registered in `.claude/settings.json`) that, only when
`CLAUDE_CODE_REMOTE=true`:

1. Installs `mise` if absent, runs `mise trust` + `mise install` (the pinned
   toolchain from `.mise/config.toml` — identical to CI).
2. Appends the mise shims dir to `$CLAUDE_ENV_FILE` so `mise run <task>`
   works for the whole session.
3. Pre-fetches `go mod download` and the envtest binaries
   (`setup-envtest use`), so the first `mise run test` doesn't pay the
   download cost.

Trade-off to decide at implementation time: synchronous (session waits for
the toolchain, no race) vs. async (faster start, first commands may race the
install). Recommend starting synchronous. Deliberately excluded: pre-building
the custom golangci-lint binary (`mise run golangci-lint-custom`) — it is a
multi-minute compile whose output lives in the repo's gitignored `bin/` and
would rebuild every session; better paid lazily on first lint.

## 5. Proposed CI guards (small, close existing holes)

1. **Generated-file drift check** (gap 5): a step in `test.yml` after
   `mise run test` — `git diff --exit-code -- config/ api/ internal/ dist/`
   (or `git status --porcelain` filtered to generated paths). Cheap, and
   turns "reviewer must notice stale CRD YAML" into a red check.
2. **`.custom-gcl.yml` ↔ mise version lockstep** (first row of §3.4's
   table): a one-line assertion in `lint.yml` (or a `lint-config`-adjacent
   mise task) that the two golangci-lint versions match, so a Renovate bump
   of one without the other fails fast instead of building a mismatched
   custom binary.

## 6. Deliberately out of scope

- **Scheduled autonomous agent runs** (cron sessions triaging the Renovate
  dashboard, auto-fixing CI): higher blast radius, needs trust built via the
  skills first. Revisit once `/update-triage` has a track record.
- **Auto-labeling / triage bots for issues**: issue volume doesn't justify
  it yet.
- **Replacing `auto_update.yml` or Renovate behavior**: both work; the
  skills wrap the *human* half of those loops rather than changing them.

## 7. Suggested rollout order

1. `/preflight` + the two CI guards (§3.3, §5) — smallest, immediately
   de-risks every other PR, and the other skills terminate in it.
2. Session-start hook (§4) — makes all agent sessions capable of actually
   running the gate.
3. `/add-widget` (§3.1) — highest-frequency feature workflow.
4. `/add-crd-field` (§3.2) and `/update-triage` (§3.4).

Each step is an independent, reviewable PR; the skills are plain Markdown
checklists, so the cost of a wrong guess is editing a text file.

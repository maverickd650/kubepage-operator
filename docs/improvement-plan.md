# Repository improvement plan (2026-07-11)

A prioritized review of the repository's current state, written so each item
can be picked up independently by a follow-on agent. The codebase itself is
in strong shape — build and `go vet` are clean, coverage excluding generated
code is ~97% (`internal/dashboard`) / ~90% (`internal/controller`), and prior
review cycles (security hardening #64, CRD architecture #70, testing
infrastructure #75, local preview #81, doc drift, and `--sample-data`) have
all been implemented. What remains is the leftover tail of issue #73 plus one
optional feature decision — not code-quality problems.

## Ground rules for whoever picks an item up

- Read `CLAUDE.md` first, then the relevant `.claude/skills/*/SKILL.md`.
- One item per PR. PR titles and every commit are Conventional Commits
  (enforced by commitlint); the suggested commit type is listed per item.
- All CI behavior goes through mise tasks in `.mise/config.toml` — never add
  raw `go test`/`helm` invocations to a workflow.
- Never hand-edit generated files (`config/crd/bases/*.yaml`,
  `zz_generated.*.go`, `*_templ.go`, `dist/` except via
  `mise run helm-chart-refresh`).
- Run `/preflight` before pushing.
- **Suggested agent tier** per item: "standard" items are mechanical enough
  for a mid-tier model (e.g. Sonnet); "advanced" items involve design
  judgment or process-global refactors and warrant a top-tier model
  (e.g. Opus). This is guidance, not a gate.

---

## P3 — Finish issue #73 item 3.1: coverage for `runManager`/`runDashboard`

**Type: `test:` · Effort: large · Agent tier: advanced**

`cmd` package coverage is ~60% with `runManager`, `runDashboard` at 0% —
they are only exercised by e2e, whose coverage can't be extracted. Do NOT
re-derive the constraints: the issue #73 comment dated 2026-07-04 documents
three hard blockers (distroless image has no `tar` so `kubectl cp` can't
extract; Go coverage only flushes at process exit; dashboard pods have no
volume-mount surface in the CRD). Start from its **option (b)**: invoke
`runManager`/`runDashboard` in-process against envtest (the way
`TestOwnDashboardImageAgainstRealAPIServer` in `cmd/main_test.go` already
does for `ownDashboardImage`), so their coverage lands in the normal unit
`cover.out` with no pod plumbing at all.

The known refactor obstacles, to spike first:

- Both functions call `ctrl.SetupSignalHandler()`, which is process-global
  and panics if called twice in one test binary. Refactor to accept a
  `context.Context` parameter (production caller passes
  `ctrl.SetupSignalHandler()`, tests pass a cancellable context).
- Error paths call `os.Exit` — return `error` up to `main()` instead, and
  keep the exit there.
- Assert goroutine hygiene with the existing `goleak` pattern
  (`internal/dashboard/dashboard_test.go`).

If the spike shows this is workable, drop the fallback; if not, the
fallback is the comment's option (a) (manager-only coverage via a Kind
hostPath after graceful scale-down) — but that could not be validated
outside CI last time, so prefer (b). Acceptance: `mise run test` exercises
both functions' startup/shutdown paths and Codecov's `cmd` number rises
accordingly; no change to production behavior.

## P4 — Issue #73 item 2.4: one-off mutation-testing audit

**Type: no PR (issue comment only) · Effort: medium · Agent tier: standard**

At ~97% line coverage, line coverage stops measuring assertion quality. Run
[gremlins](https://github.com/go-gremlins/gremlins) (or `ooze`) locally
against `internal/dashboard` and `internal/controller`, triage surviving
mutants, and post findings as a comment on issue #73 as the issue
prescribes. Strengthen genuinely weak assertions in a follow-up `test:` PR
if the audit finds any. Do **not** wire mutation testing into CI — the
issue explicitly rules that out as too slow.

## P7 — Optional feature decision: `volumes`/`volumeMounts` on `DashboardSpec`

**Type: `feat:` (needs owner sign-off first) · Effort: large · Agent tier: advanced**

Issue #73's 3.1 investigation (option (c)) noted `DashboardSpec` exposes
`spec.env` but no volume surface, which blocks any sidecar/shared-emptyDir
pattern for dashboard pods — coverage extraction was merely the first thing
to trip over it; users wanting to mount CA bundles or custom assets would
be next. This is a real API-surface decision (widens what a Dashboard
author can inject into the pod, interacts with the drift-detection
field-list in `reconcileDeployment`), so **ask the maintainer before
starting**. If approved, use the `/add-crd-field` skill end to end.

---

## Explicitly *not* on the list

Checked and found healthy, so future reviewers don't re-litigate:
generated-file drift (CI-guarded), lint/logcheck setup, secret-handling
design (reviewed and hardened in #63/#64), CRD validation strategy (CEL,
reviewed in #69/#70), e2e/envtest/fuzz/golden/property test infrastructure
(#75), htmx dashboard performance (#68), the release pipeline
(release-please + signed artifacts + SBOM), documentation drift (fixed:
`IMPLEMENTATION_PLAN.md` references, supported-widgets table, K8s floor
wording), and preview mode's `--sample-data` phase (shipped). The
`helm-chart-refresh` backup/restore logic in `.mise/config.toml` looks
fragile but is deliberate, documented in-place, and guarded by
helm-unittest assertions — leave it alone.

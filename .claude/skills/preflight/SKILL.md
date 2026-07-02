---
name: preflight
description: Run the CI gate locally before pushing — regenerate CRDs/deepcopy/templ, check for drift, lint, test, and verify commit hygiene. Use before opening a PR or when asked to check whether changes are ready to push, and as the last step of /add-widget or /add-crd-field.
user-invocable: true
allowed-tools: Read Bash(mise:*) Bash(git:*) Bash(go:*) Bash(helm:*)
---

Encodes the manual verification the PR template's checklist currently asks
for by hand (`.github/PULL_REQUEST_TEMPLATE.md`), and closes the gap where a
PR with stale generated files otherwise passes CI green.

1. `mise run manifests ::: generate ::: templ-generate` (mise's multi-task
   syntax — `mise run manifests generate` is NOT equivalent; it passes
   `generate` as an argument to the `manifests` task and errors), then
   `git status --porcelain`.
   Any dirt here is uncommitted generated output — commit
   `config/crd/bases/*.yaml`, `zz_generated.*.go`, and/or `*_templ.go` before
   continuing.
2. `go mod tidy && git diff --exit-code go.mod go.sum` (mirrors the
   `test.yml` step of the same name).
3. `mise run lint` (or `mise run lint-fix` if there are fixable findings),
   then `mise run test`.
4. Conditional checks, only if the relevant area was touched:
   - `internal/dashboard` (poller/store concurrency) touched →
     `mise run test-flaky-check`.
   - Controller, Deployment-building, RBAC, or networking logic
     (`internal/controller/*`, `dashboard_rbac.go`, `dashboard_network.go`)
     touched, and Docker is available → `mise run test-e2e`.
   - `config/`, any CRD type, or RBAC changed → `mise run helm-chart-refresh`
     should produce no further diff in `dist/chart`/`dist/install.yaml`
     beyond what's already committed, and `helm lint dist/chart` must pass.
5. Verify every commit subject in the branch matches the commitlint pattern:
   `type(scope)!: description`, where `type` is one of `feat`, `fix`, `chore`,
   `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `revert` (see
   `CLAUDE.md`). `commitlint.yml` enforces this on every non-merge commit and
   the PR title.
6. Walk the checklist in `.github/PULL_REQUEST_TEMPLATE.md` and confirm each
   box is genuinely true, not just checked.

Report back a short pass/fail summary per step — don't silently fix
unrelated findings turned up by `lint`/`test` along the way; flag them
instead so the diff stays scoped to the PR at hand.

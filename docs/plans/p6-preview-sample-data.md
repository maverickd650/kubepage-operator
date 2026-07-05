# Implementation plan: P6 — preview `--sample-data` (phase-4 stretch)

Plan for a follow-on agent implementing item **P6** of
`docs/improvement-plan.md`: the `--sample-data` flag for preview mode, so a
local preview renders fully populated cards without any reachable upstream.
Suggested agent tier: **advanced** (per the improvement plan). Everything in
this document was verified against the repository as of 2026-07-05; re-verify
line-level details before editing, but do not re-derive the architecture.

## Read first

1. `CLAUDE.md` — especially "Dashboard package (`internal/dashboard`)" and
   the conventions section.
2. `docs/design/local-preview.md` — the parent design. Phase 4 is the row
   marked "not started"; the "Explicit non-goals" section explains *why* it
   needs its own design: a per-widget notion of representative sample fields.
3. `internal/dashboard/widget.go` — `Widget`, `ClusterWidget`, `Field`
   (including `Percent` and `Highlight`), `Register`/`Lookup`/`RegisteredTypes`.
4. `internal/dashboard/poller.go` — `pollWidget`, `pollInfoWidget`,
   `monitor`; these are the three interception points.
5. `internal/dashboard/preview.go` + `cmd/main.go`'s `runPreview` — how
   preview options flow today (`PreviewOptions` → `serve` → `Poller`).
6. `internal/controller/widget_type_policy_test.go` — the drift-guard test
   pattern this plan reuses for "every widget must provide sample data".
7. `.claude/skills/add-widget/SKILL.md` — must gain a step so future widgets
   ship sample data.

## Process constraints (non-negotiable)

- **Two PRs, design first.** The improvement plan is explicit: write the
  design doc first (`docs:` PR, the way PR #76 preceded #81), get it merged,
  then implement (`feat(preview):` PR). **Do not start the implementation
  before the design PR is merged.**
- Conventional Commits on every commit and PR title (commitlint-enforced).
  PR 1: `docs(preview): design --sample-data mode` (or similar). PR 2:
  `feat(preview): add --sample-data placeholder fields per widget type`.
- Run `/preflight` before every push. If any UI change touches a `.templ`
  file, run `mise run templ-generate` and commit the regenerated
  `*_templ.go`. No `api/v1alpha1` types change in this plan, so
  `mise run manifests generate` should produce no diff — but preflight
  checks that anyway.
- Scope guard: `local-preview.md` phase 4 also mentions client-side CEL
  validation. That is **out of scope** for P6 — `--sample-data` only. Say so
  in the design doc's non-goals.
- The in-cluster `dashboard` subcommand must **not** grow the flag. Sample
  mode is preview-only.

## PR 1 — the design doc

Create `docs/design/preview-sample-data.md`, matching the tone/structure of
`docs/design/local-preview.md` (problem, proposal in one paragraph,
decisions with alternatives considered, testing, phasing). The decisions the
doc must make, with this plan's recommendations:

### D1. Where sample data comes from

Options:

(a) **Per-widget `Sample(cfg WidgetConfig) []Field` method** (recommended) —
    a new optional interface in `widget.go`:

    ```go
    // Sampler is implemented by every registered Widget to supply
    // representative placeholder Fields for preview --sample-data.
    type Sampler interface {
        Sample(cfg WidgetConfig) []Field
    }
    ```

    Taking `cfg` matters for the config-shaped widgets: `customapi` and
    `prometheusmetric` have no fixed field set — their labels come from the
    user's `config.mappings` / query config — so their `Sample` should decode
    `cfg.Config` and emit the *user's own labels* with placeholder values
    (falling back to a canned mapping when config is absent/invalid). This
    keeps the fidelity promise: the preview shows *your* card layout, not a
    generic one.

(b) A static `map[string][]Field` registry populated by a parallel
    `RegisterSample(type, fields)` call — simpler, but can't be config-aware
    and splits each widget's knowledge across two registration calls.

(c) Canned upstream HTTP responses fed through the real `Poll` via a stub
    `http.RoundTripper` — maximum fidelity (exercises the real parse path),
    but requires maintaining one fake API response per upstream, needs a
    parallel mechanism for `ClusterWidget` (fake nodes + metrics objects),
    and the parse paths are already covered by each widget's `_test.go`.
    Reject with rationale in the doc.

Recommendation: **(a)**, keeping CLAUDE.md's "adding a widget = one new
file, no poller/server/store changes" property intact — the sample lives in
the widget's own file, discovered via interface assertion, no per-type
switch anywhere.

Enforcement: `Sampler` stays a *separate* interface (changing `Widget`
itself would break every test double in `widget_test_common_test.go` and
`poller_test.go`), but a new unit test in `internal/dashboard` iterates
`RegisteredTypes()` and fails if any registered widget doesn't implement
`Sampler` or returns empty fields — same drift-guard idea as
`TestRegisteredWidgetTypesCoveredByPolicy` in
`internal/controller/widget_type_policy_test.go`.

### D2. Per-widget-type sample content

The doc should include the authoritative table (one row per registered type;
enumerate from `dashboard.RegisteredTypes()` — currently 19: cloudflared,
customapi, glances, grafana, homeassistant, iframe, kubemetrics, linkwarden,
longhorn, mealie, openmeteo, openweathermap, paperlessngx, plex, prometheus,
prometheusmetric, stash, truenas, unifi). Guidance:

- Reuse each widget's real label constants (`labels.go`:`labelVersion`,
  `labelStreams`, ... and the Status-family constants in `prometheus.go`) so
  samples can't drift from real output labels.
- Values must be **deterministic** (no randomness, no clock reads) so golden
  tests can cover the rendered output.
- Across the full set, deliberately exercise every render affordance:
  at least one sample sets `Field.Percent` (usage bar — `kubemetrics`,
  `glances`, `truenas` are natural), and at least one sets each
  `Highlight` severity (`good`/`warn`/`danger`) so the preview shows the
  stat-chip palette. This is the whole point of the feature: a designer
  iterating on `.templ`/palette changes sees every visual state.
- Special cases to call out explicitly:
  - `iframe`: its `Poll` never contacts the upstream (the *browser* loads
    the URL), so sample mode can simply return what `Poll` would —
    `iframeSrc` = `cfg.URL`, `iframeHeight` from config. The iframe itself
    will still only render if the URL is reachable from the browser; note
    this in the doc.
  - `kubemetrics` (the only `ClusterWidget`): today preview renders its
    error state via `noopClusterReader` (`internal/dashboard/preview.go`).
    Under sample mode it must instead show representative node CPU/memory
    fields with `Percent` and self-set `Highlight`, matching what
    `kubemetrics.go` really emits.
  - `datetime`/`greeting`: not registered widgets (rendered statically by
    the server, see `pollOnce`'s InfoWidget loop) — unaffected, no sample
    needed. State this so nobody "fixes" it.

### D3. Poller integration and semantics

- Plumbing: `SampleData bool` on `dashboard.PreviewOptions` →
  `dashboard.Options` → `Poller` (all in `internal/dashboard`), set only by
  `cmd/main.go`'s `runPreview` from a new `--sample-data` flag.
- Interception points in `poller.go`, all keyed off `p.SampleData`:
  1. `pollWidget`: when sampling, skip secret resolution
     (`resolveSecret`), skip `httpClientForCACert`, skip `impl.Poll`;
     take fields from `impl.(Sampler).Sample(cfg)` instead. **Still run
     `filterFields` + `applyHighlights`** — the user's `fields` filter and
     `highlight` rules are config worth previewing, and sample values
     should be chosen so highlight rules *can* trip.
  2. `pollInfoWidget`: same substitution; for a `ClusterWidget`, `Sample`
     replaces `PollCluster`.
  3. `monitor`: skip the real probe; every configured
     `ping`/`siteMonitor`/`podSelector` reports `Up` with a plausible
     latency (`"12 ms"`; `"2/2 ready"` for podSelector) so status dots/
     pills render populated. Widgets/monitors *not* configured stay absent —
     sample mode fills in data, it doesn't invent cards.
- Skipping secret resolution is a deliberate feature, not an accident:
  GitOps YAML whose `secretKeyRef`s point at in-cluster Secrets previews
  without copying secret material locally. Say so.
- Metrics: don't call `observePoll`/`monitorUp` for sampled polls —
  fabricated success would pollute the (localhost-only, but still real)
  Prometheus metrics. Cheapest shape: early-return sample branches that
  bypass the metric call sites.
- **All-or-nothing, not fallback-on-error.** A "try the real upstream, fall
  back to sample" hybrid is nondeterministic and makes it impossible to know
  whether displayed data is real. Reject it in the doc. (A user who wants
  live data simply omits the flag — that's phases 1–3.)

### D4. Making fake data unmistakable

Recommend a visible "sample data" marker in the page shell when the mode is
on (e.g. next to the version footer in `index.templ`, plumbed via a field on
`Server` or the existing version/commit pattern). Someone screenshotting a
preview must not mistake placeholder values for a live dashboard. This is
the one part of the change that touches a `.templ` file → regenerate and
commit `index_templ.go`.

### D5. Non-goals

Client-side CEL validation (the other phase-4 item — separate effort);
any change to in-cluster dashboard mode; sample data for Ingress/HTTPRoute
discovery cards (the preview loader doesn't load Ingresses at all);
per-widget sample customization by the user.

## PR 2 — implementation (after PR 1 merges)

Work items, roughly in dependency order:

1. `internal/dashboard/widget.go`: add the `Sampler` interface (doc comment
   pointing at the design doc).
2. One `Sample` method per widget file (all 19), each with a table-test
   case in the widget's existing `_test.go` asserting the sample is
   non-empty, deterministic (call twice, `reflect.DeepEqual`), and uses the
   widget's real labels. `customapi`/`prometheusmetric` samples get extra
   cases: with config (labels mirror config) and without (canned fallback).
3. Drift guard in `internal/dashboard/widget_test.go` (or a new file):
   every `RegisteredTypes()` entry implements `Sampler` with non-empty
   output, with a failure message telling the author what to add — mirror
   the message style of `TestRegisteredWidgetTypesCoveredByPolicy`.
4. `internal/dashboard/poller.go`: `SampleData` field + the three
   interception points from D3.
5. `internal/dashboard/dashboard.go` + `preview.go`: plumb
   `Options.SampleData` / `PreviewOptions.SampleData` through `serve` into
   `Poller` (and into `Server` if D4's marker is adopted).
6. `cmd/main.go` `runPreview`: `fs.BoolVar(&sampleData, "sample-data", ...)`;
   pass through `PreviewOptions`. Update the flag's mention in the README
   preview section and the CLI-shape block in `docs/design/local-preview.md`.
7. Poller-level test (pattern: existing `poller_test.go` with
   `fake.NewClientBuilder()`): build a Poller with `SampleData: true` whose
   `HTTPClient` uses a `RoundTripper` that fails the test if invoked —
   proving sample mode makes **zero** network calls — then assert the Store
   holds populated cards for a ServiceCard with widget + monitor +
   `secretKeyRef` pointing at a Secret that does not exist (proving secret
   resolution is skipped, not just surviving).
8. Golden coverage: extend `golden_test.go` with a sampled-cards fixture so
   the rendered sample output is pinned (this is what keeps "deterministic"
   honest).
9. Boot smoke test in `internal/preview/boot_test.go` style: preview against
   `config/samples/` with sample data enabled; assert `GET /fragment`
   eventually contains a known sample value rather than an error state.
10. Docs sweep, same PR:
    - `docs/design/local-preview.md`: phase table row 4 → split status
      (`--sample-data` done, CEL validation still not started) + a landed-as
      note, following the phase-2/3 postscript pattern already in that file.
    - `README.md`: preview section gains the flag with a one-line example.
    - `.claude/skills/add-widget/SKILL.md`: new step — "implement `Sample`
      and add its test; the drift-guard test fails if you skip this".
    - `CLAUDE.md` dashboard-package bullet: amend "Adding a new widget type"
      to mention `Sampler`.
11. `mise run lint-fix test`, then `/preflight`, then push. Optional: run
    `mise run preview -- --sample-data` locally (or via the `/run` skill)
    and eyeball every card renders populated — the golden test pins markup,
    not visual sanity.

## Acceptance criteria

- `go run ./cmd preview -f config/samples --sample-data` renders every
  sample card populated (fields, usage bars, highlight chips, Up monitors)
  with no network egress and no error states, including `kubemetrics`.
- Without the flag, behavior is byte-for-byte unchanged (existing golden +
  preview tests still pass untouched — any diff there is a regression).
- The drift-guard test fails if a future widget registers without a sample.
- In-cluster dashboard mode has no way to enable sampling.
- `mise run test`, `mise run lint`, `/preflight` all green; both PRs'
  commits are Conventional-Commits-clean.

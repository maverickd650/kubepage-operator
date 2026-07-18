---
name: add-widget
description: Add a new dashboard widget type end to end — implementation, tests, CRD enum markers, the widget-type policy list, regenerated CRDs/Helm chart, and README docs. Use when asked to add support for polling a new upstream service/API on the dashboard.
user-invocable: true
allowed-tools: Read Write Edit Bash(mise:*) Bash(git:*) Bash(go:*) Agent
---

Encodes the seven-place checklist for adding a widget type (see
`CLAUDE.md`'s "Dashboard package" section for the
`Widget`/`ClusterWidget`/`Sampler` interfaces). Miss a step and either
`TestRegisteredWidgetTypesCoveredByPolicy` or
`TestEveryRegisteredWidgetHasASample` fails, or `dist/` silently drifts from
`config/` (caught by CI's generated-file drift check, but better to avoid).

1. Create `internal/dashboard/<type>.go` implementing `Widget`
   (`Poll(ctx, httpClient, cfg) ([]Field, error)`) — for a typical GET
   JSON upstream, call `httpwidget.go`'s `fetchJSON(ctx, httpClient, cfg,
   "<type>", path, headers, &out)` (model on `plex.go`/`netdata.go`; it
   covers the URL-required check, endpoint join, request build, and header
   set in one call) or `fetchJSONBasicAuth` for Basic-auth upstreams (model
   on `adguard.go`). Only hand-roll the request (`buildJSONRequest` +
   `doJSONRequest`) for something `fetchJSON` can't express — a POST body
   (`stash.go`), multi-mode auth precedence (`nextcloud.go`), or a non-GET
   transport (`truenas.go`'s WebSocket, `unifi.go`/`proxmox.go`'s per-widget
   TLS client). If the widget reads the Kubernetes API directly instead of
   an HTTP upstream, also implement `ClusterWidget.PollCluster` (model on
   `kubemetrics.go`). Self-register via `Register("<type>", ...)` in an
   `init()` — the poller/server/store need no other changes.
2. Implement `Sampler` on the same type
   (`Sample(cfg WidgetConfig) []Field`): deterministic placeholder `Field`s
   for the preview subcommand's `--sample-data` mode. If the widget's
   display shape depends on its own config (e.g. `customapi`,
   `prometheusmetric`'s configured label), read `cfg.Config` and echo the
   operator's own configured labels back with a placeholder value rather
   than a generic fallback — see those two files for the pattern.
   `TestEveryRegisteredWidgetHasASample` (`widget_test.go`) fails the build
   if this is skipped.
3. Add `internal/dashboard/<type>_test.go` with table tests for both `Poll`
   and `Sample`; reuse helpers from `widget_test_common_test.go` rather than
   duplicating fixture setup.
4. Add `<type>` to the `+kubebuilder:validation:Enum` marker on
   `ServiceWidget.Type` (`api/v1alpha1/servicecard_types.go`) for a
   card widget, or `InfoWidgetSpec.Type` (`api/v1alpha1/infowidget_types.go`)
   for a header-strip widget.
5. Add `<type>` to the matching list in
   `internal/controller/widget_type_policy_test.go` —
   `serviceEntryWidgetTypes` for a `ServiceWidget.Type` addition,
   `infoWidgetPollableTypes` for a pollable `InfoWidgetSpec.Type` addition
   (skip this list only for the two statically-rendered header types,
   `datetime`/`greeting`, which are never `Register`ed).
   `TestRegisteredWidgetTypesCoveredByPolicy` fails otherwise.
6. `mise run manifests` to regenerate the CRD's enum in
   `config/crd/bases/*.yaml`, then `mise run helm-chart-refresh` so
   `dist/chart` and `dist/install.yaml` pick up the new enum value.
7. Update the supported-services table in `README.md`. Optionally extend
   `config/samples/page_v1alpha1_servicecard.yaml` if a sample aids review.
8. Run `/preflight` to close out.
9. Commit as `feat(dashboard): add <type> widget` (or `feat!:` only if this
   removes/renames an existing type — a pure addition is not breaking).

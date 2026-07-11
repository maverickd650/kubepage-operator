# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Kubernetes operator (built with Kubebuilder v4) that serves a small native
dashboard (Go + htmx, single binary, no Node/React build) for self-hosted
services (Plex, Stash, Paperless-ngx, Grafana, Prometheus, UniFi, TrueNAS,
Cloudflared, Linkwarden, Home Assistant, Mealie), entirely driven by CRDs.
The manager binary doubles as the dashboard process: `cmd/main.go` dispatches
on `os.Args[1] == "dashboard"` to run the per-`Dashboard` dashboard server
instead of the controller manager — there is no separate operand image.

Secret-backed widget config (API keys, etc.) is resolved by the dashboard
process itself, in-process, via an uncached client — the plaintext value
never sits in pod env, a ConfigMap, a projected file, or an informer cache;
it only exists in the dashboard pod's memory for the duration of one poll.

## Commands

All commands run through [mise](https://mise.jdx.dev) tasks (`.mise/config.toml`
is the single source of truth — CI uses the exact same tasks). Run `mise tasks`
for the full list.

```sh
mise install            # install pinned toolchain (Go, controller-gen, kustomize, helm, kind, kubectl, golangci-lint...)

mise run manifests      # regenerate CRDs/RBAC/webhook configs from +kubebuilder markers
mise run generate       # regenerate DeepCopy methods (zz_generated.*.go)
mise run templ-generate # regenerate Go from internal/dashboard/*.templ files

mise run lint           # golangci-lint (builds a custom binary with the logcheck plugin first)
mise run lint-fix       # same, with --fix
mise run test           # unit tests: go test $(go list ./... | grep -v /e2e) -race, via envtest (real API server + etcd)
mise run test-e2e       # e2e tests against an ephemeral Kind cluster (creates/deletes it automatically)

mise run build          # go build -o bin/manager ./cmd
mise run run            # go run ./cmd (manager mode, against your current kubeconfig context)
mise run preview        # go run ./cmd preview -f config/samples (no cluster required)
```

`mise run test` depends on `manifests`, `generate`, `templ-generate`, `fmt`,
`vet` — always run through the task rather than calling `go test` directly,
or generated code / CRDs can drift from `*_types.go`.

**Running a single test:** tests are Ginkgo (BDD) for controllers, plain
`testing` + table tests for `internal/dashboard`. After the codegen steps
above, target a package or use Ginkgo focus:

```sh
go test ./internal/controller/... -run TestControllers -v          # whole controller suite
go test ./internal/dashboard/... -run TestGrafana -v                 # one Go test func
KUBEBUILDER_ASSETS="$(setup-envtest use <ver> -p path)" go test ./internal/controller/... --ginkgo.focus="Dashboard Controller"
```

`internal/controller` tests need `KUBEBUILDER_ASSETS` set (envtest binaries) —
`mise run test` derives the K8s version from `k8s.io/api` in go.mod and sets
this up automatically; replicate that if running tests outside the task.

After editing `*_types.go` or any `+kubebuilder` marker, always run
`mise run manifests generate` before `lint-fix test`. After editing a
`.templ` file, run `mise run templ-generate` (the generated `_templ.go` is
committed, like the CRD YAML).

## Architecture

### One binary, three modes: manager, dashboard, preview

`cmd/main.go` is the single entrypoint for all three:
- **Manager mode** (default): the standard Kubebuilder controller-runtime
  manager — reconciles CRDs, creates/owns Deployments/Services/Ingresses/RBAC.
- **Dashboard mode** (`<binary> dashboard --namespace=... --dashboard-name=...`):
  serves the actual dashboard UI. One dashboard Deployment per `Dashboard`,
  running the *same image* the manager is running (resolved at manager
  startup by reading the manager's own Pod spec via the `POD_NAME`/
  `POD_NAMESPACE` downward-API env vars — see `ownDashboardImage` in
  `cmd/main.go` — since there's no way for a pod to look up its own image
  otherwise).
- **Preview mode** (`<binary> preview -f <path>`, `mise run preview`): serves
  the same dashboard UI against CRD YAML loaded from local files instead of a
  live cluster, so a Dashboard's look can be checked without installing the
  operator anywhere. `internal/preview` decodes the files into an in-memory
  `client.Client` (`sigs.k8s.io/controller-runtime/pkg/client/fake`) and
  `dashboard.RunPreview` wires it into the same `Server`/`Poller` dashboard
  mode uses — see `internal/dashboard/dashboard.go`'s `serve` helper, shared
  by both `Run` (real cluster) and `RunPreview` (in-memory), and
  `docs/design/local-preview.md` for the full design.

### CRDs (`api/v1alpha1`) and their relationship

| Kind | Short name | Purpose |
|------|-----------|---------|
| `Dashboard` | `pdash` | Owns the dashboard Deployment, Service, optional Ingress/HTTPRoute, and the per-Dashboard ServiceAccount/Role/RoleBinding the dashboard pod runs as. |
| `DashboardStyle` | `pstyle` | Site-wide look (title, theme, color, background, header style, search box) and optional tab `layout`. Exactly one per Dashboard, enforced by naming the object after the Dashboard it styles (`metadata.name == spec.dashboardRef.name`). |
| `ServiceCard` | `pcard` | One or many service cards (`spec.services`, a group's or a whole dashboard's worth in one object), each optionally with widgets polling that service's API; supports ping/siteMonitor up-down status. |
| `Bookmark` | `pbmk` | One or many static bookmark links (`spec.bookmarks`, a group's or a whole dashboard's worth in one object). |
| `InfoWidget` | `piw` | One or many header-strip widgets (`spec.widgets`, a whole dashboard's header strip in one object): `datetime`, `greeting`, or `openmeteo`, among others. |

Every config CRD (`DashboardStyle`/`ServiceCard`/`Bookmark`/`InfoWidget`)
carries `spec.dashboardRef.name` pointing at the `Dashboard` it belongs to, and
must live in the same namespace as that `Dashboard` (no cross-namespace refs).

`DashboardReconciler` (`internal/controller/dashboard_controller.go`) watches
all of these (via `Watches(...)` + a `mapToDashboard` helper) purely to keep
`Dashboard.status.bound{DashboardStyles,ServiceCards,Bookmarks,InfoWidgets}`
counts current for `kubectl get`/`describe` — it does **not** trigger any
re-render or rollout. The dashboard pod reads the config CRDs live through
its own controller-runtime cache, so a CRD change takes effect on the next
poll cycle with no Dashboard-mediated round-trip.

Cross-field schema invariants (e.g. `SecretValueSource` sets exactly one of
`value`/`secretKeyRef`; a `ServiceCard` sets at most one of `ping`/
`siteMonitor`/`podSelector`; widget `type` is one of the registered set) are
enforced as CEL `+kubebuilder:validation:XValidation` markers directly on the
`api/v1alpha1` types (K8s 1.29+, no webhook server/certs) rather than as
separate `ValidatingAdmissionPolicy` objects — the rules travel with the CRD
and show up in `kubectl explain`. The one thing that *is* still a
`ValidatingAdmissionPolicy` (`config/admission/credential_shaped_value_policy.yaml`,
K8s 1.30+) is a `Warn`-action heuristic — flagging a credential-shaped field
name (`token`, `apiKey`, ...) that uses an inline `value` instead of
`secretKeyRef` — that can't be expressed as a hard schema rule since it's a
naming heuristic, not an invariant. 1.29+/1.30+ are the floors required by
the API surface in use; the CI-tested floor is higher, **1.33** — the
`k8s-compat` workflow only exercises 1.33 since Kind v0.32.0 ships no older
node image, so older versions are expected to work but aren't exercised.

### Controller package (`internal/controller`)

Each CRD has its own reconciler file (`dashboard_controller.go`,
`dashboardstyle_controller.go`, `servicecard_controller.go`,
`bookmark_controller.go`, `infowidget_controller.go`) following the standard
Kubebuilder pattern: fetch → finalizer handling → reconcile owned resources →
update status conditions (`metav1.Condition`, see `conditions.go` for shared
reason constants). `dashboard_controller.go` is the most complex: it builds
the dashboard Deployment (`deploymentForDashboard`), and delegates RBAC/
networking specifics to `dashboard_rbac.go` (per-Dashboard ServiceAccount/
Role/RoleBinding, plus a cluster-scoped Role for the kubemetrics InfoWidget —
the only resource without an owner reference, since a namespaced `Dashboard`
can't own a cluster-scoped object, so it's cleaned up explicitly in the
finalizer) and `dashboard_network.go` (Service/Ingress/HTTPRoute). Gateway API
support is conditional: `cmd/main.go` checks once at startup via discovery
whether `HTTPRoute` is actually installed, and only then does
`DashboardReconciler` watch/manage it — a `Dashboard` with `spec.gateway.enabled`
on a cluster without Gateway API gets a clear `Available=False` condition
instead of crashing the manager.

Deployment drift detection (`reconcileDeployment`) deliberately compares only
the specific fields it derives from the `Dashboard` spec (image, args, ports,
env, probes, resources, security context, labels/annotations) rather than
`reflect.DeepEqual`-ing the whole pod spec — the API server's own defaulting
on the stored object would otherwise look like permanent drift.

### Dashboard package (`internal/dashboard`)

Wired together by `Run()` in `dashboard.go`:
- A namespace-scoped, **cached** controller-runtime client (`cluster.New`)
  reads the bound CRDs (`DashboardStyle`, `ServiceCard`, `Bookmark`,
  `InfoWidget`).
- A separate **uncached** `client.New` reads `Secret`s directly — secret
  contents must never sit in an informer's long-lived cache.
- Another uncached cluster-scoped client (`KubeReader`) serves `ClusterWidget`
  reads (nodes, `metrics.k8s.io`) that don't fit the namespace-scoped cache.
- `Poller` (`poller.go`) runs on its own ticker (`--poll-interval`, default
  15s), independent of HTTP requests, so a slow/unreachable upstream never
  blocks a page load. Each cycle it lists `ServiceCard`/`InfoWidget`,
  resolves secrets, and polls every bound widget concurrently (bounded by
  `maxConcurrentPolls = 8`), writing results into `Store` (`store.go`) and
  pruning anything no longer bound, then publishes to a `Broadcaster`
  (`sse.go`) so any open `GET /events` connection wakes up and checks
  whether the cycle actually changed anything.
- `Server` (`server.go`) serves `GET /` (page shell), `GET /fragment` (card
  grid) and `GET /header` (header-strip widgets) — splitting page load from
  data refresh means the browser tab never reloads on refresh. `GET /events`
  is a Server-Sent Events stream (`handleEvents`) that emits a
  `fragmentChanged`/`headerChanged` event the moment a poll cycle changes
  what one of those two routes would render (compared by the same
  content-hash `writeCachedHTML` uses for its ETag); the page shell's own
  script reacts by re-fetching that route via `htmx.ajax(...)` with
  `hx-swap="morph:innerHTML"` (idiomorph, vendored under
  `assets/idiomorph-ext-*.min.js`) so the DOM is patched in place — scroll
  position and open `<details>` groups survive — instead of replaced
  wholesale. htmx's own `hx-trigger="every Ns"` interval poll on `#cards`/
  `#header` stays wired up as a fallback for a browser without
  `EventSource` and to recover from a dropped SSE connection.
- Widget implementations (`grafana.go`, `plex.go`, `prometheus.go`,
  `kubemetrics.go`, etc.) implement the `Widget` interface (`widget.go`,
  `Poll(ctx, httpClient, cfg) ([]Field, error)`) and self-register via
  `Register(type, impl)` in an `init()`. A widget that reads the Kubernetes
  API instead of an HTTP upstream (only `kubemetrics` today) additionally
  implements `ClusterWidget.PollCluster`, which the poller calls instead of
  `Poll`. Every widget also implements `Sampler.Sample(cfg) []Field` —
  deterministic placeholder `Field`s the preview subcommand's
  `--sample-data` mode uses instead of polling a real upstream (see
  `Poller.SampleData`); `TestEveryRegisteredWidgetHasASample`
  (`widget_test.go`) fails the build if a widget is missing one. **Adding a
  new widget type = add a new file implementing `Widget` + `Sampler`
  (+ `init()` registration) — no changes needed to poller/server/store.**
- `.templ` files (`index.templ`, `header.templ`, `cards.templ`) are
  [templ](https://templ.guide) templates compiled to `*_templ.go` by
  `mise run templ-generate`; edit the `.templ` source, not the generated Go.

### Critical "never touch" files (see `AGENTS.md` for full kubebuilder mechanics)

- `config/crd/bases/*.yaml`, `config/rbac/role.yaml`, `config/webhook/manifests.yaml` — generated by `mise run manifests`
- `**/zz_generated.*.go` — generated by `mise run generate`
- `**/*_templ.go` — generated by `mise run templ-generate` from the matching `.templ`
- `PROJECT` — generated by the `kubebuilder` CLI
- `// +kubebuilder:scaffold:*` marker comments — the CLI injects code at these; never delete them

Scaffold new APIs/webhooks with `kubebuilder create api`/`kubebuilder create webhook`
rather than hand-writing files in `api/` or `internal/controller/` from scratch.

## Conventions

- **Status conditions**: use `metav1.Condition` (via `meta.SetStatusCondition`), not ad-hoc string fields.
- **CRD enum field naming**: field names are affirmative nouns for the thing being controlled (`collapse`, `errorDisplay`, `indexing`), never a negated verb (`disableX`, `hideX`); enum values are states of that thing (`Enabled`/`Disabled`, `Shown`/`Hidden`), and a value never repeats the field name. Avoids double negatives like `disableCollapse: Enabled` (reads as "disabling is enabled"). Operator-native toggles use PascalCase enum values; enums mirroring homepage's own YAML vocabulary (`dot`, `basic`, `light`, `dark`, `row`, ...) stay lowercase.
- **Logging**: Kubernetes message-style — capitalized, no trailing period, past tense, active voice, balanced key-value pairs (`log.Info("Created Deployment", "name", deploy.Name)`); enforced by the `logcheck` golangci-lint plugin (`.custom-gcl.yml`).
- **Reconciliation**: idempotent; re-`Get` before `Update` to avoid "object has been modified" conflicts; owner references for GC of anything that *can* have one; explicit finalizer cleanup only for cluster-scoped resources a namespaced CR can't own.
- **Commit messages / PR titles**: strictly [Conventional Commits](https://www.conventionalcommits.org/) — `type(scope)!: description`, enforced by CI (`commitlint.yml`) on every PR title and non-merge commit. PRs merge with a merge commit (not squashed), so every individual commit message matters for `release-please`'s changelog/version bump. Allowed types: `feat`, `fix`, `chore`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `revert`.

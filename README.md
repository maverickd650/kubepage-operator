# kubepage-operator

[![Tests](https://github.com/maverickd650/kubepage-operator/actions/workflows/test.yml/badge.svg)](https://github.com/maverickd650/kubepage-operator/actions/workflows/test.yml)
[![Lint](https://github.com/maverickd650/kubepage-operator/actions/workflows/lint.yml/badge.svg)](https://github.com/maverickd650/kubepage-operator/actions/workflows/lint.yml)
[![Latest release](https://img.shields.io/github/v/release/maverickd650/kubepage-operator)](https://github.com/maverickd650/kubepage-operator/releases)
[![License](https://img.shields.io/github/license/maverickd650/kubepage-operator)](LICENSE)

A Kubernetes operator that serves a small, native dashboard (Go + htmx, a
single binary, no Node/React build step) for a curated set of self-hosted
services — Plex, Stash, Paperless-ngx, Grafana, Prometheus, UniFi, TrueNAS,
Cloudflared, Linkwarden, Home Assistant, Mealie — driven entirely by CRDs.
Define services, bookmarks, and dashboard look/settings as Kubernetes
objects, and the operator runs a per-`Dashboard` dashboard Deployment that
reads those CRDs directly and polls each service's API on an interval. The
dashboard also ships a homepage-style quick-launch palette (`Ctrl`/`Cmd`+`K`,
or `/`) that fuzzy-jumps to any service or bookmark card, falling back to a
web search — built client-side from the same cards already on the page, so
it needs no extra CRD configuration.

The dashboard process resolves any Secret-backed credentials (a `ServiceCard`
widget's API key, etc.) itself, in-process — the plaintext value never
appears in pod env, a ConfigMap, or a projected file; it only ever exists in
the dashboard pod's memory for the duration of the poll. See
[`CLAUDE.md`](CLAUDE.md) for the full architecture overview and
[`SECURITY.md`](SECURITY.md) for the secret-handling rationale.

### Supported widgets

`ServiceCard`/`InfoWidget` widget `type`s, kept in sync with the registry in
`internal/dashboard/*.go` by
[`TestRegisteredWidgetTypesCoveredByPolicy`](internal/controller/widget_type_policy_test.go):

| Type | Shows | Notable config |
|------|-------|-----------------|
| `plex` | Current Plex stream count | `Secrets["token"]` (Plex `X-Plex-Token`) |
| `stash` | Stash library stats (GraphQL) | `Secrets["token"]` (Stash API key) |
| `paperlessngx` | Paperless-ngx document statistics | `Secrets["token"]` (Paperless API token) |
| `grafana` | Grafana database/version health | `Secrets["token"]` optional Bearer token |
| `prometheus` | Prometheus target health summary | none (open API) |
| `prometheusmetric` | Result of one config-driven PromQL query | `config: {query, label}` |
| `unifi` | UniFi Network controller site health | `Secrets["username"/"password"]`, `config: {site, insecureTLS}` |
| `truenas` | TrueNAS version/uptime | `Secrets["token"]` (TrueNAS API key) |
| `cloudflared` | Cloudflare Tunnel status | `Secrets["token"]`, `config: {accountId, tunnelId}` |
| `linkwarden` | Linkwarden saved-link count | `Secrets["token"]` (Linkwarden API token) |
| `homeassistant` | Home Assistant version/reachability | `Secrets["token"]` (long-lived access token) |
| `mealie` | Mealie recipe count | `Secrets["token"]` (Mealie API token) |
| `customapi` | Arbitrary JSON endpoint, JSONPath-mapped fields | `Secrets["token"]` optional, `config: {mappings: [...]}` |
| `iframe` | An embedded `<iframe>` on the card instead of stat chips | widget `url` is the embed source, `config: {height}` |
| `openweathermap` | Current weather via OpenWeatherMap (header only) | `Secrets["apiKey"]` required, `config: {latitude, longitude, units, label}` |
| `kubemetrics` | Cluster-wide CPU/memory usage (header only) | `config: {cpuLabel, memoryLabel}`; reads the Kubernetes API, not HTTP |
| `glances` | Host CPU/memory usage via Glances (header only) | `config: {apiVersion}` |
| `longhorn` | Aggregate Longhorn cluster storage usage (header only) | none beyond `URL` |
| `openmeteo` | Current weather, keyless (header only) | `config: {latitude, longitude, units, label}` |
| `datetime` | Client-side clock (header only) | static, not polled |
| `greeting` | Static greeting text (header only) | static, not polled |

Every `ServiceWidget`/`InfoWidget` also accepts an optional `caCert`
(`SecretValueSource`) to trust a self-hosted upstream's private CA instead of
a widget-specific `insecureTLS` escape hatch. "(header only)" types are valid
on `InfoWidget` but not `ServiceCard`; all others work on both.

## CRDs

| Kind | Purpose |
|------|---------|
| `Dashboard` (`pdash`) | The dashboard Deployment, Service, optional Ingress, and the per-Dashboard ServiceAccount/Role/RoleBinding the dashboard pod runs as. Every other CRD names one via `dashboardRef`. |
| `DashboardStyle` (`pstyle`) | Title, description, favicon, theme, color, background, card blur, header style, default link target, the header search box, and an optional `layout` arranging Groups into tabs. Exactly one per Dashboard — the object's name must match the Dashboard's name. |
| `ServiceCard` (`pcard`) | One service card (with optional widgets polling that service's API) in a named group. Supports an HTTP `ping`/`siteMonitor` up/down status, per-card link `target`, and `showStats`/`errorDisplay` toggles. |
| `Bookmark` (`pbmk`) | One static bookmark link in a named group. |
| `InfoWidget` (`piw`) | One header-strip widget: `datetime` (client-side clock), `greeting` (static text), `openmeteo` (current weather), or `kubemetrics` (cluster-wide CPU/memory usage). |

Every config CRD (`DashboardStyle`, `ServiceCard`, `Bookmark`, `InfoWidget`)
carries a `dashboardRef.name` naming the `Dashboard` it belongs to, and any
namespace-matching is implicit: they must live in the same namespace as that
`Dashboard`.

### Admission validation

Cross-field invariants — every secret-bearing field (`SecretValueSource`)
sets exactly one of `value` or `secretKeyRef`, a `ServiceCard` sets at most
one of `ping`/`siteMonitor`/`podSelector`, widget `type` is one of the
supported set — are enforced by CEL rules baked directly into the CRD
schemas (**Kubernetes v1.29+**), so a bad config is rejected as a `kubectl
apply` error rather than a broken widget card at poll time. Beyond the
schemas, the operator additionally ships one
[`ValidatingAdmissionPolicy`](config/admission/credential_shaped_value_policy.yaml)
(CEL, no webhook server or certificates to manage) that *warns* (doesn't
reject) when a credential-shaped field name (`token`, `apiKey`, ...) uses an
inline `value` instead of `secretKeyRef` — a naming heuristic rather than a
hard invariant, so it can't live in the schema. This requires **Kubernetes
v1.30+** (`ValidatingAdmissionPolicy` is GA from 1.30); on the Helm chart it
can be turned off with `--set admissionPolicies.enabled=false`. These are
the floors implied by the API surface used; the CI-tested floor is higher —
see [Development](#development).

### Exposing the dashboard

Every `Dashboard` always gets a `Service` (`ClusterIP` by default). Set
`spec.service.type: LoadBalancer` (e.g. for MetalLB) or `NodePort`, and
`spec.service.annotations` for anything the Service type needs (a MetalLB IP
pool, an external-dns hostname, a Tailscale annotation, ...). To expose it
beyond the cluster via a hostname instead, set one of:

- `spec.ingress` — a classic `networking.k8s.io/v1` `Ingress` (`enabled`,
  `host`, `ingressClassName`, `annotations`, `tls.secretName`).
- `spec.gateway` — a Gateway API `HTTPRoute` (`enabled`, `hostnames`,
  `parentRef.{name,namespace,sectionName}`, `annotations`), attached to a
  `Gateway` you manage separately. Only takes effect if the cluster actually
  has Gateway API CRDs installed; the manager checks once at startup
  (`kubectl logs` shows `Gateway API support enabled=...`), and a `Dashboard`
  with `spec.gateway.enabled: true` on a cluster without them reports a clear
  `Available=False` condition rather than the manager crashing.

Both can be set at once (e.g. Ingress for one hostname, Gateway API for
another); neither is required if you're reaching the dashboard via
port-forward or your own externally-managed routing.

**The dashboard has no authentication of its own by default** — see
[SECURITY.md](SECURITY.md#trust-model) before setting `spec.ingress`/
`spec.gateway` on anything beyond a trusted network. `spec.auth.basicAuthSecretRef`
offers a minimal built-in HTTP Basic gate; a real authenticating reverse
proxy in front of `spec.ingress`/`spec.gateway` is the better answer for
anything more than a homelab.

### Hardening opt-ins

Several `Dashboard` spec fields harden the default (single-admin homelab)
trust model further, all off by default so existing `Dashboard`s keep working
unchanged:

| Field | Default | Effect |
|-------|---------|--------|
| `spec.auth.basicAuthSecretRef` | unset (no auth) | Gates every dashboard route except `/healthz` behind HTTP Basic, checked against a bcrypt htpasswd Secret. See [SECURITY.md](SECURITY.md#optional-built-in-authentication). |
| `spec.metrics.enabled` | `Disabled` | Exposes the dashboard's `/metrics` port (9090) on its Service. Off by default since, unlike the manager's own metrics, the dashboard's has no authn/authz — any pod that can reach the Service port would otherwise read per-service up/down status and poll metrics. |
| `spec.networkPolicy.enabled` | `Disabled` | Creates an owner-referenced `NetworkPolicy` scoping which pods may reach the dashboard/metrics ports (`ingressNamespaceSelector`/`metricsNamespaceSelector`) and, when `egressCIDRs` is set, which addresses its pods may reach. |
| `spec.secretPolicy` | `Unrestricted` | Set to `Labeled` to restrict which Secrets a `ServiceCard`/`InfoWidget` widget may reference via `secretKeyRef`/`caCert` to only those carrying the `page.kubepage.dev/allow-widgets: "true"` label — see [SECURITY.md](SECURITY.md#trust-model) for the exfiltration path this closes. |

Per-widget, `ServiceWidget`/`InfoWidget`'s `caCert` field supplies a
PEM-encoded CA certificate (resolved the same way as any other secret-bearing
field) so a self-hosted upstream with a private CA can be verified instead of
falling back to a widget's own `insecureTLS` escape hatch (e.g. `unifi.go`).

### Scheduling

`spec.nodeSelector`, `spec.tolerations`, `spec.affinity`,
`spec.topologySpreadConstraints`, `spec.imagePullSecrets`, and
`spec.priorityClassName` all pass straight through to the dashboard pod
template — useful for mixed-arch homelab nodes, tainted Raspberry Pis, or a
single-node control plane. `spec.replicas` and `spec.containerPort` both
default (`1`, `8080`), so a minimal `Dashboard` needs neither set; see
`spec.replicas`'s doc comment for why scaling past 1 replica isn't a
supported operation given the per-replica polling behavior.

## Quickstart

```sh
# Install the CRDs
mise run install

# Deploy the controller (build/push your own image, or use an already-published one)
IMG=<some-registry>/kubepage-operator:tag mise run deploy

# Apply the sample Dashboard plus one of every config CRD
kubectl apply -k config/samples/
```

The samples under [`config/samples/`](config/samples/) show the minimal shape
of every CRD: [`Dashboard`](config/samples/page_v1alpha1_dashboard.yaml),
[`DashboardStyle`](config/samples/page_v1alpha1_dashboardstyle.yaml),
[`ServiceCard`](config/samples/page_v1alpha1_servicecard.yaml),
[`Bookmark`](config/samples/page_v1alpha1_bookmark.yaml), and
[`InfoWidget`](config/samples/page_v1alpha1_infowidget.yaml). Once applied,
`kubectl get pdash,pstyle,pcard,pbmk,piw` shows their `Ready` status and
bound counts; the dashboard Service is reachable by port-forwarding it
(`kubectl port-forward svc/dashboard-sample 8080:8080`) or by setting
`spec.ingress.enabled: true` on the `Dashboard` to expose it via an Ingress.

### To Uninstall

```sh
kubectl delete -k config/samples/
mise run uninstall   # removes the CRDs
mise run undeploy    # removes the controller
```

## Development

Tooling is managed by [mise](https://mise.jdx.dev) — it pins every tool version
(Go, golangci-lint, controller-gen, kustomize, helm, kind, etc.) in
[`.mise/config.toml`](.mise/config.toml), so local development matches CI
exactly. Docker and access to a Kubernetes cluster are the only other
prerequisites — v1.29+ for the CRDs' own CEL schema validation, v1.30+ to
additionally get the `ValidatingAdmissionPolicy`-based credential-shaped-value
warning (see [Admission validation](#admission-validation)); older clusters
work too with `--set admissionPolicies.enabled=false` on the Helm chart.
Those are the floors required by the API surface in use; the **CI-tested
floor is 1.33** (the `k8s-compat` workflow only exercises 1.33, since Kind
v0.32.0 ships no older node image) — versions between 1.29/1.30 and 1.33 are
expected to work but aren't exercised in CI.

```sh
curl https://mise.run | sh   # one-time: install mise
mise install                 # install the pinned toolchain
mise tasks                   # list available tasks
```

### Build and run

```sh
IMG=<some-registry>/kubepage-operator:tag mise run docker-build docker-push
IMG=<some-registry>/kubepage-operator:tag mise run deploy
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself
> cluster-admin privileges or be logged in as admin.

### Local preview (no cluster required)

To see what a `Dashboard` actually renders as without installing the operator
anywhere, `preview` mode loads `Dashboard`/`DashboardStyle`/`ServiceCard`/
`Bookmark`/`InfoWidget`/`Secret` YAML straight from local files and serves the
same dashboard UI code the in-cluster pod runs:

```sh
mise run preview                      # serves config/samples on :8080
go run ./cmd preview -f ./my-dashboard-manifests --open
```

Widget polling still makes real outbound requests to whatever URLs the loaded
`ServiceCard`s name, so reachable upstreams (e.g. a Grafana on your LAN) show
live data; unreachable ones render their normal error state. Editing and
saving a manifest under `-f` live-reloads it into the running preview — no
restart, no browser reload, just the next poll picking up the change.

Add `--sample-data` to render every widget/monitor with placeholder data
instead — no network calls, no secrets resolved, so you can see how a
`Dashboard` looks fully populated without any upstream reachable at all (a
visible banner marks the page so a screenshot is never mistaken for live
data):

```sh
go run ./cmd preview -f config/samples --sample-data --open
```

See [`docs/design/local-preview.md`](docs/design/local-preview.md) for the
full design.

After editing `*_types.go` or `+kubebuilder` markers, regenerate CRDs/RBAC and
DeepCopy methods, then lint and test:

```sh
mise run manifests generate
mise run lint-fix test
```

See [`AGENTS.md`](AGENTS.md) for the full kubebuilder mechanics this project
follows (project structure, never-hand-edit files, RBAC marker conventions),
and [`CLAUDE.md`](CLAUDE.md) for a higher-level architecture overview
(manager vs. dashboard binary modes, the CRD/controller/dashboard package
relationships).

## Project Distribution

### Namespace-scoped install (optional)

By default the manager holds its RBAC (including `secrets get`, needed to
provision each Dashboard's own scoped Secret access — see
[SECURITY.md](SECURITY.md#supply-chain)) cluster-wide via a `ClusterRole`/
`ClusterRoleBinding`. [`config/namespace-scoped/`](config/namespace-scoped)
is an overlay that instead binds the same `ClusterRole` via a namespaced
`RoleBinding` per watched namespace, paired with the manager's own
`--watch-namespaces` flag, for operators who'd rather not grant that
cluster-wide. See that directory's `kustomization.yaml` for setup steps and
the trade-off it makes (the `kubemetrics` `InfoWidget` needs cluster-scoped
access no matter what, so it doesn't work under this overlay).

### As a YAML bundle (Kustomize)

```sh
IMG=<some-registry>/kubepage-operator:tag mise run build-installer
```

This generates `dist/install.yaml`, containing every resource needed to
install the project (CRDs, RBAC, Deployment) with no other dependencies:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/kubepage-operator/<tag or branch>/dist/install.yaml
```

### As a Helm chart

A Helm chart packaging the CRDs and controller lives under
[`dist/chart`](dist/chart). To install it:

```sh
helm install kubepage-operator ./dist/chart --namespace kubepage-operator-system --create-namespace
```

If you change the project's API, RBAC, or manager manifests, regenerate the
chart:

```sh
kubebuilder edit --plugins=helm/v2-alpha --force
```

**NOTE:** `--force` overwrites `dist/chart`; re-apply any custom values you
had in `dist/chart/values.yaml` or `dist/chart/manager/manager.yaml`
afterwards.

## Contributing

Issues and PRs welcome. Run `mise tasks` for the full list of mise tasks,
and see [`AGENTS.md`](AGENTS.md) before touching generated files or CRD
markers.

More information can be found via the
[Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html).

## License

Apache License 2.0 — see [`LICENSE`](LICENSE).

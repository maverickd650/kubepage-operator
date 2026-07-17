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

> **New here, or configuring a dashboard rather than developing the operator?**
> Start with the task-oriented [Configuration guide](docs/configuration/README.md)
> — a plain-language walkthrough of building a dashboard, with a gentle
> [introduction to widgets](docs/configuration/widgets.md) (the part most people
> find confusing).

### Supported widgets

`ServiceCard`/`InfoWidget` widget `type`s, kept in sync with the registry in
`internal/dashboard/*.go` by
[`TestRegisteredWidgetTypesCoveredByPolicy`](internal/controller/widget_type_policy_test.go):

| Type | Shows | Notable config |
|------|-------|-----------------|
| `plex` | Current Plex stream count | `Secrets["token"]` (Plex `X-Plex-Token`) |
| `stash` | Stash library stats (GraphQL) | `Secrets["token"]` (Stash API key) |
| `paperlessngx` | Paperless-ngx document statistics | `Secrets["token"]` (Paperless API token) |
| `grafana` | Grafana dashboard/datasource/alert counts (matches gethomepage/homepage's `admin/stats`) | `Secrets["username"]`+`Secrets["password"]` (Basic auth) or `Secrets["token"]` (Bearer); needs Grafana server-admin credentials |
| `prometheus` | Prometheus target health summary | none (open API) |
| `prometheusmetric` | Result of one config-driven PromQL query | `config: {query, label}` |
| `unifi` | UniFi Network controller site health | `Secrets["apiKey"]` (Network Integration API key), `config: {site, insecureTLS}` |
| `truenas` | TrueNAS version/uptime | `Secrets["token"]` (TrueNAS API key); uses the WebSocket JSON-RPC API (`/api/current`) |
| `cloudflared` | Cloudflare Tunnel status | `Secrets["token"]`, `config: {accountId, tunnelId}` |
| `linkwarden` | Linkwarden saved-link and collection counts | `Secrets["token"]` (Linkwarden API token) |
| `homeassistant` | Home Assistant version/reachability | `Secrets["token"]` (long-lived access token) |
| `mealie` | Mealie recipe count | `Secrets["token"]` (Mealie API token) |
| `customapi` | Arbitrary JSON endpoint, JSONPath-mapped fields | `Secrets["token"]` optional, `config: {mappings: [...]}` |
| `iframe` | An embedded `<iframe>` on the card instead of stat chips | widget `url` is the embed source, `config: {height}` |
| `sonarr` | Sonarr library/queue size | `Secrets["apiKey"]` (`X-Api-Key` header) |
| `radarr` | Radarr library/queue size | `Secrets["apiKey"]` (`X-Api-Key` header) |
| `jellyfin` | Jellyfin version and active stream count | `Secrets["token"]` (`X-Emby-Token` header) |
| `jellyseerr` | Jellyseerr version and pending request count | `Secrets["apiKey"]` (`X-Api-Key` header) |
| `immich` | Immich library photo/video counts | `Secrets["apiKey"]` (`x-api-key` header) |
| `adguard` | AdGuard Home DNS query/block stats | `Secrets["username"/"password"]` (HTTP Basic auth, not an API key) |
| `pihole` | Pi-hole v6 DNS query/block stats | `Secrets["password"]` (regular or app password); session-based v6 REST API, logs in fresh every poll |
| `uptime-kuma` | Uptime Kuma public status-page monitor up/down counts | `config: {slug}` required; no auth, status page must be published |
| `portainer` | Portainer-managed Docker environment container counts | `Secrets["apiKey"]` (`X-API-Key` header), `config: {endpointId}` required |
| `argocd` | Argo CD application count by sync/health status | `Secrets["token"]` (Bearer token) |
| `gitea` | Gitea version, best-effort total repository count | `Secrets["token"]` (`Authorization: token <token>`) |
| `tautulli` | Tautulli current Plex stream count and bandwidth | `Secrets["apiKey"]` sent as the `apikey` query parameter, not a header |
| `proxmox` | Proxmox VE cluster VM/LXC counts and aggregate CPU/memory usage | `Secrets["username"]` (`user!tokenid`) + `Secrets["password"]` (API token secret), sent as `Authorization: PVEAPIToken=user=secret`; `config: {node, insecureTLS}` |
| `nextcloud` | Nextcloud CPU load, memory usage, free space, and active users | `Secrets["key"]` (`NC-Token` header, preferred) or `Secrets["username"]`+`Secrets["password"]` (Basic auth) |
| `opnsense` | OPNsense firewall CPU/memory usage and WAN interface traffic | `Secrets["username"]`+`Secrets["password"]` (API key/secret as Basic auth); `config: {wan}` (interface name, defaults to `wan`) |
| `netdata` | Netdata active alarm counts (warnings/criticals) | none (open API) |
| `speedtest` | Speedtest Tracker latest download/upload/ping result | `Secrets["apiKey"]` (Bearer token, v2 API only); `config: {version}` (1 or 2, defaults to 1) |
| `gatus` | Gatus endpoint up/down counts from the latest check | none (open API) |
| `openweathermap` | Current weather via OpenWeatherMap (header only) | `Secrets["apiKey"]` required, `config: {latitude, longitude, units, label}` |
| `kubemetrics` | Cluster-wide CPU/memory usage (header only) | `config: {cpuLabel, memoryLabel}`; reads the Kubernetes API, not HTTP |
| `glances` | Host CPU/memory usage via Glances (header only) | `config: {url, apiVersion}` |
| `longhorn` | Aggregate Longhorn cluster storage usage (header only) | `config: {url}` (Longhorn Manager base URL) required |
| `openmeteo` | Current weather, keyless (header only) | `config: {latitude, longitude, units, label}` |
| `datetime` | Client-side clock (header only) | static, not polled |
| `greeting` | Static greeting text (header only) | static, not polled |
| `logo` | A static logo image in the header (header only) | `icon` for the image; static, not polled; `config: {href}` optionally links it |

Every `ServiceWidget`/`InfoWidget` also accepts an optional `caCert`
(`SecretValueSource`) to trust a self-hosted upstream's private CA instead of
a widget-specific `insecureTLS` escape hatch. The two widget types are
disjoint sets: "(header only)" types are valid on `InfoWidget` and not
`ServiceCard`; every other type in the table is `ServiceCard`-only.

Never put a credential in a widget's `url` as a query parameter (e.g.
`?apikey=...`) — use the `Secrets`/`secretKeyRef` fields in the table above
instead. A poll failure renders the raw upstream error text on the card, and
Go's HTTP client errors include the full request URL, so a URL-embedded
credential leaks to any dashboard viewer the moment that request fails. See
[SECURITY.md](SECURITY.md#trust-model) for the full trust model.

## CRDs

| Kind | Purpose |
|------|---------|
| `Dashboard` (`pdash`) | The dashboard Deployment, Service, optional Ingress, and the per-Dashboard ServiceAccount/Role/RoleBinding the dashboard pod runs as. Every other CRD names one via `dashboardRef`. Its optional `spec.style` carries the look: title, description, favicon, theme, color, background, card blur, header style, default link target, the header search box, and an optional `layout` arranging Groups into tabs. |
| `ServiceCard` (`pcard`) | One or many service cards (`services`) in a named group, each with optional widgets polling that service's API. Supports an HTTP `monitor` up/down status (a URL, or `self` to probe the entry's own `internalUrl`/`href`) *and*, independently, a Kubernetes pod-readiness status (`app`/`podSelector`, homepage parity) — both may be set at once for two status lights on one card. Widgets without their own `url` inherit the entry's base URL (`internalUrl`, else `href`). Also supports per-card link `target` and `showStats`/`errorDisplay` toggles. |
| `Bookmark` (`pbmk`) | One or many static bookmark links (`bookmarks`) in a named group, each with an optional per-bookmark link `target`. |
| `InfoWidget` (`piw`) | One or many header-strip widgets (`widgets`): `datetime` (client-side clock), `greeting` (static text), `logo` (static header logo image), `openmeteo` (current weather, keyless), `openweathermap` (current weather via OpenWeatherMap), `glances` (host CPU/memory usage), `longhorn` (aggregate Longhorn cluster storage usage), or `kubemetrics` (cluster-wide CPU/memory usage). |

Every config CRD (`ServiceCard`, `Bookmark`, `InfoWidget`) carries a
`dashboardRef.name` naming the `Dashboard` it belongs to, and any
namespace-matching is implicit: they must live in the same namespace as that
`Dashboard`.

### Admission validation

Cross-field invariants — every secret-bearing field (`SecretValueSource`)
sets exactly one of `value` or `secretKeyRef`, a `ServiceCard` entry's
`monitor: self` requires an `internalUrl` or `href` to resolve against (the
pod monitor `app`/`podSelector` is freely combinable with `monitor`), widget
`type` is one of the
supported set — are enforced by CEL rules baked directly into the CRD
schemas (**Kubernetes v1.31+** — most rules only need v1.29, but the
`egressCIDRs` rule uses the `isCIDR()` CEL function added in 1.31, and an
older apiserver rejects the CRD at install time), so a bad config is
rejected as a `kubectl
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

Widget `config`/`options` blocks (`ServiceWidget.Config`,
`InfoWidgetEntry.Options`) are `PreserveUnknownFields` JSON, so a bad key
inside them can't be caught by the CRD schema the way the invariants above
are. Instead, `ServiceCardReconciler`/`InfoWidgetReconciler` validate each
widget's block against its type's known required/optional keys on every
reconcile: a missing required key (e.g. `cloudflared` without `tunnelId`)
sets `Available=False` with reason `InvalidWidgetConfig`; a key that isn't
recognized for that type (a likely typo, e.g. `acountId`) sets a separate
`ConfigValid=False` condition with reason `UnknownConfigKeys` — deliberately
without flipping `Available`, since an unrecognized key may just be
forward-compatible with a newer operator version. `kubectl describe
pcard`/`piw` on a misconfigured object names the offending entry, widget
type, and keys directly in the condition message.

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
  with `spec.gateway.enabled: Enabled` on a cluster without them reports a clear
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

### Discovering services automatically

Instead of (or alongside) explicit `ServiceCard` objects, a `Dashboard` can opt
into homepage-style annotation discovery: set `spec.discovery.enabled: Enabled`
and the dashboard process scans its own namespace for annotated `Ingress`/
`HTTPRoute` objects and renders one card per match, with no `ServiceCard`
required.

- `spec.discovery.sources` selects which resource kinds are scanned: `Ingress`,
  `HTTPRoute`, or both. Defaults to `[Ingress]` (unset behaves exactly like
  `[Ingress]`). `HTTPRoute` requires Gateway API CRDs on the cluster — same
  as `spec.gateway` above, a `Dashboard` requesting it without them gets a
  clear `Available=False` condition rather than the dashboard pod crashing or
  silently dropping the source.
- `spec.discovery.annotationPrefix` is the annotation key prefix a resource
  must carry to be discovered (defaults to `kubepage.io/`), e.g.
  `kubepage.io/enabled: "true"`, `kubepage.io/name`, `kubepage.io/group`
  (may itself be a `"/"`-separated path, see [Nested groups](#nested-groups)),
  `kubepage.io/icon`, `kubepage.io/description`, `kubepage.io/href`,
  `kubepage.io/monitor` (homepage's own `ping` name for the same flag is
  also honored, under either prefix, so `homepageCompat` and half-migrated
  annotations keep working).
- `spec.discovery.homepageCompat: true` additionally honors homepage's own
  `gethomepage.dev/*` annotations (https://gethomepage.dev/configs/kubernetes/)
  on any resource that doesn't carry the native prefix's own enable
  annotation, so a cluster migrating from homepage doesn't need to relabel
  everything first.
- `spec.discovery.namespaces` additionally scans a static list of namespaces
  beyond the `Dashboard`'s own — the common homelab shape of one dashboard
  namespace and apps spread across `media`/`monitoring`/`network`/etc. Off
  by default (a `Dashboard`'s discovery scans only its own namespace, the
  same blast radius as every other config CRD). Setting it widens the
  dashboard pod's RBAC: the controller creates a RoleBinding *in each named
  namespace*, granting the dashboard's ServiceAccount read-only access to
  Ingresses/HTTPRoutes there — never a `ClusterRoleBinding`, so access never
  extends beyond the namespaces actually listed. See
  [SECURITY.md](SECURITY.md)'s trust model before using this on a shared
  cluster.

When no `href` annotation is set, the card links to the resource's own first
hostname (`Ingress` rule host, or `HTTPRoute` hostname) — `https://` if an
`Ingress` has a matching TLS entry, `https://` unconditionally for
`HTTPRoute` (TLS termination there is the attaching `Gateway`'s concern, not
the route's).

Discovery annotations are scoped to what's safe on a resource anyone with
read access to it can also read: **no secrets and no widget config** — only
`href`/`icon`/`description`/`group`/`monitor`. For a card with a polled widget
(API key, metrics, ...), use an explicit `ServiceCard` instead. Note there is
currently no de-duplication between an explicit `ServiceCard` and a
discovered `Ingress`/`HTTPRoute` that would render under the same
group/name — both show up as separate cards, so avoid annotating a resource
for discovery if you already have a `ServiceCard` for the same service.

### Nested groups

`ServiceCard`/`ServiceEntry`'s `group` field (and the discovery annotation's
`<prefix>group`) accepts a `"/"`-separated path, nesting a group inside one or
more parent groups — parity with homepage's
[nested groups](https://gethomepage.dev/configs/services/#nested-groups):

```yaml
apiVersion: page.kubepage.dev/v1alpha1
kind: ServiceCard
metadata:
  name: media
spec:
  dashboardRef:
    name: dashboard-sample
  services:
    - name: Plex
      group: Media           # a top-level group, same as always
      href: http://plex.example.com
    - name: Radarr
      group: Media/Movies    # nests "Movies" inside "Media"
      href: http://radarr.example.com
    - name: Sonarr
      group: Media/TV        # a sibling subgroup, also under "Media"
      href: http://sonarr.example.com
```

`Media` renders as a parent group; `Movies`/`TV` render as collapsible
subgroups nested inside it, so collapsing `Media` hides both. `Media` doesn't
need a card of its own — if every entry only ever sets `Media/...`, the
dashboard still renders an (empty-of-direct-cards) `Media` parent so the
subgroups have somewhere to nest.

A Dashboard's `spec.style.layout` can style a subgroup the same way it styles
a top-level group, by giving `groups[].name` the same path:

```yaml
apiVersion: page.kubepage.dev/v1alpha1
kind: Dashboard
metadata:
  name: dashboard-sample
spec:
  style:
    layout:
      - name: Apps
        groups:
          - name: Media          # places the whole Media group (+ subtree) in this tab
            style: row
          - name: Media/Movies   # styles only the Movies subgroup — doesn't place it
            columns: 2
```

Rules to know:

- **Depth is capped at 3 levels** (`a`, `a/b`, or `a/b/c`; a 4th segment is
  rejected by the CRD schema) — deeper hierarchies belong in tabs instead.
- **Tab placement is root-only.** A `layout` tab entry places a group by its
  *root* name only; a nested subgroup always renders in whatever tab its root
  is placed in. A path-named `groups[].name` entry (like `Media/Movies`
  above) only styles that subgroup — it never places anything on its own, and
  the apiserver rejects a tab that lists a path entry without also listing
  its parent — and so, transitively, its root — in the same tab (there'd be
  no unambiguous way to say which tab a dangling subgroup belongs to).
- **Direct cards render before subgroups.** A parent group's own cards (e.g.
  a card whose `group` is exactly `Media`, not `Media/Movies`) always render
  above its nested subgroups — there's no ordering field spanning cards and
  subgroups together, unlike homepage's YAML list order.
- **A literal `"/"` in a pre-existing group name now means nesting.** If
  you're upgrading from a version of this operator that predates nested
  groups and happened to have a `"/"` in a group name (unusual, but the old
  schema allowed it), that group now renders as two nested levels instead of
  one flat name — rename it (e.g. to `"Media - Movies"`) to keep the old flat
  rendering.

Bookmark groups are **not** nested — homepage has no nested-bookmark-group
equivalent, so `BookmarkEntry.Group` stays a flat, top-level-only name.

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

`spec.widgetDefaults` (homepage's `providers:` block, equivalent) supplies
per-widget-type default `secrets`/`caCert` values, keyed by widget type: a
`ServiceCard`/`InfoWidget` widget of that type that doesn't set a given
secret field itself inherits the default for it; a widget's own value always
wins. One OpenWeatherMap API key can then serve every `openweathermap`
widget bound to a Dashboard, with none of them repeating their own `secrets`
stanza — see [config/samples/page_v1alpha1_dashboard.yaml](config/samples/page_v1alpha1_dashboard.yaml)
for a worked example. Defaults resolve under the exact same `secretPolicy`
rules and dashboard-pod RBAC as a widget's own `secretKeyRef` — see
[SECURITY.md](SECURITY.md#trust-model).

### Scheduling

`spec.nodeSelector`, `spec.tolerations`, `spec.affinity`,
`spec.topologySpreadConstraints`, `spec.imagePullSecrets`,
`spec.priorityClassName`, and `spec.volumes`/`spec.volumeMounts` all pass
straight through to the dashboard pod template — useful for mixed-arch
homelab nodes, tainted Raspberry Pis, a single-node control plane, or
mounting a cluster-wide CA bundle / custom background and logo assets into
the dashboard container. `spec.replicas` and `spec.containerPort` both
default (`1`, `8080`), so a minimal `Dashboard` needs neither set; see
`spec.replicas`'s doc comment for why scaling past 1 replica isn't a
supported operation given the per-replica polling behavior. If you do run
more than one replica for availability rather than throughput, pair it with
a `PodDisruptionBudget` targeting the dashboard pod's labels — the operator
doesn't create one for you.

## Quickstart

The fastest path: install the published Helm chart, which brings the CRDs
and controller in one release, no image build required.

```sh
helm install kubepage-operator oci://ghcr.io/maverickd650/charts/kubepage-operator \
  --namespace kubepage-operator-system --create-namespace

# Apply the sample Dashboard plus one of every config CRD (no local checkout needed)
kubectl apply -k 'github.com/maverickd650/kubepage-operator/config/samples?ref=main'
```

To build from source instead (e.g. for local development):

```sh
# Install the CRDs
mise run install

# Deploy the controller (build/push your own image, or use an already-published one)
IMG=<some-registry>/kubepage-operator:tag mise run deploy

# Apply the sample Dashboard plus one of every config CRD
kubectl apply -k config/samples/
```

The samples under [`config/samples/`](config/samples/) show the minimal shape
of every CRD: [`Dashboard`](config/samples/page_v1alpha1_dashboard.yaml)
(including a `spec.style` block),
[`ServiceCard`](config/samples/page_v1alpha1_servicecard.yaml),
[`Bookmark`](config/samples/page_v1alpha1_bookmark.yaml), and
[`InfoWidget`](config/samples/page_v1alpha1_infowidget.yaml). Once applied,
`kubectl get pdash,pcard,pbmk,piw` shows their `Ready` status and
bound counts, plus a `URL` column on `pdash` itself (derived from
`spec.ingress`/`spec.gateway`, falling back to the dashboard Service's
cluster-internal DNS name — see `status.url`); the dashboard Service is
reachable by port-forwarding it (`kubectl port-forward svc/dashboard-sample
8080:8080`) or by setting `spec.ingress.enabled: Enabled` on the `Dashboard`
to expose it via an Ingress.

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
prerequisites — v1.31+ for the CRDs' own CEL schema validation (most rules
only need v1.29, but the `egressCIDRs` rule uses `isCIDR()`, added in 1.31),
v1.30+ to additionally get the `ValidatingAdmissionPolicy`-based
credential-shaped-value warning (see
[Admission validation](#admission-validation)), which can be turned off with
`--set admissionPolicies.enabled=false` on the Helm chart.
Those are the floors required by the API surface in use; the **CI-tested
floor is 1.33** (the `k8s-compat` workflow only exercises 1.33, since Kind
v0.32.0 ships no older node image) — versions between 1.31 and 1.33 are
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
anywhere, `preview` mode loads `Dashboard`/`ServiceCard`/`Bookmark`/
`InfoWidget`/`Secret` YAML straight from local files and serves the same
dashboard UI code the in-cluster pod runs:

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

By default the manager holds its RBAC (including `secrets get;list;watch` —
`get` to provision each Dashboard's own scoped Secret access, `list;watch`
for a metadata-only watch that keeps `spec.secretPolicy: Labeled` Role
grants current when a Secret's `allow-widgets` label changes, never Secret
contents — see [SECURITY.md](SECURITY.md#supply-chain)) cluster-wide via a `ClusterRole`/
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

Every release publishes a signed OCI Helm chart to
`oci://ghcr.io/maverickd650/charts/kubepage-operator` — see the Quickstart
above. To install from a local checkout instead (e.g. an unreleased chart
change), the same chart lives under [`dist/chart`](dist/chart):

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

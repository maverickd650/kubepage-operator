# Combined HTTP + pod monitor status (homepage parity)

Status: shipped — implemented as described below.

## Goal

Let one `ServiceCard` services entry carry **both** an HTTP monitor
(`ping`/`siteMonitor`) **and** a Kubernetes pod monitor at the same time, and
let the pod monitor locate pods the way
[gethomepage/homepage](https://gethomepage.dev/configs/kubernetes/) does — by
`namespace` + `app` name, with `podSelector` as the explicit override.

Today `ServiceEntry` enforces *at most one of* `ping`/`siteMonitor`/
`podSelector` (a CEL `XValidation` on the struct), a single `Card.Status`
slot carries whichever probe ran, and pod status is same-namespace only
(`podStatus` lists pods through the namespace-scoped cached `Reader`, RBAC
granted by `dashboardPodsRule` in the per-Dashboard Role).

### Homepage behavior being mirrored

From homepage's `docs/configs/kubernetes.md` and its
`/api/kubernetes/status/[...service].js` endpoint:

- A service names `namespace` + `app`; pods are located with the label
  selector `app.kubernetes.io/name=<app>`.
- `podSelector`, when set, **overrides** the `app`-derived selector entirely
  (homepage's is a raw selector string; ours stays a typed
  `metav1.LabelSelector`).
- Pod status is three-valued: **running** (all pods up), **partial** (some),
  **down** (none) — plus "not found" when nothing matches.
- The Kubernetes status indicator and the `siteMonitor`/`ping` indicator are
  independent card elements: a service configured with both shows both.

Two deliberate deviations, both already established in this repo:

1. **Readiness, not phase.** Homepage checks pod `status.phase ∈
   {Running, Succeeded}`; our `podReady` checks the `Ready` condition, which
   is stricter (a pod failing its readiness probe is Running but not Ready).
   Keep readiness — it's the more truthful "is this service actually up".
2. **Typed selector.** `podSelector` stays a `metav1.LabelSelector`, not a
   homepage-style free-form selector string.

## API changes (`api/v1alpha1/servicecard_types.go`)

New optional fields on `ServiceEntry`, sitting next to `podSelector`:

```go
// app locates this service's pods by the standard
// app.kubernetes.io/name=<app> label (homepage parity), a shorthand for
// the equivalent podSelector. podSelector, when also set, wins.
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=63
// +optional
App *string `json:"app,omitempty"`

// namespace is the namespace the pod monitor (app/podSelector) lists pods
// in, defaulting to this ServiceCard's own namespace. A namespace other
// than the ServiceCard's requires the Dashboard to allow it via
// spec.monitorNamespaces (see DashboardSpec) — otherwise the entry's
// status renders Down with a card error explaining the missing grant.
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=63
// +optional
Namespace *string `json:"namespace,omitempty"`
```

CEL rule changes on `ServiceEntry` (replacing the current three-way rule):

- `ping`/`siteMonitor` stay mutually exclusive with each other:
  `(has(self.ping) ? 1 : 0) + (has(self.siteMonitor) ? 1 : 0) <= 1`.
- The pod monitor (`app` and/or `podSelector`) is now freely combinable with
  either of them — that's the whole feature. `app` + `podSelector` together
  stays *allowed*, with `podSelector` winning (homepage's documented
  override semantics), rather than rejected.
- `namespace` requires a pod monitor:
  `!has(self.namespace) || has(self.app) || has(self.podSelector)`.

Doc-comment updates: `Ping`/`SiteMonitor`/`PodSelector`/`StatusStyle` all
currently describe the old mutual exclusion; `StatusStyle` now applies to
both indicators.

On `DashboardSpec`, mirroring `spec.discovery.namespaces` exactly:

```go
// monitorNamespaces lists the namespaces (beyond the Dashboard's own,
// which is always allowed) that a bound ServiceCard's pod monitor may
// name via its entries' namespace field. Each listed namespace gets a
// RoleBinding granting this Dashboard's pod read-only pod access there.
// +optional
MonitorNamespaces []string `json:"monitorNamespaces,omitempty"`
```

An explicit allowlist rather than auto-granting whatever namespaces
ServiceCards happen to name: the existing trust model (see
`dashboardRoles`' note) says a ServiceCard author is trusted with *their own
namespace's* secrets — it does not extend to reading pod metadata anywhere
in the cluster, so the cross-namespace grant must come from whoever controls
the Dashboard, exactly like `spec.discovery.namespaces`.

After the type edits: `mise run manifests generate schemas`, refresh the
Helm chart CRDs (`dist/chart`), and update `config/samples/`. Follow the
`add-crd-field` skill.

## Status model: two independent indicators

`Card` (`internal/dashboard/store.go`) grows a second result slot instead of
overloading the existing one:

```go
// HTTP monitor (ping/siteMonitor) result: "" when unconfigured, else
// "Up"/"Down"; Latency e.g. "12ms".
Status      string
StatusStyle string
Latency     string

// Pod monitor (app/podSelector) result: "" when unconfigured, else
// "Up"/"Partial"/"Down"; PodReadyText e.g. "2/3 ready".
PodStatus    string
PodReadyText string
```

Keeping the existing `Status` fields' meaning (HTTP probe) untouched
minimizes churn in `cards.templ`, `currentHashes`, and tests; entries that
today use `podSelector` alone move their result to the new fields.

`podStatus` (poller) changes from two-valued to homepage's three:

- all matched pods Ready → `Up`
- some Ready → `Partial` (new; renders amber, between the existing
  green/red)
- none Ready, or no pods matched → `Down` (keep `M/N ready` text so
  "0/0 ready" makes the "not found" case legible, as today)

### Rendering (`cards.templ` → `mise run templ-generate`)

- `dot` style: up to two dots side by side — HTTP first, pod second — each
  with its own tooltip/aria-label (`Up (12ms)` / `Partial (2/3 ready)`).
  A `status-Partial` CSS class (amber) joins `status-Up`/`status-Down`.
- `basic` style: the status line shows both, e.g.
  `Up (12ms) · 2/3 ready`.
- `StatusStyle` (entry override, else the Dashboard's spec.style default)
  applies to both indicators — one knob, matching homepage.

## Poller changes (`internal/dashboard/poller.go`)

`monitor()` currently returns one `(status, style, latency)` from a
three-way `switch`. It becomes: probe the HTTP source (if any) *and* the pod
source (if any), returning both results. The two probes can run
sequentially inside the entry's existing `run()` slot — the pod list is a
cache read (or a single uncached list cross-namespace), so it adds no
meaningful latency next to the HTTP probe.

Pod selector resolution:

```go
selector := se.PodSelector          // wins when set (homepage override)
if selector == nil && se.App != nil {
    selector = &metav1.LabelSelector{
        MatchLabels: map[string]string{"app.kubernetes.io/name": *se.App},
    }
}
```

Namespace resolution: `se.Namespace` if set, else the ServiceCard's own.
Same-namespace lists keep using the cached `Reader`. A foreign namespace
lists through the uncached `KubeReader` (its scope is already
cluster-capable; the namespace-scoped cache can't serve other namespaces),
*after* checking it against the Dashboard's `monitorNamespaces` allowlist —
a disallowed namespace short-circuits to `Down` plus a card error naming the
fix, never an RBAC-forbidden error surfaced raw. At 15s poll intervals an
uncached pod list per foreign-namespace entry is fine; if it ever isn't, a
multi-namespace cache is the follow-up, not part of this change.

### Metrics

`monitorUp` is one gauge per `namespace/crName/entryName` label. With two
monitors per entry the sources must not fight over one series: add a
`source` label (`http` | `pods`) to the gauge, and extend
`pruneMonitorMetrics`/`monitorLabels` bookkeeping to the label pair. For
the pod gauge, `Partial` counts as up (0/1 gauge; the ready fraction is
visible on the card, not in the metric).

### Sample/preview mode

`SampleData` fabricates both results when both sources are configured
(`"Up" + sampleMonitorLatency` and `"Up" + sampleMonitorReadyText`), keeping
`internal/preview` and `--sample-data` rendering fully populated with no
RBAC. Update `boot_test.go` fixtures to cover a combined entry.

## RBAC (`internal/controller/dashboard_rbac.go`)

- Own-namespace pod reads: already granted unconditionally by
  `dashboardPodsRule`. No change.
- Cross-namespace: replicate the discovery pattern 1:1 —
  - a per-Dashboard ClusterRole (`kubepage-mon-<len>-<ns>-<name>`, new
    `mon-` prefix alongside `disc-`) carrying `dashboardPodsRule`,
  - a RoleBinding in each `spec.monitorNamespaces` entry (own namespace
    filtered out),
  - tracked in a new `Dashboard.status.monitorNamespaces` (same
    persist-superset-before-create dance as `Status.DiscoveryNamespaces`,
    and the same finalizer cleanup — these objects can't carry owner
    references).
  - created only while `monitorNamespaces` is non-empty; deleted when it
    empties, keeping the pod least-privileged.

## Test plan

- **CEL** (`monitor_source_policy_test.go`): admit `siteMonitor+podSelector`,
  `ping+app`, `app+podSelector`; still reject `ping+siteMonitor`; reject
  `namespace` without a pod monitor; admit `namespace+app`.
- **Poller** (`poller_test.go`): combined entry populates both `Status` and
  `PodStatus`; `Partial` when some pods Ready; `app` label-selector
  derivation; `podSelector` overriding `app`; foreign namespace allowed vs.
  disallowed (card error text); metric series per source, pruned when a
  source is removed.
- **RBAC** (`dashboard_rbac_test.go`): monitor ClusterRole/RoleBindings
  created/updated/removed with `monitorNamespaces`, finalizer cleanup.
- **Rendering**: `cards.templ` golden/behavior tests for two dots, Partial
  class, and `basic` combined text; `currentHashes` change detection when
  only the pod status flips.

## Implementation order

1. API fields + CEL + doc comments; `mise run manifests generate schemas`;
   Helm chart CRD refresh; samples. (`add-crd-field` skill.)
2. `Card` fields + poller combined probe + `Partial` + metrics `source`
   label; unit tests.
3. `cards.templ` + CSS + `templ-generate`.
4. `monitorNamespaces` RBAC + status tracking + finalizer; controller tests.
5. Docs: `docs/configuration/service-cards.md` (combined example mirroring
   homepage's `namespace`/`app` YAML), README feature table, CHANGELOG via
   Conventional Commit (`feat(servicecard): ...`).
6. `preflight` before the PR.

## Open questions

- **Should `ping` + `siteMonitor` also become combinable?** Homepage allows
  it (each gets its own indicator). Deferred: this plan only pairs one HTTP
  source with the pod monitor; loosening the HTTP pair is a trivial CEL
  follow-up if wanted, but doubles the indicator count on a small card for
  little signal.
- **`Partial` and `statusStyle: basic` wording** — proposal renders the raw
  status word; homepage shows its own badge text. Cosmetic, decide at
  templ-review time.

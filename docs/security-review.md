# Security review and hardening roadmap

Reviewed at commit `997ae82` (2026-07). Scope: the whole repository — manager
and dashboard modes of `cmd/main.go`, the controllers and per-Instance RBAC in
`internal/controller/`, the dashboard HTTP/render/poll surface in
`internal/dashboard/`, the CEL admission policies in `config/admission/`, the
generated RBAC in `config/rbac/`, the Dockerfile, and CI. This is a
recommendations plan, not a set of applied changes; each finding names the
affected code and a concrete future implementation.

## Threat model (as currently designed)

The codebase is consistent about two trust boundaries, both documented in code
comments but only partially in user-facing docs:

1. **CRD authors are fully trusted within their namespace.** Anyone who can
   create a `ServiceEntry`/`InfoWidget` can reference any Secret in the
   namespace and point the widget at a server they control, exfiltrating it
   (`internal/controller/instance_rbac.go`, trust-model note on
   `dashboardRoles`). Anyone who can write a `Configuration` can inject
   arbitrary JS into every viewer's browser (`spec.customJS`, by design).
2. **Dashboard viewers are trusted by network reachability.** The dashboard
   HTTP server (`internal/dashboard/server.go`) has no authentication,
   authorization, or session concept at all.

Nothing below contradicts that model; the plan is to (a) state it loudly in
user-facing docs, (b) offer opt-in controls for operators whose environment
doesn't match it, and (c) close a few gaps that are outside the model's
intent.

## What is already in good shape

Substantial hardening exists and should be preserved as-is (much of it from
PR #58):

- **Secret handling**: Secrets resolved per-poll via an uncached client, never
  in informer cache/env/files (`poller.go` `resolveSecret`); per-Instance Role
  scoped with `resourceNames` and `get` only, omitted entirely when empty
  (`instance_rbac.go`).
- **RBAC**: per-Instance ServiceAccount/Role/RoleBinding, owner-referenced;
  Ingress/HTTPRoute and cluster-metrics rules granted only while the feature
  is actually in use; cluster-scoped RBAC cleaned up by finalizer; unambiguous
  length-prefixed ClusterRole naming with hash-truncation.
- **SSRF**: link-local dial guard applied post-DNS on every outbound transport
  including the unifi insecure-TLS one (`transport.go`), closing the cloud
  metadata (169.254.169.254) route including via DNS rebinding.
- **HTTP surface**: GET-only mux, server timeouts, 2 MiB response-body cap
  (`httpwidget.go`), CSP + `X-Content-Type-Options` + `X-Frame-Options` +
  `Referrer-Policy`, `frame-ancestors 'none'`, metrics on a separate listener
  never routed by Ingress/HTTPRoute, `rel="noopener noreferrer"` on new-tab
  links, vendored htmx/fonts (no CDN script loads).
- **Rendering**: templ auto-escaping everywhere, with deliberate, documented
  escaping for the three `templ.Raw` sites (`render_helpers.go`:
  `cssStringEscape` handles `</style>` breakout, `jsStringEscape` handles
  `</script>` breakout); fixed iframe `sandbox` attribute; `frame-src https:`
  blocks `javascript:` iframes.
- **Admission**: CEL `ValidatingAdmissionPolicies` for secret-source one-of,
  monitor-source mutual exclusion, and widget-type allow-lists, with envtest
  suites that catch policy/schema/registry drift.
- **Workloads**: distroless nonroot image, `CGO_ENABLED=0`, `runAsNonRoot`,
  drop-ALL capabilities, `allowPrivilegeEscalation: false`, seccomp
  `RuntimeDefault`, user namespaces (`hostUsers`) on by default, HTTP/2
  disabled on manager webhook/metrics.
- **Supply chain**: Renovate, SHA-pinned actions, CodeQL + govulncheck
  workflows, cosign-signed releases with SBOM.

## Findings and recommendations

Priorities: **P1** = plan next, **P2** = worthwhile hardening, **P3** =
opportunistic.

### P1.1 — Dashboard has no viewer authentication

`internal/dashboard/server.go` serves the full dashboard — service names,
internal URLs, live widget data (Plex sessions, TrueNAS pools, node metrics),
up/down status — to anyone who can reach the Service, Ingress, or HTTPRoute.
`spec.ingress`/`spec.gateway` make it one field away from being
internet-facing with no auth and (unless configured) no TLS.

Recommendation, in order:

1. **Docs first**: README + SECURITY.md must state that the dashboard is
   unauthenticated and that exposure beyond a trusted network requires an
   authenticating proxy. Include a worked example (oauth2-proxy or Authelia
   via ingress annotations / HTTPRoute filter).
2. **Forward-auth friendly mode**: nothing to build for the proxy case, but
   verify `/healthz` exclusion patterns and document them.
3. **Optional built-in auth** on the `Instance` spec, smallest useful shapes:
   - `spec.auth.basicAuthSecretRef`: bcrypt htpasswd Secret, checked by
     middleware in `Routes()` (constant-time compare, applies to every route
     except `/healthz`).
   - Later, if demanded: trusted-header mode (`spec.auth.trustedHeader`) for
     SSO proxies that inject `X-Forwarded-User`.

### P1.2 — No NetworkPolicy story for dashboard pods

`config/network-policy/` only covers manager metrics. Dashboard pods accept
ingress from anywhere in the cluster and have unrestricted egress. An egress
policy is also the strongest available backstop for the SSRF surface (P2.1) —
widget URLs are CRD-supplied by design, so dial-time guards can only ever
deny-list known-bad ranges, whereas an egress NetworkPolicy can positively
scope what the pod may reach.

Recommendation: add an optional operator-managed, owner-referenced
NetworkPolicy per Instance (`spec.networkPolicy.enabled` or similar) in
`instance_network.go`:

- Ingress: dashboard port from the ingress-controller namespace (selector
  configurable), metrics port from scrape namespaces.
- Egress: DNS, the API server, and a configurable CIDR/namespace list for
  widget upstreams (default allow-all egress to stay non-breaking; the value
  is that the knob exists and is documented).

Ship example manifests either way, mirroring the existing
`allow-metrics-traffic.yaml` pattern.

### P1.3 — Dashboard metrics endpoint is unauthenticated and on the Service

`dashboard.go` serves `promhttp.Handler()` with no authn/authz (the manager's
metrics use `WithAuthenticationAndAuthorization`), and `instance_network.go`
exposes port 9090 on the ClusterIP Service. Any pod in the cluster can read
`kubepage_monitor_up{entry="ns/name"}` (reveals which internal services exist
and their up/down state) and per-widget-type poll metrics.

Recommendation (either is sufficient; both is fine):

- Make the metrics Service port opt-in on the `Instance` spec
  (`spec.metrics.enabled`, default off — pod-level scraping via PodMonitor
  keeps working without the Service port).
- Cover it in the P1.2 NetworkPolicy (ingress on 9090 only from monitoring
  namespaces).

### P2.1 — Close the remaining metadata-endpoint gap in the SSRF guard

`transport.go` deliberately scopes the dial guard to link-local ranges and
documents AWS's IPv6 metadata address `fd00:ec2::254` (unique-local, not
link-local) as an accepted gap. It is a one-line deny to close:

- Add `fd00:ec2::254` (and the documented `fd00:ec2::/32` block, per AWS
  docs) to `guardedDialControl`'s rejection set alongside link-local.
- Optionally follow with a configurable deny-list
  (`--deny-egress-cidrs` dashboard flag driven by an `Instance` field) for
  operators with other internal metadata/admin ranges; keep the built-in
  denies non-configurable.

### P2.2 — Secret-exfiltration path for CRD authors: offer an opt-in gate

`referencedSecretNames` (`instance_rbac.go`) grants the dashboard read access
to any Secret a ServiceEntry/InfoWidget names, and the widget then sends the
plaintext wherever its URL points. This is documented in code as "CRD author =
trusted with every Secret in the namespace", which is the right default for
the single-admin homelab case, but there is no control for anything else.

Recommendation:

1. Move the trust-model note from the code comment into README/SECURITY.md
   (P1.1's docs pass).
2. Plan an opt-in restriction: when the `Instance` sets
   `spec.secretPolicy: Labeled` (naming TBD), `referencedSecretNames` only
   includes Secrets labeled `page.kubepage.dev/allow-widgets: "true"`, and the
   poller surfaces a clear card error for refs to unlabeled Secrets. Default
   stays the current behavior.

### P2.3 — Manager holds cluster-wide `get` on all Secrets

`config/rbac/role.yaml` grants the manager `secrets: get` cluster-wide. The
manager never reads Secret contents; it needs the verb only to pass RBAC
escalation-prevention when creating per-Instance Roles that grant
`get`-by-name on referenced Secrets. Still, a compromised manager pod can read
any Secret in the cluster.

Recommendation: document the tradeoff in SECURITY.md, and provide a
namespace-scoped install overlay (manager watches an allow-list of namespaces;
`secrets get` granted via per-namespace Role instead of the ClusterRole) for
operators who don't want a cluster-wide grant. Full removal isn't possible
while the operator manages secret-scoped Roles — the alternative (`escalate`
verb on roles) is strictly broader.

### P2.4 — CSP depends on `'unsafe-inline'` for script-src and style-src

`server.go`'s CSP allows inline script/style because the page shell has no
nonce plumbing. Today every interpolated value is a fixed lookup, an integer,
or escaped (`render_helpers.go`), so this is defense-in-depth rather than a
live hole — but `'unsafe-inline'` means any future escaping regression in the
`templ.Raw` paths becomes full XSS.

Recommendation: generate a per-request nonce in `securityHeaders`, thread it
into `Index()` (the templates already receive per-request data), tag the
inline `<script>`/`<style>` blocks — including the CustomJS/CustomCSS ones —
and replace `'unsafe-inline'` with `'nonce-…'`. Move any static inline JS into
the embedded `assets/` bundle while there. CustomJS keeps working (it renders
server-side into a nonce-carrying block); anything injected by an attacker
without the nonce stops.

### P2.5 — Inline `value` secrets and CustomJS deserve louder warnings

- `SecretValueSource.value` invites plaintext credentials in CRDs — persisted
  in etcd, GitOps repos, and `kubectl get -o yaml` output. Keep it (it's
  useful for non-secrets like city coordinates), but document the caveat and
  consider a `Warn`-action CEL admission message when `value` is used for
  fields whose name implies a credential (`token`, `password`, `apiKey`, ...).
- `Configuration.spec.customJS` is arbitrary script in every viewer's browser.
  The field docs say "trusted, operator-supplied" — repeat that in user-facing
  docs, and note that RBAC on `configurations` is effectively RBAC on viewer
  browsers.

### P3.1 — Pin the dashboard image by digest, not tag

`ownDashboardImage` (`cmd/main.go`) copies the manager's `spec.containers`
image reference (typically a tag) into every dashboard Deployment with
`PullIfNotPresent`. On a multi-node cluster a mutated/stale tag can yield
different bits than the manager is running.

Recommendation: prefer the running digest from
`pod.Status.ContainerStatuses[].ImageID` (fall back to spec image when the
runtime returns an unusable ID, e.g. locally-loaded kind images), so dashboard
pods provably run the same image as the manager.

### P3.2 — Custom CA support instead of `insecureTLS`

`unifi.go` offers per-widget `insecureTLS` (correctly scoped, still
dial-guarded). Most homelab upstreams that trigger it have a private CA.
Recommendation: add an optional CA source to `WidgetConfig` (widget-level
`caSecretRef` resolved like other secrets, or a pod-level
`--upstream-ca-bundle` flag mounting a ConfigMap) so operators can verify
instead of skipping; extend to other widgets facing the same issue rather than
adding more per-widget `insecureTLS` flags.

### P3.3 — Document that security-context overrides can weaken defaults

`mergeOverride` (`instance_controller.go`) intentionally lets
`spec.podSecurityContext`/`spec.containerSecurityContext` override the
hardened defaults field-by-field (e.g. set `runAsNonRoot: false`), and
`spec.hostUsers: Disabled` turns user namespaces off. That's a reasonable
escape hatch; the backstop should be Pod Security Admission. Recommendation:
docs note recommending the `restricted` PSA level on dashboard namespaces, so
cluster policy — not the operator — is what refuses a weakened pod.

### P3.4 — iframe widget: same-origin caveat

`iframe.go`'s fixed `sandbox="allow-scripts allow-same-origin"` is the right
policy for cross-origin embeds, but the combination is a no-op sandbox if the
embedded URL is same-origin with the dashboard itself. No code change needed
if documented; alternatively drop `allow-same-origin` when the iframe URL's
host matches the request's `Host`.

### P3.5 — Supply-chain increments

Already strong; remaining low-cost additions: OpenSSF Scorecard workflow +
badge, SLSA build provenance attestation on releases (cosign attest alongside
the existing signing), and an audit that every workflow sets a minimal
top-level `permissions:` block.

## Phased implementation plan

**Phase 1 — documentation (no code, immediate):**
README/SECURITY.md trust-model section covering: unauthenticated viewer
surface + reverse-proxy auth example (P1.1), CRD-author-equals-secret-holder
(P2.2), CustomJS/inline-`value` caveats (P2.5), PSA `restricted`
recommendation (P3.3), iframe same-origin caveat (P3.4), TLS-on-Ingress nudge.

**Phase 2 — small code hardening (each an independent PR):**
1. `fd00:ec2::/32` in the dial guard + tests (P2.1).
2. Metrics Service port opt-in on `Instance` (P1.3).
3. Digest-pinned dashboard image from `ContainerStatuses` (P3.1).
4. Shipped example NetworkPolicies for dashboard pods (P1.2, docs half).

**Phase 3 — opt-in features (API changes, one CRD field each):**
1. `spec.auth.basicAuthSecretRef` middleware (P1.1).
2. Operator-managed per-Instance NetworkPolicy (P1.2).
3. `spec.secretPolicy: Labeled` opt-in Secret gating (P2.2).
4. Custom CA support for widget TLS (P3.2).

**Phase 4 — deeper reworks:**
1. Nonce-based CSP, drop `'unsafe-inline'` (P2.4).
2. Namespace-scoped install overlay to shed the manager's cluster-wide
   `secrets get` (P2.3).
3. Scorecard/SLSA/workflow-permissions pass (P3.5).
4. Warn-action admission message for credential-shaped inline `value` (P2.5).

## Explicit non-goals

- Blocking widget URLs that point at cluster-internal addresses: reaching
  ClusterIPs/RFC1918 is the product's purpose; egress control belongs to
  NetworkPolicy (P1.2), not the dial guard.
- Multi-tenant self-service CRD authoring: out of scope per the trust model;
  P2.2's labeled mode is a mitigation, not tenancy.
- A validating webhook server: the CEL admission-policy approach is
  deliberate (no cert management) and sufficient for current invariants.

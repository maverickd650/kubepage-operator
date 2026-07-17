# Security Policy

## Trust model

kubepage-operator is designed for a single-admin homelab, not multi-tenant
self-service. Two trust boundaries fall out of that and are load-bearing for
every decision below — read them before exposing a dashboard beyond a
trusted network:

1. **A Dashboard's blast radius is its own namespace, unless a Dashboard
   author explicitly widens it.** Every config CRD (`ServiceCard`/
   `Bookmark`/`InfoWidget`) must live in the same namespace as the
   `Dashboard` it binds to — no cross-namespace `dashboardRef`. Ingress/
   HTTPRoute annotation discovery (`spec.discovery`) follows the same rule
   by default: it only scans the Dashboard's own namespace. The one opt-in
   exception is `spec.discovery.namespaces`, a static list of additional
   namespaces to scan for the same discovery annotations — aimed at the
   common homelab shape of one dashboard namespace and apps spread across
   several others. Setting it grants the dashboard pod's ServiceAccount
   get/list/watch on Ingresses (and, if `discovery.sources` includes
   `HTTPRoute`, HTTPRoutes) in exactly the namespaces listed, via a
   RoleBinding created *in each of those namespaces* against a shared
   read-only ClusterRole — never a `ClusterRoleBinding`, so access never
   extends beyond what's explicitly named
   (`internal/controller/dashboard_rbac.go`'s `reconcileDiscoveryRBAC`).
   Whoever can set this field on a Dashboard can make its dashboard pod read
   every Ingress/HTTPRoute's hostnames and paths (not their annotations'
   secret-free contents beyond what an author already opted into publishing
   via the discovery annotations) in the named namespaces — treat editing a
   Dashboard's `spec.discovery.namespaces` with the same trust level as
   editing RBAC directly, since that's what it does.
2. **CRD authors are fully trusted within their namespace.** Anyone who can
   create a `ServiceCard`/`InfoWidget` in a namespace can name *any* Secret
   in that namespace via `secretKeyRef` and point the widget's own URL at a
   server they control — an effective read of that Secret's plaintext
   without ever needing `get secrets` RBAC directly (see the trust-model note
   on `dashboardRoles` in `internal/controller/dashboard_rbac.go`). The same
   applies to `Dashboard.spec.widgetDefaults`: anyone who can edit a
   Dashboard can name any Secret in its namespace as a per-widget-type
   default, resolved and RBAC-scoped identically to a widget's own
   `secretKeyRef` (`referencedSecretNames` in the same file walks both).
   Anyone who can write a `Configuration` can inject arbitrary JavaScript into
   every viewer's browser via `spec.customJS` — that field is documented as
   "trusted, operator-supplied", which in practice means RBAC on
   `configurations` is effectively RBAC on every viewer's browser.
   `SecretValueSource.value` (as opposed to `secretKeyRef`) additionally
   invites plaintext credentials into the CRD itself — persisted in etcd,
   any GitOps repo it's committed to, and `kubectl get -o yaml` output.
   Prefer `secretKeyRef` for anything credential-shaped; `value` remains
   useful for genuinely non-secret config (e.g. city coordinates for a
   weather widget).
3. **Dashboard viewers are trusted by network reachability.** The dashboard
   HTTP server (`internal/dashboard/server.go`) has no authentication,
   authorization, or session concept of its own. It serves service names,
   internal URLs, and live widget data (Plex sessions, TrueNAS pools, node
   metrics) to anyone who can reach the Service, Ingress, or HTTPRoute.
   `spec.ingress`/`spec.gateway` are one field away from making that
   internet-facing. On a poll failure, that same audience sees the raw error
   text (`card.Err`, `internal/dashboard/poller.go`) — and Go's `net/http`
   client errors embed the full request URL, query string included. **Never
   put a credential in a widget's `url` as a query parameter** (e.g.
   `?apikey=...`); use the widget's `Secrets`/`secretKeyRef` fields instead
   (see point 2 above), which are sent as headers or form data and never
   appear in that error text.

If your environment doesn't match this model — untrusted CRD authors, or
viewers who shouldn't see each other's data — do not rely on this operator's
defaults alone:

- **Put an authenticating reverse proxy in front of the dashboard** (e.g.
  [oauth2-proxy](https://oauth2-proxy.github.io/oauth2-proxy/) or
  [Authelia](https://www.authelia.com/) via an `nginx.ingress.kubernetes.io/auth-url`
  annotation on `spec.ingress.annotations`, or an equivalent Gateway API
  `HTTPRoute` filter for `spec.gateway`). The dashboard's own `/healthz`
  endpoint should stay excluded from any auth gate so liveness/readiness
  probes keep working.
- Alternatively, set `spec.auth.basicAuthSecretRef` on the `Dashboard` for a
  minimal built-in gate (see [Optional built-in
  authentication](#optional-built-in-authentication) below) — a bcrypt
  htpasswd Secret checked on every route except `/healthz`. This is
  intentionally basic; a real SSO/OIDC proxy is the better answer for
  anything beyond a homelab.
- **Always terminate TLS on `spec.ingress`/`spec.gateway`** when exposing the
  dashboard beyond a trusted LAN (`spec.ingress.tls.secretName`, or your
  Gateway's own listener TLS) — with no auth by default, cleartext HTTP means
  a network position between viewer and dashboard sees everything the
  dashboard serves.
- **Apply the `restricted` [Pod Security
  Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/)
  level to namespaces running dashboard pods.** `spec.podSecurityContext`/
  `spec.containerSecurityContext` deliberately let you override the
  hardened defaults field-by-field (e.g. set `runAsNonRoot: false`), and
  `spec.hostUsers: Disabled` turns off user namespace isolation — that's a
  reasonable escape hatch for unusual upstreams, but the backstop against a
  weakened pod should be cluster policy, not the operator refusing your own
  spec.
- **`iframe` widgets are same-origin-aware, not same-origin-safe.** The fixed
  `sandbox="allow-scripts allow-same-origin"` on an `iframe` `ServiceWidget`
  (`internal/dashboard/iframe.go`) is the right policy for cross-origin
  embeds, but the combination is a no-op sandbox if the embedded URL happens
  to share an origin with the dashboard itself — only point `iframe` widgets
  at services on a different origin than the dashboard.

None of the above is enforced by the operator; RBAC on the config CRDs *is*
the operator's access control, by design (see [Explicit
non-goals](#explicit-non-goals)).

### Optional built-in authentication

Setting `spec.auth.basicAuthSecretRef` on a `Dashboard` names a Secret
holding an [Apache htpasswd](https://httpd.apache.org/docs/current/programs/htpasswd.html)
file (bcrypt-hashed entries) under its `.htpasswd` key. When set, every
dashboard route except `/healthz` requires HTTP Basic credentials matching an
entry in that file, checked with a constant-time comparison. This is a
minimal gate for a homelab reachable from a semi-trusted network — it has no
session/logout/lockout concept and sends credentials on every request
(mitigated by requiring TLS, see above). Concurrent bcrypt comparisons are
capped at a small fixed limit (`maxConcurrentAuthChecks` in
`internal/dashboard/auth.go`); once that many are in flight, further
credentialed requests get `429 Too Many Requests` immediately instead of
paying bcrypt's ~50-100ms CPU cost, bounding both online credential-guessing
throughput and CPU exhaustion from a single client looping bad credentials.
This is a global cap, not a per-client one — behind an Ingress the peer IP
seen by the dashboard is the ingress controller's unless X-Forwarded-For is
explicitly trusted, so a per-IP limiter would either trust that header or do
nothing. For anything beyond this, front the dashboard with a real
authenticating proxy instead.

### Explicit non-goals

- **Blocking widget URLs that point at cluster-internal addresses.** Reaching
  ClusterIPs/RFC1918 ranges is the product's purpose — see the SSRF section
  below for the one address range that *is* blocked unconditionally.
- **Multi-tenant self-service CRD authoring.** Out of scope per the trust
  model above; `spec.secretPolicy: Labeled` (see below) is a mitigation for
  the secret-exfiltration path, not a tenancy boundary.
- **A validating webhook server.** The CEL `ValidatingAdmissionPolicy`
  approach (`config/admission/`) is deliberate — no certificate management —
  and sufficient for the invariants this project currently enforces.

## Supported Versions

This project releases continuously from `main` (see
[Releases](https://github.com/maverickd650/kubepage-operator/releases)). Only
the latest released version is supported — please upgrade before reporting an
issue.

## Reporting a Vulnerability

Please report security vulnerabilities privately using
[GitHub's private vulnerability reporting](https://github.com/maverickd650/kubepage-operator/security/advisories/new)
(Security tab → "Report a vulnerability"). Do not open a public issue for
security reports.

You should receive an initial response within a few days. If the issue is
confirmed, we'll work with you on a fix and coordinate disclosure timing
before a public advisory/release.

## Scope

As noted in the [README](README.md), this project is built with heavy AI
assistance and has not had a third-party security review — read the code
before trusting it with anything sensitive. Areas of particular interest for
reports:

- Secret handling in the dashboard process (`internal/dashboard/poller.go`'s
  `resolveSecret`): Secret contents are read via an uncached client and must
  never reach pod env, a ConfigMap, a projected file, or an informer cache —
  the plaintext should only exist in memory for the duration of one poll. See
  [`CLAUDE.md`](CLAUDE.md) for the full design rationale. The *manager's* own
  Secret watch (`internal/controller/dashboard_controller.go`'s
  `SetupWithManager`, added to keep `spec.secretPolicy: Labeled` Role grants
  current when a Secret's `allow-widgets` label changes) is metadata-only
  (`WatchesMetadata`) for the same reason — only Secret labels transit that
  informer, never contents.
- Outbound SSRF surface: every widget implementation under
  `internal/dashboard/` (`grafana.go`, `plex.go`, `prometheus.go`, `unifi.go`,
  etc.) polls a URL taken from a `ServiceCard`/`InfoWidget` CRD. Whoever can
  create those CRDs in a namespace can direct the dashboard pod's outbound
  requests anywhere reachable from it — this is expected admin-managed
  behavior, not multi-tenant self-service, but RBAC misconfigurations that let
  untrusted users author these CRDs are in scope.
- Admission-time validation: the `ValidatingAdmissionPolicies` in
  `config/admission/` (CEL) enforce the `SecretValueSource` one-of constraint
  and the widget `type` allow-list. A policy that's silently out of sync with
  the current CRD schema (e.g. after a field rename) is a reportable gap.
- HTML/JS injection via CRD-supplied free text rendered into the dashboard
  page (`internal/dashboard/*.templ`, `render_helpers.go`).

## Supply Chain

Dependencies are kept current via Renovate, GitHub Actions are pinned to
commit SHAs, and the repo runs CodeQL and `govulncheck` on every push/PR plus
a weekly schedule. Release artifacts (the container image and Helm chart)
include a generated SBOM, a SLSA build-provenance attestation, and are signed
with cosign — all keyless, verifiable via `cosign verify`/`cosign
verify-attestation` against the GitHub Actions OIDC identity
(`.github/workflows/release.yaml`). Every workflow sets a minimal top-level
`permissions:` block (`{}` unless a job needs otherwise), granting write
scopes only on the specific jobs that need them.

### Verifying a release binary

Each GitHub Release also carries cross-compiled `preview`-capable binaries
(see [`docs/design/local-preview.md`](docs/design/local-preview.md)) for
linux/darwin on amd64/arm64 and windows/amd64, alongside `checksums.txt` and
an SPDX SBOM. `checksums.txt` is keyless cosign-signed and carries a SLSA
build-provenance attestation, the same trust chain as the image/chart — so
rather than trusting each archive individually, verify `checksums.txt` once
and let its own sha256 sums vouch for every archive:

```sh
VERSION=vX.Y.Z # the release you downloaded
cosign verify-blob \
  --bundle checksums.txt.cosign.bundle \
  --certificate-identity-regexp 'https://github.com/maverickd650/kubepage-operator/\.github/workflows/release\.yaml@.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

sha256sum -c checksums.txt --ignore-missing
```

`cosign verify-blob-attestation --type slsaprovenance --bundle
checksums.txt.slsa.bundle checksums.txt` (with the same `--certificate-*`
flags) additionally confirms the binaries were produced by
`release.yaml` itself, not hand-uploaded to the release.

An [OpenSSF Scorecard](https://github.com/ossf/scorecard) workflow is a
worthwhile follow-up not yet wired in — adding it needs a third-party
GitHub Action pinned to a commit SHA per this repo's own convention above.

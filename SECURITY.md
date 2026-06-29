# Security Policy

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
  [`CLAUDE.md`](CLAUDE.md) for the full design rationale.
- Outbound SSRF surface: every widget implementation under
  `internal/dashboard/` (`grafana.go`, `plex.go`, `prometheus.go`, `unifi.go`,
  etc.) polls a URL taken from a `ServiceEntry`/`InfoWidget` CRD. Whoever can
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
a weekly schedule. Release artifacts include a generated SBOM and are signed
with cosign.

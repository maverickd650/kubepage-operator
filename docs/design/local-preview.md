# Design: `preview` — local dashboard rendering without a cluster

Status: proposed (design only — no implementation in this PR)

## Problem

Today the only way to see what a `Dashboard` actually looks like is to install
the operator on a cluster (or spin up Kind via the e2e task), apply the CRDs,
and port-forward to the dashboard pod. That's a long loop for what is often a
purely visual question: "what does this `DashboardStyle` + set of
`ServiceCard`s render as?" Contributors iterating on `.templ` files, palette
changes, or widget field layout have no fast feedback path, and users writing
their CRD YAML can't preview it before applying.

Goal: a binary you can download from a GitHub release (signed, like the image
and chart), point at a directory of CRD YAML, and get the real dashboard UI in
a browser on localhost — rendered by the exact same code that runs in-cluster.

## Proposal in one paragraph

Add a third mode to the existing single entrypoint: `cmd/main.go` already
dispatches on `os.Args[1] == "dashboard"`; add `os.Args[1] == "preview"`. The
preview mode parses Kubernetes manifests from `-f` paths into the project's
typed scheme, loads them into an in-memory `client.Reader`, and hands that
reader to the *unmodified* `internal/dashboard` `Server` + `Poller`. Release
builds of this same binary are cross-compiled for common platforms, attached
to the GitHub release, and signed/attested with the same keyless-cosign
approach the image and chart already use.

## Why a subcommand of the same binary (not a separate `cmd/preview`)

- **Fidelity is the whole point.** The architecture note in CLAUDE.md — "two
  binaries in one" — exists so the dashboard pod always runs the manager's own
  bits. Preview extends the same guarantee to the laptop: the binary attached
  to release `vX.Y.Z` renders pixel-for-pixel what the `vX.Y.Z` image renders,
  because it *is* the same binary (same `_templ.go`, same widget registry,
  same CSP/nonce handling). A separate slim binary would inevitably drift.
- **No new build graph.** The Dockerfile's `-ldflags "-X main.version=… -X
  main.commit=…"` stamping, the `mise run build` task, and the version footer
  in the page shell all carry over untouched.
- The extra weight of manager-mode code in a downloaded binary is a few MB of
  already-shared dependencies (controller-runtime is linked either way for the
  dashboard's cache client). Not worth a second `main`.

## How preview mode works

### Config loading

- `preview -f <path>` accepts one or more files or directories (repeatable
  flag; directories walked non-recursively like `kubectl apply -f`, `-R` for
  recursive). Multi-document YAML is split and decoded through the existing
  `scheme` (already registers `pagev1alpha1` + core).
- Recognized kinds: `Dashboard`, `DashboardStyle`, `ServiceCard`, `Bookmark`,
  `InfoWidget`, and `Secret` (so `secretKeyRef`-backed widget config and
  `spec.auth.basicAuthSecretRef` resolve exactly as in-cluster). Unknown kinds
  are skipped with a logged warning, so `-f config/samples/` or a user's whole
  GitOps directory Just Works.
- Objects with no `metadata.namespace` default to the target Dashboard's
  namespace (defaulted to `preview` when the Dashboard itself has none), since
  local YAML frequently omits it.
- If exactly one `Dashboard` is present, it is selected automatically;
  otherwise `--dashboard-name`/`--namespace` disambiguate (error message lists
  the candidates).
- CEL `XValidation` rules live in the CRD schema, so the API server isn't
  there to enforce them. v1 accepts what decodes; a follow-up can evaluate the
  same CEL rules client-side (kubeconform-style) and warn.

### Serving

- Build a read-only in-memory `client.Reader` over the decoded objects and
  construct `dashboard.Server` and `dashboard.Poller` directly (not
  `dashboard.Run`, which builds real cluster clients from a `rest.Config`).
  Both already depend only on `client.Reader` — the repo's own
  `server_test.go`/`browser_test.go` prove the wiring works with
  `fake.NewClientBuilder()`. Two implementation options for the reader:
  1. `sigs.k8s.io/controller-runtime/pkg/client/fake` — zero code, already
     exercised by this repo's tests. It is documented as a testing package,
     but it has no stdlib-`testing` dependency and is safe to link.
  2. A ~100-line `Get`/`List`-only reader over a `map[GVK][]client.Object`.
  **Recommendation: (1) for v1**, with the reader construction isolated in one
  file (`internal/preview/loader.go`) so swapping to (2) later is local.
- `Reader`, `SecretReader` = the in-memory reader. `KubeReader` = a stub that
  errors; the `kubemetrics` InfoWidget then shows its normal per-widget error
  state (accurately previewing "upstream unreachable" rendering).
  `GatewayAPIEnabled=false` (HTTPRoute discovery is meaningless locally).
- Widget polling runs for real against whatever URLs the ServiceCards name —
  on a homelab LAN the actual Grafana/Plex/etc. are often reachable, which
  makes preview a genuinely live dashboard; unreachable upstreams render their
  error state, which is itself worth previewing. `podSelector` status degrades
  to its error path like `kubemetrics`.
- Bind `--addr` to `127.0.0.1:8080` by default (not `:8080`): manifests may
  contain plaintext Secrets, and a dev tool shouldn't listen on all
  interfaces. Metrics listener reuses `--metrics-addr` defaulting to
  `127.0.0.1:0` (present because `Poller` records metrics, but not the point
  of preview).
- `--open` launches the default browser after the listener is up.

### Live reload (phase 2)

Watch the `-f` paths with fsnotify; on change, re-parse and atomically swap
the reader behind a small `swappableReader` wrapper (a `sync/atomic.Pointer`
around `client.Reader`). No change to `Server`/`Poller` — the next htmx
`/fragment` poll (every `RefreshSeconds`) picks up the new config, so the edit
loop is "save YAML → see it within one poll interval, no restart, no browser
reload" — the same no-full-reload property the in-cluster dashboard already
has. A failed re-parse logs and keeps serving the last good config.

### Explicit non-goals

- No mock/sample widget data in v1 (a `--sample-data` mode with representative
  `[]Field` per widget type is a nice phase-4 stretch, but needs a per-widget
  sample registry — separate design).
- Not a substitute for e2e: no controller, no Deployment/RBAC/Ingress logic is
  exercised. This previews the *dashboard*, not the operator.
- No `--kubeconfig` hybrid mode (real cluster reads + local overrides) in v1.

## CLI shape

```sh
kubepage-operator preview -f ./config/samples [-f more.yaml] \
  [--dashboard-name NAME --namespace NS] \
  [--addr 127.0.0.1:8080] [--poll-interval 15s] [--open]
```

Plus a dev-loop task so contributors never type the above:

```toml
[tasks.preview]
description = "Serve the dashboard locally from config/samples (no cluster)"
depends = ["templ-generate"]
run = "go run ./cmd/main.go preview -f config/samples --open"
```

## Release: attach and sign the binaries

Today `release.yaml` publishes only the image and Helm chart (both to GHCR,
both cosign-keyless signed with SLSA provenance attestations); GitHub releases
carry no file assets. Plan:

### Build

New mise task (keeping ".mise/config.toml is the single source of truth — CI
uses the exact same tasks"):

```toml
[tasks.build-dist]
description = "Cross-compile release binaries into dist-bin/ with version stamping"
# VERSION/REVISION env vars, matching the Dockerfile's build args
```

Targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`,
`windows/amd64`. `CGO_ENABLED=0`, `-trimpath`, same
`-ldflags "-X main.version=… -X main.commit=…"` as the Dockerfile. Each
binary packaged as `kubepage-operator_<version>_<os>_<arch>.tar.gz` (`.zip`
for windows) containing the binary + LICENSE + README, plus a single
`checksums.txt` (sha256 of every archive).

*Considered and rejected:* GoReleaser. It does exactly this out of the box,
but it would be the only release step not expressed as a mise task, its config
would duplicate the version-stamping and cosign conventions the workflow
already hand-rolls for image/chart, and this matrix is small. Revisit if the
target list grows (homebrew tap, nfpm packages, …).

### Sign and attest — mirroring the image/chart steps

New `binaries` job in `release.yaml`, gated on
`needs.release-please.outputs.release_created`, separate from `publish`
because it needs different permissions (`contents: write` to upload release
assets — `publish` deliberately holds only `contents: read`; keep it that
way). `id-token: write` for keyless cosign, pinned actions, same mise setup:

1. `mise run build-dist` with `VERSION`/`REVISION` from the release outputs.
2. `cosign sign-blob --yes --bundle checksums.txt.cosign.bundle checksums.txt`
   — signing the checksum file covers every archive transitively (their
   digests are its content), the standard blob-signing pattern and one
   signature to verify instead of ten. cosign is already pinned in
   `[tools]` (3.1.1), and v3 bundles put cert + signature + Rekor proof in
   one file.
3. Reuse the existing SLSA predicate generation step (extract the `jq`
   into a shared script or composite action under `.github/actions/` so
   image, chart, and binaries stamp identical provenance) and
   `cosign attest-blob --yes --type slsaprovenance --predicate provenance.json
   --bundle checksums.txt.slsa.bundle checksums.txt`.
4. Per-archive SPDX SBOMs via the already-used anchore/sbom-action (syft
   `file:` source), uploaded alongside — parity with the image's SBOM.
5. `gh release upload "$TAG" dist-bin/*.tar.gz dist-bin/*.zip checksums.txt
   checksums.txt.cosign.bundle checksums.txt.slsa.bundle *.spdx.json`.

### Verification docs

SECURITY.md gains a section next to the existing image/chart verification:

```sh
cosign verify-blob \
  --bundle checksums.txt.cosign.bundle \
  --certificate-identity-regexp 'https://github.com/maverickd650/kubepage-operator/\.github/workflows/release\.yaml@.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

## Testing

- **Loader unit tests** (`internal/preview`): multi-doc parsing, directory
  walking, unknown-kind skip, namespace defaulting, zero/multiple-Dashboard
  selection errors, Secret resolution end-to-end through the existing
  `SecretValueSource` path.
- **Boot smoke test**: start preview against `config/samples/`, assert `GET /`
  returns 200 and contains the sample Dashboard's title — doubles as a
  regression guard that the samples stay loadable.
- Rendering itself is already covered by the golden and `//go:build browser`
  chromedp tests; preview adds no new render paths by construction.
- Release job: dry-run the build-dist task in the existing `test.yml` (build
  all targets, no upload) so cross-compilation breakage surfaces on PRs, not
  at release time.

## Phasing

| Phase | Deliverable | Commits |
|-------|-------------|---------|
| 1 | `preview` subcommand, manifest loader, in-memory reader wiring, `mise run preview`, README + CLAUDE.md ("three modes in one") | `feat(preview): …` |
| 2 | fsnotify live reload, `--open` | `feat(preview): …` |
| 3 | `build-dist` task, `binaries` release job, signing/attestation, SECURITY.md verification docs | `build: …`, `ci(release): …`, `docs(security): …` |
| 4 (stretch) | `--sample-data` placeholder fields per widget type; client-side CEL validation warnings | separate design |

Phases 1–2 are pure additions to `cmd/main.go` + a new `internal/preview`
package; nothing in `internal/dashboard` or `internal/controller` changes,
so the blast radius on the in-cluster paths is zero.

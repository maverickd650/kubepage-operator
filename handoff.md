# Handoff: templ/htmx rendering efficiency & feature pass

Session environment had `go` (with module downloads working against
`registry.npmjs.org` and the Go module proxy) but no `mise` binary, no
`controller-gen`/`kustomize`, and no working `golangci-lint custom` build
(`golangci-lint custom` needs `git clone https://github.com/golangci/golangci-lint`,
which this sandbox's network policy blocks). Please run the following before
merging, from a machine where `mise` tasks work:

```sh
mise run lint        # golangci-lint-custom couldn't be built here at all
mise run test         # full suite incl. envtest-backed internal/controller
                       # and cmd specs — this session's sandbox has no
                       # /usr/local/kubebuilder/bin/etcd, so those two specific
                       # pre-existing tests (TestRunStopsAllGoroutinesOnContextCancel
                       # in internal/dashboard, TestOwnDashboardImageAgainstRealAPIServer
                       # in cmd) could not be exercised — they are unrelated to
                       # this change (no cmd/main.go edits) and were already
                       # failing the same way before this branch's commits.
```

## What *was* verified in this session

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `gofmt -l .` — no output (clean).
- `go tool templ generate ./...` — ran successfully, `*_templ.go` outputs
  committed alongside their `.templ` sources.
- `go test ./internal/dashboard/... ./cmd/... ./api/...` — all pass except
  the two envtest-dependent tests noted above (pre-existing, unrelated).
- No `+kubebuilder` markers or API types changed, so `mise run manifests
  generate` (controller-gen/DeepCopy) is **not needed** for this change —
  nothing in `api/v1alpha1` was touched.
- `go.mod`/`go.sum` unchanged — every addition uses only the stdlib
  (`bytes`, `compress/gzip`, `hash/fnv`, `io`, `strconv`).

## What this PR does

Follows up on a rendering-infrastructure review (templ/htmx) comparing
against gethomepage/homepage. Scope was kept deliberately narrow to avoid
colliding with PR #64 (security-review hardening), which is also open
against `main` and already implements the nonce-based CSP — see
"Overlap with PR #64" below.

1. **Vendored htmx 2.0.4 → 2.0.10** (`internal/dashboard/assets/`) — picks
   up `CSS.escape()`-hardened settle lookups, `reportValidity()` form
   handling, and several DOM/Shadow-DOM fixes accumulated since 2.0.4.
   Downloaded from `registry.npmjs.org` (checksum-verified against the
   registry's published sha1) since the jsDelivr CDN is blocked by this
   sandbox's proxy policy — worth a spot-check that the file is byte-correct
   once you're on an unrestricted network, though the checksum match should
   already cover that.
2. **`hx-preserve` on the iframe widget** (`cards.templ`) — previously every
   poll (`hx-swap="innerHTML"` on `#cards`) tore down and rebuilt the whole
   card grid, which meant an embedded iframe widget (e.g. a Grafana panel)
   fully reloaded on every refresh cycle. Giving it a stable `id` +
   `hx-preserve="true"` lets htmx carry the live DOM node across swaps.
3. **Visibility-gated polling** (`index.templ`) — `hx-trigger` on `#cards`
   and the `every Ns` half of `#header`'s trigger now carries a
   `[document.visibilityState === 'visible']` event filter, so a
   backgrounded browser tab stops firing poll requests. The header's `load`
   trigger is deliberately left unfiltered so a page opened in a background
   tab still gets its initial header render.
4. **ETag/304 + gzip on `/fragment` and `/header`** (`server.go`) — these are
   the two htmx-polled routes; they're now rendered into a buffer, hashed
   (FNV-1a, not a version counter, so it's byte-correct even though the
   output depends on more than just `Store` — `Configuration`/`Bookmark`/
   `InfoWidget` also feed in via `LoadSite`), and served with
   `Cache-Control: no-cache` + `ETag`. Browsers automatically revalidate
   with `If-None-Match` on the next poll; an unchanged dashboard now gets a
   bodyless 304 instead of re-sending the full card grid every cycle. The
   body is gzip-compressed whenever `Accept-Encoding` allows.
5. **iframe widget URL scheme restriction** (`iframe.go`) — `cfg.URL` must
   now be `http://`/`https://` (reusing `isHTTPURL` from
   `render_helpers.go`). This is defense-in-depth: the CSP's `frame-src
   https:` already blocks a `javascript:` iframe today, but this closes the
   gap at the Go layer too, independent of the CSP staying a correct
   compile-time constant.
6. **PWA manifest icon** (`server.go` + new `assets/icon.svg`) — the
   manifest previously had no `icons` array, so Chrome/Android wouldn't
   actually offer "Install" despite `display: standalone` being set. Added
   a small embedded SVG icon (`sizes: "any"`, which satisfies installability
   checks without needing multiple raster sizes).

## Overlap with PR #64 (deliberately avoided)

PR #64 (`claude/security-review-recommendations-y1iho1`) already implements
nonce-based CSP (`templ.WithNonce`/`templ.GetNonce`) touching the same
`<style>`/`<script>` tags in `index.templ`, `server.go`'s
`securityHeaders`/`contentSecurityPolicy`, and `render_helpers.go`'s
`backgroundStyle`/new `customStyle`/`customScript` helpers. That was
originally item "security #1" (nonce CSP) in the review this PR implements
— it's intentionally **not** duplicated here to avoid a straight collision;
whichever of the two PRs merges second will need a small rebase (nonce
attributes on the same tags this PR left as-is). No other file this PR
touches (`cards.templ`, `iframe.go`, the manifest-icon changes in
`server.go`) overlaps with PR #64's diff.

## Deliberately deferred (not in this PR)

- **CSS extraction to a static asset** — the ~170-line inline `<style>`
  block in `index.templ` would be a clean move to `/assets/style.css`
  (saves re-sending unchanging CSS on every full page load, and it's
  cacheable), but it's the same `<style>` tag PR #64 already modifies for
  the CSP nonce. Doing it now would guarantee a conflict; revisit once #64
  lands.
- **idiomorph adoption** (`hx-swap="morph:innerHTML"`) — would remove the
  hand-rolled `captureGroupState`/`restoreGroupState`/tab-restore JS
  entirely and give smoother swaps generally (not just for iframes), but
  it's a bigger, more opinionated change worth its own PR and testing pass.
- **View transitions** (`transition:true` on the `hx-swap`) — small, low
  risk, purely cosmetic; skipped only for time, not because of any
  conflict — a good next small PR.
- **htmx 4** — still beta (`4.0.0-beta5` as of this session), stable not
  expected until early 2027; 2.x is supported "in perpetuity" per upstream,
  so no urgency.
- **PWA `shortcuts`** (top-N services as install shortcuts) — needs access
  to ordered `Site` data inside `handleManifest`, which currently only
  builds a `pwaManifest` from `Configuration` fields, not bound
  `ServiceEntry`/`Bookmark` lists. Small follow-up, skipped to keep this PR
  focused.

# Gap analysis: kubepage-operator vs. gethomepage/homepage

**Date:** 2026-07-01 · **Compared against:** homepage as documented at
[gethomepage.dev](https://gethomepage.dev/) (widget/setting inventory as of
mid-2026)

This document compares the operator's native dashboard against
[gethomepage/homepage](https://github.com/gethomepage/homepage) along two
axes — **feature coverage** and **visual similarity** — and proposes a
prioritized plan for closing the gaps that are worth closing. It is a
roadmap document, not a commitment; each item notes where it lands in this
codebase and a rough size.

kubepage is deliberately **not** a homepage port: it is CRD-driven instead
of YAML-file-driven, a single Go binary instead of a Next.js app, and
curates a small widget set instead of shipping ~140 integrations. Several
homepage features are therefore listed as intentional non-goals rather than
gaps.

---

## 1. Where parity already exists

These homepage capabilities are already implemented (largely via the
"homepage UI parity" work in PR #47 and the layout/search work before it):

| Area | Status |
|------|--------|
| Site settings | title, description, favicon, fixed `theme` (light/dark), fixed `color` (all 22 homepage palettes), background image + blur/saturate/brightness/opacity, `cardBlur`, `headerStyle` (underlined/boxed/clean/boxedWidgets), `fullWidth`, `target`, `disableCollapse`, `groupsInitiallyCollapsed`, `useEqualHeights`, `bookmarksStyle: icons`, `disableIndexing` (robots + noindex), PWA `startUrl`, `customCSS` |
| Layout | tabs, per-group `columns`/`style: row`/`icon`/`header`/`initiallyCollapsed`/`useEqualHeights` (service groups) |
| Services | groups, ordering, href/target, description, `showStats`, `hideErrors`, multiple widgets per card, `fields` filtering, block **highlighting** (full rule set: numeric + string operators, negate, case sensitivity, valueOnly) |
| Status monitoring | `ping`, `siteMonitor`, `statusStyle: dot/basic` — plus `podSelector` (pod-readiness up/down), which is *stronger* than homepage's per-service Kubernetes pod status because it needs no external URL |
| Bookmarks | groups, `abbr`, icon, description, icons-only style |
| Icons | dashboard-icons slugs, `mdi-*` and `si-*` via Iconify (incl. `-#hexcolor` recolor), `sh-*` selfh.st icons with format suffix — full homepage prefix convention |
| Search | header search box (duckduckgo/google/bing/custom provider, target, as-you-type card filtering) |
| Quick launch | Ctrl/Cmd-K or `/` palette, fuzzy jump to any card, web-search fallthrough |
| Info widgets | `datetime`, `greeting`, `openmeteo` (weather), `kubemetrics` (≈ homepage's `kubernetes` info widget), with icon + highlight + usage-bar rendering |
| Theme/color switching | runtime switcher persisted in localStorage when Configuration doesn't fix the axis (homepage itself has no runtime switcher — this is a deliberate superset) |

## 2. Feature gaps

### 2.1 Service-widget coverage — the biggest functional gap

homepage ships **~140 service widgets**; kubepage registers **14**:
`cloudflared`, `grafana`, `homeassistant`, `kubemetrics`, `linkwarden`,
`mealie`, `openmeteo`, `paperlessngx`, `plex`, `prometheus`,
`prometheusmetric`, `stash`, `truenas`, `unifi`.

Chasing 1:1 coverage is a non-goal (each widget is hand-written Go here,
not community JS), but two things close most of the practical gap:

1. **A generic `customapi` widget** (homepage has one): URL + JSON-path
   field mappings (`label`, `jsonpath`, optional `suffix`/`format`),
   secrets via the existing `ServiceWidget.Secrets` machinery. One widget
   covers the entire long tail of "any service with a JSON status
   endpoint". kubepage's `prometheusmetric` already proves the pattern
   (arbitrary query → fields).
2. **A short curated expansion** driven by the project's homelab audience.
   Strong candidates that pair naturally with the existing set:
   qBittorrent/Transmission, Sonarr/Radarr/Prowlarr, Jellyfin/Jellyseerr,
   AdGuard Home/Pi-hole, Immich, Uptime Kuma, Gitea/Forgejo, ArgoCD.

Architecture note: adding a widget is already a one-file change
(implement `Widget` in `internal/dashboard/<type>.go` + `init()`
registration + table test); no poller/server/store changes needed.

### 2.2 Kubernetes auto-discovery — the most strategic gap

homepage discovers services from **Ingress / Traefik IngressRoute /
Gateway API HTTPRoute annotations** (`gethomepage.dev/enabled`,
`.../name`, `.../group`, `.../icon`, `.../href`, widget fields, …).
kubepage — *a Kubernetes operator* — requires an explicit `ServiceEntry`
per service. This is the one place homepage is more Kubernetes-native
than the Kubernetes-native project, and closing it removes the biggest
adoption friction (a card appears the moment an app is deployed).

Proposed shape (opt-in per Instance):

- `Instance.spec.discovery`: `enabled`, optional `ingressSelector`
  (label selector), annotation prefix (default `kubepage.io/`, with an
  optional compatibility toggle to also honor `gethomepage.dev/*` so
  existing homepage users can migrate without relabeling).
- The **dashboard process** (not the controller) lists annotated
  Ingresses/HTTPRoutes in its namespace through its existing cached
  client and synthesizes cards, merged after explicit `ServiceEntry`
  cards (explicit CRDs win on name collision). No new CRDs are written,
  so there's nothing to garbage-collect and no reconcile loop churn.
- RBAC: extend the per-Instance Role (`instance_rbac.go`) with
  `get/list/watch` on `ingresses` (+ `httproutes` when Gateway API is
  detected), only when discovery is enabled.
- Secrets in annotations are **not** supported (annotations are
  world-readable to anyone who can read the Ingress); discovered cards
  get href/icon/description/group/ping only, and a doc pointer to
  `ServiceEntry` for widget-bearing cards.

### 2.3 Info-widget coverage

homepage info widgets: glances, greeting, kubernetes, logo, longhorn,
openmeteo, openweathermap, resources, search, stocks, unifi_console,
datetime. kubepage has 4 (datetime, greeting, openmeteo, kubemetrics).

Gaps worth closing, in rough value order:

| Widget | Notes | Size |
|--------|-------|------|
| `logo` | Static image in the header strip; trivial (options: `icon`/URL, `href`) | XS |
| `resources`-style node metrics | `kubemetrics` covers cluster CPU/mem; homepage's `resources` also shows disk. A `kubemetrics` option for per-node breakdown + node disk (kubelet stats) gets closest without a host agent | M |
| `glances` | The standard answer for host metrics outside the cluster; plain JSON API, fits the `Widget` interface (also useful as a *service* widget) | S |
| `longhorn` | Popular on exactly this audience's clusters; JSON API | S |
| Second weather provider (`openweathermap`) | openmeteo already covers the no-key case; OWM for users who want it | S |
| `stocks` (Finnhub) | Low homelab relevance — defer until requested | S |
| `unifi_console` (header variant) | The `unifi` service widget exists; header placement is mostly plumbing (InfoWidget type reusing the same poll code) | S |

Also in this area: **InfoWidget `datetime` ignores its `format` option** —
`header.templ` emits `data-format`, but the clock JS in `index.templ`
hardcodes `dateStyle: "medium", timeStyle: "medium"`. homepage supports
`text_size` and `format` (Intl.DateTimeFormat options). Honoring the
already-emitted attribute is a small fix and should be treated as a bug,
not a feature.

### 2.4 Layout gaps

- **Bookmark groups don't participate in `layout`.** homepage's layout
  map styles bookmark groups too (columns, style, icon, initially
  collapsed); kubepage's `LayoutGroupSpec` only applies to service
  groups, and bookmark groups always render after all tabs with default
  styling. Extending layout resolution (`site.go` + `cards.templ`) to
  match bookmark groups by name is a contained change.
- **Nested groups (subgroups)** — homepage supports one level of group
  nesting; kubepage explicitly documents "not supported". Requires a
  `parent`/path notion on `Group` and recursive rendering. Real work,
  moderate demand — schedule after the above.
- **Per-widget/per-card refresh interval** — homepage widgets accept
  `refreshInterval`; kubepage has one global `--poll-interval`. A
  `pollIntervalSeconds` on `ServiceWidget`/`InfoWidget` (floor-clamped;
  the poller already tracks per-widget state in `Store`) covers slow
  upstreams like weather vs. fast ones like Prometheus.

### 2.5 Search & quick-launch options

homepage's `quicklaunch` settings: `provider`, `searchDescriptions`,
`hideInternetSearch`, `hideVisitURL`, `showSearchSuggestions`. kubepage's
palette is fixed-behavior (always searches descriptions, always offers
web search, no URL detection, no suggestions).

- Reuse `Configuration.spec.search` for the palette (it already defines
  provider/target); add `quickLaunch` sub-fields for the toggles. S.
- **Search suggestions** need a server-side proxy endpoint (browser CORS
  blocks direct suggestion APIs) — new `/suggest` route on the dashboard
  server with an allowlisted provider set, subject to the same SSRF
  guards as widget polling. M; do only if requested.

### 2.6 Internationalization

homepage ships 40+ locales. kubepage's `Configuration.language` currently
only sets `<html lang>`; the UI's handful of strings ("Search or filter
cards...", "Status", the quick-launch placeholder, error prefix) are
hardcoded English. The honest options:

1. Document `language` as affecting only `lang`/locale-aware formatting
   (dates already follow the browser locale) — zero cost, and
   `customCSS`-level users can live with it; or
2. Add a small message catalog (Go map, ~10 keys) for the major locales.

Recommend (1) now, (2) only on demand. Full homepage-style translation
coverage is a non-goal.

### 2.7 Smaller settings/behavior gaps

| homepage feature | kubepage state | Verdict |
|---|---|---|
| `customJS` | only `customCSS` | Add alongside customCSS (same operator-trust argument, same injection path); XS |
| Site-wide `statusStyle` / `hideErrors` defaults | per-ServiceEntry only | Add Configuration-level defaults that entries override; XS |
| `iconStyle: theme` (mdi icons tinted to theme color) | `-#hexcolor` suffix only | Add: resolve mdi/si icons with the accent color when set; XS |
| `iframe` service widget | none | Add a sandboxed `iframe` widget type (`sandbox` attr, operator-trusted URL); S |
| Version footer + `hideVersion` | no version display (version/commit are already stamped into the binary since PR #57) | Add footer + toggle; XS |
| `maxGroupColumns`, `useEqualHeights`, `groupsInitiallyCollapsed` | covered | — |
| `providers` block (shared API keys) | per-widget `secrets` with `secretKeyRef` | kubepage's model is better-suited to K8s; non-goal |

### 2.8 Intentional non-goals

- **Docker/Podman label discovery** — kubepage is Kubernetes-only by
  design.
- **1:1 port of all ~140 widgets** — `customapi` + curation instead.
- **homepage's YAML config files** — CRDs are the whole point.
- **Runtime-editable settings UI** — config flows through the API server.

## 3. Visual similarity

The overall skeleton matches homepage: header strip of info widgets →
search box → tabs → grouped card grids with icon/title/status/description
and a bottom row of stat blocks → bookmark groups. The four header styles,
22-palette theming, background-image treatment (blur/saturate/brightness/
opacity + card translucency/backdrop-blur), status dot vs. pill, highlight
tinting, and usage bars all track homepage's look closely.

Remaining visual deltas, smallest-effort-first:

1. **Typeface** — kubepage uses bundled Manrope; homepage uses the
   Tailwind system-UI stack. This reads as a (nice) deliberate identity
   choice; keep it, but document it as such.
2. **`boxedWidgets` header style** is currently rendered identically to
   `boxed` for group headers; in homepage it specifically boxes the
   *header info widgets*. The header strip does always get a panel
   background here, so the practical difference is small — make
   `boxedWidgets` toggle a stronger boxed treatment on `.header-widget`
   and leave group headers un-boxed, matching homepage semantics. XS.
3. **Header strip alignment** — homepage right-aligns info widgets
   (greeting/datetime left, resources/search right); kubepage renders one
   left-aligned wrapping row. Add left/right slots (e.g. greeting +
   datetime left, everything else right) or an `align` option per
   InfoWidget. XS–S.
4. **Initial-load flash** — the card grid is empty until the first htmx
   fetch completes; homepage shows skeleton placeholders. Server-render
   the first fragment into the page shell (the data is already in
   `Store`) and let htmx take over from there — removes the flash with
   no skeleton CSS needed. S.
5. **Card details** — homepage's icons are slightly larger (~2rem vs
   1.2rem) with the title stacked beside them, and stat blocks sit flush
   to the card bottom under `useEqualHeights`. Pixel-tuning pass over
   `index.templ`/`cards.templ` against a homepage screenshot. S.

## 4. Proposed plan

Phased so each lands independently; sizes are XS (<½ day) / S (~1 day) /
M (2–4 days) / L (1–2 weeks).

### Phase 1 — quick wins & bug-level fixes

| # | Item | Where | Size |
|---|------|-------|------|
| 1.1 | Honor `datetime` `format` option (bug) | `index.templ` clock JS + `header.templ` | XS |
| 1.2 | `logo` info widget | new InfoWidget type in `site.go`/`header.templ` | XS |
| 1.3 | `customJS` Configuration field | `configuration_types.go`, `index.templ` | XS |
| 1.4 | Site-wide `statusStyle`/`hideErrors` defaults | `configuration_types.go`, `site.go` | XS |
| 1.5 | Version footer + `hideVersion` | `index.templ`, `configuration_types.go` | XS |
| 1.6 | `boxedWidgets` styles the header strip (visual §3.2) | `index.templ` CSS | XS |
| 1.7 | Server-render first card fragment (visual §3.4) | `server.go`, `index.templ` | S |

### Phase 2 — the two strategic items

| # | Item | Where | Size |
|---|------|-------|------|
| 2.1 | `customapi` generic widget (JSON-path field mapping, secrets, highlight-compatible) | new `internal/dashboard/customapi.go` | M |
| 2.2 | Ingress/HTTPRoute annotation auto-discovery (opt-in, `Instance.spec.discovery`, `gethomepage.dev/*` compat toggle, no secret-bearing annotations) | `instance_types.go`, `instance_rbac.go`, new `internal/dashboard/discovery.go`, docs | L |

### Phase 3 — coverage expansion

| # | Item | Size |
|---|------|------|
| 3.1 | Info widgets: `glances`, `longhorn`, `openweathermap` | S each |
| 3.2 | Per-widget `pollIntervalSeconds` (ServiceWidget + InfoWidget) | M |
| 3.3 | Curated service widgets by demand (qBittorrent, Sonarr/Radarr, Jellyfin, AdGuard Home, Immich, Uptime Kuma, ArgoCD, …) — track as individual `feat(dashboard)` issues | S each |
| 3.4 | `iframe` service widget (sandboxed) | S |

### Phase 4 — layout & UX depth

| # | Item | Size |
|---|------|------|
| 4.1 | Bookmark groups in `layout` (columns/style/icon/collapsed) | M |
| 4.2 | Quick-launch options (`searchDescriptions`, `hideInternetSearch`, `hideVisitURL`) under `Configuration.spec.search` | S |
| 4.3 | Header strip left/right alignment slots (visual §3.3) | S |
| 4.4 | Card pixel-tuning pass vs. homepage screenshots (visual §3.5) | S |
| 4.5 | Nested groups (subgroups) — only if user demand materializes | L |
| 4.6 | Search suggestions proxy (`/suggest`, SSRF-guarded) — only on demand | M |

### Explicitly deferred / declined

Docker discovery, full widget-catalog parity, YAML config compatibility,
i18n message catalogs (document current `language` semantics instead),
`stocks` widget, homepage `providers` block.

## 5. Ground rules for the work

Every phase item follows the existing conventions: new widgets are
one-file additions implementing `Widget` (+ table tests, no
poller/server/store changes); CRD changes go through `*_types.go` +
`mise run manifests generate` with CEL admission where cross-field
validation is needed; `.templ` edits are followed by
`mise run templ-generate`; secrets stay on the in-process,
uncached-client resolution path; anything that adds RBAC to the dashboard
pod is scoped per-Instance in `instance_rbac.go` and called out in the PR
for review.

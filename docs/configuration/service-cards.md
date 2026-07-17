# Service cards

A **service card** (or "tile") is the main unit of a dashboard: a box with a
name, an icon, a description, a link to open the service, and optionally an
up/down status light and live [widgets](widgets.md).

You define cards with a **ServiceCard** object. One ServiceCard file can hold a
single tile or a whole page's worth — it's up to you.

## The smallest possible card

```yaml
apiVersion: page.kubepage.dev/v1alpha1
kind: ServiceCard
metadata:
  name: my-services
  namespace: dashboards
spec:
  group: Media            # the heading this card sits under
  services:
    - name: Plex
      href: http://plex.example.com
```

That's a clickable "Plex" tile under a "Media" heading. Everything else on this
page is optional polish.

## Many cards in one file

`services:` is a list — add as many tiles as you like. The top-level `group:`
acts as the default heading; any tile can override it with its own `group:`.

```yaml
spec:
  group: Media                     # default heading for tiles below
  services:
    - name: Plex
      href: http://plex.example.com
      icon: plex
    - name: Sonarr
      href: http://sonarr.example.com
      icon: sonarr
    - name: Grafana
      group: Observability         # this one sits under its OWN heading instead
      href: http://grafana.example.com
      icon: grafana
```

## Card fields

Everything you can set on a single tile (an entry under `services:`):

| Field | What it does |
|-------|--------------|
| `name` | **Required.** The tile's title. |
| `href` | Makes the title a clickable link to the service (the browser-facing URL). Also the fallback base URL for widget polls and `monitor: self` when `internalUrl` isn't set. |
| `internalUrl` | The in-cluster base URL the dashboard **pod** uses for widget polls and `monitor: self`, when it differs from `href` (e.g. `http://plex.media.svc:32400` behind an ingress). |
| `icon` | The logo. See [Icons](#icons). |
| `description` | A line of text under the title. |
| `group` | Which heading this tile appears under. Falls back to the file's top-level `group`. Supports nesting — see [Groups & nesting](#groups-and-nesting). |
| `order` | A number to control position. Lower numbers come first; ties break alphabetically. |
| `target` | `_blank` opens the link in a new tab (default), `_self` in the same tab. |
| `monitor` | An HTTP up/down status light: a URL, or `self` to probe the entry's own base URL — see [Status lights](#status-lights). |
| `app` / `podSelector` | A Kubernetes pod-readiness status light, shown alongside `monitor` if both are set — see [Status lights](#status-lights). |
| `namespace` | Which namespace `app`/`podSelector` list pods in, if not this ServiceCard's own — see [Status lights](#status-lights). |
| `statusStyle` | `dot` (a coloured dot, the default) or `basic` (a coloured status pill with status word plus latency/ready-count detail). |
| `showStats` | Set `false` to hide widget numbers but keep the tile. Default `true`. |
| `errorDisplay` | Set `false` to hide a widget's error text on a service you know is flaky. Default `true`. |
| `widgets` | Live stats pulled from the service. Its own big topic — see [Widgets](widgets.md). |

## Icons

The `icon` field is flexible. In order of how most people use it:

1. **Just name the service** — `icon: plex`, `icon: sonarr`, `icon: nextcloud`.
   These resolve to official logos from the
   [dashboard-icons](https://github.com/homarr-labs/dashboard-icons) collection.
   Try the service's lower-case name first; it usually works.
2. **A full URL** to any image — `icon: https://example.com/logo.png`. Passed
   through untouched.
3. **A generic glyph** using a short prefix, when there's no brand logo:
   - `mdi-server` — Material Design Icons
   - `si-docker` — Simple Icons
   - `lucide-database` — Lucide
   - `wi-day-sunny` — Weather Icons
   - `fa6-solid-house` — Font Awesome 6 Solid
   - `sh-something` — [selfh.st/icons](https://selfh.st/icons/)

   You can tint a glyph by appending `-#hexcolour`, e.g. `mdi-server-#ff0000`.

If you can't find a logo, the [dashboard-icons site](https://dashboardicons.com/)
lets you search the available names.

## Status lights

A tile can show up to **two independent** status lights: an HTTP one
(`monitor`) and a Kubernetes pod one (`app`/`podSelector`). Set either, both,
or neither — this mirrors
[homepage's Kubernetes status](https://gethomepage.dev/configs/kubernetes/).

**HTTP monitor** — one field, `monitor`:

- **`monitor: self`** — the usual choice. Probes the entry's own base URL
  (`internalUrl` when set, else `href` — the probe runs from the dashboard
  pod, so the in-cluster URL wins) and shows up/down **plus response time**.
- **`monitor: http://plex.example.com`** — an explicit URL, when the probe
  target differs from the entry's own base URL.

The probe is plain HTTP (not raw network ping), so it needs no special
permissions.

> **Migrating from `ping`/`siteMonitor`?** Both fields were merged into
> `monitor` — they were always the same HTTP probe. Replace either one with
> `monitor: <same URL>` (or just `monitor: self` if the URL equals the
> entry's `href`/`internalUrl`).

**Pod monitor** — for services that run as pods *in this cluster*. It watches
pod readiness instead of hitting a URL, so it needs no reachable address, and
it's three-valued: **Up** (every matching pod Ready), **Partial** (some Ready,
rendered amber), or **Down** (none Ready, or nothing matched):

- **`app: my-app`** — the usual choice, homepage parity. Shorthand for the
  standard `app.kubernetes.io/name: my-app` selector:

  ```yaml
  - name: My App
    monitor: http://my-app.example.com
    app: my-app # shows a second, pod-readiness status light alongside monitor
  ```

- **`podSelector:`** — an explicit label selector, for pods that don't carry
  the standard `app.kubernetes.io/name` label. Overrides `app` when both are
  set:

  ```yaml
  - name: My App
    podSelector:
      matchLabels:
        custom-label: my-app
  ```

- **`namespace: other-ns`** — list pods in a namespace other than this
  ServiceCard's own (requires `app` or `podSelector`). A namespace other than
  the ServiceCard's own must also be listed in the `Dashboard`'s
  `spec.monitorNamespaces` (an explicit allowlist, mirroring
  `spec.discovery.namespaces`'s cross-namespace grant), for example:

  ```yaml
  apiVersion: page.kubepage.dev/v1alpha1
  kind: Dashboard
  metadata:
    name: dashboard-sample
  spec:
    monitorNamespaces:
      - other-ns
  ```

  Without that grant the light renders Down with a card error naming the
  missing `monitorNamespaces` entry, rather than a raw permission error.

With several matching pods, the pod light's tooltip/text shows `2/3 ready`.

The look is controlled by `statusStyle` (`dot` — up to two dots — or `basic`
— a coloured status pill per light, e.g. `Up · 12ms` and `2/3 ready`), which
you can also set site-wide via the Dashboard's [spec.style](appearance.md). It applies to
both lights when both are configured.

> A status light needs **no credentials** — it only checks reachability/pod
> readiness. To show real numbers *from inside* a service (streams, disk,
> queues), you want a [widget](widgets.md).

## Groups and nesting

Tiles are organised under **group** headings. Tiles sharing a `group` render
together. To create a group, you simply name it on a tile — there's no separate
"create group" step.

### Nested groups

You can nest a group inside another using a `/`-separated path, up to **three
levels deep**:

```yaml
services:
  - name: Radarr
    group: Media/Movies        # "Movies" nested inside "Media"
    href: http://radarr.example.com
  - name: Sonarr
    group: Media/TV            # "TV" nested inside "Media"
    href: http://sonarr.example.com
```

This produces a "Media" section containing "Movies" and "TV" subsections. The
parent "Media" doesn't need its own tile — the heading appears automatically.

Full design notes live in [../design/nested-groups.md](../design/nested-groups.md),
but you don't need them for everyday use.

### Ordering groups and tiles

Give tiles (or, via [layout tabs](appearance.md#tabs-and-layout), groups) an
`order:` number to control their position. Lower first; unset sorts last; ties
break alphabetically by name.

## How many ServiceCard files should I create?

Whatever's convenient — the result is identical. Two common styles:

- **One file per group** (`media.yaml`, `infrastructure.yaml`…), each with all
  that group's tiles. Easy to find things.
- **One big file** with everything. Fewer files to manage.

There's no de-duplication across files, so don't define the same tile twice.

## Next

- Add live stats to a tile → **[Widgets](widgets.md)**
- Arrange groups into tabs, or change their column count → **[Appearance](appearance.md#tabs-and-layout)**
- A card isn't showing up → **[Troubleshooting](troubleshooting.md)**

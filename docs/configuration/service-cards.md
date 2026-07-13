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
  dashboardRef:
    name: home            # your Dashboard's name
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
  dashboardRef:
    name: home
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
| `href` | Makes the title a clickable link to the service. |
| `icon` | The logo. See [Icons](#icons). |
| `description` | A line of text under the title. |
| `group` | Which heading this tile appears under. Falls back to the file's top-level `group`. Supports nesting — see [Groups & nesting](#groups-and-nesting). |
| `order` | A number to control position. Lower numbers come first; ties break alphabetically. |
| `target` | `_blank` opens the link in a new tab (default), `_self` in the same tab. |
| `ping` / `siteMonitor` / `podSelector` | An up/down status light. Pick **one** — see [Status lights](#status-lights). |
| `statusStyle` | `dot` (a coloured dot, the default) or `basic` (up/down text). |
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

A status light shows whether a service is up. Choose **exactly one** of these
per tile (setting more than one is rejected when you apply):

- **`siteMonitor: http://plex.example.com`** — the usual choice. Fetches the URL
  over HTTP and shows up/down **plus response time**.
- **`ping: http://plex.example.com`** — similar, a lighter reachability + latency
  check. (Despite the name it uses HTTP, not raw network ping, so it needs no
  special permissions.)
- **`podSelector:`** — for services that run as pods *in this same cluster*. It
  watches pod readiness instead of hitting a URL, so it needs no reachable
  address:

  ```yaml
  - name: My App
    podSelector:
      matchLabels:
        app.kubernetes.io/name: my-app
  ```

  With several matching pods, the tile shows `2/3 ready`.

The look is controlled by `statusStyle` (`dot` or `basic`), which you can also
set site-wide in [DashboardStyle](appearance.md).

> A status light needs **no credentials** — it only checks reachability. To show
> real numbers *from inside* a service (streams, disk, queues), you want a
> [widget](widgets.md).

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

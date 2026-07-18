# Widget reference

A lookup table for every widget type. If the concepts (`type`, `url`, `secrets`,
`config`) are new, read [Widgets](widgets.md) first — this page assumes them.

**How to read the "Credential" column:** it names the field(s) to put under
`secrets:`. For example "**`apiKey`**" means:

```yaml
secrets:
  apiKey:
    secretKeyRef:
      name: your-secret
      key: apiKey
```

See [Secrets & credentials](secrets.md) for creating the Secret itself, and
that page's `secretRef` shorthand for when your Secret's key names already
match the credential field name(s) listed here — e.g. `secretRef: your-secret`
in place of the `secrets:` block above.

**Two families of widget:**

- **Service-card widgets** attach to a tile's `widgets:` list (most of the table).
- **Header widgets** attach to an [InfoWidget](header-widgets.md) instead — they
  live in the top strip, not on a card. They're marked *header* below.

The two lists don't overlap: a header widget can't go on a card, and vice versa.

---

## Service-card widgets

### Need no credentials (just a `url`)

| Type | Shows |
|------|-------|
| `prometheus` | Prometheus target-health summary (Targets Up/Down/Total) |
| `netdata` | Netdata active-alarm counts (warnings/criticals) |
| `gatus` | Gatus endpoint up/down counts from the latest check |

```yaml
- type: prometheus
  url: http://prometheus.example.com
```

### Need one token / API key

Give the credential under `secrets:` using the field name shown.

| Type | Shows | Credential (field name) |
|------|-------|-------------------------|
| `plex` | Active Plex stream count | `token` (Plex `X-Plex-Token`) |
| `stash` | Stash library stats | `token` (API key) |
| `paperlessngx` | Paperless-ngx document stats | `token` |
| `linkwarden` | Linkwarden saved-link/collection/tag counts | `token` |
| `homeassistant` | Home Assistant people-home/lights-on/switches-on counts | `token` (long-lived access token) |
| `argocd` | Argo CD app counts by sync/health | `token` (Bearer token) |
| `gitea` | Gitea version + repo count | `token` |
| `sonarr` | Sonarr library/queue size | `apiKey` |
| `radarr` | Radarr library/queue size | `apiKey` |
| `jellyfin` | Jellyfin version + active streams | `token` |
| `jellyseerr` | Jellyseerr version + pending requests | `apiKey` |
| `immich` | Immich photo/video counts | `apiKey` |

```yaml
- type: sonarr
  url: http://sonarr.example.com
  secrets:
    apiKey:
      secretKeyRef: { name: sonarr-credentials, key: apiKey }
```

### Need a username + password (or an alternative token)

| Type | Shows | Credential |
|------|-------|-----------|
| `grafana` | Grafana dashboard/datasource/alert counts | `username`+`password`, **or** `token`. Needs **server-admin** credentials (a scoped viewer key won't work). |
| `adguard` | AdGuard Home DNS query/block stats | `username`+`password` (HTTP Basic, not an API key) |
| `nextcloud` | Nextcloud CPU/RAM/disk/active users | `key` (`NC-Token`, preferred) **or** `username`+`password` |
| `opnsense` | OPNsense CPU/RAM + WAN traffic | `username`+`password` (API key/secret). Config: `wan` (interface name, default `wan`) |

```yaml
- type: grafana
  url: http://grafana.example.com
  secrets:
    username: { secretKeyRef: { name: grafana-credentials, key: username } }
    password: { secretKeyRef: { name: grafana-credentials, key: password } }
```

### Need a credential *and* a config setting

| Type | Shows | Credential | Config keys |
|------|-------|-----------|-------------|
| `unifi` | UniFi Network site health (Status, LAN/WLAN Users) | `apiKey` (Network Integration API key) | `site` (default `default`), `insecureTLS` |
| `truenas` | TrueNAS load/uptime/undismissed-alert counts | `token` (API key) | — |
| `cloudflared` | Cloudflare Tunnel status | `token` | **`accountId`, `tunnelId`** (both required) |
| `pihole` | Pi-hole v6 DNS stats | `password` (regular or app password) | — |
| `portainer` | Portainer container counts | `apiKey` (`X-API-Key`) | **`endpointId`** (required) |
| `tautulli` | Tautulli stream count + bandwidth | `apiKey` (sent as query param) | — |
| `proxmox` | Proxmox VM/LXC counts + CPU/RAM | `username` (`user!tokenid`) + `password` (token secret) | `node`, `insecureTLS` |
| `speedtest` | Speedtest Tracker latest result | `apiKey` (Bearer, v2 only) | `version` (1 or 2, default 1) |
| `mealie` | Mealie recipe/user/category/tag counts | `token` | `version` (1 or 2, default 2) |

```yaml
- type: cloudflared
  url: https://api.cloudflare.com
  secrets:
    token: { secretKeyRef: { name: cf-credentials, key: token } }
  config:
    accountId: "your-account-id"
    tunnelId: "your-tunnel-id"
```

### Special: no-auth but config required

| Type | Shows | Config |
|------|-------|--------|
| `uptime-kuma` | Uptime Kuma status-page up/down counts | **`slug`** (required). The status page must be **published**. No auth. |
| `iframe` | Embeds a web page directly on the card (instead of stats) | `url` is the page to embed; `height` (a CSS length like `"300px"`, default `"300px"`) |

```yaml
- type: uptime-kuma
  url: http://uptime.example.com
  config:
    slug: my-status-page
```

### Special: the do-anything widget — `customapi`

`customapi` reads **any** JSON web endpoint and turns chosen values into stats.
Use it for a service that has no dedicated widget above.

- `url` — the JSON endpoint.
- `secrets.token` — optional; sent as a Bearer token if set.
- `config.mappings` — **required**; a list of stats to extract. Each has:
  - `label` — the name shown on the card.
  - `jsonpath` — where to find the value in the JSON, dot-separated. Array
    positions are plain numbers: `data.0.value`.
  - `suffix` — optional text appended to the value, e.g. `"%"` or `" ms"`.

```yaml
- type: customapi
  url: http://myservice.example.com/api/status
  config:
    mappings:
      - label: Users
        jsonpath: stats.activeUsers
      - label: Load
        jsonpath: system.load.0
        suffix: "%"
```

For the JSON `{"stats":{"activeUsers":42},"system":{"load":[13,…]}}` that shows
**Users 42** and **Load 13%**.

---

## Header widgets (InfoWidget only)

These go in the top strip, configured as an [InfoWidget](header-widgets.md) — not
on a service card.

### Static (not polled, no service)

| Type | Shows | Config |
|------|-------|--------|
| `greeting` | Static text | `text` |
| `datetime` | A live clock (runs in your browser) | `format` (advanced; a JSON date-format object) |
| `logo` | A static logo image | `icon` for the image; `config.href` to link it |

### Live

| Type | Shows | Credential | Config |
|------|-------|-----------|--------|
| `openmeteo` | Current weather, **no API key needed** | — | **`latitude`, `longitude`** required; `units` (`metric`/`imperial`), `label` |
| `openweathermap` | Current weather via OpenWeatherMap | `apiKey` (required) | **`latitude`, `longitude`** required; `units`, `label` |
| `kubemetrics` | Cluster-wide CPU/RAM (reads the cluster, no URL) | — | `cpuLabel`, `memoryLabel` |
| `glances` | Host CPU/RAM via Glances | — | requires the typed `url:` field; `apiVersion` (`3`/`4`, default `4`) |
| `longhorn` | Aggregate Longhorn storage usage | — | requires the typed `url:` field — the Longhorn Manager address |

```yaml
# inside an InfoWidget's widgets: list
- type: openmeteo
  config:
    latitude: 51.5074
    longitude: -0.1278
    units: metric
    label: London
```

> Header widgets and service-card widgets both use **`config:`** for their
> extra settings, and both support a top-level typed `url:` field for the
> ones that poll an address.

---

## Notes that apply to every widget

- **`caCert`** — any widget can take a `caCert` (a `secretKeyRef` to a CA
  certificate) to trust a private/self-signed HTTPS certificate. Preferred over
  `insecureTLS`. See [Widgets → self-signed certificates](widgets.md#trusting-a-self-signed-certificate).
- **`pollIntervalSeconds`** — poll one widget less often than the rest (can only
  be slower than the global interval).
- **`fields`** / **`highlight`** — trim which stats show and colour them by value.
  See [Widgets → choosing what to show](widgets.md#choosing-what-to-show).
- **Config typos are flagged, not silently ignored** — an unrecognised `config`
  key raises a `ConfigValid=False` condition (visible in `kubectl describe`)
  without breaking the card, and a *missing required* key raises
  `Available=False`. See [Troubleshooting](troubleshooting.md).
- **Never put credentials in the `url`.** Use `secrets`. See
  [Widgets → golden rules](widgets.md#golden-rules-avoid-the-common-mistakes).

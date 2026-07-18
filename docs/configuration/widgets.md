# Widgets — a gentle introduction

Widgets are the part people find hardest, so this page goes slowly and assumes
nothing. Once it clicks, the [Widget reference](widget-reference.md) becomes a
simple lookup table.

## What a widget actually is

A [service card](service-cards.md) tile, on its own, is just a **link** with an
optional up/down light. A **widget** turns that tile *live*: it reaches into the
service, pulls out a number or two, and shows them right on the card.

- A **Plex** widget shows active streams and library totals (albums, movies, TV).
- A **Sonarr** widget shows how many episodes are queued and wanted.
- A **TrueNAS** widget shows version and uptime.
- A **custom** widget shows whatever you point it at.

Think of a widget as a tiny robot that, every 15 seconds, knocks on a service's
door, asks one question, and writes the answer on the card.

```
   ServiceCard tile "Plex"                     Plex server
  ┌───────────────────────┐                  ┌──────────────┐
  │  🎬  Plex             │   every 15s      │              │
  │  Media server         │  ───────────▶    │  "3 streams" │
  │  ● up   ⟨3 streams⟩   │  ◀───────────    │              │
  └───────────────────────┘   the widget      └──────────────┘
             ▲                 asks & shows
             └── the widget lives *inside* the tile
```

## Where a widget lives

A widget is **not** a separate file. It lives *inside* a service card, under a
`widgets:` list. A single tile can have zero, one, or several widgets.

```yaml
services:
  - name: Plex
    href: http://plex.example.com
    icon: plex
    widgets:                       # <- widgets attach here, inside the tile
      - type: plex                 # no url: inherits the tile's href
        secrets:
          token:
            secretKeyRef:
              name: plex-credentials
              key: token
```

> **Header widgets are different.** The clock, weather, and cluster-stats items
> in the strip across the *top* of the page are a separate building block called
> **InfoWidget**, configured on their own. This page is about the widgets that
> sit *on service cards*. See [Header widgets](header-widgets.md) for the top strip.

## The four parts of a widget

Every widget is described by up to four things. Only the first is always
required:

```yaml
- type: plex                       # 1. WHICH widget (required)
  url: http://plex.example.com     # 2. WHERE the service is (optional override)
  secrets:                         # 3. The KEY/PASSWORD to get in (if needed)
    token:
      secretKeyRef:
        name: plex-credentials
        key: token
  config:                          # 4. Any EXTRA settings this widget needs
    someOption: value
```

Let's take them one at a time.

### 1. `type` — which widget (always required)

The `type` picks the widget's behaviour and tells it how to talk to that
specific service. `plex` speaks Plex's language; `sonarr` speaks Sonarr's. You
must use a value from the [supported list](widget-reference.md) — a made-up type
is rejected when you apply the file.

### 2. `url` — where the service is

The base web address of the service, e.g. `http://plex.example.com` or
`http://192.168.1.50:8096`. Use the address the **dashboard** can reach — often
an internal cluster address, not the public one you use from your laptop.

**You usually don't need to set it.** A widget without its own `url` inherits
its tile's base URL: the tile's `internalUrl` when set, else its `href`. So a
tile whose widget polls the same address the card links to never repeats the
URL, and a tile that sets `internalUrl` (the in-cluster address) next to a
public `href` gives its widgets the in-cluster one automatically. Set `url`
only when the widget's address differs from both.

A few widgets don't need a `url` at all (the header-only ones like
`kubemetrics` read the cluster directly). The reference table tells you which.

### 3. `secrets` — the credential, stored safely

Most services won't hand out their stats to just anyone; they want an API key,
token, or password. That credential is sensitive, so **you never type it into
this file**. Instead you:

1. Store the credential in a Kubernetes **Secret** (a protected store).
2. **Point** the widget at it with `secretKeyRef`.

```yaml
secrets:
  token:                      # the field name this widget expects (see reference)
    secretKeyRef:
      name: plex-credentials  # the name of your Secret
      key: token              # which entry inside that Secret
```

Reading that aloud: *"for this widget's `token`, look inside the Secret called
`plex-credentials` and use its `token` entry."*

The **field name** on the left (`token`, `apiKey`, `username`, `password`, …)
is fixed **per widget type** — the [reference](widget-reference.md) lists the
right one for each. Getting it wrong is the single most common mistake.

Creating the Secret itself, a `secretRef` shorthand for when your Secret's key
names already match the widget's field names, and a shortcut to share one key
across many widgets, are covered in **[Secrets & credentials](secrets.md)**.
That page is essential reading before your first credentialed widget.

> You *can* inline a value directly (`value: my-token`) instead of
> `secretKeyRef`, but don't do it for real credentials — it ends up stored in
> plain text. The system will even warn you when you do.

### 4. `config` — extra settings

Some widgets need a little more than a URL and a key. A `cloudflared` widget
needs to know *which* tunnel; a `prometheusmetric` widget needs the query to
run. Those go in a `config:` block:

```yaml
- type: prometheusmetric
  url: http://prometheus.example.com
  config:
    query: up{job="node"}
    label: Nodes up
```

Most widgets need **no** `config` at all. The [reference](widget-reference.md)
spells out exactly which keys (if any) each type wants, and which are required
versus optional.

## Three worked examples, from simplest to trickiest

### A) No credentials at all

Some services expose stats openly. `prometheus`, `gatus`, and `netdata` need
nothing but a URL:

```yaml
- name: Prometheus
  href: http://prometheus.example.com
  icon: prometheus
  widgets:
    - type: prometheus
      url: http://prometheus.example.com
```

### B) One API key

The common case. `sonarr` wants an API key, passed as the `apiKey` field:

```yaml
- name: Sonarr
  href: http://sonarr.example.com
  icon: sonarr
  widgets:
    - type: sonarr
      url: http://sonarr.example.com
      secrets:
        apiKey:
          secretKeyRef:
            name: sonarr-credentials
            key: apiKey
```

(You'd first create the `sonarr-credentials` Secret — see [Secrets](secrets.md).)

### C) A username *and* password, plus a config option

Some services use two credentials and an extra setting. `unifi` wants an API key
and lets you name the site:

```yaml
- name: UniFi
  href: http://unifi.example.com
  icon: unifi
  widgets:
    - type: unifi
      url: https://unifi.example.com
      secrets:
        apiKey:
          secretKeyRef:
            name: unifi-credentials
            key: apiKey
      config:
        site: default        # optional; defaults to "default"
        insecureTLS: true    # only if it uses a self-signed certificate
```

Once you can read example C, you can configure any widget in the reference.

## Choosing what to show

By default a widget shows every stat it knows. You can trim and tune:

- **`fields:`** — show only the named stats. The names are the labels the widget
  produces (e.g. `queued`, `wanted`). Apply it, see the labels, then filter:

  ```yaml
  - type: sonarr
    url: http://sonarr.example.com
    secrets: { apiKey: { secretKeyRef: { name: sonarr-credentials, key: apiKey } } }
    fields: [queued, wanted]     # hide the rest
  ```

- **`highlight:`** — colour a stat green/amber/red by its value (e.g. red when a
  queue is large). See [Highlighting stats](#highlighting-stats).

- **`pollIntervalSeconds:`** — poll *this* widget less often than the rest. Handy
  for a slow or rate-limited service. It can only be **slower** than the
  dashboard's global interval, never faster.

  ```yaml
  - type: openweathermap
    pollIntervalSeconds: 600      # weather every 10 min, not every 15s
  ```

## Highlighting stats

`highlight` tints a stat by severity based on rules you write. Keyed by the
stat's label:

```yaml
- type: sonarr
  url: http://sonarr.example.com
  secrets: { apiKey: { secretKeyRef: { name: sonarr-credentials, key: apiKey } } }
  highlight:
    queued:
      rules:
        - when: gt        # greater than
          value: "20"
          level: danger   # red
        - when: gt
          value: "5"
          level: warn     # amber
      # first matching rule wins, so list the most severe first
```

- **Numeric** comparisons: `gt`, `gte`, `lt`, `lte`, `eq`, `ne`, `between`,
  `outside` (`between`/`outside` also need `value2` for the upper bound).
- **Text** comparisons: `equals`, `includes`, `startsWith`, `endsWith`, `regex`.
- **`level`** is `good` (green), `warn` (amber), or `danger` (red).
- Optional: `negate: true` flips a rule, `caseSensitive: true` for text,
  `scope: ValueOnly` to tint just the number instead of the whole chip.

Rules are checked top to bottom; the first match wins — so order from most to
least severe.

## Trusting a self-signed certificate

If a service uses a private/self-signed HTTPS certificate, the widget will fail
to verify it. Two options:

- **Preferred:** give the widget your CA certificate so it can verify properly:

  ```yaml
  - type: unifi
    url: https://unifi.example.com
    caCert:
      secretKeyRef:
        name: my-ca
        key: ca.crt
  ```

- **Quick and dirty:** some widgets accept `config: { insecureTLS: true }` to
  skip verification entirely. Fine for a lab, not recommended generally.

## Golden rules (avoid the common mistakes)

1. **Never put a credential in the `url`** (like `?apikey=abc`). If the service
   ever errors, that URL — key and all — is printed on the card for anyone to
   see. Always use `secrets`.
2. **Use the exact field name** the reference lists (`token` vs `apiKey` vs
   `key` vs `username`/`password`). A wrong name means "no credential given".
3. **Use a URL the dashboard can reach**, which may differ from the one *you*
   use in a browser.
4. **A blank or red widget is diagnosable** — `kubectl describe pcard <name>`
   and the card's own error text tell you what's wrong. See
   [Troubleshooting](troubleshooting.md).

## Next

- **[Widget reference](widget-reference.md)** — the lookup table: every type,
  what it shows, its secret field name(s), and its config keys.
- **[Secrets & credentials](secrets.md)** — create the Secret your widget points
  at, and share one key across many widgets.
- **[Header widgets](header-widgets.md)** — the different widgets in the top strip.

# Header widgets (the top strip)

The strip across the **top** of the dashboard — clock, greeting, weather,
cluster CPU/RAM — is configured separately from service cards. Each item is a
**header widget**, defined in an **InfoWidget** object.

> Don't confuse these with the widgets that sit *on service cards*
> ([Widgets](widgets.md)). They're a different building block with a slightly
> different shape, though the idea (a small live stat) is the same.

## The basic shape

One InfoWidget can hold the whole header strip. Items are a flat, ordered list —
there are no groups up here.

```yaml
apiVersion: page.kubepage.dev/v1alpha1
kind: InfoWidget
metadata:
  name: header
  namespace: dashboards
spec:
  dashboardRef:
    name: home
  widgets:
    - type: greeting
      config:
        text: Welcome home
    - type: datetime
    - type: openmeteo
      config:
        latitude: 51.5074
        longitude: -0.1278
        units: metric
        label: London
    - type: kubemetrics
```

That gives you: a greeting, a live clock, London weather, and cluster CPU/RAM —
left to right.

## Available header widgets

The full list, with their settings, is in the
[Widget reference → Header widgets](widget-reference.md#header-widgets-infowidget-only)
section. In brief:

| Type | Shows | Needs |
|------|-------|-------|
| `greeting` | Static text | `config.text` |
| `datetime` | A live clock | nothing |
| `logo` | A static logo image | `icon` (the image) |
| `openmeteo` | Weather, **no API key** | latitude + longitude |
| `openweathermap` | Weather via OpenWeatherMap | an `apiKey` + lat/long |
| `kubemetrics` | Cluster-wide CPU/RAM | nothing (reads the cluster) |
| `glances` | Host CPU/RAM | a Glances `url` |
| `longhorn` | Longhorn storage usage | a Longhorn `url` |

## Per-item settings

Each entry under `widgets:` accepts:

| Field | What it does |
|-------|--------------|
| `type` | **Required.** Which header widget (see table above). |
| `config` | The widget's settings (e.g. `text`, `latitude`). Same role as `config` on card widgets. |
| `url` | For widgets that poll an address (`glances`, `longhorn`); required for those types. |
| `secrets` | A credential, exactly like card widgets — e.g. `apiKey` for `openweathermap`. |
| `order` | A number to control left-to-right position. |
| `align` | `Left` or `Right`. Defaults sensibly: greeting/clock left, live stats right. |
| `icon` | Override the built-in icon. Weather widgets already pick an icon that tracks the current conditions, so you rarely need this. |
| `pollIntervalSeconds` | Poll this item less often (e.g. weather every 10 minutes). |

## Ordering and alignment

Homepage-style dashboards put the greeting/clock on the **left** and live stats
on the **right**. That's the default here too, so you often don't need `align`
at all. To force placement, set `align: Left` / `align: Right`. Within a side,
`order:` controls the sequence (lower first).

```yaml
widgets:
  - type: greeting
    align: Left
    order: 1
    config: { text: Good morning }
  - type: kubemetrics
    align: Right
    order: 1
```

## Weather without an API key

`openmeteo` uses the free, keyless [Open-Meteo](https://open-meteo.com/) service
— the easiest way to get weather on the header:

```yaml
- type: openmeteo
  config:
    latitude: 40.7128
    longitude: -74.0060
    units: imperial
    label: New York
```

Prefer OpenWeatherMap? Use `openweathermap` instead and supply an `apiKey`
secret (see [Secrets](secrets.md)); the `widgetDefaults` shortcut is handy so
you set that key once — see [Secrets → shared keys](secrets.md#share-one-key-across-many-widgets).

## Styling the strip

How the whole strip *looks* — underlined, boxed, plain — is set once in
the Dashboard's [spec.style](appearance.md) via `headerStyle`, not here.

## Next

- **[Widget reference](widget-reference.md)** — exact options for each header widget.
- **[Appearance](appearance.md)** — the `headerStyle` that frames the strip.
- **[Secrets & credentials](secrets.md)** — for `openweathermap`'s API key.

# Secrets & credentials

Most [widgets](widgets.md) need a credential — an API key, token, or password —
to read stats from a service. This page explains how to store that credential
**safely** and point a widget at it, plus a shortcut for sharing one key across
many widgets.

## The idea in one sentence

You put the credential in a Kubernetes **Secret** (a protected store), and the
widget **points at it by name** — the credential never appears in your dashboard
config files.

```
  your Secret                          your widget
 ┌───────────────────────┐            ┌─────────────────────────┐
 │ name: plex-credentials │  points at │ secrets:                │
 │   token: abc123…       │◀───────────│   token:                │
 └───────────────────────┘            │     secretKeyRef:        │
                                       │       name: plex-credentials
                                       │       key: token         │
                                       └─────────────────────────┘
```

## Step 1 — create the Secret

A Secret is itself a small Kubernetes object. It must live in the **same
namespace** as your dashboard. The easiest way to make one is a single command:

```sh
kubectl create secret generic plex-credentials \
  --namespace dashboards \
  --from-literal=token='YOUR-PLEX-TOKEN-HERE'
```

That creates a Secret named `plex-credentials` with one entry, `token`, holding
your Plex token.

Need several entries (e.g. a username *and* password)? Add more `--from-literal`
flags:

```sh
kubectl create secret generic grafana-credentials \
  --namespace dashboards \
  --from-literal=username='admin' \
  --from-literal=password='YOUR-PASSWORD'
```

> Prefer to manage Secrets as files, or with sealed-secrets/External Secrets/etc.?
> That's fine — the widget only cares about the Secret's **name** and the **key**
> inside it, however it got there.

## Step 2 — point the widget at it

In the widget, under `secrets:`, use the **field name the widget expects** (from
the [Widget reference](widget-reference.md)) and reference your Secret:

```yaml
widgets:
  - type: plex
    url: http://plex.example.com
    secrets:
      token:                       # the field name Plex expects
        secretKeyRef:
          name: plex-credentials   # your Secret's name
          key: token               # the entry inside it
```

Read it as: *"for this widget's `token`, use the `token` entry of the Secret
called `plex-credentials`."*

Two things to get right — they cause most credential problems:

1. **The field name on the left** (`token`, `apiKey`, `username`, `password`,
   `key`, …) is fixed **per widget type**. Check the
   [reference](widget-reference.md). Using `apiKey` where the widget wants
   `token` means "no credential given".
2. **`name` and `key`** must exactly match your Secret and the entry inside it.

## Shorthand: point at a whole Secret with `secretRef`

When your Secret's key names already match the field names a widget expects
— Plex's Secret has one key, `token`; Grafana's has `username` and
`password` — you can skip `secrets` entirely and just name the Secret:

```yaml
widgets:
  - type: plex
    url: http://plex.example.com
    secretRef: plex-credentials
```

Every key in `plex-credentials` becomes a resolved secret field, as if you'd
written it out under `secrets` with `key` equal to that key name. Grafana's
two-credential case collapses the same way:

```yaml
widgets:
  - type: grafana
    secretRef: grafana-credentials   # Secret holds both username and password
```

`secretRef` and `secrets` can be combined: `secrets` always wins per key, so
you can use `secretRef` for the common fields and `secrets` to override or
add one that doesn't match (e.g. a Secret whose key is named differently, or
one shared across widgets that need only some of its keys renamed). The long
form under `secrets` remains the way to point at a Secret whose key names
don't match the widget's field names at all.

## Please don't inline real credentials

You *can* write a value straight into the config:

```yaml
secrets:
  token:
    value: abc123          # <- DON'T do this for real credentials
```

…but then the credential sits in plain text in your config file (and in the
cluster object). The operator ships a check that **warns** you when a
credential-shaped field (`token`, `apiKey`, …) uses an inline `value` — heed it.
Inline `value` is really only for harmless, non-secret settings.

## Share one key across many widgets

If you run several widgets of the *same type* — say five services all behind one
OpenWeatherMap key, or several `sonarr`/`radarr` instances — you don't want to
repeat the same `secrets` block on every one. Set it **once** on the Dashboard
using `widgetDefaults`:

```yaml
apiVersion: page.kubepage.dev/v1alpha1
kind: Dashboard
metadata:
  name: home
  namespace: dashboards
spec:
  widgetDefaults:
    openweathermap:                 # applies to every openweathermap widget
      secrets:
        apiKey:
          secretKeyRef:
            name: openweathermap-credentials
            key: apiKey
```

Now any `openweathermap` widget bound to this dashboard inherits that `apiKey`
automatically — no `secrets` block needed on the widget itself. A widget can
still override the default by setting its own `secrets` (its own value always
wins). This is the equivalent of homepage's "providers" block.

## Restricting which Secrets widgets may use

By default, a widget may reference **any** Secret in its namespace. If several
people can edit dashboard config in a shared namespace, you may want to limit
which Secrets are reachable. Set on the Dashboard:

```yaml
spec:
  secretPolicy: Labeled
```

Under `Labeled`, a widget may only use Secrets that carry the label
`page.kubepage.dev/allow-widgets: "true"`. Label the ones you want to permit:

```sh
kubectl label secret plex-credentials -n dashboards \
  page.kubepage.dev/allow-widgets=true
```

A widget pointing at an unlabelled Secret then shows a clear error on its card
instead of reading it. The default, `Unrestricted`, keeps the simpler model
where any Secret in the namespace is usable.

See [SECURITY.md](../../SECURITY.md) for the full trust model behind this.

## A word on trust

Anyone who can create a ServiceCard/InfoWidget in a namespace can make the
dashboard read a Secret in that namespace and send its value to a URL they
choose. In other words: **treat "can edit dashboard config here" as equivalent
to "can read every Secret here."** Keep sensitive Secrets in a namespace whose
dashboard config is edited only by people you'd trust with them, and use
`secretPolicy: Labeled` to narrow the set when in doubt.

## Next

- **[Widget reference](widget-reference.md)** — the exact field name each widget
  wants under `secrets`.
- **[Widgets](widgets.md)** — where `secrets` fits among a widget's other parts.
- **[Troubleshooting](troubleshooting.md)** — a widget card showing an auth error.

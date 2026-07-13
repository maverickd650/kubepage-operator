# Configuration guide

This guide explains how to configure your dashboard. It is written for people
who want a working, good-looking dashboard **without** needing to understand
Go, controllers, or the internals of Kubernetes. If you can edit a text file
and run one command to apply it, you can configure everything here.

If a term is unfamiliar, check the [Glossary](#glossary) at the bottom of this
page first.

## Start here

New to the project? Read these in order:

1. **[Getting started](getting-started.md)** — the big picture, and your first
   working dashboard from scratch.
2. **[Service cards](service-cards.md)** — the tiles that link to your
   services, group them, and show up/down status.
3. **[Widgets](widgets.md)** — the small live stats on a card (stream counts,
   disk usage, queue sizes…). *This is the part most people find confusing, so
   it gets its own gentle walkthrough.*
4. **[Widget reference](widget-reference.md)** — a lookup table: every widget
   type, what it shows, and exactly what to fill in.

Then, as you need them:

- **[Header widgets](header-widgets.md)** — the strip across the top (clock,
  greeting, weather, cluster stats).
- **[Bookmarks](bookmarks.md)** — simple link tiles (GitHub, docs, your router…).
- **[Appearance](appearance.md)** — title, theme, colours, background, tabs, and
  the search box.
- **[Secrets & credentials](secrets.md)** — how to give a widget an API key
  **safely**, and share one key across many widgets.
- **[Troubleshooting](troubleshooting.md)** — "my card is blank / red / missing"
  and how to find out why.

## How it all fits together

Your dashboard is assembled from a handful of small configuration files. Each
file describes **one thing**, and they are linked together by name. You never
edit one giant file.

```
                    ┌────────────────────────────┐
                    │  Dashboard  (the web page)  │
                    │  name: home                 │
                    └──────────────┬─────────────┘
                                   │  everything below points
                                   │  back at it by name
        ┌────────────────┬─────────┴────────┬─────────────────┐
        │                │                  │                 │
 ┌──────────────┐ ┌─────────────┐   ┌──────────────┐  ┌──────────────┐
 │ DashboardStyle│ │ ServiceCard │   │  InfoWidget  │  │   Bookmark   │
 │ how it looks  │ │ the tiles + │   │ header strip │  │ simple links │
 │ (theme, tabs) │ │ live widgets│   │ (clock, etc) │  │              │
 └──────────────┘ └─────────────┘   └──────────────┘  └──────────────┘
```

The five building blocks:

| Building block | What it is | You need… |
|----------------|-----------|-----------|
| **Dashboard** | The web page itself. Creates the actual running dashboard. | Exactly one. |
| **DashboardStyle** | The look: title, theme, colour, background, tabs, search box. | At most one per Dashboard. |
| **ServiceCard** | One or more tiles linking to your services, with optional live **widgets** and up/down status. | As many as you like. |
| **InfoWidget** | The header strip along the top (clock, greeting, weather, cluster CPU/RAM). | As many as you like. |
| **Bookmark** | Plain link tiles, grouped like service cards but with no live data. | As many as you like. |

**The golden rule:** every building block except the Dashboard itself carries a
line that names the Dashboard it belongs to:

```yaml
spec:
  dashboardRef:
    name: home      # <- must match your Dashboard's metadata.name
```

That name is the glue. Get it right and things appear on the page; get it wrong
and they silently don't. They must all also live in the **same namespace** as
the Dashboard (see the glossary).

## The shape of every configuration file

Every file follows the same three-part pattern. Once you have seen one, you
have seen them all:

```yaml
apiVersion: page.kubepage.dev/v1alpha1   # always this line, unchanged
kind: ServiceCard                        # which building block this is
metadata:
  name: media-services                   # a name of YOUR choosing for this file
  namespace: dashboards                  # where it lives (must match the Dashboard)
spec:
  dashboardRef:
    name: home                           # which Dashboard it belongs to
  # ... the actual settings go here ...
```

- `apiVersion` / `kind` — tell Kubernetes what kind of thing this is. Copy them
  exactly.
- `metadata.name` — a label **for the file itself**. Pick something memorable.
  It is not shown on the dashboard (except for DashboardStyle, which has a
  special rule — see [Appearance](appearance.md)).
- `spec` — the real configuration.

## Applying your changes

You write these files as plain YAML and hand them to your cluster with `kubectl`:

```sh
kubectl apply -f my-card.yaml
```

Changes take effect **within a few seconds** — there is no need to restart
anything or rebuild the dashboard. The dashboard re-reads your configuration on
a timer (every 15 seconds by default) and updates the open page in place. You
usually don't even need to refresh your browser.

To see what you have and whether it's healthy:

```sh
kubectl get pdash,pstyle,pcard,piw,pbmk    # list everything, with a Ready column
kubectl describe pcard media-services      # full detail on one object, incl. errors
```

Those short names (`pdash`, `pcard`…) are covered in
[Troubleshooting](troubleshooting.md), which is the page to read the moment
something looks wrong.

## Glossary

Short, practical definitions — just enough to follow this guide.

- **YAML** — the plain-text format all these files are written in. Indentation
  matters (use spaces, never tabs), and `- ` starts a list item. That's 90% of it.
- **`kubectl`** — the command-line tool that sends your files to the cluster.
  `kubectl apply -f file.yaml` is the one command you'll use most.
- **Namespace** — a folder-like partition inside the cluster. All the pieces of
  one dashboard must live in the **same** namespace. If you don't know yours,
  ask whoever set up the cluster; a common one is where you installed the
  operator.
- **Secret** — a special, protected place to store a password or API key so it
  isn't written in plain sight inside your config files. See
  [Secrets & credentials](secrets.md).
- **CRD / "kind"** — the *type* of a building block (Dashboard, ServiceCard…).
  You don't need to care what the acronym stands for.
- **`spec`** — the "settings" section of any file. Everything you configure
  lives under `spec`.
- **Widget** — a small live stat shown on a card or in the header (e.g. "3
  streams", "72% disk"). The star of this guide — see [Widgets](widgets.md).
- **Upstream / service** — the actual application a widget or card talks to
  (Plex, your router, Grafana…).
- **Apply / `kubectl apply`** — save-and-activate. Re-applying an edited file
  updates the existing object; nothing is duplicated.

# Getting started

This page builds a real, working dashboard from nothing, one small file at a
time. By the end you'll have a page with a title, a service tile, an up/down
status light, and a clock — and you'll understand what each file does.

If you haven't yet, skim [How it all fits together](README.md#how-it-all-fits-together)
first. The one idea to hold onto: **you assemble a dashboard from several small
files, all tied together by a shared name.**

Throughout this page we'll use:

- the name **`home`** for the dashboard, and
- the namespace **`dashboards`**.

Change both to suit your setup. If you don't know which namespace to use, ask
whoever installed the operator; the pieces just all need to agree.

## Step 1 — create the dashboard page

This is the only file that actually creates a running web page. Everything else
decorates it.

```yaml
# dashboard.yaml
apiVersion: page.kubepage.dev/v1alpha1
kind: Dashboard
metadata:
  name: home
  namespace: dashboards
spec: {}          # empty {} means "use all the sensible defaults"
```

Apply it:

```sh
kubectl apply -f dashboard.yaml
```

`spec: {}` is not a mistake — a Dashboard needs no settings at all to work. The
defaults give you one replica listening on port 8080. How you actually reach
that page in a browser (port-forward, Ingress, LoadBalancer…) is a hosting
concern covered in the main [README](../../README.md); this guide is about what
goes *on* the page.

Check it came up:

```sh
kubectl get pdash -n dashboards
# NAME   READY   REPLICAS   SERVICES   BOOKMARKS   WIDGETS   URL   AGE
# home   True    1          0          0           0               30s
```

`READY: True` means you're good. Open the page — it'll be nearly empty with a
default title. We'll fix that next.

## Step 2 — give it a title and a theme

Appearance lives directly on the Dashboard itself, under `spec.style`.

```yaml
# dashboard.yaml
apiVersion: page.kubepage.dev/v1alpha1
kind: Dashboard
metadata:
  name: home
  namespace: dashboards
spec:
  style:
    title: Home Lab
    description: My self-hosted services
    theme: dark
    color: blue
```

```sh
kubectl apply -f dashboard.yaml
```

Refresh the page: it now says "Home Lab" with a dark blue theme. There's a lot
more you can do here (backgrounds, tabs, search) — see
[Appearance](appearance.md) — but this is enough for now.

## Step 3 — add your first service tile

A **ServiceCard** file holds one or more tiles. Each tile links to a service and
can show its up/down status. Let's add Plex.

```yaml
# media.yaml
apiVersion: page.kubepage.dev/v1alpha1
kind: ServiceCard
metadata:
  name: media
  namespace: dashboards
spec:
  dashboardRef:
    name: home
  group: Media                       # the heading these tiles appear under
  services:
    - name: Plex
      href: http://plex.example.com  # clicking the tile opens this
      icon: plex                      # a nice logo (see below)
      description: Media server
      monitor: self                   # up/down light probing href
```

```sh
kubectl apply -f media.yaml
```

You now have a "Media" section with a Plex tile that turns green when Plex is
reachable and red when it isn't. No credentials needed for that — a status
light just checks whether the URL answers.

- **`icon: plex`** pulls the official Plex logo automatically. Most services
  work by just naming them (`grafana`, `sonarr`, `nextcloud`…). Full details in
  [Service cards → Icons](service-cards.md#icons).
- **`group: Media`** is just a heading. Add more tiles under the same group and
  they cluster together.

Want more tiles? Add more entries under `services:` — see
[Service cards](service-cards.md).

## Step 4 — add a live widget (the interesting part)

A status light only says up or down. A **widget** shows real numbers pulled from
the service — for Plex, the number of active streams. Widgets attach *inside* a
service tile:

```yaml
# media.yaml (updated)
apiVersion: page.kubepage.dev/v1alpha1
kind: ServiceCard
metadata:
  name: media
  namespace: dashboards
spec:
  dashboardRef:
    name: home
  group: Media
  services:
    - name: Plex
      href: http://plex.example.com
      icon: plex
      description: Media server
      monitor: self
      widgets:
        - type: plex                     # inherits its URL from href
          secrets:
            token:
              secretKeyRef:
                name: plex-credentials     # a Secret you create
                key: token
```

Most widgets need a credential (here, a Plex token). You store that credential
in a **Secret** and *point* the widget at it — you never paste the token into
this file. Creating that Secret and understanding `secretKeyRef` is a topic of
its own:

- **[Widgets](widgets.md)** — the gentle, step-by-step explanation of what a
  widget is and how the pieces (`type`, `url`, `secrets`, `config`) fit.
- **[Secrets & credentials](secrets.md)** — how to create the `plex-credentials`
  Secret referenced above.

Don't apply this version until you've read those two pages and created the
Secret — otherwise the card will show an error (which is harmless, just untidy).

## Step 5 — add a clock to the header

Finally, the strip across the top. Those are **InfoWidget** entries. A clock and
a greeting:

```yaml
# header.yaml
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
```

```sh
kubectl apply -f header.yaml
```

You now have a greeting and a live clock up top. Weather, cluster CPU/RAM, and
more are covered in [Header widgets](header-widgets.md).

## Where you are now

You've got a titled, themed page with a service tile, a status light, and a
header clock — and you know where each of those came from. From here:

| To do this… | Read… |
|-------------|-------|
| Add more tiles, groups, nested groups, status lights | [Service cards](service-cards.md) |
| Understand and add live widgets | [Widgets](widgets.md) → [Widget reference](widget-reference.md) |
| Store an API key safely for a widget | [Secrets & credentials](secrets.md) |
| Weather, cluster stats, header layout | [Header widgets](header-widgets.md) |
| Backgrounds, tabs, the search box | [Appearance](appearance.md) |
| A blank/red/missing card | [Troubleshooting](troubleshooting.md) |

## A note on file organisation

You can put everything in one YAML file (separate documents with `---`) or split
it across many — the cluster doesn't care. A common, tidy approach is one file
per group: `media.yaml`, `infrastructure.yaml`, `downloads.yaml`. Each can hold
many tiles. Do whatever keeps *you* sane.

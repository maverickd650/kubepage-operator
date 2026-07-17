# Troubleshooting

When something doesn't appear, or a card is blank or red, the cluster can almost
always tell you *why*. This page shows how to ask.

## The two commands you need

**List everything and see what's healthy:**

```sh
kubectl get pdash,pcard,piw,pbmk -n dashboards
```

Those short names are:

| Short name | Building block |
|------------|----------------|
| `pdash` | Dashboard (appearance lives at `spec.style` on this object) |
| `pcard` | ServiceCard |
| `piw` | InfoWidget |
| `pbmk` | Bookmark |

Each row has a `READY` column. `True` is good; `False` means look closer.

**Get the full story on one object, including errors:**

```sh
kubectl describe pcard media -n dashboards
```

Scroll to the **Conditions** and **Events** at the bottom — that's where the
reason for any problem is spelled out in plain language.

## Reading conditions

Objects report their health as **conditions**. The ones you'll meet:

| Condition | Meaning |
|-----------|---------|
| `Available: True` | All good. |
| `Available: False` (reason `DashboardNotFound`) | `dashboardRef` names a Dashboard that doesn't exist in this namespace, or `dashboardRef` is unset and this namespace has **no** Dashboard at all. |
| `Available: False` (reason `AmbiguousDashboardRef`) | `dashboardRef` is unset and this namespace has **more than one** Dashboard — there's no sole Dashboard to default to. The message names every candidate; set `dashboardRef` explicitly to pick one. |
| `Available: False` (other reasons) | Something's wrong — the `Reason`/`Message` say what. |
| `ConfigValid: False` (reason `UnknownConfigKeys`) | A `config` key isn't recognised — likely a **typo**. The card still works; the unknown key is just ignored. |

A common `Available: False` reason is **`InvalidWidgetConfig`** — a widget is
missing a *required* setting (for example `cloudflared` without a `tunnelId`, or
`uptime-kuma` without a `slug`). The message names the offending widget and the
missing key.

## Symptom → cause

### "My card / widget / bookmark isn't on the page at all"

Almost always the **`dashboardRef.name` doesn't match**, the namespace has more
than one Dashboard and no explicit `dashboardRef` to disambiguate, or the
object is in the **wrong namespace**.

1. If `dashboardRef` is set, confirm it points at the right Dashboard:
   ```sh
   kubectl get pcard media -n dashboards -o jsonpath='{.spec.dashboardRef.name}'
   ```
   It must exactly equal your Dashboard's `metadata.name`.
2. If `dashboardRef` is unset, confirm the namespace has **exactly one**
   Dashboard — an unset ref only defaults when there's a sole one to bind to:
   ```sh
   kubectl get pdash -n dashboards
   ```
   Two or more Dashboards and no `dashboardRef` leaves the object unbound
   (`Available: False`, reason `AmbiguousDashboardRef`); add `dashboardRef`
   naming the one it belongs to.
3. Confirm both live in the **same namespace**. Cross-namespace links don't work.
4. Check the Dashboard's bound counts — they should include your object:
   ```sh
   kubectl get pdash home -n dashboards
   # SERVICES / BOOKMARKS / WIDGETS columns count what's bound
   ```

### "My style settings aren't applying"

Appearance lives at `spec.style` on the Dashboard object itself, not a
separate object — double-check you're editing the right Dashboard (`kubectl
edit pdash home -n dashboards`) and that the fields are nested under `style:`,
not directly under `spec:`. See [Appearance](appearance.md).

### "The card shows a red error / auth message"

The widget reached the service but was refused or couldn't parse the reply. In
order of likelihood:

1. **Wrong secret field name.** You used `apiKey` where the widget wants `token`
   (or vice-versa). Check the exact name in the
   [Widget reference](widget-reference.md).
2. **Wrong or missing credential.** The Secret name/key don't match, or the key
   itself is wrong. Verify the Secret exists and has the right entry:
   ```sh
   kubectl get secret plex-credentials -n dashboards -o jsonpath='{.data}'
   ```
3. **`secretPolicy: Labeled` is on** and the Secret isn't labelled — see
   [Secrets → restricting](secrets.md#restricting-which-secrets-widgets-may-use).
4. **Wrong URL.** Use an address the *dashboard* can reach (often an internal
   cluster address), not the one you use from your laptop.
5. **Self-signed certificate.** Add a `caCert`, or `config: { insecureTLS: true }`
   for the widgets that support it — see
   [Widgets → self-signed certificates](widgets.md#trusting-a-self-signed-certificate).

The card prints the raw error from the service, which usually points straight at
the cause.

### "The card is blank / shows nothing"

- If it's a **widget** you expect stats from: the widget may be returning no
  fields, or `showStats: false` is set on the card. Also check `fields:` — if it
  lists labels that don't match what the widget produces, everything gets
  filtered out. Remove `fields:` temporarily to see the real labels.
- If it's a **status light** you expect: make sure you set `monitor` (a URL
  or `self`) and/or `app`/`podSelector`. (`ping` and `siteMonitor` were
  merged into `monitor` — replace either with `monitor: <same URL>`, or
  `monitor: self` when the URL equals the entry's `href`/`internalUrl`.)

### "A config value I set is being ignored"

Run `kubectl describe pcard <name>` and look for a `ConfigValid: False` /
`UnknownConfigKeys` condition — it lists keys that aren't recognised for that
widget type. This is almost always a spelling mistake (e.g. `acountId` for
`accountId`). The [reference](widget-reference.md) has the correct spellings.

### "My change didn't take effect"

- Changes apply within ~15 seconds (the poll interval). Give it a moment; you
  rarely need to refresh the browser.
- Confirm the apply actually succeeded — re-run `kubectl apply -f file.yaml` and
  read the output. A validation error means nothing was saved.
- Confirm you edited the object in the **right namespace**.

## Previewing before you apply (advanced)

If you want to check a dashboard's look *without* touching a cluster, the
operator has a local preview mode that renders your YAML files directly. See the
main [README](../../README.md#local-preview-no-cluster-required) and
[../design/local-preview.md](../design/local-preview.md). Add `--sample-data` to
fill every widget with placeholder numbers so you can check layout without any
real services or credentials.

## Still stuck?

- Re-read the page for the building block involved — most "it won't work" cases
  are a name mismatch or a wrong secret field name, both covered above.
- `kubectl describe` on the object is the single most useful step; its
  `Message` fields are written to be read.

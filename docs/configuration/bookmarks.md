# Bookmarks

Bookmarks are the simplest building block: plain link tiles, grouped like
service cards but with **no live data, no status light, and no widgets**. Use
them for the odds and ends you want one click away — GitHub, documentation, your
router's admin page, a webmail login.

If you find yourself adding a [service card](service-cards.md) with nothing but a
name and a link, a bookmark is the tidier choice.

## The basic shape

```yaml
apiVersion: page.kubepage.dev/v1alpha1
kind: Bookmark
metadata:
  name: dev-links
  namespace: dashboards
spec:
  dashboardRef:
    name: home
  group: Developer          # the heading these bookmarks sit under
  bookmarks:
    - name: GitHub
      href: https://github.com/
      abbr: GH
    - name: Go Docs
      href: https://pkg.go.dev/
      abbr: GO
      description: Go package documentation
    - name: Wikipedia
      group: Reference      # override the default group for this one
      href: https://wikipedia.org/
      abbr: WK
```

Like service cards, `bookmarks:` is a list, the top-level `group:` is the default
heading, and any entry can override it with its own `group:`.

## Bookmark fields

| Field | What it does |
|-------|--------------|
| `name` | **Required.** The bookmark's label. |
| `href` | **Required.** Where it links to. See [Allowed link types](#allowed-link-types). |
| `group` | Heading it appears under. Falls back to the file's top-level `group`. |
| `abbr` | A short (≤ 8 char) badge shown when there's no icon, e.g. `GH`. |
| `icon` | An icon instead of the abbreviation. Same rules as [service card icons](service-cards.md#icons). If both `icon` and `abbr` are set, the icon wins. |
| `description` | A line of text under the name. Defaults to the site's hostname. |
| `target` | `_blank` (new tab) or `_self` (same tab). |
| `order` | A number to control position (lower first). |

## Icon or abbreviation?

- Set **`icon`** for a recognised service (`icon: github`) or any image URL —
  see [service card icons](service-cards.md#icons).
- Set **`abbr`** for a short text badge when there's no good logo (`abbr: GH`).
- Set neither and you'll get a hostname-derived description with a generic look.

There's also a site-wide **icons-only** style — set `style.bookmarksStyle: icons` in
the Dashboard's [spec.style](appearance.md) to render every bookmark as just its icon, no
text.

## Allowed link types

Bookmarks accept more than web links, but only from a safe list:

- `https://…`, `http://…`
- `ftp://…`, `sftp://…`, `ssh://…`, `rdp://…`, `vnc://…`, `smb://…`
- `mailto:you@example.com`, `tel:+15551234567`

Anything else (notably `javascript:` and `data:`) is rejected when you apply —
this is a deliberate safety measure.

## Grouping

Bookmarks group under headings exactly like service cards, **except** they do
**not** support nested (`Media/Movies`) groups — a bookmark's `group` is always a
single top-level heading.

## Next

- **[Service cards](service-cards.md)** — when you want status/widgets, not just a link.
- **[Appearance](appearance.md)** — `bookmarksStyle: icons` and where groups appear.

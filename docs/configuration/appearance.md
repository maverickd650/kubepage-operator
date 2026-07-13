# Appearance

Everything about how your dashboard *looks* — title, theme, colour, background,
tabs, the search box — lives in a single **DashboardStyle** object.

## The one rule to remember

A DashboardStyle's `metadata.name` **must be identical to the Dashboard it
styles**, and `dashboardRef.name` must match too. This is how a dashboard is
guaranteed exactly one look — you literally cannot create a second style for the
same dashboard.

```yaml
apiVersion: page.kubepage.dev/v1alpha1
kind: DashboardStyle
metadata:
  name: home            # <- MUST equal the Dashboard's name
  namespace: dashboards
spec:
  dashboardRef:
    name: home          # <- same again
  title: Home Lab
```

## A well-rounded example

```yaml
spec:
  dashboardRef:
    name: home
  title: Home Lab
  description: Self-hosted services
  favicon: https://cdn.jsdelivr.net/gh/walkxcode/dashboard-icons/png/homepage.png
  theme: dark
  color: slate
  headerStyle: boxed
  cardBlur: md
  background:
    image: https://example.com/background.png
    opacity: 40
```

## Everyday settings

| Field | What it does |
|-------|--------------|
| `title` | Browser tab title and header heading. Defaults to "kubepage". |
| `description` | Subtitle under the title. |
| `favicon` | URL to the little browser-tab icon. |
| `theme` | Force `light` or `dark`. Omit to give visitors a light/dark toggle. |
| `color` | Force a colour palette (see [Colours](#colours)). Omit for a palette switcher. |
| `headerStyle` | Frames the top strip: `underlined`, `boxed`, `clean`, or `boxedWidgets` (each header widget in its own box). |
| `target` | Default link behaviour for all cards: `_blank` (new tab) or `_self`. |
| `fullWidth` | `true` uses the whole window width instead of a centred column. |
| `language` | UI language code, e.g. `en`, `fr`, `de`. |
| `hideVersion` | `true` hides the version/commit footer. |

### Colours

`color` accepts one of: `slate`, `gray`, `zinc`, `neutral`, `stone`, `amber`,
`yellow`, `lime`, `green`, `emerald`, `teal`, `cyan`, `sky`, `blue`, `indigo`,
`violet`, `purple`, `fuchsia`, `pink`, `rose`, `red`, `white`.

## Backgrounds

Set a background image and tune how it blends with the page:

```yaml
background:
  image: https://example.com/wallpaper.jpg
  blur: sm            # backdrop blur: sm, md, xl, ...
  saturate: 50        # colour saturation %
  brightness: 75      # brightness %
  opacity: 40         # blend with the theme colour, 0–100
```

Pair a background with `cardBlur: md` (a frosted-glass effect on cards) for a
polished look.

## Groups: collapsing, columns, equal heights

Site-wide group behaviour:

| Field | What it does |
|-------|--------------|
| `collapse` | `true` (default) gives each group header an expand/collapse control; `false` makes them plain and always-open. |
| `groupsInitiallyCollapsed` | `true` starts every group collapsed on first load. |
| `useEqualHeights` | `true` makes all cards in a group the same height. |
| `bookmarksStyle` | Set to `icons` to render bookmarks as icons only, no text. |
| `statusStyle` | Default status-light look — `dot` or `basic` — when a card doesn't set its own. |
| `errorDisplay` | Default for whether widget error text shows on cards. |

Individual groups can override several of these — see [Tabs and layout](#tabs-and-layout).

## Tabs and layout

By default every group renders one after another down the page. The `layout`
block lets you instead arrange groups into **tabs**, and restyle individual
groups.

```yaml
layout:
  - name: Infrastructure          # a tab
    groups:
      - name: Observability
        columns: 3                # render this group in 3 columns
        icon: grafana             # icon on the group header
  - name: Apps                    # another tab
    groups:
      - name: Media
        style: row                # lay this group's cards in a single row
      - name: Media/Movies        # style a nested subgroup
        columns: 2
```

Key points:

- Each entry under `layout` is a **tab** with a `name` and a list of `groups`.
- Any group you **don't** list still appears, gathered into a trailing "Other"
  tab — so adding tabs never hides content.
- Per-group knobs: `columns` (1–6), `style` (`row` or `column`), `icon`,
  `header` (`false` hides the group's header), `initiallyCollapsed`,
  `useEqualHeights`.
- **Nested groups and tabs:** if you place a nested subgroup like `Media/Movies`
  in a tab, its parent (`Media`) must be in the *same* tab — otherwise it's
  ambiguous which tab it belongs to, and the file is rejected. The subgroup
  always follows its parent's tab.

## The search box

`search` configures the header search box — type-ahead filtering of your cards
plus an Enter-to-web-search fallback:

```yaml
search:
  provider: duckduckgo      # duckduckgo | google | bing | custom
  filterCards: true         # filter cards as you type (default)
  target: _blank
```

For a private/self-hosted search engine use `provider: custom` and supply a
`url` (the query is appended as `?q=…`):

```yaml
search:
  provider: custom
  url: https://searx.example.com/search
```

Other toggles: `searchDescriptions` (match card descriptions in the Ctrl/Cmd-K
palette), `internetSearchEntry`, and `visitURLEntry` (offer to jump straight to a
typed-in URL). All default to on.

## Advanced: custom CSS / JS

For looks the fields above can't reach, `customCSS` and `customJS` inject raw
CSS/JavaScript into the page. This is trusted, operator-supplied content — treat
it with the same care as anything you'd run on the page, and reach for it only
when a built-in field won't do.

```yaml
customCSS: |
  .card { border-radius: 16px; }
```

## Progressive Web App / indexing

- `startUrl` — the start URL when the dashboard is installed as an app
  (defaults to `/`).
- `indexing` — `true` (default) lets search engines index the page; `false`
  blocks crawlers via `robots.txt` and a `noindex` tag. Set `false` for a
  private dashboard on a public address.

## Next

- **[Service cards](service-cards.md)** — the groups these settings arrange.
- **[Header widgets](header-widgets.md)** — the strip `headerStyle` frames.
- **[Troubleshooting](troubleshooting.md)** — if a style change doesn't appear.

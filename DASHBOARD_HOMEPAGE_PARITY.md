# Dashboard / homepage visual parity — remaining gaps

Tracking doc for follow-up work after migrating `internal/dashboard`'s
templates from `html/template` to [templ](https://templ.guide) and
restyling the service-card and header-strip widget displays to match
[homepage](https://github.com/gethomepage/homepage)'s actual rendering
(verified against its real source, not memory: `src/components/services/widget/{block,container}.jsx`
and `src/components/widgets/widget/{resource,resources,container,greeting,datetime}.jsx`).

## Done in this pass

- Service card stats (`ServiceEntry` widget `Fields`) now render as homepage's
  `Block` does: equal-width tinted chips spanning the card's full width,
  value on top, bold/uppercase label below — see `.stats`/`.stat` in
  [index.templ](internal/dashboard/index.templ) and the markup in
  [cards.templ](internal/dashboard/cards.templ).
- Header strip widgets (`InfoWidget` `Fields`) now render value-before-label
  like homepage's `Resource`, with no muted-color distinction between the
  two, and the per-widget box uses homepage's translucent
  `boxedWidgets`-style tint instead of a solid panel fill.
- Greeting/clock text sizes now match homepage's defaults (`text-xl`/
  `text-lg`) instead of inheriting the small widget-box font size.

## Known remaining gaps

1. **No icon on header-strip widgets.** Homepage's `Resource` component
   always renders an icon to the left of each value/label pair (e.g. a CPU
   glyph next to the CPU% stat). Our `HeaderWidget`/`Card` types have no
   icon field for header-type cards at all — this needs a schema change
   (`InfoWidget` spec + `internal/dashboard/site.go`'s `HeaderWidget`) before
   it can be rendered, not just a template change.

2. **`openmeteo` header widget only shows temperature.** Homepage's real
   `openmeteo` widget shows a weather icon + condition text (e.g. "Partly
   cloudy") as a secondary line, sourced from a WMO weather-code → condition
   mapping (`utils/weather/openmeteo-condition-map` in homepage's repo) and
   a `wmo.<code>-<day|night>` i18n string. `internal/dashboard/openmeteo.go`
   only computes a single `{Label, Value}` temperature field today — adding
   condition text requires a code→description table and a day/night
   calculation from sunrise/sunset, mirroring homepage's `openmeteo.jsx`.

3. **No percentage usage bar.** Homepage's `Resource` component renders a
   `<UsageBar percent={...}>` under any stat that has a `percentage` prop
   (e.g. CPU/memory). Our `kubemetrics` widget reports CPU/Mem as plain
   `{Label, Value}` percent strings with no bar. Would need a `Percent
   *int` (or similar) on `Field`, plus a small CSS progress-bar component.

4. **No highlight/threshold coloring.** Homepage's service `Block` supports
   `BlockHighlightContext`/`evaluateHighlight` — conditionally coloring a
   stat chip (e.g. red) when its value crosses a configured threshold. We
   have no equivalent; every stat chip renders with the same neutral tint
   regardless of value.

5. **No live visual diff against homepage's actual demo.** This pass
   grounded styling in homepage's *source code* (exact Tailwind classes),
   not a rendered screenshot — the Chrome browser tool wasn't connected
   during this session. Worth a side-by-side screenshot comparison against
   <https://demo.gethomepage.dev/> once available, to catch any remaining
   spacing/sizing drift (e.g. exact `.stat` chip padding, card shadow
   depth, status-dot/pill colors — ours are hand-picked, not pulled from
   homepage's actual theme palette).

6. **Service-card "missing widget type" / error states not compared.**
   Homepage has dedicated styling for an unrecognized widget type
   (`service-missing`) and widget poll errors (`components/widgets/widget/error.jsx`).
   Our `.err` class is a single hand-rolled style; not checked against
   homepage's actual error-state markup/colors.

None of the above block the current PR — they're each independent,
incremental follow-ups. Suggested order: (1) header widget icons, since (2)
and (3) both build on having an icon slot to put a weather/usage glyph in.

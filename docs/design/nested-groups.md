# Design: nested service-card groups

Status: implemented.

## Problem

[homepage](https://gethomepage.dev/configs/services/#nested-groups) lets a
group in `services.yaml` contain other groups, rendering sub-sections inside a
parent section:

```yaml
- Media:
    - Movies:
        - Radarr: ...
    - TV:
        - Sonarr: ...
```

This operator explicitly does not support that today. `ServiceEntry.Group` and
`BookmarkEntry.Group` are documented as "always names a top-level group"
(`api/v1alpha1/servicecard_types.go`, `bookmark_types.go`), grouping is a flat
bucket-by-string (`groupCards` in `internal/dashboard/server.go`), and the
rendered page is a flat list of `<details class="group">` sections per tab.
Users migrating a homepage config with nested groups have to flatten them into
names like "Media – Movies", losing the visual hierarchy and the ability to
collapse a whole parent section at once.

Goal: parity with homepage's nested service groups — a group can contain both
cards and child groups, child groups render (and collapse) inside their
parent, and the Dashboard's `spec.style.layout` can place and style child
groups — without breaking any existing flat config.

## Constraints that shape the design

1. **Groups are not objects.** A group exists only implicitly, as the string
   `ServiceEntry.Group` / `ServiceCardSpec.Group` carries (or the
   `<prefix>group` discovery annotation resolves to). There is no group CRD to
   hang a `parent` field on, so the hierarchy has to be expressed either in
   that string or per-entry.
2. **CRD structural schemas cannot recurse.** A `LayoutGroupSpec` that
   contains `groups []LayoutGroupSpec` is not expressible — controller-gen
   rejects self-referential types, and the apiserver's structural-schema rules
   forbid them. Any homepage-style "nested keys in `layout:`" mirror has to be
   either depth-bounded with distinct sibling types or path-addressed.
3. **Existing configs must keep rendering identically.** Flat `group: Media`
   stays a top-level group; the empty layout and the implicit "Other" tab keep
   working.

## Options considered

### A. Path in the existing `group` field — **recommended**

`group: "Media/Movies"` means group `Movies` nested inside group `Media`.
One field, no new API surface, arbitrary (but validation-bounded) depth, and
the layout's `LayoutGroupSpec.Name` addresses a subgroup by the same path.
The `/` separator is also what the discovery annotation
(`kubepage.io/group: "Media/Movies"`) naturally passes through, so
Ingress/Service discovery gets nesting for free.

Cost: `/` becomes structural. A pre-existing group literally named with a
slash (allowed today, likely nonexistent in practice) would start rendering as
a nested pair. Acceptable in `v1alpha1`; called out in the release notes.

### B. Explicit `parentGroup` field on entries

`group: Movies` + `parentGroup: Media`. Explicit, no string parsing — but the
group's *name* stops being its identity: two `Movies` subgroups under
different parents collide in every map keyed by name (`layoutTabs`,
`layoutGroupsByName`, the client's `data-group-name` collapse state), so the
internal identity becomes a path anyway, and the API caps depth at 2 unless a
`grandparentGroup` appears. Worse ergonomics for the same internal model.

### C. Nesting declared only in `DashboardStyle.layout`

Entries keep flat names; the layout says which groups nest where. Rejected:
it splits one fact across two objects (a card's group membership no longer
tells you where it renders), the layout is optional today and would become
required for nesting, and constraint 2 still forces a depth-bounded pair of
types (`LayoutGroupSpec` → `LayoutSubgroupSpec`).

## Proposal in one paragraph

Treat `/` in a group name as a hierarchy separator, capped at three segments.
`Card.Group` keeps carrying the full path end-to-end (store, poller, SSE
hashing all unchanged); the display layer (`groupCards`) gains a tree-building
step that turns path-keyed flat groups into `cardGroup`s with a `Subgroups
[]cardGroup` field (Go recursion is fine — only the CRD schema can't recurse),
`cards.templ` renders subgroups as nested `<details>` inside the parent's
block, and `LayoutGroupSpec.Name` accepts the same path syntax so a subgroup
can be styled (`columns`, `style`, `icon`, `header`, `initiallyCollapsed`,
`useEqualHeights`) exactly like a top-level group. Tab placement stays
root-only: a subgroup always lives in whatever tab its root group is in.

## API changes (`api/v1alpha1`)

- **`ServiceEntry.Group` / `ServiceCardSpec.Group`**: replace the "top-level
  only" doc comments with the path semantics, and add a pattern bounding shape
  and depth (no empty segments, no leading/trailing slash, max 3 levels):

  ```go
  // +kubebuilder:validation:Pattern=`^[^/]+(/[^/]+){0,2}$`
  ```

  Segment length stays governed by the existing `MaxLength=256` on the whole
  field. Depth 3 matches what a dashboard can visually sustain and mirrors
  homepage's practical usage; deep hierarchies belong in tabs.
- **`LayoutGroupSpec.Name`**: same pattern + updated docs — a path names the
  subgroup to place/style. New CEL rule *per tab* keeping placement root-only
  and unambiguous: a path entry's root segment must also appear in the same
  tab's `groups` list (so `Media/Movies` can't be listed under tab A while
  `Media` sits in tab B):

  ```go
  // on LayoutTabSpec:
  // +kubebuilder:validation:XValidation:rule="self.groups.all(g, !g.name.contains('/') || self.groups.exists(r, g.name.startsWith(r.name + '/')))",message="a nested group entry's parent group must be listed in the same tab"
  ```

  The rule checks for an ancestor *prefix* entry rather than splitting out
  the root segment: `split()` plus the original MaxItems=64 on a tab's
  `groups` blew the apiserver's static CEL cost budget by 2.8x (found by
  envtest in CI). `startsWith` + capping `groups` at MaxItems=32 brings the
  quadratic scan back under budget, and applied to every path entry the
  prefix check still makes the root required transitively.
  (`contains`/`startsWith` are in the CEL environment the apiserver has
  always shipped for CRD validation; well below the repo's effective 1.31
  floor.) Ordering of subgroups within a parent follows the tab's `groups`
  list order when placed there, else entry `Order`/name as today.
- **`BookmarkEntry.Group`**: unchanged. homepage does not nest bookmark
  groups; keep the "not supported" comment and skip the pattern so nothing
  tightens there.

After editing: `mise run manifests generate schemas`, plus
`helm-chart-refresh` (CRD YAML lands in `dist/chart` and `dist/install.yaml`),
per the `add-crd-field` skill.

## Display-layer changes (`internal/dashboard`)

### Grouping (`server.go`)

- `cardGroup` gains `Subgroups []cardGroup` and a `Path string` (full
  `a/b/c`; `Name` becomes the leaf segment for the heading).
- `groupCards` still buckets by the full `Card.Group` string in first-seen
  order, then a new `nestGroups([]cardGroup) []cardGroup` pass builds the
  tree: split each path, materialize missing ancestors (a card at
  `Media/Movies` with no direct `Media` card still needs a `Media` parent —
  header shown, zero direct cards), attach children to parents preserving
  first-seen order. Direct cards render before subgroups within a parent
  (homepage interleaves by YAML order; a flat CRD list has no such order, so
  cards-then-subgroups is the deterministic choice — documented limitation).
- `layoutTabs`: placement keys on root paths only; the per-tab style
  resolution walks the tree, matching each node's full path against the tab's
  `LayoutGroupSpec`s (a flat `map[path]LayoutGroupSpec` built per tab).
  Unmatched subgroups inherit nothing new — same site-wide defaults as today.
  The "Other" tab collects unplaced *root* groups with their whole subtree.
- `layoutGroupsByName` (bookmark styling) is unaffected: bookmark groups stay
  flat, and a path-named layout entry simply never matches a bookmark group.

### Rendering (`cards.templ`, `index.templ`)

- `serviceGroupBlock` becomes recursive (templ handles recursive components
  fine): after the group's grid, `for _, sg := range g.Subgroups {
  @serviceGroupBlock(sg, ...) }`, wrapped in a `div.subgroups` inside the
  parent's `<details>`/plain block so collapsing the parent hides children.
- `data-group-name` uses the full path — the existing capture/restore
  (`captureGroupState`/`restoreGroupState` in `index.templ`) and idiomorph
  morphing then preserve open/closed state per subgroup with no JS changes,
  since paths are unique where bare leaf names are not.
- CSS: `details.group details.group` gets indentation and a smaller summary
  (step down the existing `summary` type scale once per level; depth ≤ 3 keeps
  this a two-rule addition). `@media (prefers-reduced-motion)` rule already
  matches by selector, no change.
- Search-filter JS hides cards by `data-name`; group auto-hide behavior (if
  any) applies per `<details>` and nests naturally — verify in the browser
  test rather than special-casing.
- Regenerate with `mise run templ-generate`; `_templ.go` is committed.

### Untouched by design

`Store`, `Poller`, SSE change-hashing, `Card` shape, secret resolution — all
key off the opaque `Card.Group` string and full-fragment hashes, so nesting is
invisible to them. Discovery (`discovery.go`) needs only a doc-comment update:
the `<prefix>group` annotation value may now be a path.

## Testing

- `server_test.go`: table tests for `nestGroups` (ancestor materialization,
  ordering, depth 3, path styled via layout, root-only tab placement,
  "Other"-tab subtree).
- `golden_test.go` / `visual_test.go`: new fixture with a nested sample —
  goldens regenerate, screenshots reviewed.
- `browser_test.go`: collapse a parent, morph a fragment update, assert
  subgroup open-state survival (the `data-group-name` path claim above).
- `api/v1alpha1` envtest CEL cases: pattern accepts `a`, `a/b/c`; rejects
  `/a`, `a//b`, `a/b/c/d`; tab rule rejects an orphaned `Media/Movies` entry.
- `config/samples`: extend the `ServiceCard` sample with one nested group so
  `mise run preview -f config/samples` shows it.

## Phasing

| Phase | Scope | Ships as |
|-------|-------|----------|
| 1 | API pattern + docs, `nestGroups`, recursive template, CSS, tests, samples | `feat(dashboard): nested service-card groups` |
| 2 | Layout path styling + per-tab CEL rule, root-only placement | `feat(api): style nested groups from DashboardStyle layout` |
| 3 | README/docs section (homepage-migration note incl. `/`-in-name caveat), discovery doc comment | `docs: nested groups` |

Each phase passes `preflight` independently; phase 1 alone is already useful
(nesting with default styling).

## Out of scope

- Nested bookmark groups (no homepage equivalent).
- Interleaving cards between subgroups within a parent (no ordering source in
  a flat list; revisit if `Order` should compare across cards and subgroups).
- A group CRD or any new kind — the string path keeps groups implicit, as
  today.

## Open questions

- Should the `/` separator be escapable (`\/`) for a literal-slash group name?
  Proposed: no — undocumented edge, and `v1alpha1` may break it.
- Does `GroupsInitiallyCollapsed` apply per level (parent collapsed hides
  children anyway)? Proposed: yes, uniformly — cheapest and matches the
  "every group" wording.

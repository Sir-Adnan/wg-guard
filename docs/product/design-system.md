# Design system

WG-Guard's panel, authentication and public subscription surfaces share one design system:
shadcn/ui component anatomy, rendered by Go `html/template` with plain CSS and minimal vanilla
JavaScript. The default visual language is Vercel/Linear: compact density, hairline borders,
monochrome surfaces, monospace technical values and underline tabs. Visual presets restyle every
surface — colors, fonts, radius, shadows, spacing, density and layout composition — without
page-specific rules. The [UI/UX contract](ui-ux.md) defines product behavior; this document
defines how it is built.

## Layers

| Layer | Location | Owns |
| --- | --- | --- |
| Presets | `web/themes/*.json` | shadcn tokens per mode, fonts, radius, shadows, spacing, layout axes |
| Tokens | `web/styles/02-tokens.css` | derived scales (radius, spacing, type, density, motion, z-index) |
| Base | `web/styles/10-base.css` | reset, typography, focus, icons, technical values |
| Components | `web/styles/20-components/*.css` | Button, Badge, Card, Field, Table, Dialog, Menu, Tabs, Chart… |
| Layout | `web/styles/30-layout/*.css` | app shell (sidebar/header), page, auth, public subscription |
| Pages | `web/styles/40-pages/*.css` | the few compositions unique to one workspace |
| Utilities | `web/styles/90-utilities.css` | meter widths and small single-purpose helpers |

The server concatenates `web/styles` in path order into one cached `/assets/css/app.css` at
startup, inserting the built-in preset's tokens after the `@layer` declaration. There is no build
tool and no Node.js runtime. Cascade layers (`reset, tokens, base, components, layout, pages,
utilities`) keep ordering explicit, so a page rule never needs `!important` to beat a component.

Rules for contributors:

- Components read tokens only. No hex colors, pixel radii or font names outside `web/themes` and
  `02-tokens.css` (status tones use `--success`/`--warning`/`--info`/`--destructive`).
- Use logical properties (`inline-start`, `padding-inline`) so RTL needs no overrides; the few
  physical exceptions (transforms, charts, LTR technical values) are commented.
- Spacing derives from the preset's `--spacing` (`--s-1` … `--s-16`), radius from `--radius`
  (`--radius-xs` … `--radius-xl`), so a preset reshapes everything consistently.
- A new page composes existing components first. A page stylesheet in `40-pages` is the last
  resort and stays small.

## Visual presets

A preset is one reviewed JSON file in `web/themes`, named after its `id`:

```json
{
  "schema": 2, "id": "zen", "name": "Zen", "group": "tweakcn",
  "source": "https://tweakcn.com/themes/<theme-id>",
  "ui": {"density": "comfortable", "sidebar": "inset", "tabs": "underline", "surface": "flat"},
  "cssVars": {"theme": {"font-sans": "Inter, sans-serif", "radius": "0.5rem"},
              "light": {"background": "…", "primary": "…"}, "dark": {"background": "…"}},
  "contrast": {"light": {"primary-foreground": "#191919"}}
}
```

- `cssVars` uses tweakcn's export shape and shadcn token names. Every mode defines the 31 color
  tokens (`background` … `sidebar-ring`, `chart-1` … `chart-5`). Optional keys: fonts, `radius`,
  shadow ingredients or the precomputed `shadow-*` scale, `letter-spacing`/`tracking-*`, `spacing`,
  and WG-Guard's `success`/`warning`/`info` (+ `-foreground`), `primary-text`, `destructive-text`.
- `ui` picks the layout axes, written as attributes on `<html>`:
  - `density`: `compact` (Vercel/Linear), `default` (shadcn) or `comfortable`;
  - `sidebar`: `plain` (flush with a hairline), `inset` (content in a rounded panel) or
    `floating` (sidebar as a panel);
  - `tabs`: `underline` or `segmented`;
  - `surface`: `flat` (borders only) or `raised` (cards with a small shadow).
- `contrast` holds computed readability corrections; the source values stay untouched.
- `group` is `wg-guard` (project-authored), `shadcn` (official palettes) or `tweakcn` (imports,
  which must name their source page).

`internal/web/theme` validates each file (allowlisted keys, values that cannot end a declaration,
open a block or load a resource, the closed `ui` values, file name = id) and generates CSS:
the built-in preset's rules apply to `:root`; every other preset is a separate
`/assets/css/themes/<id>.css` loaded only while active. A key a preset sets in one mode only is
restated in the other from the default, so light values never leak into dark mode. Fonts are
self-hosted (see THIRD_PARTY.md); a source stack's generic families move after the bundled
faces, and Persian text always uses Vazirmatn, which covers Arabic script only.

Adding a preset:

```sh
go run ./tools/theme-import -id my-theme -source https://tweakcn.com/themes/<id> \
    -ui default,inset,underline,raised < export.json > web/themes/my-theme.json
go test ./internal/web/theme
```

The importer accepts tweakcn's registry JSON (`https://tweakcn.com/r/themes/<id>`) or the CSS from
the theme's Code button, drops keys WG-Guard does not consume, and computes `contrast` so every
text pair reaches WCAG AA (4.5:1) in both modes. The test suite fails a preset whose pairs fall
below that ratio.

## Components

Markup is semantic HTML with component classes; behavior hooks are `data-*` attributes handled
by `web/static/js/ui.js` (shell, menus, dialogs, tooltips, toasts, command palette),
`presentation.js`, `select.js` and the feature modules.

| Component | Markup |
| --- | --- |
| Button | `.btn` (outline) + `.btn--primary` / `--secondary` / `--ghost` / `--destructive` / `--danger-ghost` / `--link`; sizes `--xs` / `--sm` / `--lg`; `--icon`; `.btn-group` |
| Badge | `.badge` + `--success` / `--warning` / `--destructive` / `--info` / `--muted` / `--outline` / `--primary`; status badges start with `<span class="dot"></span>` |
| Card | `.card` › `.card-header` (`.card-title`, `.card-description`, `.card-action`) · `.card-content` · `.card-footer`; `.card--flush` for lists and tables; inner regions use `.panel`, never a nested card |
| Field | `.field` › `label.field-label` + control + `.field-description` + `.field-error`; `.field-grid` (`--2`, `--3`) for forms |
| Controls | `.input`, `.textarea`, `.select` (native), `.input-group` › `.input-group-addon`, `.checkbox`, `.radio`, `.switch` (`role="switch"`), `.choice`, `.choice-card`, `.toggle-group`, `.chip` |
| Tabs | `nav.tabs` › `a.tab[aria-current="page"]` or `button.tab[aria-selected]`; style follows `data-tabs` |
| Table | `.table-frame` › `.table-wrap` › `table.table`; `.table--cards` turns rows into labeled cards under 800px (`td[data-label]`, `.cell-primary`, `.col-check`, `.col-actions`) |
| Item | `.item-group` › `.item` (`.item-media`, `.item-content` › `.item-title` + `.item-description`, `.item-actions`); `a.item-link` makes the row clickable |
| Stat | `.stat-grid` › `.stat` (`.stat-label`, `.stat-value` + `.stat-unit`, `.stat-meta`) |
| Alert | `.alert` + `--info` / `--success` / `--warning` / `--destructive` › icon, `.alert-title`, `.alert-description`, `.alert-action` |
| Empty | `.empty` (`--bordered`, `--compact`) › `.empty-icon`, `.empty-title`, `.empty-description`, `.empty-actions` |
| Dialog | `dialog.dialog` › `.dialog-header` (`.dialog-heading` › `.dialog-title` + `.dialog-description`) · `.dialog-body` · `.dialog-footer`; sizes `--sm`/`--lg`/`--xl`; becomes a bottom sheet on phones |
| Sheet | `dialog.sheet` › `.sheet-header` · `.sheet-body` · `.sheet-footer` |
| Menu | `.menu-anchor` › trigger button + `.menu[role=menu]` › items, `.menu-label`, `hr` |
| Collapsible | `details.collapsible` › `summary` + `.collapsible-content`; `.accordion` groups them |
| Misc | `.kbd`, `.separator`, `.avatar`, `.spinner`, `.skeleton`, `.progress` › `.progress-bar` (`w0`…`w100`), `.code`, `.code-block`, `.copy-field`, `.dl`, `.kv` › `.kv-row` |

Page composition: `.page-header` › `.page-heading` (`h1.page-title`, `.page-description`) +
`.page-actions`; `.section` › `.section-header`; layout helpers `.stack*`, `.cluster`, `.row-between`,
`.grid-2/3/4`, `.grid-auto`, `.split` › `.split-aside`, `.toolbar`, `.savebar`, `.bulk-bar`.

Technical values (IPs, keys, versions, ports, matchers) use `.tech`: LTR isolation, monospace,
tabular digits. Human numbers with localized units use `.num` (tabular, isolated, UI font).
Never put Persian words inside `.tech`.

## Shell

The app shell (`web/templates/layout_app.html`, `web/styles/30-layout/shell.css`) is the shadcn
Sidebar block: brand, a search button that opens the command palette, static labeled groups
rendered from `View.Nav` (`internal/web/navigation.go`, shared with the palette) and an account
menu. The header holds the sidebar toggle, the page title and locale/theme controls. The desktop
sidebar collapses to an icon rail with tooltips; below 960px it is a modal drawer. The preset's
`sidebar` axis picks the composition (`plain`, `inset` or `floating`). The command palette
(⌘K / Ctrl+K / `/`) filters the same destinations plus quick actions and a user search.

`.content` caps the page column at `--content-max` (80rem). `pageClass` in
`internal/web/render.go` adds `.content--narrow` (48rem) on `/new` and `/edit` routes and
`.content--settings` (64rem) on `/settings`; `.content--wide` (96rem) exists but no route uses it.

## Subscription page designs

The public subscription page has one shared body (`web/templates/page_sub.html`: notice,
section tabs, devices, guide, details) and a per-design hero. Adding a design takes:

1. its ID in `subLayouts` (`internal/web/sub.go`; the first entry is the default) and in the
   `subscription.layout` options (`internal/settings/defaults.go`);
2. a hero template `sub_hero_<id>` in `web/templates/partial_sub_heroes.html`. A hero renders
   the identity, status and what is left, and the page's only `h1` (`#subscription-name`). It
   composes the shared defines in `page_sub.html` (`sub-plan`, `sub-data-label`,
   `sub-data-value`, `sub-time-value`, `sub-expiry`, `sub-devices-count`, `sub-meters`,
   `sub-status-badge`) rather than deriving values again;
3. an `{{else if eq .Data.Layout "<id>"}}` branch in the `subcontent` dispatch chain in
   `page_sub.html` (the final `else` renders the default `sub_hero_ticket`);
4. a stylesheet `web/styles/40-pages/sub-designs/<id>.css`. Rules for the hero's own classes
   need no prefix; rules that restyle the shared body are scoped to `.sub--<id>` or
   `body[data-sub-layout="<id>"]`. The server bundles `web/styles` in sorted path order, and
   `sub-base.css` (the shared body) sorts before the `sub-designs/` directory, so it always
   loads first and a design overrides it at equal specificity whatever its ID;
5. `appearance.layout.<id>` and `appearance.layout.<id>_hint` catalog strings in fa and en
   (`internal/i18n/catalog_ui_appearance.go`) and a schematic thumbnail
   (`.layout-thumb[data-layout="<id>"]` in `web/styles/40-pages/appearance.css`, five `<i>` parts).

`TestSubscriptionDesignsAreComplete` (`internal/web/sub_test.go`) fails when a `subLayouts` entry
lacks its `sub_hero_<id>` template or `sub-designs/<id>.css`, or when that file is bundled before
`sub-base.css`. It does not check the dispatch branch, settings options, catalog strings or
thumbnail. Status color comes from `--status-tone`, set once per `data-status`. Retiring a
design needs a migration that maps its stored value to a successor (see
`migrations/0019_subscription_layouts.sql`).

## Verification

`go test ./internal/web/theme` validates every preset (schema, safety, contrast in both modes,
CSS generation). Visual checks cover desktop and phone widths, both locales and both modes for
the default preset plus at least one preset of each `ui` combination touched by a change.

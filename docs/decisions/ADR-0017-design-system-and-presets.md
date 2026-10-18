# ADR-0017 — shadcn-anatomy design system with data-only visual presets

Status: accepted · Date: 2026-10-17

## Context

The panel's CSS had grown into one large stylesheet plus per-workspace files with page-specific
looks, and visual presets only recolored a token bridge. The product owner asked for a complete
redesign on shadcn/ui components with a Vercel/Linear default, and for selectable themes
(including tweakcn exports) that change everything — colors, fonts, radius, shadows, spacing,
density and layout — for the panel, its default, and the public subscription page. Production
must stay free of Node.js, Tailwind and build tools, under a strict CSP.

## Decision

- Rebuild every surface on shadcn/ui component anatomy written as plain CSS in `web/styles`,
  ordered by cascade layers and concatenated by the server at startup into one cached
  `app.css`. Components read only tokens; spacing and radius scales derive from the preset.
- Store each preset as reviewed JSON in `web/themes` using tweakcn's `cssVars` shape and shadcn
  token names, plus four closed layout axes (density, sidebar, tabs, surface) written as `<html>`
  attributes. `internal/web/theme` validates files and generates CSS; the built-in preset ships
  inside `app.css` and every other preset is a separate stylesheet loaded only while active.
- Store computed WCAG AA corrections beside the untouched source values; tests reject presets
  whose text pairs fall below 4.5:1 in either mode. Import with `tools/theme-import`.
- Self-host every font; restrict Vazirmatn to Arabic script so Persian is always readable.
- Keep the subscription page's default preset/mode in the installation row; a visitor's own
  choice is a cookie honored only for registered IDs.

## Consequences

- A new theme is one data file and a test run; feature templates never branch on presets.
- Previews are cheap: the Appearance gallery paints miniatures from generated swatch variables
  and previews a preset by loading its stylesheet and setting the layout attributes.
- No runtime theme code or remote CSS; values are allowlisted keys with declaration-safe values.
- Bundled fonts add repository weight (each face is loaded only when a preset uses it).

## Alternatives rejected

- Tailwind/shadcn React components: require Node tooling and a client runtime.
- Recoloring only: cannot deliver the requested density, layout and typography changes.
- Loading tweakcn CSS at runtime: remote, unreviewed and incompatible with the CSP.

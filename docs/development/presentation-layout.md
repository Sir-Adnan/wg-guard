# Presentation layout follow-up — 2026-10-05

Implementation source: `76776bebaa39d12f424f7138180410937f02187e`.

The owner supplied regressions in help placement, interface metadata/capacity,
desktop backup creation, and chart inspection, then requested Standard/From template
creation tabs. The shared [presentation contract](../product/presentation-system.md)
describes the implemented behavior; this record separates local evidence from host
and full-product certification.

## Implemented scope

- Help buttons attach to field labels, legends or actual section headings. Checkbox
  cards are excluded; hints containing validation remain visible. Shared label rows
  align paired controls, and the popover does not add blank rows or close its drawer.
- Interface identity and network stacks separate technical CIDRs, status/MTU and
  human capacity. Backup creation is a compact desktop header/input/action row with
  a stacked phone layout and no nested full-width action box.
- The shared create-user partial provides Standard and From template modes on page,
  drawer and bulk creation. Account identity, optional ready devices, notes and tags
  are common. Modes preserve field values and disable inactive entitlement controls.
  An empty template-mode submission cannot silently create unlimited manual terms.
- Existing bounded SVG charts gain structured localized inspectors, true circular
  pixel markers, separate radial percentage gauges, and subtle areas that preserve
  unavailable gaps. A top-layer popup beside the pointer avoids intercepting plot
  movement; keyboard/touch and native data-table paths remain.

The public REST/OpenAPI contract, telemetry collector, entitlement model, backup
archive and installer state schemas are unchanged. `creation_mode` is an optional
panel-form control, not a new REST field. No React/Recharts or production dependency
was added. The [shadcn chart examples](https://ui.shadcn.com/charts/area) inform
composition, not data semantics or a library migration.

## Local verification

Fresh Windows web/i18n tests and focused new form/SVG regressions passed, as did
focused vet and Linux Go 1.26 race checks for web/i18n. The targeted race run took
about 111 seconds for web; it is not a full repository/host certification. The asset
budget check passed; no new production packages were installed.

Chromium and WebKit each passed five representative fa/en light/dark cells at
320/390/768/1440px, two native/no-JavaScript cells and one touch/Popover-unavailable
cell. Those checks include actual page/drawer mode switching, retained values,
disabled inactive controls, help keyboard/dismissal, HTMX idempotence, server error
visibility, compact backup width/height, separate CIDR/capacity, structured chart
hover/keyboard and touch dismissal. The existing 16-cell presentation matrices and
native interface fallback also passed within their applicable scope. A focused
Claude + fa/dark desktop run plus native/touch fallback checked preset compatibility.
Rendered desktop/phone and inspector images were visually inspected.

## Workspace and compact-selection follow-up

The subsequent owner review requested a smaller volume/duration picker, an inset
sidebar with direct Domains/HTTPS navigation, and an Appearance redesign. The same
change now implements shadcn Select-inspired combobox/listbox presentation over
native preset values, a scrollable selected list and canonical editable value/unit
fields. Desktop places the picker alongside those fields; narrow phones give it one
full-width row. HTMX enhancement is idempotent, inactive creation modes disable the
proxy as well as the native select, and Escape returns focus without closing the
create drawer. Popover-unavailable browsers temporarily place the list directly in
the native dialog so card clipping and blur do not hide it.

The sidebar adds disclosure groups, an inset desktop frame and a topbar rail toggle.
Domains/HTTPS remains gated by `server.view` and absent from reseller navigation.
Appearance separates style, numerals and reset, presents the gallery beside live
preview/application controls on desktop, and keeps personal and owner-default
actions distinct. Numeral submissions return to their own tab. See the updated
[presentation contract](../product/presentation-system.md) for these behaviors.

Fresh Chromium and WebKit focused runs cover the real create page/drawer, manual
editing, preset selection/typeahead/keyboard scrolling, disabled-mode transitions,
Escape/outside dismissal, phone touch and the positioned fallback. The five
representative cells, two no-JavaScript cells, sidebar state/current location and
Appearance tab/preview checks passed in both engines. Their 16-cell presentation
and shared foundation checks also passed. Foundation navigation now explicitly
awaits completed asynchronous UI initialization before sending keyboard input.
Rendered desktop/phone dropdown and Appearance images were visually inspected.
Fresh Appearance checks also passed 80 preset/locale/mode/viewport cells in each
engine, including personal/default persistence and the mobile public QR surface.
These emulated viewports are not a physical mobile check. Fresh focused Linux race,
vet and Linux/amd64 build cover the
resulting web/i18n/source assets. Asset measurement remains an engineering
observation; no new production package was added.

An initially reproduced chart hover failure was corrected: the positioned tooltip
could cover the pointer and intercept continued movement. The successful corrected
runs use viewport-clamped top-layer positioning beside the pointer; the earlier
failing run is not counted as a pass.

No fresh axe scan, physical mobile/browser, real VPN/TLS/backup host mutation, full
panel matrix or new release is claimed here. Exact main CI remains a separate gate;
[Phase 20](phase20.md) records the owner's separate real-host restore observation.

# Panel presentation primitives

The Go/HTMX panel keeps its embedded browser runtime. Common primitives follow
shadcn's [Field](https://ui.shadcn.com/docs/components/base/field),
[Tabs](https://ui.shadcn.com/docs/components/base/tabs) and
[Popover](https://ui.shadcn.com/docs/components/base/popover) composition and interaction
patterns without introducing React or a production package runner.
Compact preset pickers follow [Select](https://ui.shadcn.com/docs/components/base/select);
desktop navigation follows the [inset sidebar](https://ui.shadcn.com/blocks/sidebar#sidebar-08)
composition while preserving the existing permissions and native/mobile navigation.

## Numerals

Migration 0016 adds `appearance_defaults.digits` and per-admin `appearance_digits`.
Values are `latin` and `persian`; an empty personal value inherits the installation
default. Latin is the fresh/upgrade default regardless of language. The owner changes
the installation default; signed-in accounts change only their own overrides. Preset,
mode and language remain independent. Settings links to Appearance, where both controls
are available. Preferences do not change API numerics, stored quotas, passwords, URLs
or client configurations.

`internal/i18n/digits.go` is the final display layer. Canonical formatters retain ASCII.
Web translations normalize hardcoded Persian/Arabic numeral literals by default; the
Persian option changes human counts, amounts, dates and durations. IP/CIDR examples,
profile names, versions and string arguments such as usernames remain canonical. Form
inputs, copied values and identifiers keep their original values. Calendar and glyph
choice are independent: Persian language uses Jalali and English uses Gregorian.

## Guidance and structure

`presentation.js` enhances field hints and explicitly marked guidance into help
buttons beside their labels or section headings, including fieldset legends. A
checkbox card is not a section heading. Original content/description IDs remain available to
assistive technology. Hover/focus opens help, click/touch pins it, and Escape/outside
interaction closes it. Native popovers provide top-layer placement and viewport
clamping; browsers without Popover receive a positioned disclosure. Without JavaScript
the original descriptions stay inline. The button exposes expanded/controlled state;
Escape returns focus and does not close the containing form dialog. Help never creates
a separate empty row, and paired field labels reserve the same touch-target height so
their inputs align. Validation errors, including errors nested inside hints, status
information and operational warnings stay visible. HTMX swaps receive the same
idempotent enhancement; there is no untrusted HTML injection or idle polling.

Cleanup uses searchable dropdowns for data kinds, eligible status unions and owners,
with Select all, partial-selection states and collapsed selected summaries. Its date
basis uses a radio menu. Menus share the same top-layer controller, support touch,
arrow focus movement, outside dismissal and Escape, and preserve their native details
fallback. Changing presentation cannot grant additional deletion permissions.

The tab primitive registers existing panels, supports directional arrows and Home/End
with roving focus, and opens the panel containing a server validation error. Without
JavaScript all sections remain accessible. Hidden panels retain submitted input values.
Interface forms group General, Address pools and Profile; packet parameters and overflow
pool editing are collapsible. Tab groups and panels compose the shared `form-stack`
layout so nested fieldsets retain card padding, heading alignment and spacing at every
viewport, including the all-sections fallback without JavaScript. Shared CSS uses
existing semantic tokens for restrained headers, type, borders, controls and focus
states across presets.

Interface collections group name, state and MTU as an identity stack. CIDRs are
individually LTR-isolated on separate lines; the human capacity summary sits below
them rather than joining a technical address. This composition also survives the
existing table-to-card mobile transition. The backup-create card spans its workspace,
with a compact heading/input/action row on desktop and stacked controls on small screens.

User creation shares account identity and ready-device provisioning between Standard
and From template tabs in both the full page and drawer. Switching preserves entered
values, disables inactive entitlement inputs and keeps notes/tags common. Template mode
requires a selected template on submission; its server-owned terms cannot be overridden
by hidden manual values. Native forms keep all sections reachable without JavaScript.

Volume and duration presets use compact dropdowns inside the value/unit group, with
a shadcn Select-inspired trigger, scrollable list and selected checkmark. A native
select owns the preset value and a bounded, idempotent enhancement supplies the
combobox/listbox presentation. Only one list opens at a time; it uses the top layer
when available and a positioned fallback inside the drawer otherwise. Arrow keys,
Home/End, typeahead, Escape/focus return, outside dismissal and touch are supported.
Technical volume labels retain LTR isolation in either language.
Desktop keeps the three controls on one line; phones stack only the preset dropdown.
Selecting a preset fills canonical value/unit fields without adding a submitted field;
manual editing updates or clears the selected preset. Without JavaScript the inert
quick-fill picker is hidden and the ordinary fields remain available. No scrolling
chip strip or multi-row button matrix consumes the card's unused half.

The inset desktop shell is composed from the existing sidebar/drawer primitives,
with disclosure groups, a topbar rail trigger and current-location state. Rail mode
temporarily expands all groups and restores their prior disclosure choices when
expanded again. The System group starts collapsed off its active routes; native
navigation remains expanded without JavaScript. Domains/HTTPS has its own `server.view`
navigation item; Settings is current only at its own root. Neither link exposure nor
group presentation grants permissions or exposes domains to reseller navigation.

Appearance uses separate Visual style, Numerals and Reset panels with keyboard tabs
and section-address preservation. The gallery and a live sample/application column
compose one form; the owner's shared-default controls use a disclosure and explicit
confirmation, separate from personal application. Preset, mode, digits, language and
direction semantics stay independent. Numeral submissions return to their own panel.

Dashboard charts use the existing bounded server-rendered SVGs with chronological
lines and subtle area segments; missing samples stay gaps in both. Latest CPU, memory
and disk percentages have radial gauges separate from their history. Structured
inspection shows a timestamp and localized series/value rows, uses semantic preset
colors and fixed-size circular markers, and clamps in the viewport using a top-layer
popover beside the pointer. Hover is
hoverable/dismissible; click/touch pins the selected sample, keyboard arrows/Home/End
inspect chronological points, and Escape/outside interaction or a swap closes it.
Exact native data tables and no-JavaScript plots remain available. These are local
SVG compositions inspired by the shadcn examples, not an embedded Recharts runtime.

## Pool editor

The editor offers automatic suggestions, prepared /24–/20 CIDRs, manual input and
overflow selection. Suggestions show capacity and unavailable/reserved/current/full
states from a bounded read-only snapshot. `/interfaces/pools/suggestions` is an
authenticated panel route, not a REST extension. Saving still runs authoritative
transaction and host-route validation. A CIDR reserved by another profile cannot be
assigned even if it has no devices. A full current pool advances allocation to another
configured pool on that profile; all full returns `DEVICE_POOL_EXHAUSTED`. See
[address maintenance](../operations/cleanup.md).

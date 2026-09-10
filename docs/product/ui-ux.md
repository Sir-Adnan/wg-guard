# UI/UX design system

Phase 10 design contract, approved 2026-09-10. Implementation/verification status and requirement
ownership live in [phase10.md](../development/phase10.md). Existing pages adopt this contract in
that order; this document is not a claim that the complete redesign is already verified.

## Architecture and visual direction

Go `html/template` renders the product; HTMX enhances targeted regions and ordinary forms/links
retain meaningful fallback paths. `internal/web` calls existing services directly, never the REST
API. Typed page/view data owns formatting and states; templates own semantic markup. No React,
SPA, Tailwind, heavy framework, chart runtime, QR runtime or production Node.js dependency.

Premium means precise hierarchy, spacing, typography, consistency, usability and restraint.
Shadcn-like neutral surfaces carry the product: white/zinc light, near-black `#09090b` dark,
monochrome primary actions, restrained semantic colors, no purple identity. Apple/iOS influence
is limited to refined depth and responsive feedback. Glass/blur/gradients/highlights are optional
on selected chrome/overlays, with an opaque readable fallback. No neon, glow, decorative card
movement, constant animation or blur-heavy content surfaces.

Three layers keep the system maintainable:

1. CSS custom properties: semantic background/text/border/status/focus colors, spacing on a 4px
   grid, type/line-height, control sizes, radii, elevation, motion and stacking tiers.
2. Primitives: Button/IconButton, Field/Input/Select/Toggle/Checkbox, Badge/Status, Card, Alert,
   Toast, Skeleton, EmptyState, Tabs, Dialog/Sheet, Menu/Popover/Tooltip, Progress and Pagination.
3. Compositions: page header, list toolbar/selection, entity summary, technical value/copy,
   metric/chart, Advanced section and target-specific destructive confirmation.

Reuse named template partials and CSS variants; do not create a generic component DSL or copy
page-specific markup into every surface. Shared vanilla modules own behavior through `data-*`
hooks; they use delegation or idempotent initialization and release stale references on swaps.
Business-specific form code stays outside shared overlay/navigation behavior. Preserve strict
CSP, same-origin assets, session/CSRF checks and no-store sensitive responses.

## Layout and navigation

Desktop uses a compact permission-aware sidebar: Manage (Dashboard, Users, Plans, Interfaces),
System (Backups, Administrators, API Tokens, Webhooks, Audit, Settings). Server authorization stays
authoritative; unauthorized destinations are absent. The topbar supplies page context, including
root/dashboard equivalence, and reachable language/theme controls. Collapsed navigation still
has accessible names and current-location state.

Below 960px the sidebar becomes a modal navigation drawer: close button/Escape/backdrop, contained
focus, inactive background, scroll containment and return to the trigger. Resizing across the
boundary releases mobile state. Do not add bottom navigation without a demonstrated workflow.
Forms/settings use a centered narrow tier (about 760px); operational content uses a fluid tier
capped at 1400px and 1560px on large displays. Keep 320px layouts usable without hiding data.

Lists transform into intentional cards when columns stop being useful (initial table tier 800px),
preserving labels, selection, action order and semantics. Do not duplicate whole desktop/mobile
DOM trees. Long values wrap or have a deliberate readable/copyable presentation. Compact dialogs
can become sheets on phones; keyboard, safe areas, short landscape heights and touch targets are
part of layout, not afterthoughts. The final viewport/browser matrix is owned by Phase 10.

## Typography, localization and technical data

Vazirmatn is self-hosted, OFL, `font-display: swap`, shared by Persian and English; current assets
are Regular/400 and SemiBold/600 WOFF2 files, not locale unicode-range subsets. Keep weights and
payload limited; a future subset change requires measurement and honest notices. Body/form text
must remain readable on phones; headings, metadata and numeric metrics have distinct hierarchy.

All product copy, including dynamic JavaScript feedback and scope/event descriptions, comes from
`internal/i18n` fa/en catalogs. Static key parity is necessary but dynamic-key/rendered-copy checks
and human terminology review are also required. Resolve language on the server and set `lang` and
`dir`; use CSS logical properties with only necessary physical exceptions. Default locale is fa;
theme defaults to system and explicit light/dark preferences work across app, auth and subscription.

Technical values (IP/CIDR/ports/keys/counters) use LTR isolation, Latin digits and tabular numerals;
copy returns the original value, never formatted/truncated text. Mixed-language names use bidi
isolation. Dates remain Jalali in fa and Gregorian in en; show the time basis where relevant.
Directional navigation icons mirror appropriately; data plots/time axes do not reverse merely
because surrounding copy is RTL. Scope/permission/event machine identifiers stay stable in forms
and API; localized human labels/descriptions are primary UI text.

## Interaction, states and accessibility

Every component defines applicable default, hover, focus-visible, pressed, selected, disabled,
loading, success, warning, error and destructive states. Hover only enhances interactive targets;
keyboard/touch provide equivalent operations. Controls have accessible names, visible focus and
44px touch targets. Use native HTML semantics first; adding ARIA roles requires implementing the
matching keyboard behavior. Focus must not be hidden behind sticky chrome/overlays.

Menus support open/focus, arrows/Home/End, Escape and focus return; Tab can leave and dismiss.
Dialogs/sheets contain focus and restore it to the invoker, including after successful actions.
There is one active modal interaction; nested controls remain operable. Tooltips never contain
essential instructions or require hover. Calendar keyboard/date-entry behavior belongs to its
form migration; it must support Jalali/Gregorian labels without changing stored date semantics.

Forms have visible labels, associated hints and field errors, with a focusable error summary.
A failed submission preserves nonsensitive input, reveals invalid Advanced fields, and explains
whether anything changed. Secrets are never redisplayed. Pending actions prevent accidental
repeat submission, announce busy state, and recover after errors/back navigation. Keep the actual
submitter's name/value and server validation authoritative. Success feedback does not require JS
and routine updates avoid flooding live regions. Empty/no-results/unavailable/error are distinct,
with a useful next action. Destructive confirmation names the target, count and consequence.

Motion is short (120–240ms), purposeful, mainly opacity/transform; no idle pulse or decorative
movement. Loading animation exists only while loading. `prefers-reduced-motion` disables overlay,
skeleton, chart and other nonessential animation without hiding feedback. Both themes meet AA
contrast for text and controls; status/series never depend on color alone. Verify keyboard,
screen-reader reading/announcements, zoom/reflow and touch, not only DOM attributes.

## Dashboard and charts

Redesign presentation while retaining the Phase 9 data contract: one scheduler-owned bounded
180-point history, ten-second live refresh, hidden-tab requests paused; durable traffic comes
from rollup tables. Never sample the host in a page request or add per-card polling pipelines.
Show user totals/active/distinct-online/expired/exceeded/expiring, active peers, CPU/RAM/disk,
process pressure, host RX/TX, VPN rates/traffic and node/AWG health according to operational value.

Use lightweight server-rendered SVG with shared chart tokens, readable labels/units/legends,
fixed percentage scales and honest time ranges/gaps. Charts have textual summaries and accessible
names; detail must be obtainable without hover. Line styles/labels distinguish RX/TX. Zero, empty,
unavailable, stale and partial data are distinct; refresh preserves reading/focus and geometry.
Do not imply long resource history from traffic rollups or invent new telemetry contracts for
visual symmetry. Disk and health can use compact meters/status instead of decorative line charts.

## Settings and asset gates

Settings uses grouped section navigation, progressive disclosure and one editor per setting;
operations remain in their dedicated screens. Defaults, runtime/next-use effects, destructive
consequences and external host ownership are explained. Save/validation/secret semantics are
specified in [Phase 10](../development/phase10.md#settings-save-decision-100-prerequisite).

| Asset | Initial gzip budget |
|---|---|
| JavaScript total (HTMX + application modules) | ≤ 30 KiB |
| CSS total | ≤ 25 KiB |
| Fonts total | ≤ 150 KiB |
| Typical list-page HTML | ≤ 60 KiB |

`scripts/check-assets.sh` enforces JS/CSS/fonts in CI; rendered HTML is a QA measurement, not yet
an automated CI assertion. Measure SVG, full/fragment HTML, request count and rendering/layout cost
as well. Increase a budget only for a demonstrated UX need with before/after evidence and updated
checks/docs; avoid duplicate assets, eager QR loading and duplicated mobile DOM. Assets remain
prebuilt/embedded with cache-busted URLs and notices; no compilation at deployment time.

No screen is accepted merely because it renders. Milestone checks cover representative desktop/
phone, both locales/themes and changed states; 10.7 owns the full route/state/browser/performance
matrix and relevant live TLS deployment evidence. Unavailable engines/devices remain unverified.

# UI/UX design system

Phase 10 design contract, approved 2026-09-10. Implementation/verification status and requirement
ownership live in [phase10.md](../development/phase10.md). Existing pages adopt this contract in
that order; this document is not a claim that the complete redesign is already verified.

## Architecture and visual direction

Go `html/template` renders the product; HTMX enhances targeted regions and ordinary forms/links
retain meaningful fallback paths. `internal/web` calls existing services directly, never the REST
API. Typed page/view data owns formatting and states; templates own semantic markup. No React,
SPA, Tailwind, heavy framework, chart runtime, QR runtime or production Node.js dependency.

This is a ground-up redesign: legacy layout, hierarchy, markup and interactions are not design
constraints. Preserve proven backend semantics and security while replacing weak frontend
compositions. No page, workflow, dialog or state may remain visually legacy at closure.

Premium means precise hierarchy, spacing, typography, consistency, usability and restraint.
Shadcn-like neutral surfaces carry the product: cool white light and near-black dark, with a
recognizable cyan-to-indigo accent, distinct semantic colors and readable contrast. Apple/iOS-like
polish includes refined depth and responsive feedback. Glass, blur, gradients, shadows and
highlights may support dashboard/status cards, operational forms, overlays, auth/onboarding, public
surfaces and other useful focal points, with readable fallbacks. Effects enhance hierarchy; avoid
neon, excessive glow/blur, decorative motion and continuously moving chrome.

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

Persian uses self-hosted Vazirmatn, OFL, `font-display: swap`, Regular/400 and SemiBold/600 WOFF2.
English uses the native system sans stack (SF/Segoe UI as available), with no additional font
download. Judge weight, rhythm and mixed-script readability in both locales; a future subset
change requires measurement and honest notices. Body/form text
must remain readable on phones; headings, metadata and numeric metrics have distinct hierarchy.

All product copy, including dynamic JavaScript feedback and scope/event descriptions, comes from
`internal/i18n` fa/en catalogs. Static key parity is necessary but dynamic-key/rendered-copy checks
and human terminology review are also required. Resolve language on the server and set `lang` and
`dir`; use CSS logical properties with only necessary physical exceptions. Default locale is fa;
theme defaults to Light. Explicit Light, Dark or System preferences override that default across
app, auth and subscription, including a dark OS with no saved preference.

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
Checkbox glyphs stay compact inside 44px hit areas; preset buttons and calendar days also meet
the touch target. At 320px the calendar uses the available width with seven 44px columns.
Directional navigation icons mirror in RTL; plots and technical values retain their data direction.

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
names. Pointer inspection shows the exact sample at a position; focus plus Arrow/Home/End exposes
the same values to keyboard users, and native detail tables remain the non-hover source of truth.
Line styles/labels distinguish RX/TX. Zero, empty,
unavailable, stale and partial data are distinct; refresh preserves reading/focus and geometry.
Do not imply long resource history from traffic rollups or invent new telemetry contracts for
visual symmetry. Disk and health can use compact meters/status instead of decorative line charts.
Exact ten-second sample values are available on demand in a stable table outside the live swap;
its timestamps include seconds and UTC. Historical traffic offers a separate per-bucket table.
Snapshot reading and focus never trigger extra samplers or automatic table replacement.

## Settings and performance

Settings uses grouped section navigation, progressive disclosure and one editor per setting;
operations remain in their dedicated screens. Defaults, runtime/next-use effects, destructive
consequences and external host ownership are explained. Save/validation/secret semantics are
specified in [Phase 10](../development/phase10.md#settings-save-decision-100-prerequisite).
Its separate lightweight `settings.js` module tracks dirty state without repeated announcements;
keyboard focus clears the sticky Save bar. Native focus/anchor scrolling is immediate so pointer
actions do not race page movement. Session TTL and drift-policy saves require a service restart;
saving does not restart the service. Backup operations link to the single Settings editor.

Administration uses native forms/disclosures and selected-account editors, with human permission/
event labels and unchanged submitted identifiers. Valid family wildcards round-trip; owner roles
retain service protection. Full-form webhook saves reject an empty event selection; rejected URLs
never redisplay userinfo. Audit summaries compact UUIDs while details retain exact copyable values.

Build the strongest premium UX within the lightweight architecture, then optimize unnecessary
cost without degrading it. There are no numerical asset-size targets, review thresholds or
ceilings for JS, CSS, fonts, SVG, full HTML or fragments. `scripts/check-assets.sh` reports raw/gzip
sizes in CI and fails for missing assets, never solely for size. Browser QA measures full/fragment
HTML, request count, loading, layout shift and rendering cost. Investigate unexpected regressions,
duplicate assets/markup, expensive repeated work and polling; defer expensive assets such as QR
until requested. Assets remain prebuilt/embedded with cache-busted URLs and notices; no deployment
compilation or unnecessary dependencies.

No screen is accepted merely because it renders. Milestone checks cover representative desktop/
phone, both locales/themes and changed states; 10.7 owns the full route/state/browser/performance
matrix and relevant live TLS deployment evidence. Unavailable engines/devices remain unverified.

## Operational forms and collections

Auth/setup use a focused access layout; public subscriptions have independent customer navigation
and technical summaries. Error pages/fragments keep their HTTP status, query-selected public
locale and layout boundary. Session expiry returns to a safe full page, including during polling.
QR loads on demand through a shared viewer. Public downloads give retry feedback and preserve
the native file-link fallback. Connection-disabled states do not invent new config-access rules.
Public presentation excludes admin notes and encrypted key carriers; capability URLs stay private.

Interfaces and Plans use shared `form-page`/`form-stack` section cards and `form-panel` disclosures,
with one primary save action. Technical numeric/range fields use text controls with appropriate
input modes so server validation can redisplay exact invalid input. Field errors and a focusable
summary accompany a failed save; fresh secrets are cleared with explicit retry guidance. Profile
generation remains server-owned and sealed; changing a generated value changes its provenance.
Plain mode hides inactive parameter controls when JavaScript is available, with a native fallback.

The interface form presents Standard, Performance, Balanced, Resilient, Suggested and Automatic
profiles as a responsive selector. New forms suggest the first free `awgN`; Suggested is the
initial enhanced profile and exposes its complete advanced set while keeping I1–I5 empty. The user
form uses username as the sole visible identity, supports multiline notes, generates an optional
eight-character word-plus-digits username, keeps quota/duration shortcuts in compact scrollable
rows, and treats a blank device limit as unlimited while creating one ready configuration by
default. A configured default device limit is shown and governs provisioning. User filtering is
live with a native submit fallback, and bulk controls do not appear until at least one visible user
is selected. The public subscription is a standalone connection pass with status, usage, expiry,
device delivery and setup guidance rather than an admin-page derivative.

`collection`/`entity-table` retain one semantic table on desktop and transform its rows to labeled
cards on phones. Name/edit links, availability, exact technical units and action menus have stable
positions. Missing secondary counts/references display unavailable, never an invented zero.
Plans and Interfaces enforce their existing read/write permissions at the server boundary as
well as in navigation and controls; a read-only account can inspect lists without mutation affordances.

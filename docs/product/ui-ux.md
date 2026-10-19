# UI/UX design system

Phase 10 design contract, approved 2026-09-10. Implementation/verification status and requirement
ownership live in [phase10.md](../development/phase10.md). Every inventoried panel, authentication
and public-subscription surface adopts this contract; Phase 10 acceptance is complete.
The post-v0.1.0 appearance extension below adds visual presets without reopening that acceptance.

## Architecture and visual direction

The [refactor program](../development/refactor-program.md) preserves this design
system. The Domains and HTTPS workspace is specified in the
[domain/TLS contract](../operations/domains-and-tls.md#settings--domains-and-https).

Go `html/template` renders the product; HTMX enhances targeted regions and ordinary forms/links
retain meaningful fallback paths. `internal/web` calls existing services directly, never the REST
API. Typed page/view data owns formatting and states; templates own semantic markup. No React,
SPA, Tailwind, heavy framework, chart runtime, QR runtime or production Node.js dependency.

This is a ground-up redesign: legacy layout, hierarchy, markup and interactions are not design
constraints. Preserve proven backend semantics and security while replacing weak frontend
compositions. No page, workflow, dialog or state may remain visually legacy at closure.

Premium means precise hierarchy, spacing, typography, consistency, usability and restraint.
The 2026-10 redesign rebuilt every surface on shadcn/ui component anatomy with a Vercel/Linear
default: compact density, hairline borders, monochrome surfaces, monospace technical values,
underline tabs and little empty space, so important facts read first. Visual presets (below)
restyle the whole product, including layout composition. Effects stay restrained: no neon,
excessive glow/blur, decorative motion or continuously moving chrome. The
[design system](design-system.md) defines files, tokens, components and the preset format.

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

## Visual presets

A preset restyles the whole product, from colors, fonts, radius, shadows and spacing to density
and shell composition; feature pages carry no preset conditions. The catalog holds the built-in
`wg-guard-neutral` ("WG-Guard", Vercel/Geist style), Linear, the official shadcn palettes and
tweakcn imports that name their source page. Status meaning, readable contrast (WCAG AA 4.5:1 for
every text pair in both modes), focus visibility, accessibility and reduced motion outrank source
fidelity. No remote CSS, executable theme code or runtime build tool is used. The
[design system](design-system.md#visual-presets) defines the file format, layout axes, importer
and generated stylesheets.

Appearance has distinct axes: visual preset, Light/Dark/System mode, and independent locale/
direction/Latin technical digits. A preset changes none of the latter. The account's saved preset
overrides the installation default; an unrecognized saved ID falls back safely. The browser theme
cookie is the personal mode override; otherwise the installation mode applies, then Light.
Owner-only panel-default actions require confirmation and never rewrite personal choices.

The public subscription page has its own owner-set default preset and mode (blank follows the
panel default). A visitor may pick another preset from the page; that choice is a browser cookie
(`wg_sub_preset`) honored only for registered IDs, and their explicit mode choice still wins.
Reset my appearance clears the personal preset and mode override; reset panel default restores
the built-in preset, Light, Latin digits and the subscription defaults.

Appearance has keyboard tabs (Theme, Subscription page for the owner only, Numerals and Reset),
and the open tab is kept in the URL fragment; Subscription page and Numerals saves return to
their own tab. Theme shows every preset as a miniature drawn from its own colors, radius, font
and sidebar composition; selecting one previews it live across the whole page (layout axes
included) without saving, and leaving restores the saved appearance. Applying it changes only
the account; the owner's panel default is a separate card in the same tab with an explicit
confirmation. The subscription page default lives in its own tab.

## Layout and navigation

Desktop uses the shadcn Sidebar block: brand, a search button for the command palette, and
permission-aware groups (Manage, System, Preferences) from one Go definition (`View.Nav`) shared
with the palette. Server authorization stays authoritative; unauthorized destinations are absent.
The header supplies the sidebar toggle, page context and language/theme controls. The desktop
sidebar collapses to an icon rail with tooltips and keeps accessible names and current-location
state. The command palette (⌘K / Ctrl+K / `/`) filters the same destinations plus quick actions
and a user search; it is an enhancement, never the only path.

Below 960px the sidebar becomes a modal navigation drawer: close button/Escape/backdrop, contained
focus, inactive background, scroll containment and return to the trigger. Resizing across the
boundary releases mobile state. The drawer has one shape for every preset: the `sidebar`
composition axis (plain, inset, floating) applies above 960px only. The sidebar footer's account
menu opens upward at the trigger's width and holds the account identity, Appearance, the command
palette and sign-out. Do not add bottom navigation without a demonstrated workflow.
Content is a centered fluid column with a per-route cap: create/edit forms (`/new`, `/edit`)
48rem, Settings 64rem and every other workspace 80rem. Keep 320px layouts usable without hiding
data.

Lists transform into intentional cards when columns stop being useful (initial table tier 800px),
preserving labels, selection, action order and semantics. Do not duplicate whole desktop/mobile
DOM trees. Long values wrap or have a deliberate readable/copyable presentation. Compact dialogs
can become sheets on phones; keyboard, safe areas, short landscape heights and touch targets are
part of layout, not afterthoughts. The final viewport/browser matrix is owned by Phase 10.

## Typography, localization and technical data

Persian uses self-hosted variable Vazirmatn (OFL, `font-display: swap`), restricted to the
Arabic Unicode range so mixed text keeps the preset's Latin face for Latin runs. Latin UI and
monospace faces come from the active preset (Geist and Geist Mono by default) and are self-hosted
WOFF2 files loaded only when used, with system fallbacks. Persian text is never letter-spaced.
Judge weight, rhythm and mixed-script readability in both locales. Body/form text
must remain readable on phones; headings, metadata and numeric metrics have distinct hierarchy.

All product copy, including dynamic JavaScript feedback and scope/event descriptions, comes from
`internal/i18n` fa/en catalogs. Static key parity is necessary but dynamic-key/rendered-copy checks
and human terminology review are also required. Resolve language on the server and set `lang` and
`dir`; use CSS logical properties with only necessary physical exceptions. Default locale is fa;
theme defaults to Light. Explicit Light, Dark or System preferences override that default across
app, auth and subscription, including a dark OS with no saved preference.

Technical values (IP/CIDR/ports/keys/identifiers) use LTR isolation, Latin digits and tabular numerals;
copy returns the original value, never formatted/truncated text. Mixed-language names use bidi
isolation. Isolation preserves the characters' reading order; metric cards, table cells and
summaries still align their values to the locale's visual start edge (right in fa). Numeric and
date form controls keep LTR editing order but align at that same RTL edge in Persian. Dates remain
Jalali in fa and Gregorian in en; show the time basis where relevant.
Human counts, amounts, dates and durations use the independent appearance number preference:
Latin by default, optional Persian, with owner defaults and per-account inheritance. Input,
copy and REST values remain canonical. See [presentation primitives](presentation-system.md).
Directional navigation icons mirror appropriately; data plots/time axes do not reverse merely
because surrounding copy is RTL. Scope/permission/event machine identifiers stay stable in forms
and API; localized human labels/descriptions are primary UI text.

## Interaction, states and accessibility

Every component defines applicable default, hover, focus-visible, pressed, selected, disabled,
loading, success, warning, error and destructive states. Hover only enhances interactive targets;
keyboard/touch provide equivalent operations. Controls have accessible names, visible focus and
44px touch targets: on coarse pointers buttons grow to 36-40px and an invisible hit area extends
them to 44px, while text fields, selects, tabs, navigation rows and calendar days are 44px
themselves, so dense desktop layouts stay compact. Use native HTML semantics first; adding ARIA roles requires implementing the
matching keyboard behavior. Focus must not be hidden behind sticky chrome/overlays.
Checkbox glyphs stay compact inside 44px hit areas; preset buttons and calendar days also meet
the touch target. At 320px the calendar uses the available width with seven 44px columns.

Menus support open/focus, arrows/Home/End, Escape and focus return; Tab can leave and dismiss.
Menus, select listboxes, help and choice popovers close on a press outside them.
Dialogs/sheets contain focus and restore it to the invoker, including after successful actions.
They close with Escape, their close/cancel controls and a press that starts and ends on the
backdrop. A dialog whose form holds unsaved input, or whose submission is pending, stays open on a
backdrop press and gives a brief nudge instead, so no typed work is lost to a stray click.
Single-choice `.select` controls render a themed listbox (`select.js`): the native select keeps
the value, validation and submission, options are read when the list opens (scripts may replace
them), `<optgroup>` labels become group headings and `data-native` keeps the browser picker.
There is one active modal interaction; nested controls remain operable. Tooltips never contain
essential instructions or require hover. Calendar keyboard/date-entry behavior belongs to its
form migration; it must support Jalali/Gregorian labels without changing stored date semantics.
Its month controls keep the picker open, the previous control is disabled when all earlier days
are unavailable, and the picker remains clickable within the create-user drawer on narrow screens.

Forms have visible labels, associated hints and field errors, with a focusable error summary.
A failed submission preserves nonsensitive input, reveals invalid Advanced fields, and explains
whether anything changed. Secrets are never redisplayed. Pending actions prevent accidental
repeat submission, announce busy state, and recover after errors/back navigation. Keep the actual
submitter's name/value and server validation authoritative. Success feedback does not require JS
and routine updates avoid flooding live regions. Empty/no-results/unavailable/error are distinct,
with a useful next action. Destructive confirmation names the target, count and consequence.

Motion is short (120–240ms), purposeful, mainly opacity/transform; no idle pulse or decorative
movement. The one exception is the sign-in/setup brand panel: its aurora and orbit art turns
slowly (36–110s loops, transform only) on a surface that holds no controls, and stops under
`prefers-reduced-motion`. Loading animation exists only while loading. `prefers-reduced-motion` disables overlay,
skeleton, chart and other nonessential animation without hiding feedback; it removes
transitions outright (`transition: none`) rather than shortening them, because a near-zero `all`
transition still leaves an inherited `visibility` change hidden for the first frame and loses
synchronous focus into a just-opened drawer or dialog. Both themes meet AA
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

Settings uses grouped section navigation that marks the section being read
([section navigation](presentation-system.md#section-navigation)), progressive disclosure and one
editor per setting; operations remain in their dedicated screens. Defaults, runtime/next-use effects, destructive
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
Administrator and token editors provide observer, operator, full-access and clear presets without
removing granular scope choices. Webhook editors provide select-all and clear actions over the
complete event catalog.
The permission shortcuts classify each current scope explicitly. Read only excludes private
configurations and customer-link capabilities; Operations includes routine purchase, customer,
usage and successor-plan actions but excludes bulk deletion and administrative settings. Full
access selects every currently offered exact scope, never a family wildcard that would extend to
future permissions. The owner token form offers REST scopes only; reseller creation/edit and
reseller token forms use the same shortcut classification within their allowable ceilings.
The user detail exposes Reset Usage as a confirmed visible action when `traffic.update` is
granted, alongside renewal/add-time and Add data allowance. The user list offers those actions from each
row; one pair of shared quick-action dialogs serves the paginated list, while native detail-page
forms remain the no-JavaScript path. On narrow screens, user rows retain the key identity,
status, usage and expiry in a compact card; search stays visible and additional filters collapse.
The successor-plan editor collapses when no plan is queued, and the detail summary uses a two-column
phone layout. API-token and webhook screens explain that bots authenticate with scoped Bearer
tokens and that webhooks are optional notifications.
The panel's Add data allowance raises the finite quota and leaves recorded consumption intact;
its corresponding REST operation is `quota/add`, distinct from the REST `traffic/add` meter
correction. Only `users.update` may top up a
quota, while `traffic.update` controls Reset Usage. Unlimited accounts use Edit to establish a
finite limit first. If the quota increase lifts a traffic-only block, the panel reconciles peers.

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

System health is a read-only `node.read` workspace with independent readiness,
runtime-application, accounting-freshness and telemetry cards. Pending desired state
is retained rather than described as a completed network change. Last attempt,
current activity and process-local counters remain distinct; missing/stale/future
evidence never becomes a healthy zero. The page reuses bounded observations and
the existing DB readiness probe, with no reconciliation, host commands or additional
sampler. Refresh and technical disclosure have native keyboard/no-JavaScript paths.
Related recovery links follow their own permissions; the sidebar/dashboard entry
is hidden without node-wide read permission. TLS/client traffic acceptance remains
separate from a responding panel or ready node.

Backups uses four server-navigated sections: Archives, Verify & restore, Schedules
and Delivery. Its archive collection has bounded cursor pages and explicit page-size
selection. Verification is a read-only outcome with no restore confirmation;
preparing a restore produces a resumable private report. Source inventory/provenance,
enabled owner access, collapsed technical details and the target boot/TLS boundary
precede the explicit approval. The pending state remains awaiting managed restart,
and cancellation targets that exact request. Saved/incomplete reviews have bounded
management and explicit discard. Report views suppress the initial import/selection
forms to avoid duplicated actions. Shared workspace tabs, semantic badges/receipts,
question-mark guidance and responsive cards retain native/no-JavaScript operation,
fa/en parity, number preferences and isolated Latin technical values.

Auth/setup use a focused access layout. From 1024px, sign-in and first-run setup split into the
form pane and a brand panel (product promise, three capabilities, decorative orbit art tinted by
the preset's primary and chart colors); phones and tablets keep one centered column over a quiet
glow. Fields carry leading icons, the password reveal sits beside the value in both directions,
Caps Lock is announced under the password and a pending sign-in shows progress on its button.
Error pages keep the plain centered column. Public subscriptions have independent customer navigation
and technical summaries. Error pages/fragments keep their HTTP status, query-selected public
locale and layout boundary. Session expiry returns to a safe full page, including during polling.
QR loads on demand through a shared viewer. Public downloads give retry feedback and preserve
the native file-link fallback. Connection-disabled states do not invent new config-access rules.
Public presentation excludes admin notes and encrypted key carriers; capability URLs stay private.
The owner picks one of four installation-wide designs under **Appearance → Subscription page**,
next to the page's own theme and mode: **Ticket** (the default: a boarding pass with a tear-off
status stub), **Membership card** (a wallet-style card in the theme's primary color beside the
usage tiles), **Minimal** (text-first, no frames) and **App** (usage rings; on phones the section
tabs become a floating bottom bar). A design changes only the hero; the body below it is shared,
with the same status, QR and config behavior. **Preview** opens the owner-only
`/appearance/subscription/preview`, which renders the form's unsaved design, theme and mode with
fictional sample data, reads no real user or device and leaves every action inert. Migration
`0019_subscription_layouts.sql` maps the retired layouts (pass and split to ticket, compact to
minimal). See the [design system](design-system.md#subscription-page-designs) for adding one.
Public navigation does not cover scrolling content. The public theme menu stays next to its
trigger and inside the viewport in both directions; changing Light/Dark/System remains a visitor preference. Shared
buttons use restrained elevation, with color, border and focus state carrying the interaction
instead of a large ambient shadow or persistent touch-hover glow.
Config downloads use a short, stable device-specific `.conf` filename and browser-safe binary
attachment type; the REST text representation remains unchanged.
Usage and transfer metrics keep Latin numbers separate from localized units, so RTL never reverses
their meaning. The all-device QR view contains exactly one image per actual device; hidden
single-device controls must stay hidden even when component layout styles change.

Interfaces and Templates compose shared Card sections, Fields and Collapsible disclosures
(Interfaces adds `.fieldset` groups and `.choice-card` profile selectors; see the
[design system](design-system.md#components)), with one primary save action. Technical
numeric/range fields use text controls with appropriate input modes so server validation can redisplay exact invalid input. Field errors and a focusable
summary accompany a failed save; fresh secrets are cleared with explicit retry guidance. Profile
generation remains server-owned and sealed; changing a generated value changes its provenance.
Plain mode hides inactive parameter controls when JavaScript is available, with a native fallback.

The interface form presents Standard, Performance, Balanced, Resilient, Suggested and Automatic
profiles as a responsive selector. New forms suggest the first free `awgN`; Suggested is the
initial enhanced profile and exposes its complete advanced set while keeping I1–I5 empty. The user
form uses username as the sole visible identity, supports multiline notes, generates an optional
eight-character word-plus-digits username, keeps quota/duration shortcuts in compact preset
dropdowns, and treats a blank device limit as unlimited while creating one ready configuration by
default. A configured default device limit is shown and governs provisioning. User filtering is
live with a native submit fallback, and bulk controls do not appear until at least one visible user
is selected. Panel quotas and traffic displays use decimal SI MB/GB (1 MB = 1,000,000 B;
1 GB = 1,000,000,000 B), while the public API transports exact integer bytes. Panel speed
controls use decimal MB/s and convert exactly at the view boundary (1 MB/s = 8,000 Kbps),
while the domain and public REST contract remain Kbps (1 Kbps = 1,000 bits/s).
User-row subscription copy has a non-Clipboard-API
fallback; QR and configuration downloads are separate actions. The create drawer owns its header,
scrolling body and footer as one viewport-bounded grid, while the full-page form remains the native
fallback. The user-detail subscription card stays in document flow. The public subscription is a
standalone customer page with status, usage, expiry,
device delivery and setup guidance rather than an admin-page derivative.
Every subscription design shows the following:

- a status notice only when there is something to say: not started yet, running low, or
  unusable (with the next step). A ready, healthy subscription needs no banner;
- a **running-low** warning while the subscription is usable but has used at least 90% of its
  quota or has three or fewer days left (`data_low`, `time_low`, `both_low`). It only informs;
  enforcement stays in accounting;
- three sections as CSS radio tabs: **Connect** (device cards), **Setup guide** and **Details**
  (data and time breakdown, plan and status). They work without JavaScript, `#connect`, `#guide`
  and `#details` open a section, the tab row sticks under the header on phones, and without
  `:has()` support every section is shown;
- in the guide, platform tabs (Android, iPhone/iPad, Windows, macOS, Linux; JavaScript
  pre-selects the visitor's OS) and one accordion item per app. The first app is open and opening
  another closes it. App order and official links are defined in
  [client apps](../integrations/client-apps-and-warp.md#client-apps-on-the-subscription-page);
- command-line steps (amneziawg-tools) as LTR copy fields for the config path and the
  `awg-quick up` command; the copy button appears with JavaScript, falls back to selecting the
  text, and announces the result in one polite status region;
- a short **Common questions** accordion (no internet, one configuration per device, keeping the
  link private, re-importing after a change);
- for each device, its usage ("2 GB used") and, while it is enabled but offline, "Last connected
  3h ago" or "Not connected yet", then its IPv4;
- for each device, **Download config**, **QR code**, **Copy key** and **Open in AmneziaVPN**,
  at least 36px under every density, in a two-column grid: Download spans the row,
  QR code and Copy key share one, and Open in AmneziaVPN takes its own, so nothing clips
  horizontally on phones. Open in AmneziaVPN follows the platform: the `vpn://` link on Android,
  the share sheet with a `.vpn` file on iPhone and iPad (a second tap if Safari drops the first),
  and a `.vpn` download with import instructions on desktop. Copy key hands the pending text to
  the clipboard inside the tap, which Safari requires.

Meter widths and ring angles come from fixed classes (`w0…w100`), not inline styles, so the
strict CSP holds.

**Routing** (`/routing/*`) has three tabbed pages, each doing one job:

- **Outbounds**: kind picker, then a form for that kind. Cards show health, latency, when it was
  last checked, exit IP with a country flag, for WARP the WARP state and Cloudflare data centre,
  the outbound a chained one goes through, traffic and everything that uses it (full list on
  hover). An outbound in use offers no Delete. WARP cards have **New WARP identity** behind a
  confirmation; its toast reports the old and new exit IP. The form has **Route the tunnel
  through** for WARP and WireGuard outbounds (it explains that this is how WARP gets another
  country), suggests WARP endpoints and has the **Open websites over IPv6** switch. The switch is
  disabled with an explanation for a config without an IPv6 address. Cards of bridged outbounds
  show the IPv6 exit, the bridge state and the bridged IPv6/IPv4 connection counts. Below the
  cards, **Site test** picks a known service or a URL (the URL field appears only for a custom
  site) and shows a result table for this server and every enabled outbound: verdict badge,
  reported region, HTTP status, address family, time and final host. The table becomes cards on
  phones; HTMX swaps only the results.
- **Balancers**: radio cards for the strategies. A warning appears when a per-connection
  strategy is chosen. Members show a weight, or a Priority for failover.
- **Rules**: an ordered list with up/down moves (the page returns to the moved rule), a
  **Going direct** badge on rules whose outbound is not usable, the default route, a site check
  (linking to the site test with the name filled in) and geodata management. Encrypted DNS is its own card with a live state (Active, Waiting for
  a domain rule, Off), visible help and the strict Private DNS warning. The rule form groups
  presets by category as multi-select chips. It has a matcher textarea with syntax help and
  geosite suggestions. Target selects list direct and block, then outbounds and balancers in
  option groups.
- Failure flashes (refused delete, apply pending, geodata errors) render as error alerts.

Technical values (matchers, IPs, endpoints, marks) are isolated as LTR in both locales.

The create drawer keeps every section at natural height and scrolls its body independently;
focused controls must stay visible in short desktop and phone viewports, including WebKit. User
device menus separate single/all-device QR viewing from single/all-device config downloads.
Copy confirmation appears beside its trigger, while general status toasts sit near the top with
distinct success/error treatments. Tooltips render above clipping containers and remain available
on keyboard focus. Subscription “revoke” explicitly replaces link and device credentials after a
consequence confirmation; no duplicate regenerate/restore control appears in the product UI.
Audit uses compact event summaries with progressive details and copyable full identifiers.

Dashboard recovery shortcuts create/download a fresh archive or open the restore workspace. The
Backups workspace separates create, streamed `.wgg` import and validated restore review; importing
from another node never mutates live data. Pointer activation must not trigger keyboard-only focus
scroll corrections on Settings, profile selectors, menus or dialogs.

The dashboard node card distinguishes installed/observed software identities from available
versions and sends authorized operators to a dedicated Update Center. Published stable panel
releases and reviewed AmneziaWG bundles remain separate, with an explicit confirmation for every
upgrade, downgrade or reinstall. An active operation has one concise polite status region; polling
pauses in hidden tabs, returns no replacement for unchanged state and refreshes the catalog once
the lifecycle operation finishes. Catalog failure, unavailable host bridge, queued, running,
completed and failed states never imply an update was applied. Development commits and free-form
version input are absent from the web workflow.

The v0.1.7 [maintenance workspace](../operations/update-center.md) extends this boundary with
Overview/Versions/Operations/Recovery navigation, separate panel/manager/tools/bundle identity,
real release-page navigation and escaped release details. Mobile uses a native collapsible release
browser; desktop keeps the selection alongside the list. Shared workspace/action tokens remain
preset-aware; primary buttons use semantic solid fills unless a registered preset supplies its
own treatment. `update.read` grants observation without execution. Readiness, reserved UTC windows,
stage revisions, safe history/reports and referenced lifecycle archives preserve the existing
host broker and lifecycle engine; no arbitrary command or automatic reboot UI is introduced.

Collections use `.table--cards`: one semantic table on desktop whose rows become labeled cards
below 800px. Name/edit links, availability, exact technical units and action menus have stable
positions. Missing secondary counts/references display unavailable, never an invented zero.
Templates and Interfaces enforce their existing read/write permissions at the server boundary as
well as in navigation and controls; a read-only account can inspect lists without mutation affordances.

The cleanup workspace reuses the same semantic cards, fields, table, confirmation dialog and
shared calendar; historical dates are enabled only on cleanup triggers. It separates account
deletion, chart history and database-space maintenance. Account previews show exact impact,
expire after ten minutes and refuse changed selections. `cleanup.manage` is an explicit panel
permission; compaction remains owner-only. Interface forms show primary/overflow pools and
registered free/total counts without inferring simultaneous connection capacity.

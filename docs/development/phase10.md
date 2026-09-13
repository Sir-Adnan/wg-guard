# Phase 10 — Product UI/UX redesign

Status: **complete (2026-09-13)**. The owner approved
this order on 2026-09-10, with the owner's 2026-09-12 continuation clarifications taking precedence.
The complete requirement input is the tracked
[Phase 10 prompt](../prompt/PropmtForPhase10.md); this document owns execution/evidence and
[UI/UX](../product/ui-ux.md) owns the lasting design contract. Phase 11 has not started and no
public release is authorized.

## Objective and boundaries

Deliver shadcn-like precision, Apple/iOS-like polish and restrained modern minimalism across
every panel and public surface. Preserve Go + SQLite + SSR/HTMX/minimal vanilla JavaScript,
existing business services, bilingual fa/en, RTL/LTR, and English-only host tools. No React, SPA,
Tailwind, heavy frontend framework or production Node runtime. Build-time tooling needs a concrete
justification. Visual migration does not justify API changes or new product features.

## Milestones

Accessibility, localization, responsive layout and interaction states apply to **every** milestone;
10.6 consolidates them rather than introducing them. Coherent commits must build and pass
`make test`; avoid micro-commits and repeated full QA.

| Milestone | Deliverable and exit boundary |
|---|---|
| **10.0 — Inventory and design contract** | Complete coverage below; settle Settings save/validation; define components, charts, navigation, responsive tiers and QA. Implement only a necessary bounded Settings prerequisite. |
| **10.1 — Foundation and application shell** | Tokens, white/near-black/system themes, typography, Lucide, shared primitives/feedback, accessible overlays, permission-aware shell, common auth/public/error foundations. Verify representative list/form/public compositions before broad migration. |
| **10.2 — Interfaces and plans** | Lists/create/edit/actions, complete advanced AWG/profile-preview presentation, lossless semantics and client re-import consequences; preserve pinned service behavior. |
| **10.3 — Primary operations and dashboard** | Users/create/edit/bulk/detail, devices, config/QR, admin subscription lifecycle; then dashboard and charts using existing telemetry/rollups. |
| **10.4 — Settings and administration** | Grouped Settings, backups/restore/schedules/Telegram, admins, tokens, webhooks/deliveries, audit; human scope/permission/event labels and descriptions. |
| **10.5 — Public and authentication surfaces** | Complete login/onboarding/auth states, public subscription/download experience, page and fragment errors. Foundations originate in 10.1. |
| **10.6 — Product consistency** | Cross-page copy, state, responsive, keyboard/touch, accessibility and RTL/LTR audit; remove legacy patterns and duplicate assets. |
| **10.7 — Final acceptance** | Full browser/state/performance matrix, relevant TLS/VPS workflow checks, docs/CI, coherent integration into main and exact-revision report. No public release. |

Interfaces/plans precede consuming user workflows. Settings IA is agreed in 10.0, with final UX
after those workflows expose defaults and consequences. A redesigned control does not close a
Phase 11 lifecycle finding.

## Complete requirement mapping

| Prompt requirement family | Implementation owner | Acceptance |
|---|---|---|
| Complete redesign, no legacy page/subpage/state | 10.1–10.5 | Inventory, 10.6/10.7 |
| Hierarchy, spacing, alignment, density, typography, radii, borders | 10.1 | Component contract and representative compositions |
| White/zinc light, near-black dark, no purple identity, system preference | 10.1 | Four locale/theme combinations plus system switching |
| Restrained glass/blur/gradients/shadows/highlights/elevation | 10.1 | Selective hierarchy, readable opaque fallback, no decorative movement |
| Cards/forms/buttons/tables/toolbars/sheets/dialogs/dropdowns/popovers | 10.1; compositions 10.2–10.5 | Semantics, focus, touch, clipping, swap lifecycle |
| Tabs/badges/alerts/toasts/skeletons/empty states/confirmations | 10.1; adoption 10.2–10.5 | Shared state vocabulary and actionable feedback |
| Hover/focus/pressed/selected/disabled/loading/success/warning/error/destructive | 10.1–10.5 | Applicable states; keyboard/touch equivalents |
| Fast purposeful motion/reduced motion, no idle decoration | 10.1 | Overlay/loading/chart transitions and reduced-motion checks |
| Consistent high-quality SVG/Lucide, no raster UI icons | 10.1 | Embedded sprite and notices when changed |
| CPU/RAM/host RX-TX/VPN/active users-peers/disk/node-AWG health | 10.3 | Source/time/unit/availability and accessible chart system |
| Dashboard backup/restore shortcuts and verified panel/core version transitions | 10.3–10.4 owner continuation | Existing backup review plus catalog-only host lifecycle bridge; no arbitrary version/command input |
| Intentional desktop/mobile, table/cards, dialog/sheet, max widths | 10.1–10.5 | 320 through ultrawide without viewport overflow or inaccessible data |
| Settings usefulness/defaults/grouping/duplication/help/Advanced/consequences | 10.0 contract; 10.4 UX | Atomic submitted-save boundary; one primary editor per setting |
| Human scope/permission/webhook-event wording, stable identifiers | 10.4 | fa/en coverage and unchanged submitted identifiers |
| Technical IP/CIDR/port/key/traffic/time/status readability | 10.1–10.5 | Bidi isolation, Latin digits, tabular figures, faithful copy |
| Full fa/en copy, RTL/LTR, accessibility, keyboard/touch | 10.1–10.5; audit 10.6 | Dynamic-copy coverage and functional checks |
| Premium UX with observable, efficient assets; no numerical size ceilings | 10.1–10.7 | Measured JS/CSS/SVG/fonts/HTML, loading/rendering cost; eliminate waste without weakening UX |
| Targeted tests, one final full matrix, preserve backend | All | Changed-risk tests; no repeat of unrelated successful VPS drills |
| API/OpenAPI/examples/tests only for public contract changes | Affected milestone | Contract-diff review; visual wording alone changes no API |
| Concise docs, README/ROADMAP/status/architecture as affected | All; final 10.7 | No scratch/report/plan files; honest verification levels |
| Green CI, coherent commits, merge/push/main verification/branch cleanup | 10.7 | Exact merged revision; release needs separate owner approval |
| Final report; no Phase 11 before Phase 10 closure | 10.7 | Coverage, architecture, QA, assets, VPS, remaining limits |

## Route, workflow and state inventory

Source: `internal/web/web.go`, templates and handler error paths. Method variants/fragments belong
to their parent workflow. Machine responses (`/api/v1`, health/metrics), API documentation at
`/docs`, and host terminal management retain their own contracts.

| Surface | Routes, subflows and patterns | Owner |
|---|---|---|
| Chrome | App/auth/sub layouts, `/prefs/locale`, `/logout`, theme, context, permission-aware nav, menu/drawer, flash/confirm | 10.1 |
| Interfaces | `/interfaces`, `/new`, `/{id}/edit`, enable/disable/delete, `/profile-preview`; basic/advanced/provenance/re-import warning | 10.2 |
| Plans | `/plans`, `/new`, `/{id}/edit`, enable/disable/delete; quota/duration/rate controls and interface references | 10.2 |
| Users | `/users`, `/new`, `/{id}`, `/{id}/edit`, `/bulk`, `/bulk-action`; filters/sort/cursor, selection, drawer/fallback, enable/disable/delete/restore/renew/add/reset traffic | 10.3 |
| Devices/admin subscription | User device creation; `/devices/{id}` enable/disable/regenerate/delete/config/qr; user `/sub` create/regenerate/revoke/restore, share/copy | 10.3 |
| Dashboard/software | `/`, `/dashboard`, `/dashboard/live`, `/dashboard/chart`, `/updates`, `/updates/status`, `/updates/request`; counters, attention, live resources/health, rollups, recovery shortcuts and verified software transitions | 10.3–10.4 |
| Settings | GET/POST `/settings`; section links, field errors, secret set/replace/clear, saved/retry/dirty states | 10.4 |
| Backups | `/backups`, create/delete/download/import, restore preview/confirm/cancel, schedules create/update/delete/toggle, Telegram test, pending-restart banner | 10.4 |
| Admins/tokens | `/admins` create/password/permissions/enable/delete, owner protection; `/tokens` create/revoke, show-once secret/scopes/CIDR/expiry | 10.4 |
| Webhooks/audit | `/webhooks` create/detail/update/rotate/delete/redeliver, delivery history; `/audit` filters/cursor/metadata | 10.4 |
| Auth | `/login`, `/onboarding` GET/POST, invalid credentials/input, throttle, expired session, owner already provisioned, locale/theme | 10.5 |
| Public subscription | `/sub/{token}`, device QR/config; status/traffic/expiry, empty, revoked/unknown token, disabled/unavailable device, rate-limit/download failure | 10.5 |
| Errors | Unknown route, invalid request, permission denial, CSRF/session failure, service failure; page versus HTMX fragment | Foundation 10.1; adoption 10.5 |

Each surface covers applicable normal/populated, first-use empty, filtered empty, loading,
validation/transport/server error, success, unavailable/stale/partial data, disabled,
permission-limited and destructive states. Preserve nonsensitive input on errors; never re-render
secrets. Bulk actions identify count/selection scope/partial outcomes; cursor links retain filters.
Do not invent loading states for synchronously available content.

## Settings save decision (10.0 prerequisite)

One submitted Save is all-or-nothing for submitted registry overrides, including secrets. Parse
and validate supplied fields, prepare encrypted values, commit one SQLite transaction, then
invalidate cached values. Failed parse/validation/encryption/write changes no override; success
follows commit. Persistence atomicity does not promise a multi-read runtime snapshot or host apply.

Preserve existing semantics: absent keeps; blank numeric keeps; blank text uses that key's
reset/default behavior; blank secret keeps and explicit clear wins over replacement. Errors keep
exact nonsensitive input and saved metadata/options; secrets stay blank with actual saved presence
indicated. Single-setting CLI/API contracts and validators remain unchanged. Final section forms,
dirty navigation and impact explanations belong to 10.4, after this bounded prerequisite.

Settings groups: identity/status; user defaults/presets; network/client defaults; subscription/
downloads; accounting/retention; access/API/session; backup integration. Use internal section links
and one primary editor per setting. Backup/restore remain operational destinations linking to
settings. Advanced controls stay reachable and explained; defaults/validation are not changed for
visual reasons.

## 10.1 execution boundary

1. Rework `web/static/css/app.css` foundations and shell with semantic surfaces, width/density,
   focus and reduced-motion tokens. Reuse `partial_*.html` for repeated markup, with no generic
   component framework. Preserve page hooks until their migration.
2. Extract shared behaviors from `web/static/js/app.js` to a small same-origin module: menus,
   dialogs/drawer focus/inert, theme/feedback, pending-submit recovery, delegation/swap cleanup.
   Business-specific forms remain separate; no public API dependency is added.
3. App/auth/sub layouts share head/preferences/feedback and page context; permission-aware nav
   and error foundations retain HTTP/session/CSRF behavior.
4. Behavioral changes use failing tests first. Scoped web/i18n tests and local browser checks
   cover shell plus representative list/form/public compositions. Page-specific calendar/chart
   redesign and full-page migrations are not claimed in 10.1.

## Verification and completion

During implementation run changed-risk handler/template/form/permission/i18n and interaction
checks. At coherent commits build and `make test` pass. At 10.7 run build/unit/vet and relevant
CI/race gates, not Phase 11 soak/load/network certification.

Final widths: **320, 360, 390, 430, 768, 1024, 1280, 1440, 1920, 2560, 3440** CSS pixels, plus
breakpoint edges, short-height/landscape and zoom. Cross fa/en × light/dark × applicable states;
check system preference separately. Cover Chromium, Firefox and WebKit/Safari; Android Chrome and
iOS Safari on real devices when available. Record engine/version and emulated/physical coverage
honestly. Check geometry and keyboard/touch as well as visual composition.

Run the full matrix once at 10.7; after corrections rerun affected cells. Relevant TLS/VPS checks
cover UI workflows, metrics, QR/config/download and backup/permissions; unchanged successful
network/lifecycle drills remain accepted. Request VPS credentials only at that gate. Never capture
credentials, subscription capabilities, raw configs or show-once secrets in evidence.

RB-006 closes only when every inventory surface uses the new system, critical workflows pass
locale/theme/input/viewport checks, no raw key/legacy page/viewport overflow remains, accessibility
and measured asset/HTML/performance gates pass, and docs/CI agree with the exact revision. Update
existing docs, CHANGELOG/README/notices as affected, without redundant reports. API/OpenAPI needs
an actual public contract change. Phase 11/12 remain separate; redesigned controls alone do not
certify backend lifecycle findings. Final integration includes coherent commits, merge/push/main verification and
temporary branch cleanup; public publication always requires explicit owner approval.

## Current evidence

10.0: requirement/route/state ownership and Settings save contracts are adopted. Submitted
settings validate and commit atomically; SQLite rollback, safe redisplay and scoped race/browser
checks passed. No public API schema or validation rule changed.

10.1 (`33d0101`): shared shell, tokens, preferences, feedback, overlays and submission lifecycle.
Scoped web/i18n, Chrome fa/en × light/dark phone/desktop and WSL build/unit/vet checks passed.

10.2 (`cf0d145`): responsive Interfaces/Plans collections and forms; lossless validation/profile
editing, secret-free views and existing permission boundaries. Chrome 48 composition cells,
mutations and native no-JS profile transitions passed, as did WSL build/unit/vet. Maximum checked
HTML was 5,629 B gzip. No REST/OpenAPI change or VPS drill.

10.3: Users/create/edit/bulk/detail, device/config/QR/subscription workflows and dashboard are
migrated. Safe retry forms, permission boundaries, unavailable references and exact sample/chart
data have targeted coverage. Chrome 152.0.7977.84 passed 64 fa/en/theme/390–1440 cells, keyboard/
QR workflows and a separate actual no-JS device retry; inspected visuals and scoped review fixes
are complete. WSL build/unit/vet passed. Measurements: JS 29,305 B, CSS 14,588 B, fonts 101,750 B,
SVG 3,095 B gzip; maximum checked HTML 6,803 B. Light default, native English typography and
measurement-only asset tooling implement the owner continuation decisions. REST/OpenAPI unchanged.

Full Firefox/WebKit, viewport/state, physical-device and relevant TLS/VPS acceptance remain 10.7.
10.4: all 34 Settings editors, seven groups/defaults/effects/dirty saves; native backup/schedule/
restore review; redesigned admins, tokens, webhooks/deliveries and audit. Scoped tests cover failed
reads, exact retries, paused schedules, family wildcards, webhook empty selections and credential
redaction. Chrome 152.0.7977.84 passed 80 locale/theme/390–1440 cells, native schedule edits,
Settings keyboard/save and credential lifecycle checks. Focused review fixes and WSL build/unit/vet
passed. Gzip: JS 29,434 B, CSS 16,348 B, fonts 101,750 B, SVG 3,095 B; maximum HTML 8,352 B.
Native scrolling now settles immediately; Settings focus clears its sticky Save bar. No REST change.

10.5: redesigned login/onboarding, public subscription and status-preserving page/fragment errors.
Credentials clear on validation; safe login return survives locale/session changes and HTMX expiry;
unknown public paths remain outside admin chrome. Lazy shared QR and public download recovery have
scoped tests. Chrome 152.0.7977.84 passed 56 locale/theme/phone/desktop cells, onboarding/login and
download/QR checks plus admin QR smoke; focused fixes and WSL build/unit/vet passed. Gzip: JS
30,580 B, CSS 17,649 B, fonts 101,750 B, SVG 3,095 B; maximum public/auth HTML 2,844 B.
QR error tests assert absence of config/secrets directly rather than limiting HTML size.
10.6: removed orphan presentation code, corrected role/count/unit copy, contrast/control boundaries,
RTL icons/tooltips and touch targets. All template icon references resolve. Scoped web/i18n and
WSL build/unit/vet passed; Chrome/axe 4.13.0 covered representative normal/auth/public and 59 state
compositions in both design directions, plus eight touch cells/1,232 targets, theme persistence,
reduced motion and equivalent-zoom reflow. Fixed state/hover findings were rerun only where affected.
Gzip: JS 29,912 B, CSS 17,193 B, fonts 101,750 B, SVG 3,137 B. Full cross-browser acceptance remains open.
These scoped milestones do not close RB-006 or start Phase 11; no public release is authorized.

10.7: Chromium 152.0.7977.84, Firefox 153.0 and WebKit 26.5 cover all 24 app
routes, seven auth/public surfaces and 59 state compositions across fa/en, light/dark and the full
viewport set (5,760 composition cells per engine). Supplemental checks cover keyboard/touch,
short landscape, equivalent 200% reflow, reduced motion, saved Light/Dark/System behavior and
native fallbacks. Findings in focus exposure/return, QR retry, responsive cards/restore notices,
no-script startup and chart-label readability were fixed and rerun only where affected. The final
measurement is JS 30,699 B, CSS 17,835 B, fonts 101,750 B and SVG 3,137 B gzip; maximum checked HTML
is 10,794 B gzip, measured fragments are 1,738–6,292 B gzip and checked pages use at most 11
requests. The delayed-font cold-page probe observed at most 0.0201 CLS after removing the startup
sidebar shift. WSL build/unit/race/vet, formatting, module verification and `govulncheck` with Go
1.26.6 pass. REST/OpenAPI remains unchanged.

The exact code candidate `32fdbe1` then passed a fresh Ubuntu 24.04 amd64 Docker install with direct
ACME HTTPS. Live Chrome covered secure login, Light default plus Dark/System, fa/en and RTL/LTR,
18 app/auth/public routes at 1440/390 px, Interface/Plan/User validation and creation, device and
bulk flows, subscription revoke/restore, atomic Settings save, backup/schedule, healthy Dashboard,
and zero page errors or HTTP 5xx. The served QR independently decoded to the downloaded config;
Doctor passed interface/backend, nftables, Docker forwarding, backup and access checks. Credentials,
raw configs and QR pixels were not retained, and complete owned cleanup restored the dedicated VPS.
The final documentation/integration revision passed the repository commit gate and exact-revision
main CI. RB-006 is closed. Physical-device runs were unavailable and are not claimed; the complete
emulated engine/viewport/touch coverage above is retained as the acceptance evidence.

Owner-directed refinement (`d0ba6f6`, `8cf4e13`): Interfaces, Plans, Users/create/edit, Dashboard
and the public subscription surface received a more expressive premium composition while retaining
the shared contract. New interfaces suggest the first free `awgN` and offer Performance, Balanced,
Resilient, Suggested and Automatic advanced profiles. Generated advanced fields and safety flags
are populated, I1–I5 remain opt-in, every profile receives a fresh HPK, Suggested uses MTU 1280,
and the default client DNS is `1.1.1.1, 1.0.0.1`. User search is live, zero-selection bulk chrome is
hidden, generated usernames are exactly five letters plus three digits, blank device limit means
unlimited/one initial config, and technical charts expose exact samples to pointer, touch and keyboard.
The interface preset enum is the only additive REST/OpenAPI change.

Exact Ubuntu 24.04 amd64 Docker clients passed Performance, Balanced, Resilient, Suggested and
Automatic runtime/config parity, handshake, gateway, public IPv4, DNS, HTTPS and NAT. That gate
corrected Performance padding from `5-35` to verified `10-35`. It also exposed AUD-050; `150a574`
now removes runtime state while the disabled ownership row remains, restores retry state after a
failed reconcile, and deletes the row only after success. Full local unit/build/vet, scoped race,
Chrome/WebKit affected-route and exact Docker API create/delete/cleanup gates pass. Current assets
remain measured without ceilings (JS 99,356/32,338 B, CSS 123,955/24,204 B, fonts 101,716/101,750 B,
SVG 12,669/3,225 B raw/gzip). The later refreshed three-engine gate supersedes the transient local
Firefox development-bundle failure. No public release was made and Phase 11 remains unstarted.

The subsequent owner refinement rebuilds the dense Users/User Detail and administration workspaces,
fixes viewport-bounded create drawers, copy/password action geometry, QR versus config actions,
theme/menu and pointer-scroll behavior, and presents panel speed limits in MB/s without changing the
Kbps API/domain contract. Dashboard recovery can create/download a fresh archive; Backups can stream
an imported `.wgg` from another node into the existing validate/review/approve workflow. Permission
and event presets accelerate administration while retaining granular choices. This is a Web Panel
and panel-only backup-route change; REST/OpenAPI remains unchanged. Full Go build/unit/vet and the
affected Chromium/WebKit foundation, route, interaction and consistency gates pass; the refreshed
three-engine 10.7 matrix below closes the transient browser limitation. Current raw/gzip observations
are JS 102,357/33,111 B, CSS 141,679/26,740 B, fonts 101,716/101,750 B and SVG 12,669/3,225 B.

The dashboard node card now exposes a dedicated Update Center for administrators with
`update.manage`. It separates the observed panel/AWG identities, published stable panel releases
and the complete reviewed core-bundle catalog. Each transition requires a product confirmation;
active status refreshes without repeating unchanged live-region output. The web process can write
only a schema-checked catalog identity into a private fixed queue. A host-owned systemd path/oneshot
then converts it to bounded existing lifecycle arguments, retaining backup, compatibility, health
and rollback checks. Development commits, arbitrary argv/paths and internal errors never cross
this panel boundary. Install/uninstall own the bridge for Docker and native deployments, and
`update-broker-install` repairs it under the lifecycle lock. These are session-authenticated panel
routes, so REST/OpenAPI remains unchanged.

The refreshed local 10.7 gate covers 25 app routes (1,600 locale/theme/viewport cells), 61 state
compositions (3,904 cells), 448 auth/public cells, foundation behavior and eight supplemental
touch/orientation cells with 1,248 effective targets in Chromium 152, Firefox 153 and WebKit 26.5.
After findings, only affected cells were rerun. Axe 4.13.0 passes 240 representative routes and
122 representative states. The fixes include AA light-theme secondary/semantic colors, 44px user
preset targets, WebKit keyboard-focus exposure, Firefox 320px user-status wrapping and Persian-font
preload; a delayed-font probe now observes 0.00025 CLS. Current measured raw/gzip assets are JS
103,005/33,291 B, CSS 146,114/27,224 B, fonts 101,716/101,750 B and SVG 12,669/3,225 B; no size
ceiling applies. Build/unit/race/vet, CI fixtures, systemd unit verification and `govulncheck` on
Go 1.27 pass.

Exact revision `d9eb18e86e7a71c60be87fdcfb68d2feb8d789d0` passed a fresh private Ubuntu
24.04.4 amd64 Docker installation. Host and container build identities matched; container health,
the authenticated `/updates` boundary, root-owned systemd units, 0600 broker marker and
`systemd-analyze verify` passed. A real queue request for the already-installed reviewed
`awg-2026-09` bundle completed through the host oneshot with no request/running residue and a
healthy panel. Broker repair was idempotent; dry-run removal included every broker artifact and
complete owned removal restored the VPS to its initial clean state. Credentials and raw product
data were not retained. The continuation was fast-forwarded into `main`; Phase 11 remains
unstarted and no public release was created.

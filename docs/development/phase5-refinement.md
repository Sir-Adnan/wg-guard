# Phase 5 refinement — roadmap & progress

This is the working checklist for the Phase 5 UI/UX refinement pass (2026-08-31). It is
maintained as work progresses; when the pass completes it is frozen and its outcome is
recorded in [status.md](status.md). Scope guard: everything here stays inside the Phase 5
surface area (no Phase 6 settings/admin/token/backup screens; presets get a storage home the
Phase 6 settings UI can bind to).

## Design direction

- shadcn/ui-inspired neutral palettes (zinc-based light, near-black refined dark), one
  distinctive WG-Guard accent family, subtle borders/shadows, visible focus rings.
- Both directions first-class: desktop is designed, mobile is designed (not adapted).
- No new runtime dependencies: hand-written CSS/JS, existing vendored HTMX + Lucide sprite.
- Asset budgets stay enforced by `scripts/check-assets.sh` (raised only with justification).

## Stages

### Stage 1 — Input correctness (quota/duration precision) — **done 2026-08-31**
- [x] Exact decimal quota parsing (no float64 rounding): `0.2 GB` → 200000000 bytes.
- [x] Unit-aware quota input (GB / MB) server-verified; small values first-class
      (100 MB, 6-hour accounts).
- [x] Fix `View.GB` display rounding (edit forms showed `0` for 0.2 GB).
- [x] Duration input model: value + unit (hours / days / months) instead of fractional days.
- [x] Exact expiry date on create (server-stored, not a derived duration).

### Stage 2 — Subscription links — **done 2026-08-31**
- [x] Per-user subscription token: 256-bit crypto/rand, SHA-256-hashed at rest.
- [x] Public unauthenticated page `/sub/{token}`: traffic used/limit, status, expiry,
      duration, devices with per-device QR + config download + copy.
- [x] Token lifecycle: create on user creation, regenerate (old dies), revoke/restore;
      public page honors all three states.
- [x] Rate-limited public surface (same limiter family as login); tokens never logged.
- [x] Admin UI: subscription card on user detail (copy / regenerate / revoke) + quick-copy
      on the users list; `subscription.base_url` setting (Phase 6 settings UI binds to it).

- [ ]- [x] — Create-user experience — planned
- [ ] Slide-over drawer form (no separate page for create; edit stays on its page).
- [ ] Username generator (pronounceable `word-word-nn`), manual entry still first-class.
- [ ] Display name optional; defaults to the username when empty.
- [ ] Auto-create devices on creation (default on; count = device limit, hard cap 10).
- [ ] Traffic quota presets (20/50/70/100/150/200/300/500/700/1000 GB) stored in the
      settings registry (`users.quota_presets_gb`) for Phase 6 management.
- [ ] Duration presets (1/3/6 months, 1 year) + custom value+unit.
- [ ] Premium calendar date-picker: Jalali for fa, Gregorian for en (vanilla JS, no deps).

### Stage 4 — Users page redesign — planned
- [ ] Premium rows: identity column (avatar, username, display name, plan), status badge,
      traffic meter, expiry (+relative), devices, last activity.
- [ ] Quick-share popover per row: subscription link copy, per-device QR/download.
- [ ] Row menus reposition with a fixed-position strategy (no clipping inside
      `overflow` containers — fixes the "detail expands underneath the card" issue).
- [ ] Bulk bar, filters, empty states restyled; mobile card layout redesigned.

### Stage 5 — Theme & shell redesign — planned
- [ ] New light + dark palettes (zinc neutrals, refined dark `#09090b` base — not harsh),
      surface hierarchy, borders, shadows, hover/focus states.
- [ ] Sidebar: desktop collapsible compact mode (icon rail + tooltips, persisted),
      nav groups, redesigned brand/footer.
- [ ] Topbar: breadcrumbs/title hierarchy, theme + language moved into a profile menu,
      sticky with blur.
- [ ] Component polish pass: buttons, inputs, selects, switches, badges, cards, tables,
      menus, dialogs, toasts, tooltips, seg control, empty/skeleton states.
- [ ] Footer (auth screens) redesigned.

### Stage 6 — Panel-wide polish — planned
- [ ] Dashboard: metric cards (icon chips), chart card, host metrics, live refresh styles.
- [ ] Plans: card-grid list, refined form with presets/segmented controls.
- [ ] Interfaces: list + form reorganized into clear sections with progressive disclosure.
- [ ] Login + onboarding: premium split/brand treatment, refined steps.
- [ ] Dialogs/drawers: unified overlay, radius, shadow, animation language.

### Stage 7 — Interfaces / AmneziaWG parameters — planned
- [ ] Upstream research against the pinned versions (tools v3.1, dkms 1.0.0, go v3.1):
      parser-accepted key list confirmed from `config.c`; runtime verification state pinned
      in `docs/integrations/amneziawg.md`.
- [ ] Storage + validation + rendering for the full accepted set: S3, S4,
      HeaderProtectionKey, ContentPaddingAddition, RekeyAfterTime, RekeyTimeout,
      RejectAfterTime, KeepaliveTimeout, MaxHandshakeAttempts, RandomTrailers,
      DisableCookies, AdvancedSecurity — all capability-gated OFF by default with an
      explicit "runtime verification pending (Phase 8 VPS matrix)" notice.
- [ ] H1–H4 / ContentPaddingAddition / timer ranges (`N` or `N-M`) per upstream dump format.
- [ ] Generate/randomize actions for safely generatable params (H1–H4, junk sizes, HPK).
- [ ] Client-config parity: client conf renders the same gated params when set.
- [ ] Interface form UX: sections (Networking / Obfuscation / Advanced), defaults, hints,
      warnings for client-compatibility constraints (iOS I1–I5, RandomTrailers history).

### Stage 8 — Verification & delivery — planned
- [ ] Full test suite green (Windows + WSL2 `-race`), gofmt/vet clean.
- [ ] Asset budgets re-measured (JS grew for the calendar/drawer; justify or optimize).
- [ ] Browser QA circuit: desktop 1366/1440 + mobile 390, fa/en, light/dark, RTL checks.
- [ ] Workflow checks: create user with presets, calendar, auto-configs; sub link lifecycle;
      small accounts (100 MB / 6 h; 0.2 GB); QR + config delivery.
- [ ] Docs updated (status.md, CHANGELOG, ui-ux.md, amneziawg.md, project-structure.md).
- [ ] Commits + push; final summary.

## Deferred (explicitly out of this pass)

- Settings screen for preset management (Phase 6) — storage ships now, UI later.
- Admin/token/webhook/audit/backup screens (Phase 6).
- Runtime verification of 2.0/3.x obfuscation params (requires real VPS — Phase 8 matrix).
- Aggregate "sing-box/clash subscription" format endpoints (candidate for a later phase;
  needs an upstream-supported format decision — not assumed).

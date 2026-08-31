# Phase 5 refinement — roadmap & progress (frozen 2026-08-31)

This checklist tracked the Phase 5 UI/UX refinement pass (2026-08-31). All stages are
complete; the outcome is recorded in [status.md](status.md) and the verification record
below. Scope guard held: everything stayed inside the Phase 5 surface area (no Phase 6
settings/admin/token/backup screens; preset storage shipped so the Phase 6 settings UI can
bind to it).

## Design direction

- shadcn/ui-inspired neutral palettes (zinc-based light, near-black refined dark), one
  distinctive WG-Guard accent family, subtle borders/shadows, visible focus rings.
- Both directions first-class: desktop is designed, mobile is designed (not adapted).
- No new runtime dependencies: hand-written CSS/JS, existing vendored HTMX + Lucide sprite.
- Asset budgets stayed within `scripts/check-assets.sh` limits (JS 24.1/30 KiB gz,
  CSS 10.0/25 KiB gz).

## Stages

### Stage 1 — Input correctness (quota/duration precision) — **done**
- [x] Exact decimal quota parsing (no float64 rounding): `0.2 GB` → 200000000 bytes.
- [x] Unit-aware quota input (GB / MB) server-verified; small values first-class
      (100 MB, 6-hour accounts).
- [x] Fix `View.GB` display rounding (edit forms showed `0` for 0.2 GB).
- [x] Duration input model: value + unit (hours / days / months) instead of fractional days.
- [x] Exact expiry date on create (server-stored, not a derived duration).

### Stage 2 — Subscription links — **done**
- [x] Per-user subscription token: 256-bit crypto/rand, SHA-256-hashed at rest,
      AES-GCM-encrypted for panel re-display (never logged).
- [x] Public unauthenticated page `/sub/{token}`: traffic used/limit, status, expiry,
      duration, devices with per-device QR + config download.
- [x] Token lifecycle: created on user creation, regenerate (old dies), revoke/restore;
      public page honors all three states with a single not-found response.
- [x] Rate-limited public surface (fixed-window per IP); tokens masked in logs.
- [x] Admin UI: subscription card on user detail (copy / regenerate / revoke) + quick-copy
      and share menu on the users list; `subscription.base_url` setting (Phase 6 binds).

### Stage 3 — Create-user experience — **done**
- [x] Slide-over drawer form on the users page (`/users/new` stays as fallback, shared
      `user_create_fields` template partial).
- [x] Username generator (pronounceable `word-word-nn`), manual entry still first-class.
- [x] Display name optional; details de-emphasized.
- [x] Auto-create devices on creation (default on; count = device limit, hard cap 10).
- [x] Traffic quota presets (20/50/70/100/150/200/300/500/700/1000 GB) stored in the
      settings registry (`users.quota_presets_gb`) for Phase 6 management.
- [x] Duration presets (1/3/6/12 months via `users.duration_presets_months`) + custom
      value+unit.
- [x] Calendar date-picker: Jalali for fa, Gregorian for en (vanilla JS; the leap-year
      algorithm is exhaustively verified against server-side conversions).

### Stage 4 — Users page redesign — **done**
- [x] Premium rows: identity cell (avatar, username, display name, plan chip), status badge
      with pulse dot, traffic meter, expiry (+relative hint), devices, last activity.
- [x] Quick-share popover per row: subscription page link, link copy, per-device QR/download
      (backed by one batch device query per page).
- [x] Row menus reposition with fixed coordinates + viewport flipping (no clipping inside
      overflow containers — fixes the "detail expands underneath the card" issue); menus
      close on scroll instead of drifting.
- [x] Bulk bar, filters, empty states restyled; mobile card layout verified at 390 px.

### Stage 5 — Theme & shell redesign — **done**
- [x] New light + dark palettes (zinc neutrals, refined dark `#09090b` base), surface
      hierarchy, borders, shadows, hover/focus states.
- [x] Sidebar: desktop collapsible compact mode (68 px icon rail + tooltips, persisted in
      localStorage), redesigned brand/footer with compact icon row.
- [x] Component polish pass: buttons, inputs, selects, switches, badges, cards, tables,
      menus, dialogs, toasts, tooltips, seg control, empty/skeleton states.
- [x] Auth screens: richer background treatment.

### Stage 6 — Panel-wide polish — **done**
- [x] Dashboard metric icon chips, chart card, host metrics, live refresh styles.
- [x] Plans/interfaces/login/onboarding inherit the refreshed tokens and components.
- [x] Dialogs/drawers unified overlay/radius/shadow/animation language.

### Stage 7 — Interfaces / AmneziaWG parameters — **done**
- [x] Upstream research against the pinned versions: value formats verified from
      `amneziawg-tools` v3.1 `src/config.c` (S3/S4 u16; padding/timers `N`/`N-M` u16
      ranges; HeaderProtectionKey base64 32-byte; flags `on`/`off`; **AdvancedSecurity is a
      peer-section key — deferred, not guessed**). Recorded in `docs/integrations/amneziawg.md`.
- [x] Storage (migration 0005) + validation + reconcile spec + setconf rendering + dump
      parsing + client-config parity for the gated set: S3, S4, HeaderProtectionKey,
      ContentPaddingAddition, RekeyAfterTime, RekeyTimeout, RejectAfterTime,
      KeepaliveTimeout, MaxHandshakeAttempts, RandomTrailers, DisableCookies — all
      off by default with an explicit runtime-verification-pending warning.
- [x] Drift safety: gated-parameter mismatch is report-only (`Obfuscation.LegacyVerified`);
      a runtime that ignores gated params can never trigger recreate loops.
- [x] Magic headers crypto/rand generated at profile creation (presets no longer hardcode
      weak sequential values); randomize action + recommended defaults on enable in the form.
- [x] Interface form UX: sections, I1–I5 inputs with iOS warning, gated-advanced collapse
      with warnings for RandomTrailers/DisableCookies.

### Stage 8 — Verification & delivery — **done**
- [x] Full suite green (Windows + WSL2 `-race`, all 34 packages), gofmt/vet clean.
- [x] Asset budgets re-measured within limits.
- [x] Browser QA circuit (details below).
- [x] Workflow checks: presets, calendar, auto-configs, sub-link lifecycle, small accounts,
      QR + config delivery.
- [x] Docs updated (status.md, CHANGELOG, ui-ux.md, amneziawg.md, project-structure.md).
- [x] Commits + push; final summary.

## Verification record (2026-08-31)

- Full suite green on Windows; `-race` suite green in WSL2 Ubuntu (all 34 packages).
- Browser QA (fake backend, real browser): onboarding; interface create with obfuscation
  defaults autofill, magic-header randomization and gated advanced params; create-user
  drawer (username generator, quota/duration chips, Jalali calendar pick → ISO write,
  auto-devices device-1/device-2, sub link ensured); users list quick-share menu (fixed
  position, inside viewport at 390 px); sub-link lifecycle (rotate kills old URL, revoke
  404, restore 200); public sub page fa/en with valid QR PNGs and config parity
  (Jc/S1/S2/H1–H4 present in the downloaded .conf).
- Desktop 1366 + mobile 390, fa/RTL + en/LTR: no horizontal overflow on any page.
- Dark palette verified live (`#09090b` bg, `#111113` surfaces, `#26262a` borders).
- Sidebar collapse: 68 px rail, labels hidden, nav centered, persisted.
- Log hygiene: no subscription tokens or secrets in the server log.
- Extra fix found during QA: onboarding password hint was double-formatted
  (`%!(EXTRA int=10)`).

## Deferred (explicitly out of this pass)

- Settings screen for preset management (Phase 6) — storage ships now, UI later.
- Admin/token/webhook/audit/backup screens (Phase 6).
- Runtime verification of 2.0/3.x obfuscation params (requires real VPS — Phase 8 matrix).
- AdvancedSecurity (per-peer key in upstream; needs device-level plumbing — see
  docs/integrations/amneziawg.md).
- Aggregate "sing-box/clash subscription" format endpoints (candidate for a later phase;
  needs an upstream-supported format decision — not assumed).

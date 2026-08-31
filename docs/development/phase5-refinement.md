# Phase 5 refinement — roadmap & progress

Two refinement passes are tracked here. Round 1 (2026-08-31, earlier the same day) is frozen
and recorded at the bottom. **Round 2** is the active checklist.

## Round 2 — UI/UX refinement pass 2 (active)

Direction: a distinctive premium WG-Guard design system, validated against the operator's
reference panel (warm sand/cream neutrals, ink primary, layered surfaces) and shadcn
component quality — not a copy. The pinned-runtime AmneziaWG parameter support is verified on
a real VPS before interface defaults/generation are built on it.

### Stage 1 — Real-VPS environment + AmneziaWG runtime verification — **planned**
- [ ] Dedicated test VPS (Ubuntu 24.04 KVM) prepared: SSH key auth, pinned PPA packages
      (`amneziawg-tools` + `amneziawg-dkms`), kernel module loaded. Test credentials stay
      out of the repository (key-based access, no secrets in docs/logs).
- [ ] Runtime acceptance + round-trip matrix on the **kernel module** (the Phase 8-critical
      gap): S3/S4, H1–H4 plain and `N-M` ranges, HeaderProtectionKey,
      ContentPaddingAddition, RekeyAfterTime/RekeyTimeout/RejectAfterTime/KeepaliveTimeout/
      MaxHandshakeAttempts ranges, RandomTrailers/DisableCookies flags, I1 template/hex,
      peer PersistentKeepalive range, dump field fill under netlink.
- [ ] Constraint rejections re-verified on the kernel backend (duplicate H, Jmin>Jmax,
      S1+56==S2, explicit zero block).
- [ ] Findings recorded in docs/integrations/amneziawg.md (verification log) + fixture;
      capability gating updated to match reality.

### Stage 2 — Design system rework (palette, no purple) — **planned**
- [ ] Warm sand/cream light palette with ink (near-black) primary; layered surface tokens
      (card-1/2/3, input surfaces, page glow) like the reference system.
- [ ] Warm dark mode lifted off pure black (balanced dark neutrals, same hue family).
- [ ] Accent/status tints re-derived (info/success/warning/danger + brand accent used
      sparingly); charts, login/onboarding, public sub page recolored consistently.
- [ ] Subtle low-contrast gradients only where they add depth (page glow, hero surfaces).

### Stage 3 — Shell, sidebar, responsiveness — **planned**
- [ ] Intentional layouts from small phones (360) through tablets to large/very large
      monitors: content max-width + centered composition on big screens instead of
      unbounded stretch.
- [ ] Tablet/laptop breakpoints reviewed (sidebar default states per breakpoint); mobile
      drawer polish (scrim, focus, safe areas).
- [ ] Sticky, compact page headers; consistent page gutters.

### Stage 4 — Create-user redesign — **planned**
- [ ] Fix: drawer close (X/Cancel/Escape) — close handlers were bound to `dialog.modal`
      only, the drawer is `dialog.drawer`.
- [ ] Fix: calendar opened *behind* the drawer — the popover appended to `document.body`
      can never paint above a `<dialog>` top layer; it must live inside the dialog.
- [ ] Declutter: identity (username + generator, display name), traffic as segmented
      "packages / custom", duration as segmented "duration / exact date / no expiry" —
      duration picker and calendar never visible at the same time.
- [ ] Live expiry preview ("expires <date> · in N months") computed client-side,
      Jalali-aware for fa.
- [ ] Advanced section (plan, interface, start policy, speeds, tags) collapsed by default;
      device/config count as a compact stepper on the main path.
- [ ] Sensible defaults from settings: default interface, default device count, default
      quota, default duration (normal path = fill 4 fields, press Create).

### Stage 5 — Panel settings page — **planned**
- [ ] Settings screen for the non-secret panel knobs: quota/duration presets, create
      defaults, subscription base URL, config filename prefix/suffix.
- [ ] Scope guard: no Phase 6 material (admins, tokens, webhooks, backups, audit) on it.

### Stage 6 — Config download filenames — **planned**
- [ ] Downloads named `username-device.conf` (sanitized, collision-safe) everywhere configs
      are delivered: web, API, public sub page.
- [ ] Optional filename prefix/suffix settings applied consistently; QR filenames follow.

### Stage 7 — Dashboard redesign — **planned**
- [ ] Information hierarchy: headline metrics, traffic chart emphasis, host card, and
      actionable lists (expiring soon, quota exhausted) where the data already exists.
- [ ] Premium card composition, spacing, mobile layout.

### Stage 8 — Interfaces form: defaults + full randomization — **planned**
- [ ] New-interface form prefilled with sensible defaults (verified against Stage 1).
- [ ] "Randomize" generates every supported, safe-to-generate parameter (magic headers,
      junk/padding sizes, timer ranges, header-protection key, padding ranges) — not just
      the legacy subset; manual editing stays possible for everything.
- [ ] Basic/advanced organization keeps the normal create flow short.

### Stage 9 — Users, plans, remaining pages consistency — **planned**
- [ ] Users page polish pass under the new palette; plans, auth screens, dialogs, menus,
      empty states aligned to the same system.

### Stage 10 — Verification & delivery — **planned**
- [ ] Full suite green (Windows + WSL2 `-race`), gofmt/vet clean, asset budgets measured.
- [ ] Browser QA circuit: desktop + mobile × fa/en × light/dark; create-user, sub lifecycle,
      interfaces, settings, downloads workflows.
- [ ] Real-VPS verification pass (integration tests + runtime matrix evidence).
- [ ] API/OpenAPI + docs updated (settings keys, filenames, verification log); status.md
      honest; CHANGELOG; commits + push; final summary. Stop before Phase 6.

---

## Round 1 — UI/UX refinement pass 1 (frozen 2026-08-31)

All 8 stages shipped and pushed (`1c9b7c2..37d2df5`): exact quota/duration parsing;
subscription links + public `/sub/{token}` page (hashed+encrypted tokens, rate limiting);
create-user drawer (generator, presets, auto-configs, verified Jalali calendar); users page
redesign with root-cause menu fix; zinc theme + collapsible shell; capability-gated AWG
2.0/3.x parameters verified against pinned `config.c`; docs/QA/push complete. Verification
record and deferred list for round 1 live in git history of this file and in
[status.md](status.md).

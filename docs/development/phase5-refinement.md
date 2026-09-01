# Phase 5 refinement — roadmap & progress

Two refinement passes are tracked here. Round 1 (2026-08-31, earlier the same day) is frozen
and recorded at the bottom. **Round 2** is complete.

## Round 2 — UI/UX refinement pass 2 (complete 2026-08-31)

Direction: a distinctive premium WG-Guard design system, validated against the operator's
reference panel (warm sand/cream neutrals, ink primary, layered surfaces) and shadcn
component quality — not a copy. The pinned-runtime AmneziaWG parameter support was verified
on a real VPS before interface defaults/generation were built on it.

### Stage 1 — Real-VPS environment + AmneziaWG runtime verification — **done**
- [x] Dedicated test VPS (Ubuntu 24.04 KVM) prepared: SSH key auth, pinned PPA packages
      (`amneziawg-tools` v3.1.20260812 + `amneziawg-dkms` 1.0.0), kernel module loaded.
      Test credentials stay out of the repository (key-based access, no secrets in
      docs/logs/source).
- [x] Runtime acceptance + round-trip matrix on the **kernel module**: S3/S4, H1–H4 plain
      and `N-M` ranges, HeaderProtectionKey, ContentPaddingAddition, all five timer ranges,
      RandomTrailers/DisableCookies, I1 template + hex, and peer PersistentKeepalive range
      were accepted and echoed by the 29-field/8-field dump. `AdvancedSecurity` returned success
      but had no dump field; Phase 8 source review proved that result was an ignored kernel
      attribute, not a round trip.
- [x] Constraint semantics pinned down: HPK requires a coherent S1–S4 block with every value at
      least 12; VPS evidence additionally requires S3/S4 in the same setconf message. Duplicate H
      intervals and the all-zero block are rejected;
      `Jmin > Jmax` and `S1+56==S2` accepted by the kernel (userspace rejects them —
      WG-Guard validates locally regardless); explicit zeros cannot clear set params;
      `awg setconf` requires explicit `[Interface]` headers; module/link name is
      `amneziawg`. Evidence: `docs/integrations/fixtures/verify-vps-kernel-matrix.txt`.
- [x] Findings recorded in docs/integrations/amneziawg.md (verification log) and turned
      into code. Phase 8 re-audits and tightens the earlier HPK and range validation rules;
      gated drift stays report-only until the supported contract is complete.

### Stage 2 — Design system rework (palette, no purple) — **done**
- [x] Warm sand/cream light palette with ink (near-black) primary; layered surface tokens;
      page-glow token.
- [x] Warm dark mode lifted off pure black (same hue family, `#171512` base).
- [x] Petrol brand accent for links/focus/nav-active/chart RX/meters; status tints
      re-derived warm; favicon recolored; charts/auth/sub pages follow tokens.
- [x] Subtle gradients only (page glow, auth backgrounds).

### Stage 3 — Shell, sidebar, responsiveness — **done**
- [x] Content max-width 1400 px (1560 px ≥1920) — centered composition on large screens.
- [x] Tablet default collapsed rail (≤1180 px, only without an explicit operator
      preference); stale mobile drawer closed on resize past 960 px.
- [x] Table→card switch raised to 800 px; ≤480 px density (topbar/gutters/typography);
      safe-area insets; `viewport-fit=cover` + light/dark `theme-color` metas on all three
      layouts.

### Stage 4 — Create-user redesign — **done**
- [x] Fix: drawer close — `X`/Cancel were bound to `dialog.modal` only; now every
      `<dialog>` handles close buttons (verified live).
- [x] Fix: calendar rendered behind the drawer — popovers appended to `<body>` can never
      paint above a `<dialog>` top layer; the calendar now mounts inside the dialog
      (verified live: Jalali month grid above the drawer form).
- [x] Declutter: segmented "packages / custom" traffic; segmented "duration / exact date /
      no expiry" — picker and calendar never visible together; hidden-mode inputs are
      disabled so only the visible mode submits; configs stepper replaces the duplicated
      checkbox + device-limit pair (auto-create always on, count = stepper).
- [x] Live expiry preview ("Expires <date> · in N days") computed client-side, Jalali for
      fa; plus `RelIn` server-side future-relative hints on lists/detail/sub page (fixes
      "just now" shown for future expiries).
- [x] Advanced section collapsed (plan, interface, start policy, speeds, tags).
- [x] Sensible defaults from settings: default interface (configured id → first enabled),
      default device count, default quota, default duration (0 = no-expiry). Normal path =
      fill username → Create.

### Stage 5 — Panel settings page — **done**
- [x] `/settings` (nav item "Settings"): traffic/duration package lists, create defaults,
      default interface, `subscription.base_url`, download filename prefix/suffix —
      non-secret registry keys only; every write through registry validators; first failure
      re-renders with the submitted values and the offending field marked.
- [x] Scope guard held: no Phase 6 material (admins, tokens, webhooks, backups, audit).

### Stage 6 — Config download filenames — **done**
- [x] `[prefix]username-device[suffix].conf` via `clientconf.ConfigFilename` (ASCII-safe
      sanitized parts), applied uniformly to web, API and public sub-page downloads; QR
      responses get matching inline `.png` names; verified live with and without prefix.
- [x] `downloads.filename_prefix` / `downloads.filename_suffix` settings (validator-capped)
      exposed on the settings page; OpenAPI documents the filename convention.

### Stage 7 — Dashboard redesign — **done**
- [x] "Needs attention" card: expiring ≤7 days / quota-exhausted / expired, capped at 5
      rows each, status+expiry-indexed queries, rendered only when non-empty; Jalali-aware
      dates + future-relative hints.
- [x] Traffic tile promoted next to online; balanced metric grid (4+3 at desktop widths);
      single page title (topbar shows the brand; pages own their `h1`).

### Stage 8 — Interfaces form: defaults + full randomization — **done**
- [x] Enable-prefill extended (padding `10-100` joins the balanced junk defaults).
- [x] "Randomize all parameters" fills every supported, safe-to-generate parameter with
      `crypto.getRandomValues`: Jc/Jmin/Jmax, S1/S2 (S1+56≠S2), S3/S4, four distinct u32
      magic headers, 32-byte base64 HeaderProtectionKey, padding + five timer ranges.
      Flag checkboxes stay operator-owned (RandomTrailers default off upstream).
      Verified live; the generated profile was accepted by our own validation + storage.
- [x] H1–H4 remain plain u32 by design (ranges are accepted by the kernel but break
      pairwise-distinct validation semantics and older clients) — documented.
- [x] HPK⇒S3/S4 kernel constraint enforced in `iface.ValidateObfuscation` + UI hint;
      gated-warning text updated to kernel-verified status.

### Stage 9 — Users, plans, remaining pages consistency — **done**
- [x] All pages re-rendered under the new tokens; smoke-rendered every route (200, no
      template errors); login/onboarding/sub layouts get the same metas; settings nav item
      added. Users page verified live in fa/dark and en/light.

### Stage 10 — Verification & delivery — **done**
- [x] Full suite green on Windows (34 packages); `-race` green in WSL2 Ubuntu.
- [x] **Full suite with `-tags integration` green on the real VPS** (34 packages, kernel
      module loaded, root) — the integration-tagged tunnel/firewall/shaper/network paths
      run against the real kernel backend, not just WSL2 userspace.
- [x] gofmt/vet clean; asset budgets measured: JS 25.3/30 KiB gz, CSS 10.5/25 KiB gz,
      fonts 101.8/150 KiB gz.
- [x] Browser QA circuit (fake backend, real browser): login (warm dark) → dashboard
      (attention card, Jalali dates, RTL) → users → create drawer (generator, defaults,
      segmented modes, calendar-above-dialog, date pick → ISO, X close, create → auto
      device) → interfaces (randomize-all → create awg1 with the full gated set) →
      settings save → public sub page; desktop 1366 + mobile 390, fa/en × light/dark, no
      horizontal overflow.
- [x] Log hygiene: no tokens/keys/passwords in the server log.
- [x] API/OpenAPI + docs updated (status.md, CHANGELOG, amneziawg.md, this roadmap);
      commits + push; final summary. Stopping before Phase 6.

## Round 1 — UI/UX refinement pass 1 (frozen 2026-08-31)

All 8 stages shipped and pushed (`1c9b7c2..37d2df5`): exact quota/duration parsing;
subscription links + public `/sub/{token}` page (hashed+encrypted tokens, rate limiting);
create-user drawer (generator, presets, auto-configs, verified Jalali calendar); users page
redesign with root-cause menu fix; zinc theme + collapsible shell; capability-gated AWG
2.0/3.x parameters verified against pinned `config.c`; docs/QA/push complete. Verification
record and deferred list for round 1 live in git history of this file and in
[status.md](status.md).

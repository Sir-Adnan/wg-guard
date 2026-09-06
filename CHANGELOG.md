# Changelog

All notable changes to WG-Guard are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning is
[SemVer](https://semver.org/). The REST API (`/api/v1`) is a compatibility contract from its
first release — see [docs/architecture/api.md](docs/architecture/api.md).

## [Unreleased]

### Changed
- **Public VPN endpoint classification:** reject CGNAT, mapped IPv4 bypasses and relevant
  special-use ranges during explicit input and automatic address selection. Syntax/address
  eligibility checks do not claim real reachability through routing, NAT or firewalls.
- **Installer delivery phase planned:** inserted Phase 8.1 between completed Phase 8 and
  operational observability. It owns GitHub acquisition, terminal UX, prerequisites, compatible
  AWG version policy and lifecycle recovery. The Phase 9 design branch is paused; Phases 9–12
  retain their scope and final public publication remains owner-approved. No implementation or
  new compatibility claim is implied by this planning change.
- **Phase 8 configuration integrity complete:** supported AWG values are lossless across storage,
  API/OpenAPI, forms, runtime, reconciliation, backup/restore, configs, subscriptions, and QR.
  The exact Ubuntu 24.04 candidate passed three-surface decode equality, recommended/randomized
  kernel-client handshakes and bidirectional traffic, recommended userspace-daemon traffic,
  browser presentation, actual-secret diagnostics scanning, and owned-resource cleanup.
- **Peer-sync interface integrity:** real traffic testing found that a peers-only kernel
  `awg syncconf` clears the interface private key. Peer replacement now preserves the validated
  complete live interface section and byte-verifies it after apply; invalid or changed state
  fails closed without exposing config or key material.
- **Honest userspace-backend status:** the Phase 8 audit found that `backend_mode="userspace"`
  is stored and reported but never consumed by boot/reconciliation, and no component supervises
  `amneziawg-go`. Doctor and deployment documentation no longer promise automatic fallback;
  Phase 11 owns the bounded lifecycle and observed-mode certification (AUD-019). Manually started
  userspace integration remains valid protocol/config compatibility evidence.

### Added
- **Recoverable lifecycle transactions (M3):** exclusive Linux lock, durable private state and
  operation journal, immutable previous artifacts, source-aware install/update, mandatory remote
  fetch semantics, same-data-contract rollback, safe removal targets, and catalog-only core
  maintenance. Unproven data compatibility retains a visible restore-required state instead of
  starting old code. Automated tests and independent review pass; VPS certification remains pending.
  Local first-owner provisioning and coordinated cross-contract restore follow in M4/M5.
- **Installer prerequisite and core checks:** Ubuntu 24.04 package preparation, explicit manual
  routes on other Linux systems, pinned compatible AWG catalog, loaded/disk build-identity
  reporting, immutable runtime-image preparation and retryable trusted TLS readiness. Explicit
  TCP ports and IP-only VPN endpoints are preserved. Full lifecycle integration remains in progress.
- **GitHub build acquisition:** bounded release/commit selection, checksummed private staging,
  immutable source builds with temporary verified Go tooling, a first-entry bootstrap, and local
  amd64/arm64 candidate artifact generation. No published-release installation is claimed;
  runtime deployment integration and the redesigned terminal flow follow in Phase 8.1.
- **Bounded build-command output:** configured subprocess execution retains at most 1 MiB per
  output stream while draining the child pipes, covered by an actual oversized-child regression.
- **Independent QR verification:** test-only `gozxing` v0.1.1 decoding now verifies direct,
  REST, admin, and public-subscription PNGs against the canonical private-key-bearing config
  without printing it. Coverage includes empty and Persian/English UTF-8 data, complete configs,
  2.3 KB near-capacity payloads, deterministic PNG output, headers, authorization isolation, and
  oversized input; the decoder and its transitive module are absent from production binaries.
- **Canonical AWG profile policies:** one injectable server-side generator now owns plain,
  recommended, and randomized profiles for the service, REST API, and admin panel. Recommended
  profiles keep client-risk fields off and receive unique per-profile headers; randomized
  profiles generate relationship-safe J/S values, four non-overlapping H ranges, a coupled
  S1–S4/header-protection-key set, and bounded timer ranges using `crypto/rand`. Property tests
  validate 10,000 generated profiles, entropy failures never return partial values, API presets
  are applied with documented mutual-exclusion rules, and the authenticated CSRF panel preview
  replaces browser-side protocol randomness. Stored header-protection keys are write-only on edit.
- **Phase 8 audit and pinned AWG contract:** reproducible Windows/WSL/CI baselines, a material
  findings ledger, exact tools/kernel/userspace source revisions, and a field-by-field capability
  matrix covering placement, range widths, backend observability, clearing, parity, and gating.
  `AdvancedSecurity` is explicitly unsupported at the pins because the kernel setter ignores it,
  userspace rejects it, and ordinary dumps cannot observe it.
- **Release-readiness roadmap:** the former monolithic hardening phase is now an independently
  gated Phase 8–12 program: configuration/QR integrity, operational observability, complete
  UI/UX redesign, production certification, and release-candidate engineering. The active
  checklist, cross-phase blockers, audit findings, and compatibility state are tracked under
  `docs/development/` without claiming future work as implemented.
- **Installer initial settings** (post-review addendum to Phase 7): optional wizard sections
  for the VPN network defaults (AWG listen-port allocation range, first-interface VPN pool
  via the new `network.default_pool` registry key, client MTU, client DNS resolvers) and
  Telegram backups (bot token, chat ID, daily schedule — skippable); values equal to the
  registry defaults are never persisted, and the panel domain is seeded as `node.endpoint`
  so the first exported client config carries a working `Endpoint` line. Seeds apply before
  the service first boots through `settings set KEY -stdin` (secret via stdin, never argv)
  and the new `wg-guard backup schedule-add` verb.
- **Phase 7 — deployment & installer** (tracked in `docs/development/phase7.md`; drills on a
  real Ubuntu 24.04 VPS with a public domain):
  - Built-in ACME (`tls.mode=acme`): autocert issuance/renewal, HTTP-01 challenge sidecar on
    `tls.acme_http_port` (default 80), certificates cached under the data dir, and a redirect
    fallback that targets the configured domain + the real TLS port on any panel port.
  - `wg-guard install`: interactive wizard (Docker default, native systemd secondary,
    `--yes` non-interactive) with preflight, a hardened systemd unit, a generated compose
    project with a TLS-mode-aware healthcheck, the mode-aware host CLI, and kernel-module boot
    persistence (`/etc/modules-load.d/wg-guard.conf`) with a DKMS rebuild ladder for
    post-upgrade kernel mismatches.
  - `wg-guard update`: pre-upgrade backup → image/binary swap → health check → automatic
    rollback; `--rollback` recovers interrupted updates from the state-recorded artifact.
  - `wg-guard uninstall --dry-run` (removes only state-recorded artifacts; data and
    installer-installed packages kept unless explicitly purged) and `wg-guard status`.
  - Multi-stage Dockerfile (Ubuntu 24.04 + pinned `amneziawg-tools` from `ppa:amnezia/ppa`,
    amd64/arm64) and a reference compose file under `deploy/`.
- **Phase 6 — backup / ops** (tracked in `docs/development/phase6.md`; verification matrix in
  `docs/development/status.md`):
  - `internal/backup` archive engine: `.wgg` archives (manifest + `VACUUM INTO` snapshot +
    boot config + master key, per-file SHA-256), optional age encryption with the single
    backup password (ADR-0008), retention pruning, local + Telegram delivery sinks.
  - Scheduled backups: daily / every-N-hours / weekly schedules run in-process by the central
    scheduler (once-per-minute due scan; a missed window runs exactly once); automatic
    pre-migration backups.
  - Restore: stage-then-swap engine — decrypt, checksum (incl. container CRCs), schema gate,
    out-of-place migrate, integrity check, environment review; CLI applies with the service
    stopped, the panel stages and the swap happens at the next boot with `*.pre-restore`
    safety copies. The archived boot config is never applied automatically.
  - Panel ops screens: `/backups` (archives, create-now, restore wizard, schedules, Telegram
    test), `/admins` (roles + permission matrix, password resets), `/tokens` (show-once
    plaintext, scopes/CIDR/expiry), `/webhooks` (CRUD, secret rotation, delivery list +
    redeliver), `/audit` (paged, filtered, expandable metadata) — each behind its panel
    scope, with a permission-aware System nav group.
  - Full settings UI (now `node.settings`-gated): identity, networking, accounting/retention,
    API rate limit + session TTLs, backups (write-only age password + bot token with
    set/clear semantics; ≥8-char password enforced by the registry).
  - CLI: `backup create|list|telegram-test`, `restore`, `settings list|get|set`,
    `doctor [--fix]`, `secrets rotate` — the master-key rotation trigger, including the
    missing interface-key carrier (rotation previously would have orphaned interface keys).
  - `wg-guard doctor`: platform, permissions, tool pin, kernel module, DB integrity,
    interface/peer drift, nftables, sysctl, tc, disk, endpoint DNS, TLS cert expiry, NTP,
    backup posture; `--fix` reuses the boot orchestration and refuses while the service runs.
  - Visual rebase: shadcn-style neutral theme (white light / near-black dark, zinc scale, ink
    primary, semantic tones only, monochrome charts) replacing the warm-sand palette, and
    per-page content-width tiers (centered narrow forms/settings; fluid tables/dashboards).
  - New dependency: `filippo.io/age` (ADR-0008) and `golang.org/x/term` (no-echo passphrase
    prompts).
- **Phase 5 refinement round 2 — warm-sand design system, responsive shell, create-user
  decluttering + defaults, settings screen, descriptive download filenames, dashboard
  hierarchy, full AWG randomization, real-VPS kernel verification** (same verification
  standard; tracked in `docs/development/phase5-refinement.md`):
  - Warm-sand design system replacing the zinc/indigo one: warm neutral light palette with
    ink primary and a petrol brand accent, lifted warm dark mode (no pure black), layered
    surface + page-glow tokens, recolored charts, favicon, auth and public pages.
  - Responsive shell: viewport-scaled content width (1400/1560 px), tablet default collapsed
    rail, table→card at 800 px, small-phone density + safe areas, `theme-color` metas.
  - Create-user redesign: segmented "packages / custom" traffic and "duration / exact date /
    no expiry" (calendar and duration picker never visible together), live Jalali-aware
    expiry preview, configs stepper, collapsed advanced section; fixed the drawer close
    (`X`/Cancel were bound to modal dialogs only) and the calendar rendering *behind* the
    dialog (now mounted inside the top layer).
  - Create-form defaults from settings (`users.default_quota_gb`, `default_duration_months`,
    `default_device_limit`, `default_iface_id` with first-enabled fallback): normal path is
    fill-username → Create.
  - Panel settings screen (`/settings`): traffic/duration package lists, create defaults,
    default interface, subscription base URL, filename prefix/suffix — registry-validated
    writes with error redisplay; Phase 6 ops screens remain out of scope.
  - Config downloads named `[prefix]username-device[suffix].conf`
    (`downloads.filename_prefix`/`_suffix` settings, sanitized by
    `clientconf.ConfigFilename`) uniformly across web, API and the public sub page; QR
    responses get matching inline names; OpenAPI updated.
  - Dashboard: "needs attention" card (expiring ≤7 d / quota-exhausted / expired, capped
    lists), traffic tile promoted, single page title, future-relative expiry hints
    (`i18n.FormatRelativeUntil` — "in 3 days" / "۳ روز دیگر").
  - Interfaces: "Randomize all parameters" (junk sizes, init packets, distinct magic headers,
    header-protection key, padding + timer ranges via `crypto.getRandomValues`), richer
    enable-prefill, and the kernel-verified HPK⇒S3/S4 constraint enforced in validation.
  - Real-VPS verification: dedicated Ubuntu 24.04 KVM node with the pinned PPA kernel module;
    runtime acceptance/round-trip matrix for the whole 2.0/3.x parameter set (H ranges, timer
    ranges, HPK⇒S3/S4 same-message coupling, clearing semantics, headerless-setconf
    rejection, backend constraint differences) — `docs/integrations/amneziawg.md` +
    `fixtures/verify-vps-kernel-matrix.txt`.
- **Phase 5 refinement round 1 — subscription links, create-user redesign, theme/shell overhaul,
  capability-gated AWG parameters** (same verification standard as Phase 5; tracked in
  `docs/development/phase5-refinement.md` with a verification record):
  - `internal/subscription` + migration 0004: per-user subscription links — 256-bit
    crypto/rand tokens (SHA-256-hashed for lookup, AES-GCM-encrypted for re-display, never
    logged), ensured at user creation, regenerate/revoke/restore lifecycle; public
    rate-limited `/sub/{token}` page (fa/en) with traffic meter, expiry, per-device QR +
    config download; admin subscription card + users-list quick-share; `subscription.base_url`
    setting reserved for the Phase 6 settings screen.
  - Create-user drawer: username generator, settings-driven quota/duration preset chips
    (`users.quota_presets_gb`, `users.duration_presets_months`), auto device provisioning
    (count = device limit, cap 10), exact expiry date; Jalali (fa) / Gregorian (en) vanilla-JS
    calendar with an exhaustively verified leap-year algorithm.
  - Users page redesign: identity rows, per-row quick-share (QR/download/link copy via one
    batch device query), fixed-coordinate menu positioning that can no longer be clipped by
    overflow containers.
  - Theme + shell: zinc-neutral light and refined near-black dark palettes, 8/12 px radii,
    component polish pass, persisted 68 px desktop sidebar rail with tooltips.
  - Interfaces: capability-gated 2.0/3.x AWG parameters (S3, S4, HeaderProtectionKey,
    ContentPaddingAddition, RekeyAfterTime, RekeyTimeout, RejectAfterTime, KeepaliveTimeout,
    MaxHandshakeAttempts, RandomTrailers, DisableCookies) through storage → validation →
    reconcile → setconf → dump → client-config parity (migration 0005); value formats
    verified against the pinned `amneziawg-tools` v3.1 `src/config.c`; gated drift is
    report-only so an unsupported runtime cannot trigger recreate loops; magic headers are
    crypto/rand generated at profile creation.
- **Phase 5 — Web UI** (all unit tested, `-race` clean in WSL2; end-to-end browser-verified on
  desktop + mobile, fa/en × light/dark, over the running node):
  - `internal/web`: server-rendered panel on `html/template` + HTMX partial swaps + one vanilla
    ES module — session-auth pages (login with language/theme pickers, first-run onboarding
    wizard), users (list/search/filter/pagination, bulk actions, tri-state forms, detail with
    renew/traffic/device management), devices (add, enable/disable, key regeneration, session-
    gated config download + QR), plans and tunnel interfaces (CRUD, enable/disable, AWG
    obfuscation parameter section with plain↔obfuscated rotation warning, device-count delete
    guard), dashboard (user metrics, host card, CSP-safe server-rendered SVG traffic chart with
    24 h/7 d/30 d ranges, 30 s live refresh paused on hidden tab).
  - `internal/i18n`: embedded fa/en catalogs with enforced key parity, per-admin language
    persisted, full RTL via CSS logical properties, Jalali dates for fa, LTR/Latin-digit data.
  - Design system: CSS custom-property tokens, light/dark/system themes (`data-theme` +
    `prefers-color-scheme`, cookie-persisted), embedded Lucide SVG sprite, responsive shell
    (sidebar → drawer below 960 px), WCAG-AA contrast both themes.
  - `internal/hoststats`: on-demand `/proc` readers (CPU from counter deltas, mem/disk/load/
    uptime) with no background polling; graceful "unavailable" state off Linux.
  - `internal/clientconf`: config/QR renderer shared by API and web; QR raster drawn manually
    (rsc.io/qr's `code.Image()` leaves modules unscaled — regression-tested).
  - Asset budgets enforced by `scripts/check-assets.sh`: JS ≤ 30 KiB gz, CSS ≤ 25 KiB gz,
    fonts ≤ 150 KiB gz (final measurements in docs/development/status.md).
- **Phase 4 — REST API** (all unit tested, `-race` clean; real-tc/ingress paths integration
  tested in WSL2):
  - Full `/api/v1` management surface (`internal/api`): users (CRUD, lifecycle, traffic
    add/set/reset, series), bulk create + bulk actions, devices (CRUD, enable/disable,
    key regeneration, on-demand config + QR), plans, interfaces (incl. obfuscation params),
    settings (redacted secrets), webhooks (CRUD, redeliver), stats, node health/info, and
    public ops endpoints (`/healthz`, `/readyz`, `/openapi.json`, `/docs`).
  - Conventions: one error envelope with stable machine codes + `X-Request-Id`; keyset cursor
    pagination (limit ≤ 500, id tiebreak); idempotency keys (24 h replay window, 409 on key
    reuse, `Idempotency-Replayed: true`); per-token rate limiting (fixed 60 s window,
    live-reloadable, 0 = unlimited); tri-state PATCH semantics (absent = no change, null =
    clear to unlimited) for independent up/down speed limits.
  - **Independent upload/download speed limits** on users and plans (`speed_limit_down_kbps`,
    `speed_limit_up_kbps`; migration 0002 converts the Phase 3 column). Download = tc HTB on the
    interface egress; upload = ingress qdisc mirroring into an IFB device with an HTB tree on
    client source IPs. Directions apply, rebuild and clean up independently; either direction
    unset means unlimited. Live-verified against iproute2 + kernel 6.18 in WSL2 (documented in
    docs/architecture/networking.md).
  - Durable webhooks (`internal/webhook`): events commit in the same transaction as the state
    change via a recorder seam injected into user/device/accounting services; worker pass every
    5 s with capped concurrency (4), 10 s timeout, exponential backoff (30 s × 2ⁿ, cap 6 h),
    dead-letter after `webhooks.max_attempts`; HMAC `X-WG-Signature` signing; secrets AES-GCM
    encrypted at rest, shown exactly once; 7-day payload retention; event catalog V1
    (user.*/device.*/node.started).
  - Node runtime (`internal/serve` + `wg-guard serve`): config → migrations → master key →
    settings → services → boot bring-up → HTTP(S) listener → central scheduler; TLS modes
    manual/proxy/dev implemented (ACME deferred to the installer phase with a clear error);
    graceful shutdown drains HTTP, finishes the running job, closes the DB; reconcile passes
    serialized across API and accounting; config-gated `/metrics` (Prometheus text);
    `-backend fake` dev/bench mode.
  - `wg-guard token create|list|revoke|scopes` CLI (the token-minting path until Phase 5).
  - Metrics collector (`internal/metrics`): health/ready gates, request-class counters,
    accounting-cycle gauges; `internal/api` route table doubles as the OpenAPI coverage input
    (test-enforced, both directions).
  - Settings: `node.id`, `node.endpoint`, `network.client_allowed_ips`,
    `network.client_persistent_keepalive`, `webhooks.max_attempts`, `api.rate_limit_per_minute`.
  - Dependency: `rsc.io/qr` v0.2.0 (BSD-3-Clause, zero transitive deps) for QR rendering.
- **Phase 3 — limits & accounting** (all unit tested, `-race` clean; tc and real-runtime paths
  integration tested in WSL2):
  - Centralized scheduler (`internal/scheduler`): one goroutine, due-heap, sequential jobs,
    panic recovery, catch-up-once semantics, live interval changes (Phase 4 composes the jobs).
  - Delta accounting pipeline (`internal/accounting`): one dump per interface per cycle, the
    `new < last ⇒ reset ⇒ count current and re-baseline` invariant, one transaction per cycle
    writing only changed rows; restart/recovery safe (kernel-continue and link-recreate paths).
  - Quota & expiry enforcement: edge-triggered `traffic_exceeded` and `expired` transitions with
    audit entries; transitions trigger a reconciliation pass so blocked users actually lose
    their peers. First-connection activation stamps `activated_at`/`expires_at` idempotently.
  - Traffic mutations: reset (one-op unblock, no double counting), add/remove (charged-counter
    corrections with level-check at mutation time); all audit-logged.
  - tc shaper (`internal/shaper`): HTB egress, one class per user with per-device-IP filters,
    rendered-state rebuild via one `tc -b` batch, change detection, restart recovery, cleanup on
    limit removal; restored at bring-up and re-ensured by the accounting cycle. Upload (ingress)
    shaping deferred per docs/architecture/networking.md.
  - Traffic samples & rollups: accumulator flushed on `accounting.sample_flush_seconds`
    (default 300 s), hourly/daily rollup upserts in the same transaction, retention pruning.
  - Settings: `accounting.sample_flush_seconds`, `accounting.sample_retention_hours`,
    `accounting.rollup_hourly_days`, `accounting.rollup_daily_days`.
  - Benchmarks recorded (WSL2): idle cycle @100 devices 0.32 ms; @1000 devices 2.7 ms; @1000
    devices all-active 10.1 ms; sample flush @1000 devices 8.3 ms (budget ≤ 15 ms).
- **Phase 2 — AWG backend & networking** (unit tested, `-race` clean; userspace paths
  integration-tested against the pinned runtime in WSL2):
  - `subprocess` package: the single exec choke point — explicit argv only (never a shell),
    per-command timeout, structured exit errors; stdout (which can contain key material) is
    never embedded in errors or logs (`internal/subprocess`).
  - Real `TunnelBackend`: pinned `awg` CLI driven through the choke point — setconf/syncconf
    renderer, 29-field AWG v3.1 dump parser (field-count gate rejects unknown formats loudly),
    interface lifecycle via iproute2 with link rollback, verify-after-apply gate,
    tools-version probe (`internal/tunnel/amneziawg`).
  - `network` package: `ip link/addr` wrappers with missing-link classification, idempotent
    IPv4 forwarding via sysctl (`internal/network`).
  - `firewall` package: namespaced `table inet wgguard` applied as rendered state (atomic
    `nft -f` delete+recreate, no duplicate rules on re-apply), forward-accept + masquerade,
    ufw/firewalld coexistence detection with idempotent `ufw route allow` for managed
    interfaces (`internal/firewall`).
  - `boot` package: bring-up orchestration (tooling probe → forwarding → reconcile → firewall
    → coexistence) shared by the CLI and the future `serve`; per-interface error collection so
    one broken profile cannot abort bring-up; audit record (`internal/boot`).
  - `wg-guard reconcile` CLI command: full boot bring-up with a human-readable report.
  - Integration tests (`-tags integration`, WSL2/root): real amneziawg-go setconf/syncconf/
    dump round-trip incl. verify gate and runtime constraint rejection; real nftables
    apply/re-apply/remove cycle.
- **Phase 1 — core foundation** (all unit tested, `-race` clean):
  - Boot config (TOML + env overrides) with TLS-exposure validation (`internal/config`).
  - SQLite layer: WAL, busy_timeout, bounded pool, `txlock=immediate` write transactions
    (proven by lost-update test), embedded forward-only migrations (full 17-table contract)
    (`internal/database`, `migrations/`).
  - Typed runtime settings registry with validation, encrypted secret values, redaction,
    concurrency-safe cache (`internal/settings`).
  - Secrets at rest: AES-256-GCM (stdlib), 0600 master-key file, crash-safe master-key
    rotation with dual-key window; settings + device/interface key carriers
    (`internal/secrets`).
  - Authn/authz: argon2id (RFC 9106 low-memory), admin sessions (hashed tokens, idle +
    absolute expiry), API tokens (`wg_`, SHA-256, scopes, CIDR allowlist), centralized
    permission registry, owner-protection rules (`internal/auth`, `internal/token`,
    `internal/admin`).
  - Domain services: plans; users (lifecycle, renew, soft delete/restore); devices
    (device-limit race-safe enforcement, pool allocation with gateway reservation);
    tunnel interfaces/profiles (ports, subnets, obfuscation constraint matrix, presets,
    encrypted server keys) (`internal/plan`, `internal/user`, `internal/device`,
    `internal/iface`).
  - `TunnelBackend` abstraction + in-memory fake backend; key generation via stdlib
    X25519 (`internal/tunnel`, `internal/tunnel/fake`).
  - DB↔backend reconciliation engine with drift policy (report/adopt/remove) and
    stale-peer removal (`internal/reconcile`).
  - Audit log service (`internal/audit`); shared domain types + machine error codes
    (`internal/domain`).
- Documentation system under `docs/` (product, architecture, integrations, operations,
  development, ADRs) with the original specification archived under `docs/archive/`.
- Project scaffold: Go module (`github.com/Sir-Adnan/wg-guard`), `wg-guard` CLI skeleton
  (`version`), Makefile, lint configuration, GitHub Actions CI (fmt/vet/test/race, amd64+arm64
  builds).
- Verified AmneziaWG upstream pinning results (WSL2 Ubuntu 24.04, `ppa:amnezia/ppa`) recorded in
  `docs/integrations/amneziawg.md`.

### Fixed
- **Phase 8 configuration-boundary hardening:** endpoint overrides and I1–I5 now reject
  newline/control injection before persistence, and client rendering fails closed on corrupt
  stored text before decrypting keys. Reconciliation now loads, applies, and drift-compares all
  five I fields and refuses any invalid stored profile before backend mutation. Panel-generated
  profile labels require an exact AES-GCM-sealed, session-bound preview (or an unchanged existing
  generated profile); generated-policy validation matches the generator's exact bands/ranges,
  and S2 sampling is bounded without bias or retry loops. REST regressions and OpenAPI document
  the safe input contract.
- **Phase 8 real-host evidence harness:** the isolated Ubuntu 24.04 gate now tracks ownership for
  every cleanup target, verifies its full external-command and pinned-runtime prerequisites,
  compares canonical API/database/kernel/userspace/client-config state, independently decodes QR
  bytes from REST/admin/subscription surfaces, exercises bidirectional UDP/TCP traffic, and scans
  logs for the run's actual secrets. The harness is implemented and syntax-checked; a successful
  dedicated-VPS run remains required before Phase 8 completion.
- **Multi-interface runtime discovery:** `awg show interfaces` returns names separated by spaces
  on one line, but the backend split only on newlines. With two live interfaces it reported one
  combined foreign name. Parsing now follows the pinned whitespace contract; a reproducing unit
  test and the complete concurrent privileged integration suite pass.
- **Phase 8 migration/restore integrity:** real pre-0007 archives now prove forward migration of
  scalar H values and legacy keepalive while current archives prove exact true-range preservation
  through stage/apply and boot-time consumption, including rollback mirrors and unrelated foreign
  keys. The restore environment review queried a nonexistent table and therefore hid every
  staged tunnel interface; it now reports `tunnel_interfaces`. A privileged pinned-userspace
  integration test also applies, dumps, and reapplies every supported range-bearing field and a
  peer keepalive interval without drift.
- **QR rasterization:** the grayscale canvas is now explicitly initialized white before black
  modules are painted, restoring a valid four-module quiet zone and scannable contrast. The
  previously all-black PNG now fails pre-fix and passes an independent decoder after the fix;
  oversized admin and subscription QR requests return a no-store client error rather than 404.
- **Canonical client configuration delivery:** the renderer now places every AWG client/interface
  field before `[Peer]`, honors the selected tunnel interface's MTU, preserves every supported
  scalar/range field, canonicalizes AllowedIPs, and rejects corrupt stored DNS/AllowedIPs/
  PersistentKeepalive before decrypting keys. A literal full-field golden and cross-surface test
  prove direct, REST, admin, and subscription downloads have identical bytes, filenames, secret-
  safe headers, and exactly one final newline; config mismatches no longer dump keys into CI logs.
- **Phase 8 AWG range/API integrity:** H1–H4 and every u16 timer/keepalive range now preserve
  both endpoints through migration 0007, storage, setconf/dump, drift correction, forms, and
  client rendering. The management API uses explicit lower-snake-case DTOs with scalar-number /
  range-string compatibility, complete OpenAPI coverage, write-only HPK handling, and strict
  rejection of unknown or trailing JSON fields. The client PersistentKeepalive setting now
  accepts `0`, `N`, or `N-M` through one range-aware key while retaining rollback data.
- **Installer: the interactive wizard never asked for the deployment mode** (the Docker
  default was preset, making the mode question dead code) and **`--yes --domain X --tls
  proxy|manual|dev` silently became an ACME install** (the wizard's TLS `askChoice` always
  ran and `--yes` answered with prompt defaults). The mode question now fires (Enter =
  Docker), `--yes` skips prompting entirely, and the TLS question only fires when the mode
  is genuinely unset.
- **Uninstall summary printed a garbled future-tense sentence** ("uninstalled. will be
  purged") — it now states the outcome: "Data purged (…)" / "Data kept at …".
- **Fresh installs produced client configs without an `Endpoint` line**: `node.endpoint`
  is now seeded from the panel domain at install time.
- **CI: three jobs red after the Phase 6 push** (vet, arm64 build, govulncheck — all the same
  root cause): the bare `wg-guard` pattern in `.gitignore` matched the `cmd/wg-guard/`
  DIRECTORY, so every file added there after the rule existed (`backup.go`, `settings.go`,
  `secrets.go`) was silently ignored and never pushed; CI checked out a tree whose `main.go`
  referenced undefined functions. Patterns are now root-anchored and the files committed.
- **UI: nine raw translation keys leaked** (`common.utc`, `hooks.empty_*`, `hooks.status_*`,
  `tokens.empty_*`, `ifaces.obf.randomize_all`) plus a duplicate randomize button bound to a
  non-existent key — all keys added in fa+en, Persian "dead" badge wording naturalized, and
  two audit tests now walk every embedded template against the catalogs so a future leaked
  key fails CI instead of shipping.
- Expiry hints showed "just now"/"همین حالا" for future dates (the relative-time helper was
  past-only and clamped skew); expiry columns now use a future-relative form ("in 3 days").
- Create-user drawer: `X`/Cancel did nothing (close handlers were bound to `dialog.modal`
  only, the drawer is `dialog.drawer`); the calendar popover appended to `<body>` rendered
  *behind* the `<dialog>` top layer — both fixed at the root.
- **CI: flaky token-forgery test** (`internal/token` `TestVerifyRejectsForgedAndRevoked`,
  failed ~1 in 16 runs): the forged token was built by overwriting the last plaintext
  character with a fixed symbol, which reproduces the original whenever the minted token
  already ends in that symbol — the final base64url character of a 32-byte token carries only
  4 bits of entropy. The mutation now guarantees a different token, and an unknown but
  well-formed token is covered explicitly.
- **API: traffic series endpoint** returned errors for rollup queries (nonexistent
  `rx_delta`/`tx_delta` columns) and mixed hourly + daily rows without a granularity filter —
  found while building the dashboard chart; regression test added over the API.
- **Web: small quotas misdisplayed** — edit forms rendered a 0.2 GB limit as `0`
  (`%.0f` rounding), so re-saving an honest value failed or zeroed it; quota values are now
  parsed digit-by-digit into exact bytes and displayed with exact scaled formatting
  (`0.2`, `100 MB`, `6-hour` accounts round-trip; regression tests).
- **Web: onboarding password hint double-formatted** (`%!(EXTRA int=10)` visible on the
  first-run page) — the catalog string was formatted twice (service + template `printf`).

### Changed
- **CI**: the test job runs on the minimum supported toolchain (`1.25.x`, per `go.mod`) in
  addition to `stable`; a `govulncheck` job scans all packages with a pinned tool version
  (documented since Phase 1 but previously missing from the workflow).
  [docs/development/workflow.md](docs/development/workflow.md) synced.
- Phase 2 discoveries pinned in `docs/integrations/amneziawg.md`: the runtime rejects
  explicit-zero obfuscation params (`EINVAL`) and keeps obfuscation params omitted from
  `setconf` — so plain↔obfuscated profile transitions recreate the link (reconcile-owned);
  plain profiles omit the obfuscation block entirely (an earlier explicit-zeros design was
  corrected by the integration test).
- Reconcile engine: per-interface failures are collected into `Report.Errors` (bring-up
  continues; `wg-guard reconcile` exits non-zero) instead of aborting the whole pass; fresh
  create/recreate counts peer adds and forces a peer re-sync.
- `tunnel.InterfaceSpec` gains `Address` (gateway CIDR): a link-creating backend needs the
  interface address at bring-up; the phase-1 draft omitted it.
- Terminology consistency: "management/REST API" (never "public API"); backup password
  documented as strictly optional; ports/MTU/DNS/interface cap framed as recommended
  configurable defaults pending the Phase 11 production matrix.
- `tunnel_interfaces` contract gains `public_key` + `private_key_encrypted`; `users` gains
  `deleted_at` (soft delete); package `internal/iface` (renamed from planned
  `internal/interface`, a Go keyword).

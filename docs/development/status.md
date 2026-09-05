# Status

Feature-level verification matrix, maintained with every phase (AGENTS.md rule: never claim
more than this table says). Statuses: `designed` → `implemented` → `unit tested` →
`integration tested` → `production verified`; items that fundamentally need real hardware stay
marked `requires real VPS`.

## Phase 8.1 — GitHub delivery & lifecycle (active, 2026-09-05)

User-authorized insertion between completed Phase 8 and planned Phase 9. The existing
`codex/phase9-observability` design-only branch is preserved and paused. See
[phase8.1.md](phase8.1.md) for scope and evidence gates.

| Item | Status |
|---|---|
| Bootstrap, release/commit selection and artifact identity | implemented + unit/shell-fixture tested in `5ec46e0`; local amd64/arm64 candidates built/checksummed, amd64 executed on Linux; [CI passed](https://github.com/Sir-Adnan/wg-guard/actions/runs/33995279156); independent review and integrated VPS verification pending |
| Terminal installer and management UX | designed; not implemented |
| OS prerequisites and compatible AWG selection | designed; not implemented |
| Transactional update/rollback and bounded restore improvements | designed; not implemented |
| Backup schedule management and Telegram workflow | designed; not implemented |
| Dedicated Ubuntu 24.04 Docker/native lifecycle verification | pending new implementation; prior Phase 7/8 evidence does not certify this change |

No new REST API is planned; shared CLI/panel services remain authoritative. API/OpenAPI must be
updated if implementation changes their contract. Full matrix certification remains Phase 11.

## Phase 5 — Web UI (complete, 2026-08-31; two refinement passes same day)

The two refinement passes (round 1: subscription links, create-user redesign, users-page
redesign, theme + shell redesign, capability-gated AWG 2.0/3.x parameters; round 2: warm-sand
design system, responsive shell, create-user decluttering + defaults, panel settings screen,
descriptive download filenames, dashboard hierarchy, full AWG randomization, real-VPS kernel
verification) are tracked in [phase5-refinement.md](phase5-refinement.md) with verification
records; asset budgets after round 2 are JS 25.3/30 KiB gz, CSS 10.5/25 KiB gz, fonts
101.8/150 KiB gz.

All items below are **implemented + unit tested** (`go test ./...` green on Windows/Go 1.27 and
WSL2 Ubuntu/Go 1.26; `go test -race ./...` green in WSL2 — Windows has no C toolchain for the
race runtime, so race verification runs in WSL2, same as CI). The panel was additionally
exercised end-to-end in a browser against the running node (`-backend fake`): desktop 1366/1440
and mobile 390 widths, fa/en × light/dark, including login → onboarding → users → devices →
plans → interfaces circuits and the config/QR session gating. Frontend asset budgets are
enforced by `scripts/check-assets.sh`: JS 19.5/30 KiB gz, CSS 8.4/25 KiB gz, fonts
101.8/150 KiB gz.

| Item | Status |
|---|---|
| i18n foundations: embedded fa/en catalogs with enforced key parity, per-admin language persisted to the admin record, `<html lang/dir>` server-side, full RTL via CSS logical properties (one stylesheet both directions), Jalali dates for fa / Gregorian for en, language-neutral LTR data (IPs, keys, counters — Latin digits, tabular numerals) | ✅ implemented + unit tested (parity, conversion, handler-level rendering both locales) |
| Design system + shell: CSS custom-property tokens, light/dark/system themes (`data-theme` + `prefers-color-scheme`, cookie-persisted choice), Lucide SVG sprite (embedded, no icon runtime), one vanilla ES module (drawer, dropdown menus, `<dialog>` modals, password/obfuscation toggles, toasts), responsive shell (sidebar → drawer + scrim below 960 px, focus/aria state synced) | ✅ implemented + unit tested; browser-verified desktop + mobile |
| Session auth pages: login (username/password, show-password, session-expired + account-created alerts, language/theme pickers), first-run onboarding wizard (owner account + node identity), CSRF on all mutating forms, login throttling, session cookie flags | ✅ implemented + unit tested (handler-level: redirects, bad credentials, CSRF rejection) |
| Users: list (search, status filter, cursor pagination, bulk select + enable/disable/delete/renew/reset-traffic/add-traffic), tri-state create/edit form (empty = absent on create, empty = explicit clear on edit), detail page (overview list, renew, add traffic, reset traffic, device management) | ✅ implemented + unit tested (CRUD flows, bulk, permission gating) |
| Devices: add (name + optional IP hint), enable/disable, key regeneration with revocation of the old peer, config download + QR modal, delete with confirm; per-user device limit enforced; config/QR endpoints are session-gated and never log key material | ✅ implemented + unit tested; browser-verified |
| Dashboard: user metric cards (total/active/waiting/online/expiring/expired/exceeded/total traffic), host card (CPU/RAM/disk/load/uptime read from `/proc` on demand — `internal/hoststats`, no background polling; graceful "unavailable" state off Linux), CSP-safe server-rendered SVG traffic chart (24 h/7 d/30 d ranges, zero-filled buckets, nice-axis scaling, SI-suffixed axis labels), live fragment refresh every 30 s with `data-pause-hidden` (Page Visibility hook in app.js), no-JS fallback links | ✅ implemented + unit tested (chart buckets/escaping/geometry, hoststats fixtures incl. degradation, live fragment) |
| Plans: CRUD + enable/disable with per-plan live user counts and interface references; tri-state limit fields (traffic GB, duration days, up/down kbps) | ✅ implemented + unit tested (CRUD flow) |
| Interfaces: CRUD + enable/disable, AWG obfuscation parameter section (jc/jmin/jmax/s1/s2 and lossless scalar/range H1–H4), strict numeric/range parsing in both directions, plain↔obfuscated edit warns that clients must re-import profiles, delete guarded while devices exist, auto port + subnet defaults, immutable name/port/subnet on edit | ✅ implemented + unit tested (CRUD, exact range form round trip in fa/en, malformed/overlap no-mutation regressions) |
| `internal/clientconf`: one config/QR renderer shared by API and web (bounded payload, pinned QR params); the manually scaled raster is initialized white with a four-module quiet zone, and an independent test-only decoder proves exact config equality on direct/REST/admin/subscription paths | ✅ implemented + unit tested + real-browser/VPS/client verified; all HTTP PNGs decode to canonical bytes and those bytes imported into real clients. Physical optical-camera scan was unavailable and is explicitly unperformed. |
| API correctness fixes surfaced by web work: `/api/v1` traffic series used nonexistent `rx_delta`/`tx_delta` columns for rollups and mixed granularities — now correct per-granularity sums (regression test over the API) | ✅ implemented + unit tested |

Refinement pass additions (same status rules as above):

| Item | Status |
|---|---|
| Exact quota/duration parsing: digit-by-digit decimal → bytes/seconds (no float64), GB/MB unit select, hours/days/months duration units, exact expiry date on create (noon-UTC convention), legacy form fields still accepted | ✅ implemented + unit tested (0.2 GB, 100 MB, 6 h round-trips) |
| Subscription links (`internal/subscription` + migration 0004): 256-bit crypto/rand token, SHA-256-hashed lookup + AES-GCM-encrypted re-display, ensured at user creation, regenerate/revoke/restore lifecycle; public rate-limited `/sub/{token}` page (fa/en) with traffic meter, expiry, per-device QR + config download; admin card + list quick actions; `subscription.base_url` setting; tokens never logged (path masked) | ✅ implemented + unit tested (service + handler lifecycle incl. cross-user device isolation) |
| Create-user drawer on the users page (shared partial with the `/users/new` fallback): username generator, settings-driven quota/duration preset chips (`users.quota_presets_gb`, `users.duration_presets_months`), auto device provisioning (count = device limit, cap 10, per-device transactional), Jalali (fa) / Gregorian (en) vanilla-JS calendar — leap-year algorithm exhaustively verified against server-side conversions | ✅ implemented + unit tested (auto-device flow, quota/duration exactness); calendar verified in browser |
| Users page redesign: identity rows, quick-share menu (per-device QR/download + sub-link copy, batch device load), fixed-coordinate menu positioning (no clipping in overflow containers; close on scroll) | ✅ implemented + unit tested; browser-verified desktop + 390 px mobile |
| Theme + shell refinement: zinc-neutral light palette, refined dark palette (`#09090b` base), 8/12 px radii, component polish (buttons, inputs, badges, tables, menus, dialogs, empty states, metric icon chips), desktop sidebar collapse to a persisted 68 px icon rail with tooltips, compact footer icon row | ✅ implemented; palettes/collapse verified live in browser |
| Interfaces: explicit advanced 2.0/3.x parameters (S3, S4, HeaderProtectionKey, ContentPaddingAddition, RekeyAfterTime, RekeyTimeout, RejectAfterTime, KeepaliveTimeout, MaxHandshakeAttempts, RandomTrailers, DisableCookies) through the full chain — migration 0005 plus lossless range migration 0007, iface validation, reconcile spec, setconf rendering, dump parsing, client-config parity; value formats verified from pinned `config.c`; every dump-observable field including I1–I5 is applied, compared, and corrected exactly, while HPK removal recreates the link; endpoint/I values have injection-safe single-line validation and corrupt rows fail closed | ✅ implemented + unit tested; complete kernel-module round-trip verified on a real VPS, supported recommended/randomized profiles passed real kernel clients, and recommended passed pinned userspace traffic. Client-specific unsafe fields remain gated rather than advertised across every app/platform. |

Round 2 refinement additions (same status rules as above):

| Item | Status |
|---|---|
| Warm-sand design system: warm neutral light palette + ink primary + petrol brand accent, lifted warm dark mode (no pure black), layered surface/page-glow tokens, recolored charts/favicon/auth/public pages; shadcn-grade component language without copying it | ✅ implemented; verified live in browser (fa/en × light/dark) |
| Responsive shell: content max-width grows with viewport (1400 px, 1560 px ≥1920), tablet default collapsed rail (≤1180 px, only without an explicit operator preference), table→card switch at 800 px, small-phone density + safe-area insets, `viewport-fit` + `theme-color` metas | ✅ implemented; verified at 1366 + 390 px, no horizontal overflow |
| Create-user redesign: segmented "packages / custom" traffic, segmented "duration / exact date / no expiry" (picker and calendar never shown together), live Jalali-aware expiry preview (`RelIn` future-relative hints), compact configs stepper replacing checkbox+limit duplication, advanced options collapsed (plan/interface/policy/speeds/tags); drawer close fix (`dialog` binding) and calendar mounted inside the `<dialog>` top layer | ✅ implemented + unit tested; drawer flows verified live (generator → defaults → calendar pick → ISO write → create → auto-device) |
| Create-form defaults from settings (`users.default_quota_gb`, `users.default_duration_months`, `users.default_device_limit`, `users.default_iface_id` with configured-id→first-enabled fallback): the normal path is fill-username → Create | ✅ implemented + unit tested; prefill verified live |
| Panel settings screen (`/settings`, nav item added): traffic/duration package lists, create defaults, default interface, `subscription.base_url`, download filename prefix/suffix — non-secret registry keys only, every write through registry validators with submitted-value redisplay on error. Phase 6 screens (admins/tokens/webhooks/backups/audit) deliberately still absent | ✅ implemented + unit tested (save/redisplay/prefill round-trip); verified live |
| Descriptive config download filenames: `[prefix]username-device[suffix].conf` (`downloads.filename_prefix`/`_suffix`), sanitized ASCII parts (`internal/clientconf.ConfigFilename`), applied uniformly to web, API and public sub-page downloads; QR responses get matching inline `.png` names; documented in OpenAPI | ✅ implemented + unit tested; Content-Disposition verified live with and without prefix |
| Dashboard hierarchy: "needs attention" card (expiring ≤7 d / quota-exhausted / expired — status+expiry-indexed queries, capped at 5 rows each, rendered only when non-empty), traffic tile promoted, single page title (topbar shows the brand, pages own their `h1`), balanced metric grid | ✅ implemented + unit tested (attention lists incl. seeded statuses); verified live in fa/dark + en/light |
| Interfaces form: "Randomize all parameters" (crypto.getRandomValues in-page: junk sizes, init packets, distinct magic headers, 32-byte base64 header-protection key, padding + timer ranges), richer enable-prefill (padding `10-100` added), HPK⇒S3/S4 kernel constraint enforced in validation + hinted in UI, gated-warning text updated to kernel-verified status | ✅ implemented + unit tested (validation cases); full-generation create verified live |
| Real-VPS verification environment: dedicated disposable Ubuntu 24.04 KVM node; pinned PPA `amneziawg-tools` + DKMS kernel module; runtime parameter matrix (acceptance, round-trip, constraint enforcement, clearing semantics, setconf header requirement) executed against the kernel module | ✅ executed 2026-08-31 — evidence in `docs/integrations/fixtures/verify-vps-kernel-matrix.txt`; integration test suite run recorded in the phase 5 refinement doc |

Deferred / honest notes within Phase 5 scope:

- Phase 6 screens (administrators, API tokens, webhooks, audit log, backup/restore) are
  deliberately absent; the settings screen added in round 2 covers only the non-secret panel
  knobs and is explicitly not the Phase 6 settings area.
- Browser QA ran through in-app browser automation; the screenshot pipeline was intermittently
  unavailable, so a few mobile-layout checks were verified via DOM geometry (scroll widths,
  overflow probes) instead of pixel inspection. A short human pass on real phone hardware is
  still worthwhile before release.
- Client-app compatibility for the 2.0/3.x obfuscation set (incl. timer/padding ranges) varies
  per platform; the parameters stay capability-gated off-by-default with report-only drift.
  Phase 8 source review corrected `AdvancedSecurity` from "deferred" to **unsupported** at the
  pins: the kernel setter ignores it, userspace rejects it, and ordinary dumps omit it.
- Aggregate "sing-box/clash subscription" format endpoints remain a candidate for a later
  phase (needs an upstream-supported format decision — not assumed).
- The 30 s live-refresh pause-on-hidden behavior is covered by the attribute + JS hook; its
  visual effect was observed manually, not soak-tested.
- The advanced 2.0/3.x obfuscation parameters are parser-, kernel-, and userspace-observable at
  the pinned revisions, so Phase 8 corrects their drift exactly. The supported generated subsets
  passed real isolated clients; client-specific unsafe fields remain off by default pending the
  broader Phase 11 platform/client matrix.
- The subscription page exposes per-device configs to whoever holds the (unguessable) link;
  the link is treated as a capability credential — rotate or revoke it to cut access.

## Phase 4 — REST API (complete, 2026-08-30)

All items below are **implemented + unit tested** (`go test ./...` green on Windows/Go 1.27 and
WSL2 Ubuntu/Go 1.26; `go test -race ./...` green in WSL2 — zero failures across 31 packages).
Where marked **integration tested**, behavior was exercised against real iproute2 `tc` (egress
HTB and the IFB ingress path) and the real pinned userspace runtime in WSL2
(`go test -tags integration`, root).

| Item | Status |
|---|---|
| Full `/api/v1` management surface: users (CRUD, enable/disable, renew, traffic add/set/reset, traffic series), bulk create + bulk actions (enable, disable, delete, renew, reset_traffic, add_traffic, update), devices (CRUD, enable/disable, key regeneration with old-key revocation, config + QR download), plans, interfaces (explicit lossless AWG DTOs; write-only HPK), settings (secrets redacted as `value: null` + `secret_set`; PATCH validates per registry), webhooks (CRUD, redeliver), user/device/node stats, public `node/health` | ✅ implemented + unit tested (handler-level over the full middleware chain; unknown/trailing JSON rejection and OpenAPI/DTO range parity) |
| Route table as single source of truth: one `routeDef` table drives the mux, the OpenAPI document AND the coverage test (every route documented with correct scope/pagination/idempotency; reverse check: every documented operation is registered; mux smoke: anonymous requests never receive a bare route-miss 404 envelope) | ✅ implemented + unit tested (both directions + smoke) |
| Middleware chain: request-id (client-supplied validated), panic-recover, security headers, CORS (API only), 1 MiB body cap, structured request logging (method/path/status/duration/request_id — never bodies, query strings or auth headers), authn/authz (any token verification failure = 401; scope gap = 403) | ✅ implemented + unit tested |
| Error envelope `{"error":{code,message,request_id}}` with stable machine codes; every domain error maps to exactly one HTTP status family; `X-Request-Id` echoed/set on every response | ✅ implemented + unit tested |
| Keyset cursor pagination: `limit` ≤ 500, opaque base64url cursor, `julianday()` ordering (RFC3339Nano strings do not sort lexicographically), id tiebreak for same-microsecond rows (deterministic, not insertion-ordered within one µs), NULL `expires_at` sentinel, full filter set incl. literal `%`/`_` LIKE semantics | ✅ implemented + unit tested (cursor walk over full sets, filter/sort matrix) |
| Idempotency keys: 1–128 printable, `method+path+body` hash, PK-insert claim arbiter, response snapshot replay with `Idempotency-Replayed: true`, 409 `IDEMPOTENCY_KEY_REUSED` on hash mismatch, claim released on failure, 24 h TTL pruned by housekeeping | ✅ implemented + unit tested (replay, conflict, release-on-failure) |
| Per-token rate limiting: fixed 60 s window, `X-RateLimit-*` + `Retry-After` headers, 429 `RATE_LIMITED`, `api.rate_limit_per_minute` (default 600, 0 = off) applied live without restart | ✅ implemented + unit tested |
| Tri-state PATCH semantics (`domain.Opt*`): absent = no change, JSON null = clear to unlimited/none, value = set — `encoding/json` snake_case keys are DTO-owned; independent direction updates verified end-to-end | ✅ implemented + unit tested |
| **Independent up/down speed limits** (`speed_limit_down_kbps`/`speed_limit_up_kbps` on users + plans; migration 0002 converts Phase 3's single column in-transaction): download = tc HTB egress on the interface, upload = ingress qdisc + `mirred egress redirect` into `ifb-<iface>` with an HTB tree matching client SOURCE IPs; directions apply/rebuild/clean up independently; either unset = unlimited; missing IFB support fails upload only with an explicit error while download keeps working | ✅ implemented + unit tested (golden renders, direction independence, determinism, IFB-failure confinement); **integration tested** against real tc + real IFB in WSL2 |
| Durable webhooks: event row inserted in the SAME transaction as the state change (recorder seam injected into user/device/accounting), per-endpoint delivery rows fan out at emit time; worker pass every 5 s — indexed due query, capped concurrency (4), 10 s timeout, backoff 30 s × 2ⁿ cap 6 h, dead after `webhooks.max_attempts` (12), HMAC `X-WG-Signature: t=<ts>,v1=<hex>`; secrets AES-GCM encrypted at rest, shown exactly once, rotatable, never echoed or logged; redeliver resets pending; events pruned after 7 d | ✅ implemented + unit tested (delivery + signature verify over HTTP, backoff/dead-letter arithmetic, redeliver, prune cascade, rotation); production endpoint behavior **requires real VPS** |
| Lifecycle webhook events V1: user.created/updated/enabled/disabled/expired/traffic_exceeded/first_connected, device.created/deleted, node.started (emitted once the node is serving) | ✅ implemented + unit tested |
| Node runtime (`internal/serve` + `wg-guard serve`): config → mkdirs → DB/migrations → master key → settings (node.id hostname fill) → services → boot bring-up (findings logged, tooling drift reported) → TLS listener → scheduler; HTTP timeouts (10 s header / 30 s read / 60 s write / 120 s idle, 64 KiB headers); graceful shutdown drains HTTP → finishes the running job → closes DB; `-backend fake` dev/bench mode (no host networking, loud warning) | ✅ implemented + unit tested (lifecycle, restart over the same data dir, manual-TLS serving with TLS-1.2 floor, ACME deferral error, full API through the running node) |
| TLS modes: manual (TLS 1.2 min), proxy (plain, explicit), dev (loopback-only plaintext enforced by config validation); ACME designed (ADR-0011) and rejected with a clear message until the Phase 7 installer | ✅ manual/proxy/dev implemented + unit tested; ACME deferred by design |
| Scheduler composition: accounting cycle + expiry pass (`accounting.interval_seconds`, live re-applied from inside the job), sample flush (`accounting.sample_flush_seconds`), webhook pass (5 s), housekeeping (10 min: idempotency + session + traffic-history + webhook prunes, rate-limit reload); ONE goroutine | ✅ implemented + unit tested (scheduler semantics Phase 3; composition exercised in serve lifecycle tests) |
| Serialized reconciler: boot, accounting/enforcement and API-triggered reconcile passes share one engine behind a mutex — concurrent AWG operations on one interface cannot interleave | ✅ implemented + unit tested (composition) |
| Metrics & health: `/healthz` (liveness) + `/readyz` (bring-up done + DB ping) public; `/metrics` Prometheus-text endpoint **config-gated off by default** (uptime, request classes, accounting cycle stats, goroutines, heap) | ✅ implemented + unit tested |
| `wg-guard token create/list/revoke/scopes` CLI: mints/inspects tokens without boot (migrations included, works on a fresh node); plaintext printed once, never stored/logged; least-privilege scopes required at create | ✅ implemented + unit tested via binary smoke test |
| OpenAPI 3.0.3 + no-JS `/docs` reference: hand-authored, coverage-tested (see route table above) | ✅ implemented + unit tested |
| Settings added: `node.id`, `node.endpoint`, `network.client_allowed_ips`, range-aware `network.client_persistent_keepalive`, `webhooks.max_attempts`, `api.rate_limit_per_minute` | ✅ implemented + unit tested (registry and bilingual form validation; migration 0007 preserves the legacy scalar value) |
| Behavior correction: `SetStatus` now keeps the `enabled` flag consistent with lifecycle status (disabled/suspended/expired/traffic_exceeded ⇒ disabled; active/waiting ⇒ enabled) — Phase 1's set-status left `enabled=true` on disabled accounts, which also gated device creation | ✅ implemented + unit tested (documented here as a deliberate fix) |
| Benchmarks — API (Go bench over `httptest`, rate limiter off): 20-row user list @1000 users **2.4 ms** (WSL2/Go 1.26; **1.0 ms** on Windows/Go 1.27); LIKE search @1000 **2.4 ms**; full 1000-row cursor walk (50 pages) **83 ms**; bulk create 100 **3.7 ms**; device config render incl. AES-GCM key decrypt **65 µs**. Idle RSS/CPU via `scripts/bench-idle.sh` (`-backend fake`, WSL2 Ubuntu, 10-min windows): @100 users+devices **18 MB / 0.01 % CPU** (budget 50 MB / 0.5 %); @1000 users+devices **21 MB / 0.02 % CPU** (budget 80 MB / 0.5 %) — both §8 stress points met with ≥ 3.8× memory and 25× CPU headroom. Stripped linux/amd64 binary 13.8 MB (budget ≤ 30 MB) | ✅ measured |

Deferred within Phase 4 scope (honest notes):

- **ACME/autocert is not implemented** — designed in ADR-0011, implementation deliberately
  deferred to the Phase 7 installer (it brings the x/crypto autocert import and port-80
  lifecycle that belong with deployment); `tls.mode=acme` fails with a clear, actionable error.
- **Webhook delivery against real production endpoints** (real receivers, TLS trust chains,
  real replay windows) **requires real VPS** — the HTTP receiver used in tests verifies
  signatures and backoff semantics locally.
- **Real-traffic shaping measurement** (1000-shaped-peer tc benchmark, production degradation
  policy) remains Phase 11; correctness of both directions is integration
  tested.
- **Regenerate-key revocation of a stale peer is best-effort by IP match**: the old peer dies
  on the next reconcile pass because the device IP is re-applied under the new key; if the
  backend lost the interface entirely, recreation removes everything anyway.

## Phase 3 — Limits & accounting (complete, 2026-08-30)

All items below are **implemented + unit tested** (`go test ./...` green on Windows/Go 1.27 and
WSL2 Ubuntu/Go 1.26; `go test -race ./...` green in WSL2 — zero failures across 26 packages).
Where marked **integration tested**, behavior was exercised against real iproute2 `tc` and the
real pinned userspace runtime in WSL2 (`go test -tags integration`, root).

| Item | Status |
|---|---|
| `scheduler` package: the single centralized scheduler — one goroutine, due-heap, sequential jobs (no overlap, no per-user timers), panic-recovered jobs, catch-up runs a missed job ONCE (anchored at finish+interval, never N back-to-back runs), live interval re-registration (incl. from inside the running job), one-shot `At` for future retries (webhooks, Phase 4) | ✅ implemented + unit tested (deterministic step-level tests incl. replace-while-running and hot-loop regression) |
| `accounting` delta cycle: one dump per enabled interface per cycle, delta invariant `new < last ⇒ reset ⇒ count current from zero and re-baseline`, one BEGIN IMMEDIATE transaction per cycle, only changed rows written (idle devices/users are not rewritten — asserted via `updated_at`) | ✅ implemented + unit tested (fake backend simulates counters/handshakes) |
| Restart/recovery: totals live in SQLite; a fresh Service over the same DB counts down-time traffic when the backend kept counters (kernel case) and takes the reset path when the link was recreated (userspace case) — no usage loss, no double counting | ✅ implemented + unit tested |
| First-connection activation: first observed handshake on a waiting-first-connection user stamps `activated_at`, sets `expires_at = now + duration`, flips to active; idempotent (later cycles never move the dates); a never-activated account returns to waiting (not active) on traffic reset | ✅ implemented + unit tested |
| Quota enforcement (RX+TX): edge-triggered — an active/waiting account at/over its limit flips to `traffic_exceeded` (`disable_reason=traffic_limit`) exactly once with an audit entry; deltas keep counting for blocked accounts; admin-blocked and soft-deleted users are counted but never re-labelled. Recovery is an admin action (reset/add/remove traffic), never automatic | ✅ implemented + unit tested |
| Expiry enforcement: set-based pass — live users past `expires_at` in `active`/`waiting_first_connection` flip to `expired` + audit; already-blocked accounts keep their status (renewal semantics differ per status); deleted users untouched | ✅ implemented + unit tested |
| Enforcement stops traffic: transitions trigger a reconcile pass through the `Reconciler` seam (expired/quota-exceeded users lose their peers — not just a status flip) | ✅ implemented + unit tested |
| Traffic mutations: `ResetTraffic` (zero user + device totals, one-op unblock, baselines kept so no double counting), `AddTraffic`/`RemoveTraffic` (charged-counter corrections, rx-then-tx, floored at zero, saturating add; level-check at mutation time trips or reactivates); all audit-logged | ✅ implemented + unit tested |
| `shaper` package: tc HTB per interface — one class per (user, interface) with one u32 filter per device IP (aggregate enforcement of the user-level limit), unshaped users pass via HTB direct service (`default 0`); rendered-state rebuild = `qdisc del` + one `tc -b` batch (`qdisc add`; `replace` rejected by HTB — verified live); change detection (identical desired state = zero subprocesses); first ensure per process = restart recovery; cleanup on limit removal | ✅ implemented + unit tested (golden renders, determinism, change detection); **integration tested** against real tc in WSL2 (apply/no-dup-rebuild/cleanup on a dummy interface) |
| Shaper wiring: restored at bring-up (`boot`, tc failures are non-fatal findings with remedy), re-ensured by the accounting cycle within one interval of a limit change | ✅ implemented + unit tested |
| Traffic samples & rollups: deltas buffered in an in-memory accumulator, flushed every `accounting.sample_flush_seconds` (default 300 s) — one transaction upserting `traffic_samples` (bucket-aligned) and accumulating hourly/daily `traffic_rollups` (same txn ⇒ no double count); prune enforces sample (24–48 h), hourly (30 d) and daily (1 y) retention settings | ✅ implemented + unit tested (bucket spanning, accumulation, prune) |
| Accounting cycle against the real pinned runtime: real 29-field dump → delta pipeline → correct no-op state for a non-handshaken peer | ✅ **integration tested** in WSL2 (amneziawg-go v3.1.20260828); real-traffic counting **requires real VPS** |
| Benchmarks (WSL2, Go 1.26, `go test -bench`): idle cycle @100 devices **0.32 ms**; idle cycle @1000 devices **2.7 ms**; cycle @1000 devices all-active **10.1 ms**; sample flush @1000 devices **8.3 ms** — the ≤ 15 ms/cycle budget holds with 8× headroom at the stress point | ✅ measured (budget: archive proposal §8) |
| Settings added: `accounting.sample_flush_seconds` (300), `accounting.sample_retention_hours` (48), `accounting.rollup_hourly_days` (30), `accounting.rollup_daily_days` (365) | ✅ implemented + unit tested (registry validation) |

Deferred within Phase 3 scope (honest notes):

- **Upload (ingress) shaping was deferred in Phase 3** — docs/architecture/networking.md pinned
  "tc (HTB) per device IP on the interface egress" and "separate upload/download designed for
  later". Independent IFB ingress shaping was subsequently implemented in Phase 4; its
  production-scale benchmark belongs to Phase 11.
- **Per-user speed limits apply to tunnel egress only** and require `tc` (iproute2) on the host;
  a missing tc is surfaced as a boot finding, never silently ignored when limits are configured.
- **Scheduler composition into a long-running service lands with `serve` (Phase 4)** — the
  package, its jobs contract, and all catch-up/panic/replace semantics are implemented and
  tested; `serve` will register the accounting/expiry/flush/prune jobs and the webhook worker.
- RSS/idle-CPU soak (`scripts/bench-idle.sh`) remains a Phase 4/8 deliverable once the server
  runs; static budgets hold (stripped linux/amd64 binary 8.1 MB, ≤ 30 MB budget).

## Phase 2 — AWG backend & networking (complete, 2026-08-29)

All items below are **implemented + unit tested** (`go test ./...` green on Windows/Go 1.27;
`go test -race ./...` green in WSL2 Ubuntu/Go 1.26 — zero failures across all 22 packages).
Where marked **integration tested**, the behavior was exercised against the real pinned
runtime in WSL2 (`go test -tags integration`, amneziawg-tools v3.1.20260812 +
amneziawg-go v3.1.20260828, and real nftables).

| Item | Status |
|---|---|
| `subprocess` package: the single exec choke point — explicit argv, per-command timeout, structured exit errors (stderr only in errors; stdout never logged) | ✅ implemented + unit tested (timeout behavior verified on Linux) |
| `awg` conf renderer: setconf (full interface + peers) and syncconf (peers-only); explicit listen port always; PSK/endpoint/keepalive/I1–I5 omitted when unset so syncconf preserves stored values | ✅ implemented + unit tested (golden assertions) |
| 29-field dump parser: AWG v3.1 interface line + 8-field peer lines; field-count gate rejects stock-WireGuard (4-field) and unknown formats loudly; H `N-M` tolerance; `(none)`/`(null)`/`off` sentinels | ✅ implemented + unit tested against the Phase 0 fixture |
| Real backend: `ListInterfaces` (pinned whitespace-separated multi-name output) / `CreateInterface` (ip link add → setconf → verify → addr → up, link rollback on any failure) / `RemoveInterface` / `ApplyInterfaceConfig` (setconf + verify-after-apply) / `SyncPeers` (syncconf) / `Dump` (missing interface → canonical not-found) | ✅ implemented + unit tested (scripted runner asserts exact argv, file contents, rollback); **integration tested** against simultaneous pinned userspace daemon interfaces |
| Verify-after-apply gate: post-apply dump must match applied key/port/obfuscation, or hard error (converts silent upstream mismatches into visible failures instead of reconcile churn) | ✅ implemented + unit tested; **integration tested** (caught the real persist-params behavior below) |
| Pinned runtime facts discovered: explicit-zero obfuscation block → `EINVAL`; omitted obfuscation keys persist across setconf ⇒ plain↔obfuscated transitions recreate the link | ✅ verified in WSL2; documented in [../integrations/amneziawg.md](../integrations/amneziawg.md) |
| `network` package: `ip link add <name> type amneziawg` (+mtu), addr, up, delete (missing-link classification), `sysctl` IPv4 forwarding (idempotent, read-before-write) | ✅ implemented + unit tested (scripted `ip`/`sysctl`); sysctl verified live in WSL2 |
| `firewall` package: rendered-state `table inet wgguard` (forward accept priority 10, masquerade postrouting priority 100, rules commented), atomic delete+recreate via `nft -f`, probe-based idempotency, `Remove` tolerant of absence | ✅ implemented + unit tested (scripted nft); **integration tested** against real nftables in WSL2 (apply/re-apply-no-duplicates/remove) |
| Firewall-manager coexistence: ufw detection (status/verbose, routed-policy parsing), idempotent `ufw route allow in on awgN` when ufw runs, firewalld detection, findings with remedies | ✅ implemented + unit tested; ufw/firewalld behavior on a production host **requires real VPS** |
| `boot` package: tooling probe → IPv4 forwarding → reconcile → firewall → coexistence, with per-interface error collection (one broken profile cannot abort bring-up) and audit record | ✅ implemented + unit tested (fake backend + scripted runner) |
| `wg-guard reconcile` CLI: boot bring-up outside the service; prints versions/counters/drift/errors/coexistence findings; non-zero exit when an interface failed | ✅ implemented (manual run on VPS pending) |
| Reconcile engine refinements: obfuscation-mode transitions recreate the link and re-sync peers; fresh-create peer adds counted; per-interface error collection | ✅ implemented + unit tested |
| `TunnelBackend` spec completion: `InterfaceSpec.Address` (gateway CIDR) — a link-creating backend needs the address at bring-up; phase-1 draft omitted it | ✅ implemented + unit tested |

Deferred within Phase 2 scope (honest notes):

- The Phase 2 kernel link path was initially unit-tested only because WSL2 cannot load the
  module. It was subsequently verified on the Ubuntu 24.04 VPS, including netlink dump and
  advanced setconf behavior; the broader OS/architecture matrix belongs to Phase 11.
- **Userspace daemon supervision** (spawn/adopt/monitor/stop `amneziawg-go`, restart behavior)
  did not land with serve/installer. The Phase 8 audit confirmed that `backend_mode` is currently
  metadata: boot/reconciliation always use the kernel-link path and node status echoes intent,
  not observation. Config/dump paths are backend-transparent against a manually started daemon;
  automatic fallback and honest observed-mode reporting are Phase 11 AUD-019 gates.
- **RSS/idle-CPU measurement** remains a Phase 3/8 deliverable once the server runs; static
  budgets hold (stripped linux/amd64 binary 8.1 MB, ≤30 MB budget).
- `ufw status verbose` routed-policy parsing is based on documented upstream output formats;
  edge variants will be confirmed on the VPS matrix (the ensure-route call runs regardless, so
  the classic failure mode is covered even if parsing misses a variant).

## Phase 1 — Core foundation (complete, 2026-08-29)

All items below are **implemented + unit tested** (`go test ./...` green on Windows/Go 1.27;
`go test -race ./...` green in WSL2 Ubuntu/Go 1.26 across all 17 packages), unless noted.

| Item | Status |
|---|---|
| Boot config (TOML + env overrides, TLS-mode security validation) | ✅ implemented + unit tested |
| SQLite open (WAL, busy_timeout, bounded page cache, capped pool) + embedded migrations | ✅ implemented + unit tested |
| Write-transaction serialization (BEGIN IMMEDIATE) — the invariant behind device limits & IP allocation | ✅ implemented + proven by a 16-goroutine lost-update test |
| Settings registry: typed catalog, defaults, validators, secrets encrypted at rest, redaction, concurrency | ✅ implemented + unit tested (incl. -race concurrency test) |
| Secret store: AES-256-GCM (stdlib), master key file 0600, crash-safe rotation (dual-key window) | ✅ implemented + unit tested (rotation abort + prev-key recovery) |
| Permission registry (scopes, family wildcards) + authz matrix | ✅ implemented + table-driven matrix test |
| Password hashing argon2id (RFC 9106 low-memory profile) | ✅ implemented + unit tested |
| Admin sessions: hashed tokens, absolute + idle expiry, revocation, prune | ✅ implemented + unit tested |
| API tokens: `wg_` prefix, SHA-256 at rest, scopes, expiry, CIDR allowlist, revocation | ✅ implemented + unit tested |
| Admin accounts: owner bootstrap-once, owner-protection rules, session revocation on password change/disable | ✅ implemented + unit tested |
| Interface/profile service: port allocation, subnet validation/overlap, MTU, obfuscation constraint matrix (kernel-README set), presets, interface count cap from settings | ✅ implemented + unit tested |
| Interface server keypair: generated (stdlib X25519), private key encrypted at rest | ✅ implemented + unit tested |
| User service: creation (immediate/first_connection), lifecycle transitions, renew modes, soft delete/restore, username reservation | ✅ implemented + unit tested |
| Device service: **device-limit race test (50 goroutines vs limit 10 → exactly 10)**, pool allocation with gateway reservation + exhaustion, duplicate-key rejection, IP release on delete | ✅ implemented + unit tested (race-tested) |
| `TunnelBackend` interface + in-memory fake (setconf/syncconf/dump semantics, failure injection) | ✅ implemented + unit tested (incl. -race) |
| Reconcile engine: recreate missing, correct param drift, re-apply missing peers, remove stale peers, drift policy (report/adopt/remove) for truly unknown peers, foreign interfaces untouched | ✅ implemented + unit tested against the fake |
| Audit service (append, recent, prune) | ✅ implemented + unit tested |

Deferred within Phase 1 scope (honest notes):

- **CLI**: `serve` remains a stub — the HTTP layer arrives in Phase 4; no user-facing CLI
  commands were added in Phase 1 (nothing to operate yet).
- **Resource measurement**: static budgets hold (stripped linux/amd64 CLI binary 1.5 MB;
  12.6 MB test-binary upper bound with SQLite linked — well under the ≤30 MB budget; pool
  capped at 8 conns; argon2id memory cost only on login). RSS/idle-CPU measurement is a
  Phase 4/8 deliverable (`scripts/bench-idle.sh`) once the server runs.
- **Key rotation CLI/UI trigger** lands with Phase 6 (the engine + carriers are done and tested).

## Phase 0 — Documentation & scaffold (complete, 2026-08-29)

| Item | Status |
|---|---|
| Documentation tree restructured; original spec archived | ✅ implemented |
| Repo scaffold (module, CLI skeleton, Makefile, lint config, CI) | ✅ implemented + unit tested (`go build/test` green) |
| Go toolchain (local + CI) | ✅ verified (Go 1.27 local, stable in CI) |
| Amneziawg-tools pin + parser/dump/UAPI facts | ✅ verified in WSL2 — [../integrations/amneziawg.md](../integrations/amneziawg.md) |
| Userspace daemon (v3.1.20260828) build + runtime | ✅ verified in WSL2 (TUN, setconf, dump) |
| DKMS module build | ✅ build verified; ⚠️ module load + netlink dump **requires real VPS** |
| PPA on Ubuntu 26.04 | ✅ verified with noble-suite pin (workaround documented) |

## Phase 6 — Backup / ops (complete, 2026-08-31)

All items below are **implemented + unit tested** (`go test ./...` green on Windows/Go 1.27 and
WSL2 `-race` 36 packages; `go test -tags integration ./...` green on the real VPS kernel
host — 36 packages, 0 FAIL). Real-VPS verification of the operational drills is recorded per
line and in [phase6.md](phase6.md), including **two real-host defects the phase found and
fixed**: the reconcile engine's link-creation spec omitted the decrypted interface private key
(empty-key setconf rejected by the pinned kernel tooling), and the kernel module's plain-profile
dump baseline (H1..H4 = 1,2,3,4) tripped verify-after-apply — both regression-tested and
re-verified on the VPS. The phase also rebased the visual system on the shadcn-style neutral
direction (white light / near-black dark, zinc scale, ink primary — [product/ui-ux.md](../product/ui-ux.md))
and added content-width tiers so forms/settings render centered on a narrow column while
tables/dashboards keep the fluid width.

| Item | Status |
|---|---|
| `internal/backup` archive engine: `.wgg` = tar.gz {manifest.json with per-file SHA-256, `VACUUM INTO` snapshot, boot config (source file or synthesized), master key}; optional age encryption with the single backup password (ADR-0008, `filippo.io/age` — the phase's only new dependency); retention pruning; atomic publish (0600) | ✅ implemented + unit tested (plain + encrypted round-trips, corrupt/truncated rejection, retention, wrong-password) |
| Scheduled backups: `backup_schedules` table (migration 0006 replaces the Phase 1 placeholder), daily/interval/weekly kinds stored UTC, once-per-minute due scan on the central scheduler, missed window runs exactly once, per-schedule retention | ✅ implemented + unit tested (fake clock due/advance/no-refire); **fired on the real VPS** (due scan → archive, retention kept 2) |
| Delivery sinks: local (default, `<data>/backups` 0700/0600) + Telegram `sendDocument` (credentials encrypted at rest, 50 MB limit guard, unencrypted-delivery warning); `BackupSink` interface open for future sinks | ✅ implemented + unit tested (httpest stub verifies multipart shape + no-token-in-errors); real Bot-API delivery **requires a live bot — not verified** |
| Restore engine: Stage (decrypt → untar with member allowlist → checksums incl. container CRC drain → schema gate vs embedded migrations → out-of-place migrate → integrity check) → environment report; ApplyStaged swaps with `*.pre-restore` safety copies and never touches the live boot config (staged as `*.restored`) | ✅ implemented + unit tested; **restore round-trip drilled on the real VPS** (staged-at-boot consumption with safety copies + audit, and immediate CLI apply with the service stopped) |
| Boot consumption: `serve` consumes `restore.pending` BEFORE opening the database (audit-logged); failures never abort boot. Automatic pre-migration backups land in `backups-auto` (plain, retention 5) | ✅ implemented + unit tested; **verified on the real VPS** (pre-migration path exercised at boot on the drill node) |
| CLI: `backup create|list|telegram-test`, `restore` (service-running guard; stages when up), `settings list|get|set` (secrets masked, writes audit-logged), `doctor [--fix]`, `secrets rotate` | ✅ implemented + unit tested (doctor) + CLI smoke; **doctor, rotation, settings and restore exercised on the real VPS** |
| `doctor`: platform/privileges/paths/perms/tools/module/DB integrity/interface drift + peer counts/nft/sysctl/tc/disk/endpoint DNS/TLS cert expiry/NTP (timedatectl)/backup posture; honest `skip` per platform; `--fix` reuses the boot orchestration and refuses while the service runs | ✅ implemented + unit tested; **executed on the real VPS** (read-only checks + `--fix` refusal/repairs recorded in phase6.md) |
| Master-key rotation trigger: `secrets.Rotate` wired with all three carriers — device keys, **interface private keys (carrier added in this phase; rotation previously would have orphaned them)**, encrypted settings; crash-safe dual-key window | ✅ implemented + unit tested; **rotation round-trip on the real VPS** (device configs decrypt under the rotated key; kernel peer intact) |
| Panel `/backups` (backup.manage): archive table (badges, download/restore/delete), create-now modal (optional password), restore wizard (stage → environment review → confirm stages for restart / discard), schedule CRUD with segmented kind picker, telegram status + probe, pending-restore banner | ✅ implemented + unit tested (handler-level: lifecycle, validation redisplay, gating); browser-verified fa/en × light/dark |
| Panel ops screens: `/admins` (roles + permission matrix, password reset, owner protection surfaced), `/tokens` (show-once plaintext with select-on-focus, CIDR, expiry), `/webhooks` (CRUD, generated secret shown once, rotation, per-endpoint delivery list + redeliver — panel-only by design), `/audit` (keyset pages, action-prefix/actor filters, expandable metadata) | ✅ implemented + unit tested incl. permission gating (limited admins redirected; webhooks.read can view but not mutate) |
| Full settings UI (node.settings scope): identity, users defaults/presets, networking (MTU/DNS/AllowedIPs/ports/interface cap/drift policy), accounting + retention, API rate limit + webhook attempts + session TTLs, subscription/downloads, backups (retention, telegram chat, write-only age password + bot token with set/clear semantics; registry enforces ≥8 chars) | ✅ implemented + unit tested (save/redisplay/secret set-clear round-trip/gating) |
| Settings are no longer any-signed-in-admin territory: GET+POST `/settings` require `node.settings`; ops screens require their panel scopes (`backup.manage`, `admins.manage`, `api_tokens.manage`, `webhooks.read/write`, `audit.view`); nav System group renders per permissions | ✅ implemented + unit tested |
| Visual rebase: shadcn-style zinc token system (see preamble), favicon/theme-color updates, per-page content width tiers | ✅ implemented; browser-verified both themes |

Honest notes within Phase 6 scope:

- Telegram delivery against the real Bot API is unverified (no test bot token was available);
  the sink's HTTP shape, size guard, and error paths are stub-verified.
- Restore's environment review is report + endpoint/node-id guidance; full pre-apply editing of
  every setting lives in the normal Settings flow after apply (documented in
  [../operations/backup-restore.md](../operations/backup-restore.md)).
- `doctor` peer-count and tc checks are heuristic warnings; the reconciler remains the
  authority for interface/peer state.
- The panel restore wizard intentionally does not upload archives (CLI owns file-path restore);
  the panel restores from the node's local sink.
- Aggregate "sing-box/clash subscription" endpoints remain a later-phase candidate.

## Phase 7 — Deployment & installer (complete, 2026-08-31)

All items below are **implemented + unit tested** (Windows/Go 1.27 + WSL2 `-race`; the install
package exercises the full install/update/uninstall/rollback flows against an in-memory `Host`
seam, including health-checked rollback paths). Deployment drills ran on the dedicated Ubuntu
24.04 VPS with the real domain — per-item evidence in [phase7.md](phase7.md), which also lists
**seven real-host defects the phase found and fixed** (ETXTBSY on self-install, shim argv bug,
`status` hash output, DKMS module stranded after a kernel upgrade, missing `backup create
-reason`, update not recreating the container, prompt defaults overriding explicit flags)
plus the interrupted-update recovery command.

| Item | Status |
|---|---|
| Built-in ACME (ADR-0011): `tls.mode=acme` via `autocert` (x/crypto; x/net+x/text join as indirect deps — no new module), HTTP-01 sidecar on `tls.acme_http_port` (default 80) bound synchronously (busy port fails boot loudly), cert cache `<data_dir>/acme`, TLS 1.2 floor, redirect fallback that targets the configured domain + real TLS port (autocert's built-in fallback hardcodes :443) and never bounces arbitrary Host headers | ✅ implemented + unit tested (wiring: sidecar host-policy gate, redirect target, port-busy boot failure, shutdown closes both listeners); **issuance verified on the real VPS** (Let's Encrypt cert for the drill domain, HTTPS 200, port-80 302) |
| Interactive installer `wg-guard install`: Docker default / native secondary, domain→ACME derivation, panel/challenge ports, image; `--yes` non-interactive (flags + defaults, never reads stdin, never overrides an explicit `--tls` with a prompt default); preflight (root, completed-install refusal, busy ports, DNS warning, docker/systemd presence); artifacts: boot config 0600, compose project, hardened systemd unit (ProtectSystem=strict, NET_ADMIN+NET_BIND_SERVICE bounding set, ip_forward sysctl re-admitted, MDE), host CLI, install-state contract, module boot persistence | ✅ implemented + unit tested (plan resolve/validation, renderers golden, full flows on the in-memory host); **both modes installed on the real VPS** |
| Installer initial settings (post-review addendum): optional wizard sections — AWG port allocation range, first-interface VPN pool (new `network.default_pool` key honored by the interface service for awg0), client MTU, client DNS, Telegram backups (bot token via `settings set KEY -stdin`, chat, daily `backup schedule-add`); default values never persisted; panel domain seeded as `node.endpoint`; seeding before first boot (registry caches in memory), seed failure aborts before service start | ✅ implemented + unit tested (scripted wizard incl. empty-token skip, seed mapping + default-skip + secret-stdin assertions, seed-failure abort, cmd round-trips, iface pool defaults); **verified on the real VPS** (fresh interactive install with customized values + `--yes` defaults reinstall, values visible in `settings list`/panel, first-interface pool honored) |
| Host shim (docker mode): mode-aware dispatch via the install state — panel/data commands exec into the container (`docker exec -i`), host commands (`install/update/uninstall/status/doctor/version`) run locally, `serve` refused with compose hints | ✅ implemented + unit tested (routing table); **routing verified on the real VPS** (status host-side, backup list in-container, serve refusal) |
| `wg-guard update`: pre-upgrade backup in the owning environment (version-tolerant retry for images predating `-reason`), compose-as-source-of-truth image switch + pull (best-effort for local images) + recreate, or staged binary swap with `<bin>.pre-update` kept; health-checked **automatic rollback**; `--rollback` re-deploys the state-recorded artifact after an interrupted update | ✅ implemented + unit tested (update/rollback flows incl. unhealthy rollback paths); **verified live**: docker update recreated the container on the new tag; a broken image triggered automatic rollback; a deliberately killed update was recovered with `--rollback`; native `--binary` swap healthy |
| `wg-guard uninstall`: `--dry-run` plan, stops the node, removes only state-recorded artifacts (compose/unit/host CLI/modules-load entry/config/state); data + installer-installed packages kept unless `--purge-data`/`--purge-packages` | ✅ implemented + unit tested (dry-run non-mutation, kept/purged data); **verified in both modes on the real VPS** |
| `wg-guard status`: install state, image, container/unit status line (via new capturing `Host.Output`), mode-aware health probe | ✅ implemented + unit tested; **verified on the real VPS** (docker + native) |
| Docker image + reference compose: multi-stage CGO_ENABLED=0 build onto ubuntu:24.04 + pinned amneziawg-tools (ppa:amnezia/ppa) + nftables + iproute2; `deploy/compose.yaml` reference; installer-generated compose adds a TLS-mode-aware healthcheck | ✅ built and run on the real VPS (amd64); **registry publication of versioned multi-arch tags is the Phase 12 release pipeline** — `--image` override is the documented path until then |
| Kernel module lifecycle: `/etc/modules-load.d/wg-guard.conf` boot persistence; DKMS recovery ladder when the module is registered for a different kernel series (headers for the running kernel → `dkms autoinstall` → `depmod -a` → modprobe) | ✅ implemented + unit tested; **the reboot + kernel-upgrade scenario verified live** (module auto-loaded at boot after the rebuild) |
| i18n raw-key leak class eliminated: template-vs-catalog audit tests walk every embedded template (constant `.T` keys must resolve in BOTH locales) + lifecycle status labels pinned | ✅ implemented + unit tested (the audit test fails CI on any future leak); all 9 leaked keys fixed in fa+en |

Honest notes within Phase 7 scope:

- The official `wgguard/wg-guard` registry image is not published yet (Phase 12 release
  pipeline); all image drills used locally-built tags through the documented `--image` flag.
- ACME renewal is automatic (autocert) but only initial issuance was observed on the drill
  window; renewal exercises the same cache + challenge path.
- The ACME redirect fallback intentionally redirects to the configured domain (not the request
  Host) — plain-HTTP probes with forged Host headers cannot be bounced to third-party origins.
- Debian 12 has no AmneziaWG PPA build. The installer warns, but automatic userspace lifecycle
  is not implemented; Debian support therefore remains unverified and blocked on Phase 11
  AUD-019 rather than silently falling back.
- Browser QA ran through the live ACME deployment (onboarding → dashboard → settings → ops
  screens; fa/en × light/dark × 390/1440/2560): zero horizontal overflow, no raw i18n keys on
  any route. At 2560 px the screenshot pipeline returned stale composited frames, so ultrawide
  correctness was verified via DOM geometry (symmetric auto margins, max-width tier, no
  overflow) — same documented pipeline limitation as Phase 5.

## Phase 8 — Audit & configuration integrity (complete, 2026-09-05)

The baseline audit, pinned-source contract, lossless scalar/range model and migration 0007 are
complete. Supported values round-trip through storage, REST/OpenAPI, strict bilingual forms,
settings, apply/dump/reconcile, backup/restore, canonical downloads, subscriptions, and decoded QR.
One server-side `crypto/rand` generator owns recommended/randomized profiles, with 10,000-case
property coverage and sealed session-bound panel previews. Unsafe/client-specific options remain
gated and `AdvancedSecurity` remains unsupported at the pins.

The exact commit-stamped Ubuntu 24.04 gate proved normalized API/DB/runtime/config/QR equality;
recommended and randomized decoded configurations established kernel-client handshakes and
bidirectional ICMP/UDP/TCP, while recommended also passed the exact pinned userspace daemon.
Real-browser QA passed fa/en, RTL/LTR, light/dark, desktop and 390 px mobile without QR clipping or
overflow. A physical optical camera was unavailable and is explicitly unperformed; independent
decoding of the actual HTTP PNGs and real-client import of those exact bytes provide the content
and interoperability evidence. The VPS gate also exposed a critical peer-sync defect: a peers-only
`syncconf` cleared the interface private key. The backend now preserves and post-verifies the
complete live interface section, with unit, integration, and real-traffic regressions green.

The configured userspace mode still has no production daemon lifecycle; Phase 11 AUD-019 owns
that feature and certification. Execution: [phase8.md](phase8.md); sanitized real-host evidence:
[`../integrations/fixtures/verify-phase8-vps-2026-09-05.txt`](../integrations/fixtures/verify-phase8-vps-2026-09-05.txt);
cross-phase status: [release-readiness.md](release-readiness.md).

## Phases 9–12

| Phase | State | Scope |
|---|---|---|
| 8.1 — GitHub delivery & lifecycle | active; design recorded | GitHub acquisition, terminal UX, prerequisites, compatible AWG, recovery and backup management |
| 9 — Operational observability | planned; design branch paused | Live node/AWG metrics, dashboard telemetry, CLI logs, redaction, seven-day bounded retention |
| 10 — Product UI/UX redesign | planned; not implemented | Complete shadcn-style page/state migration, Settings IA, responsive QA, fa/en copy and accessibility |
| 11 — Production certification | planned; not implemented | Security/race/soak/performance, real traffic, recovery drills, OS/arch/backend/deployment matrix |
| 12 — Release candidate | planned; not implemented | Checksummed/multi-arch artifacts, repository/docs/API freeze, candidate install/upgrade and final report |

## Requires real VPS or client verification (carried forward)

- Phase 9: Docker/native operational logs, retention, live metrics under real traffic.
- Phase 11: nftables/NAT/firewall coexistence, 1000-shaped-peer tc, Ubuntu 22.04/24.04,
  Debian 12, amd64/arm64, kernel/userspace, Docker/native, recovery and TLS drills.
- Phase 12: installation and upgrade from the exact release-candidate artifacts. Public release
  and registry publication remain owner-approval gated.

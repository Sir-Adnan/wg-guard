# WG-Guard — Initial Deliverable (pre-Phase 0)

Status: **Proposed — awaiting approval.** No production implementation code written yet.
Sources: `docs/wg-guard_SPEC.md` (product truth) + primary-source research into the AmneziaWG
ecosystem (repo state as of 2026-08). Facts are cited inline; anything not verified against an
authoritative source is explicitly marked **UNVERIFIED — pin in Phase 0**.

---

## 1. Repository assessment

- Repo contains exactly one file: `docs/wg-guard_SPEC.md` (commit `1ee7d35`, one uncommitted
  whitespace/CRLF-level modification). No code, no license, no CI, no toolchain config.
- Local dev machine: Windows 10/11, Git Bash. **Go is not installed** (Phase 0 item).
- WSL2 is available with an **Ubuntu** distro (currently stopped) and Docker Desktop 29.7.2 —
  usable for real Linux integration testing without a separate VPS (with the caveat that WSL2 is
  not a faithful VPS network environment; kernel-module verification should still be repeated on a
  real KVM VPS before release).
- Target platforms per spec: Ubuntu 22.04/24.04, Debian 12, amd64 + arm64.

## 2. AmneziaWG integration strategy (researched, not guessed)

### 2.1 The upstream components

| Component | Repo | License | Role |
|---|---|---|---|
| amneziawg-tools | github.com/amnezia-vpn/amneziawg-tools | **GPLv2** (`COPYING`) | `awg` CLI + `awg-quick` (fork of wireguard-tools) |
| amneziawg-go | github.com/amnezia-vpn/amneziawg-go | **MIT** | Userspace tunnel daemon (fork of wireguard-go) |
| amneziawg-linux-kernel-module | github.com/amnezia-vpn/amneziawg-linux-kernel-module | GPLv2 (kernel code) | Kernel module `amneziawg`, installed via DKMS |

Verified facts that drive the design:

- `awg` subcommands: `show`, `showconf`, `set`, `setconf`, `addconf`, `syncconf`, `genkey`,
  `genpsk`, `pubkey` (man page `src/man/wg.8`; pages kept under the `wg` names, binaries are
  branded `awg`/`awg-quick`).
- `awg show <iface> dump` (documented in wg.8): first line = `private-key, public-key,
  listen-port, fwmark` (tab-separated); each peer line = `public-key, preshared-key, endpoint,
  allowed-ips, latest-handshake, transfer-rx, transfer-tx, persistent-keepalive`. **This is the
  accounting/handshake data source.**
- `awg-quick` subcommands: `up`, `down`, `save`, `strip`, `show`, `showconf`; default config
  directory `/etc/amnezia/amneziawg/<iface>.conf`. `awg-quick` also handles Address/DNS/MTU/Table
  routes and firewall marks itself — which we deliberately will NOT use in the production path
  (see 2.3).
- The tools support **both** backends via the same CLI: kernel module through netlink and
  userspace daemons through a UAPI unix socket (`src/ipc.c`, `src/ipc-uapi.h`, `src/netlink.h` —
  same architecture as wireguard-tools). Exact AWG UAPI socket path: **UNVERIFIED — pin in Phase 0**.
- The stock man page documents no AmneziaWG keys; the extended keys live in the config parser
  (`src/config.c`). The CLI historically lagged the protocol (I1 lines were rejected until an
  Oct 2025 fix, amneziawg-tools issue #31) → **version pinning is mandatory, and the exact accepted
  key list of the pinned tool build must be dumped in Phase 0**.
- Kernel module README documents the legacy (1.0) parameter set with constraints: `Jc` 1–128
  (rec. 4–12), `Jmin < Jmax ≤ 1280`, `S1 ≤ 1132`, `S2 ≤ 1188`, `S1+56 ≠ S2`, `H1..H4` pairwise
  distinct (rec. 5..2^31−1). **All parameters must match between client and server except
  Jc/Jmin/Jmax (client-side) and the client-side I1–I5 signature packets.**
- Protocol generations shipped by upstream: 1.0 (legacy), 1.5 (adds I1–I5), 2.0 (adds S3/S4,
  removes j1–j3/ITIME; **not backward compatible** — official statement), 3.x (header protection,
  range timers; requires client ≥ 5.0.1.5). Kernel-module support for 1.5/2.0 params: README still
  documents only 1.0, module tags jumped to v3.0/v3.1 in 2026 → **UNVERIFIED — detect at runtime**.
- Known upstream bugs to respect: arm64 H4 corruption under 1.5 (amneziawg-go #110), iOS fails to
  start with I1–I5 configs (#115), RandomTrailers panic (#178), IPv6-disabled hosts resetting the
  listen port (#148), Ubuntu 24.04 `amneziawg` meta-package install failure (amneziawg-tools #417).
- Official install on Ubuntu/Debian: Launchpad PPA `ppa:amnezia/ppa`, packages
  `amneziawg-tools` + `amneziawg-dkms` (avoid the broken `amneziawg` meta-package on 24.04);
  DKMS needs `linux-headers-$(uname -r)`, `dkms`, `build-essential`.
- Licensing consequence: **execute, never vendor.** GPLv2 amneziawg-tools is executed as a
  subprocess (clean); redistributing its binaries in an installer requires offering its source
  (public upstream satisfies this) and keeping notices. MIT amneziawg-go could even be imported as
  a library (no `internal/` packages), but we choose subprocess anyway (see decision D2).

### 2.2 Industry-standard integration pattern (verified across panels)

wg-easy forks (spcfox, w0rng), WGDashboard ≥ 4.2.0, and Amnezia's own container scripts all manage
AWG by **shelling out to `awg`/`awg-quick`** and parsing `awg show dump` for rx/tx/handshake. No
panel imports amneziawg-go as a library; none use netlink directly. Amnezia has no official
self-hosted web panel (only the client-app-over-SSH model and a paid business product).
WG-Guard follows the subprocess pattern but improves on it: it owns firewall/NAT in its own
namespaced nftables table instead of letting `awg-quick` mutate global firewall state.

### 2.3 Strategy summary

- WG-Guard talks to AWG **exclusively through the pinned `awg` CLI** (explicit argv, timeouts,
  no shell interpolation), wrapped behind `internal/tunnel.TunnelBackend`.
- Interface bring-up is performed by WG-Guard itself (`ip link add awgN type amneziawg`,
  `awg setconf`, address/MTU/routes) — **not** by `awg-quick` — so that all firewall and routing
  changes are namespaced, auditable, and idempotent.
- Bulk peer changes use `awg syncconf` (renders a temp conf file, diffs without resetting active
  sessions); single-peer changes use `awg set awgN peer …`.
- Stats/handshakes via one `awg show awgN dump` per interface per accounting cycle.
- Phase 0 pins exact upstream versions and dumps the real CLI/config behavior into
  `docs/upstream/AMNEZIAWG.md` (the spec requires recording this; nothing is assumed from memory).

## 3. Proposed architecture

### 3.1 Process model

One Go binary (`wg-guard`), one process, embedded UI, SQLite, zero runtime dependencies beyond
the AWG tooling and standard Linux facilities. systemd manages lifecycle. Internal structure:

```
cmd/wg-guard/               CLI entry (serve, version, status, doctor, backup, restore, update, admin)
internal/
  config/                   TOML config + env overrides, validation
  database/                 SQLite open (WAL, busy_timeout, FK), migrations runner, tx helpers
  domain/                   shared types: IDs, status enums, disable reasons, machine error codes
  user/  device/  plan/     domain packages: model + service + repository per package
  admin/ auth/              owner/admin accounts, argon2id, sessions, CSRF, permission registry
  token/                    API tokens (hash storage, scopes, CIDR allowlists)
  tunnel/                   TunnelBackend interface + types (spec §4)
    amneziawg/              pinned AWG implementation (exec wrapper, conf renderer, dump parser,
                            capability detection)
    fake/                   in-memory backend for tests/dev (no root required)
  firewall/                 namespaced nftables table `wgguard`, NAT/forward rules, ownership markers
  network/                  ip link/addr/route wrappers, sysctl checks, interface detection
  shaper/                   tc (HTB/ifb) speed limiting, deterministic rebuildable rendering
  accounting/               delta-based counters, persistence, quota enforcement
  scheduler/                one centralized scheduler (expiry sweep, accounting poll, webhook
                            retries, idempotency cleanup, backups) — no per-user goroutines/timers
  webhook/                  event bus (bounded), HMAC signing, delivery worker w/ backoff
  audit/                    audit log writes (never secrets)
  api/                      REST /api/v1: handlers, middleware, errors, pagination, idempotency, OpenAPI
  web/                      admin UI: html/template handlers, session middleware
web/                        templates/, static/ (embedded via go:embed; prebuilt, no Node at runtime)
migrations/                 numbered SQL migrations (embedded)
packaging/                  systemd unit, sysctl fragment, env file
scripts/                    dev helpers
docs/                       spec, architecture, upstream pinning notes, API docs
install.sh                  HTTPS-only bootstrap (downloads verified release artifact, runs installer)
```

Dependency direction: `api`/`web` → domain services → `tunnel`/`firewall`/`network`/`database`.
No cycles; no `utils` packages.

### 3.2 Key architectural decisions (D1–D10) — approval requested

- **D1 — Obfuscation profiles map to tunnel interfaces.** AWG obfuscation parameters
  (S1–S4, H1–H4, I1–I5) live in the `[Interface]` section and are therefore shared by *all peers*
  on an interface — per-peer profiles are impossible upstream. To honor spec §6/§14 (per-user /
  per-plan obfuscation profile), WG-Guard manages **multiple AWG interfaces, one per active
  profile** (e.g. `awg0` = Automatic, `awg1` = Strong). A user's profile selects the interface its
  devices' peers are placed in; each interface gets its own IPv4 sub-pool from the configured VPN
  subnet. Creating a *new* profile or changing an interface's params restarts only that interface
  (peers re-applied via syncconf; active sessions of that interface drop once — documented).
- **D2 — Subprocess CLI, not library import.** amneziawg-go is MIT and technically importable,
  but importing couples the panel process to a beta-quality fork's device stack (a panic there
  kills the panel; version churn is high). Every existing panel uses the CLI; GPLv2 tools are
  cleanly executed as subprocesses. Rejected alternative recorded here.
- **D3 — Kernel module primary, userspace fallback.** Installer prefers `amneziawg-dkms` (best
  throughput, no extra resident process); if DKMS build prerequisites are unavailable it falls
  back to the MIT-licensed `amneziawg-go` daemon (one process per interface, UAPI socket). The
  `TunnelBackend` implementation is identical from the panel's perspective because `awg` talks to
  both. Backend mode is reported via `/api/v1/node`.
- **D4 — WG-Guard owns firewall/NAT in a namespaced nftables table** (`table inet wgguard`,
  rules commented `wgguard:managed`). We never flush or modify foreign tables (Docker, ufw, admin
  rules). NAT = masquerade (or explicit SNAT), forward policy scoped to `awgN` interfaces.
  Uninstaller removes only the `wgguard` table. SSH-lockout risk mitigated: we only *add*
  scoped rules; we never set global policies.
- **D5 — SQLite via `modernc.org/sqlite` (pure Go, no CGO).** Enables `CGO_ENABLED=0` static
  binaries for amd64/arm64, no gcc/dkms toolchain needed for WG-Guard itself. Trade-off: slightly
  lower peak throughput than mattn/go-sqlite3 — irrelevant for a control plane, decisive for
  install simplicity. WAL mode, `busy_timeout=5s`, foreign keys ON, short transactions.
- **D6 — Routing: Go 1.22+ `net/http` ServeMux**, no router framework. Spec allows "net/http or
  Chi"; the API surface is small and static, method+wildcard patterns suffice, zero deps.
- **D7 — Secrets at rest.** Device private keys and webhook secrets are encrypted with
  AES-256-GCM using a node-local master key generated at install (file `0600` outside the DB
  backup path by default, included in opt-in full backups with warnings). Admin passwords:
  argon2id. API tokens: `wg_` + 32 chars crypto/rand base62, stored as SHA-256 with an indexed
  prefix for lookup (GitHub-PAT style; argon2 is unnecessary per-request for 192-bit random
  tokens — justified deviation from "hash with password-grade KDF", documented in docs/security).
  Root-attacker limitation documented (spec §44).
- **D8 — Protocol generation default: 1.0 legacy parameter set** (randomized per interface within
  kernel-README constraints, using crypto/rand) — the only generation verified to work with the
  kernel module and the widest client range. `I1–I5` (1.5) exposed as client-side extras where the
  pinned tooling supports parsing. 2.0/3.x are **not enabled by default** (officially
  non-backward-compatible; kernel support UNVERIFIED) — surfaced later as capability-gated flags
  with explicit client-compatibility warnings.
- **D9 — "Online" = derived recently-active state** (spec §15 already requires this): default
  threshold = handshake within the last 3 minutes (configurable); raw `latest-handshake` timestamp
  is exposed as authoritative everywhere.
- **D10 — Frontend: server-rendered `html/template` + HTMX + minimal Alpine.js + a hand-written
  CSS design system** (tokens per spec §53.2), Lucide SVG subset compiled into an embedded sprite,
  server-generated QR (PNG, `skip2/go-qrcode`), server-generated SVG sparklines. Assets are
  minified at development time and committed/embedded — **production installs never need Node**.
  JS payload budget: ≤ ~50 KB gzipped total (HTMX ~14 KB + Alpine ~15 KB + app code); CSS ≤ ~25 KB
  gzipped. No chart/QR/icon runtime libraries.

### 3.3 Resource-efficiency design (spec §54)

- One scheduler goroutine owns all periodic work (default cadences: accounting 30 s, expiry sweep
  60 s, webhook retry 15 s tick, housekeeping 10 min) — sleeps via timer, no busy loops, no
  per-user goroutines/timers.
- One `awg show dump` exec per interface per accounting cycle (30 s); peer deltas computed in
  memory; **one SQLite transaction per cycle** writes only changed rows (counters, handshakes,
  status transitions). No per-packet or per-peer transactions.
- Peer listing/queries are paginated (cursor) in both DB and API; nothing loads "all users" into
  memory at once. Webhook queue is bounded; drop-oldest event bus; all caches bounded.
- Exec wrapper enforces timeouts, captures stderr safely, never interpolates input into a shell;
  peer ops batch through `awg syncconf` where possible.
- `wg-guard doctor` will include an idle-usage self-check; a benchmark script measures idle RAM/
  CPU/startup at 0/10/100/1000 peers (spec §54.7).

## 4. Dependency list (with justification)

Go (CGO_ENABLED=0, Go ≥ 1.22):

| Dependency | Why | Alternatives rejected |
|---|---|---|
| `modernc.org/sqlite` | Pure-Go SQLite (WAL/FK supported); static cross-compile, no C toolchain | mattn/go-sqlite3 (CGO build pain), gorm (heavy ORM) |
| `golang.org/x/crypto` | argon2id password hashing (RFC 9106) | hand-rolled crypto (never) |
| `BurntSushi/toml` | Small, stable, human-friendly config format | JSON config (worse to hand-edit), YAML dep (larger) |
| `golang.org/x/time/rate` | Token-bucket rate limiting (login/API) | hand-rolled limiter (bug-prone) |
| `skip2/go-qrcode` | Server-side QR generation (zero client JS) | client QR JS lib (payload cost) |
| Vendored static assets: HTMX, Alpine.js, Lucide SVG subset | UI interactivity within budget | React/Vue (forbidden by spec), icon fonts |

Standard library covers: HTTP server/routing, sessions (own SQLite-backed), CSRF, crypto/rand,
AES-GCM, HMAC, exec, embed. Tests use stdlib `testing` + `httptest` (no testify) — explicit over
convenient. OpenAPI: hand-authored `openapi.json` + a route-coverage test; `/docs` renders a
lightweight server-generated reference (no 2 MB JS viewer).

## 5. Database / domain model (Phase 0 output, summarized)

Tables (all with `created_at`/`updated_at`, FKs ON, CHECK constraints on enums):

- `tunnel_interfaces` — name (`awg0`), listen_port, ipv4_subnet, mtu, obfuscation profile params
  (Jc, Jmin, Jmax, S1, S2, H1–H4, optional I1–I5 blob refs), enabled, backend mode.
- `users` — id (UUID v7), username UNIQUE, display_name, note, tags, status
  (`active|disabled|suspended|expired|traffic_exceeded|waiting_first_connection`), disable_reason
  (`manual|expired|traffic_limit|admin_action`), traffic_limit_bytes NULL=unlimited, traffic_used_rx/tx,
  speed_limit_kbps NULL, device_limit, plan_id NULL, interface_id (profile), start_policy
  (`immediate|first_connection`), duration_seconds NULL, activated_at, expires_at NULL,
  last_activity_at, enabled flag, metadata JSON.
- `devices` — id, user_id FK, name, interface_id, ipv4_address (UNIQUE per interface), public_key
  UNIQUE, private_key_encrypted, preshared_key_encrypted, enabled, status, last_handshake_at,
  last_endpoint, rx_bytes, tx_bytes (accumulated), last_rx/last_tx (last raw counter snapshot for
  delta logic), created/updated.
- `plans` — id, name, traffic_limit, duration, start_policy, device_limit, speed_limit,
  profile/interface selector, enabled.
- `admins`, `admin_sessions`, `api_tokens` (prefix, hash, scopes JSON, expires_at, enabled,
  cidr allowlist, last_used_at), `webhook_endpoints`, `webhook_deliveries` (bounded retry state),
  `audit_log`, `idempotency_keys` (key, request hash, response snapshot, expires_at), `settings`
  (namespaced key/value), `migrations`.

Allocation: per-interface IPv4 pool with UNIQUE(interface_id, ipv4_address) constraint;
allocation in a transaction with retry on conflict; IPs released on permanent device delete.
Delta accounting invariants (spec §10): new < last ⇒ counter reset ⇒ count current value as the
delta and re-baseline (prevents negative deltas and reset corruption); peer delete snapshots
final usage; restart-safe (accumulated totals live in SQLite, not in AWG).

## 6. API design summary

- Base `/api/v1`, token auth (`Authorization: Bearer wg_…`), scopes per spec §20 (superset of
  admin permissions where applicable), errors as spec §23 envelope with `request_id`, cursor
  pagination (`limit` ≤ 500, `cursor`), filters/sorting per spec §22, `Idempotency-Key` persisted
  middleware on POST create/bulk/renew/traffic (spec §24).
- Endpoints per spec §21 plus documented deviations: bulk actions via
  `POST /api/v1/users/bulk-action` `{action, user_ids, params}`; QR via `GET /api/v1/devices/{id}/qr`
  (PNG with `Cache-Control: no-store`); node capabilities via `GET /api/v1/node/health`
  (node id, versions, backend mode, capability list per spec §27).
- Config endpoints (`/config`, `/qr`) require `configs.read`; responses always `no-store`;
  private keys never logged; API returns config with private key only to tokens with
  `configs.read` (documented).
- OpenAPI served at `/openapi.json`; a test asserts every registered route appears in the spec.

## 7. Security model

- AuthN: argon2id admins; server-side sessions (HttpOnly, Secure, SameSite=Lax, rotating on
  login, absolute + idle expiry); login rate limiting (per-IP and per-account lockout, audit
  logged); CSRF: per-session token required on all mutating form/HTMX requests (header or field).
- AuthZ: central permission registry (spec §17 list is canonical); owner role is immutable and
  cannot remove itself; every handler declares required permission; UI hiding is never the gate.
- Tokens: hashed, scoped, expiring, CIDR-allowlisted, revocable; `last_used_at` updated in batch.
- Transport: four exposure modes — admin-provided TLS cert, built-in ACME (autocert; requires
  port 80/443 and a domain), loopback HTTP behind an external reverse proxy, explicit dev mode
  (loopback-only, loud warnings). Installer refuses public plain-HTTP for the management API.
- Input: strict JSON decoding (unknown-field rejection where sensible), size limits (1 MB default),
  request timeouts, panic recovery → 500 envelope without stack traces.
- Subprocess: `exec.Command` with explicit argv only; no shell; secrets passed via stdin/temp
  files (0600) never argv where avoidable (`awg set` takes keys via file/stdin paths).
- Audit: every privileged action with actor, IP, request id; secrets and keys never logged
  (log scrubber + redaction list per spec §40).
- Threat model note (docs/security.md): root on the VPS can read memory/keys/master key — WG-Guard
  protects against remote/web attackers, not root; documented honestly.

## 8. Installer design

- `install.sh` = tiny verified bootstrap: checks root, detects OS/arch via `uname`, downloads the
  release tarball + `checksums.txt` from GitHub over HTTPS, verifies SHA-256, then executes the
  embedded Go installer (`wg-guard installer`) — never runs unverified code (spec §36).
- The Go installer (hand-rolled minimal TUI, zero deps, box style per spec) executes the 20 steps:
  env validation, apt setup of `ppa:amnezia/ppa`, installs `amneziawg-tools` + `amneziawg-dkms`
  (+ headers/dkms/build-essential) with userspace fallback (D3), sysctl forwarding, creates
  `wgguard` nftables table + NAT, creates system user/dirs with 0750/0600 perms, writes TOML
  config, generates master key + owner credentials (printed once), writes systemd unit
  (hardening: `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, ambient
  caps for `NET_ADMIN`), starts service, health-checks, prints dashboard URL + one-time password.
- Uninstaller: stops service, optionally preserves DB/backups/config, removes only WG-Guard-owned
  firewall resources (D4), removes AWG packages only if they were installed by us and unused.
- Update: `wg-guard update` / UI button — GitHub release check → download + checksum (+ signature
  when release process grows one) → DB backup → atomic binary replace → restart → health check →
  automatic rollback to previous binary if startup health fails. No auto-update (spec §34).

## 9. Testing strategy

- Unit + repository tests on SQLite (temp file DB, real migrations) — no root needed.
- API tests via `httptest` with the `fake` tunnel backend (spec §45) covering authz, quotas,
  expiration, first-connection, device-limit races, idempotency replay, webhook signatures.
- Amneziawg package tests: exec wrapper against a scripted fake `awg` binary (fixture dumps) —
  parser goldens from real output captured in Phase 0.
- Race tests in CI; migration tests (up/down/upgrade-from-backup).
- Integration tests (build tag `integration`): real networking in WSL2 Ubuntu / CI ubuntu-runner
  using the userspace backend; kernel-module path executed on a real VPS and recorded per spec
  §52's implemented/unit/integration/needs-VPS classification.
- Benchmarks: accounting cycle, bulk create 100/1000, user list at 1000 peers, idle RAM/CPU.

## 10. Implementation milestones

- **Phase 0 — Research & foundation** (first actionable phase): install Go toolchain locally;
  spin up WSL2 Ubuntu 24.04; install pinned AWG packages from the PPA; **record verified facts**
  (exact tool version, `config.c`-level accepted keys, `awg set` runtime acceptance of obfuscation
  keys, UAPI socket path, kernel vs userspace behavior, dump output on the pinned build) into
  `docs/upstream/AMNEZIAWG.md`; scaffold repo (go.mod, CI, LICENSE=Apache-2.0 proposed, README
  stub, lint/format config); define domain model + migrations + `TunnelBackend` interface + fake
  backend + exec wrapper skeleton. Acceptance: `go test ./...` green in CI, upstream doc complete,
  schema committed.
- **Phase 1 — Core**: config, database layer, user/device/plan services + repos, fake backend,
  tests (incl. device-limit race, cursor pagination).
- **Phase 2 — AWG integration**: real backend (interface lifecycle, syncconf peer ops, dump
  parsing, capability detection, recovery on restart); integration-tested in WSL2 userspace mode.
- **Phase 3 — Limits**: accounting pipeline (deltas, resets, persistence), expiry scheduler,
  first-connection activation, device limits enforcement, tc shaper (deterministic renderer +
  rebuild + tests with fake tc).
- **Phase 4 — REST API**: tokens/scopes middleware, all §21 endpoints, idempotency, bulk actions,
  error envelope, OpenAPI + coverage test, rate limits.
- **Phase 5 — Web UI**: auth/sessions/CSRF, design system + shell (light/dark/system), dashboard,
  users table + details + bulk-create flow, plans, admins, tokens, settings; visual QA pass per
  spec §53.27/§57.
- **Phase 6 — Webhooks / audit / backup**: delivery worker with backoff + HMAC, audit UI,
  backup/restore (DB + master key handling + pre-migration snapshots).
- **Phase 7 — Installer**: bootstrap script, Go TUI installer, systemd hardening, uninstall,
  update flow, doctor.
- **Phase 8 — Hardening**: security review, race/integration tests on WSL2 + real VPS matrix,
  performance benchmarks (idle + 10/100/1000 peers), docs complete, release workflow with
  checksums.

## 11. Risks, contradictions, open questions

**Spec contradictions / gaps resolved here (approval requested):**

1. **Per-user obfuscation profiles vs upstream reality** — obfuscation params are per-interface
   (D1 multi-interface design is the resolution). Alternative: single global profile only
   (violates spec §6/§14). If you prefer one interface, say so and I'll simplify.
2. **Profile parameters are shared server↔client** (S1–S4/H1–H4) while only Jc/Jmin/Jmax and
   I1–I5 are client-side — "Automatic" must therefore randomize per *interface*, and any profile
   change regenerates all client configs of users on that interface (configs are versioned; the
   UI/API must prompt users to re-download). Documented as a product consequence.
3. **Plain WireGuard clients** cannot connect to an interface with non-zero obfuscation params;
   AWG client apps (AmneziaVPN, amneziawg-android/apple/windows) are required. Compatibility
   matrix will be documented per spec §5/§47; "compatible with appropriate AmneziaWG clients" is
   the honest scope. Mixed plain-WG support is possible only with an all-zero "compat" profile.
4. **Speed limit granularity**: spec lists speed limit per user; AWG has no per-peer shaping, so
   tc shapes per device IP (all devices of a user share the limit per device). Aggregated
   per-user shaping across devices is not planned (complexity, marginal value) — flagged.

**Upstream risks (mitigations):**

- Tooling lags protocol releases (issue #31) → pin exact versions; capability detection; never
  emit keys the pinned parser rejects.
- Kernel-module 1.5/2.0 support contradictory in docs → runtime detection; default = legacy 1.0
  params (verified constraint set); 2.0/3.x behind explicit flags later.
- amneziawg-go bug history (arm64 H4 corruption #110, iOS I1–I5 #115, RandomTrailers panic #178,
  port reset on IPv6-disabled hosts #148) → userspace mode is a fallback, not the default; we
  avoid the buggy feature surface (no RandomTrailers/j1–j3/ITIME; explicit listen port always set).
- Ubuntu 24.04 PPA meta-package bug (#417) → installer installs `amneziawg-tools` and
  `amneziawg-dkms` directly.
- DKMS requires kernel headers + build toolchain on the VPS → installer precheck + userspace
  fallback (D3) + doctor diagnostics.
- `awg-quick` would fight us over firewall state → not used in the production path (2.3).
- tc HTB with thousands of classes costs CPU → benchmark at 1000 shaped peers in Phase 8; if
  beyond budget, document a recommended peer cap for shaping with graceful degradation.

**Environmental:**

- Development happens on Windows; all Linux-path code is developed against the WSL2 Ubuntu
  environment + CI. WSL2 is *not* a VPS substitute for firewall/NAT behavior — final networking
  verification requires a real KVM VPS run (recorded per spec §52).
- Go toolchain missing locally → installed in Phase 0.
- WG-Guard license: proposal **Apache-2.0** (permissive, GPLv2-compatible for aggregation of
  separate subprocess programs, keeps upstream notices requirements simple). Your call.

**Open questions for you (blocking nothing else):**

1. Approve D1 (multi-interface obfuscation profiles)?
2. Approve D3 (kernel module primary, userspace fallback)?
3. Approve D8 (default protocol generation = legacy 1.0; 1.5 I1–I5 opt-in; 2.0/3.x deferred)?
4. Approve WG-Guard under Apache-2.0?
5. Default ports: dashboard `8080` (TLS 8443 when enabled) and AWG listen port randomized
   30000–50000 at install (docs recommend low ports ≤ 9999 for censored networks — make it an
   installer prompt)?
6. English-only UI for V1 (i18n later), or ship en+ru from day one (Amnezia audience)?

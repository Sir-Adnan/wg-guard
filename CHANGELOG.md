# Changelog

All notable changes to WG-Guard are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning is
[SemVer](https://semver.org/). The REST API (`/api/v1`) is a compatibility contract from its
first release — see [docs/architecture/api.md](docs/architecture/api.md).

## [Unreleased]

### Added
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
    `network.client_keepalive_seconds`, `webhooks.max_attempts`, `api.rate_limit_per_minute`.
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

### Changed
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
  configurable defaults pending the Phase 8 VPS matrix.
- `tunnel_interfaces` contract gains `public_key` + `private_key_encrypted`; `users` gains
  `deleted_at` (soft delete); package `internal/iface` (renamed from planned
  `internal/interface`, a Go keyword).

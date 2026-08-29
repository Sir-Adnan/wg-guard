# Status

Feature-level verification matrix, maintained with every phase (AGENTS.md rule: never claim
more than this table says). Statuses: `designed` → `implemented` → `unit tested` →
`integration tested` → `production verified`; items that fundamentally need real hardware stay
marked `requires real VPS`.

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
| Real backend: `ListInterfaces` / `CreateInterface` (ip link add → setconf → verify → addr → up, link rollback on any failure) / `RemoveInterface` / `ApplyInterfaceConfig` (setconf + verify-after-apply) / `SyncPeers` (syncconf) / `Dump` (missing interface → canonical not-found) | ✅ implemented + unit tested (scripted runner asserts exact argv, file contents, rollback); **integration tested** against the pinned userspace daemon |
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

- **Kernel link path (`ip link add type amneziawg`) is unit-tested only** — the WSL2 kernel
  cannot load the amneziawg module; module load + netlink dump format + kernel-backend parity
  of the new setconf facts are the Phase 8 VPS matrix.
- **Userspace daemon supervision** (spawn/monitor `amneziawg-go`, PID files, restart) is a
  deployment concern and lands with serve/installer (Phase 4/7); the backend's config/dump
  paths are already backend-transparent (verified: identical CLI operations against the
  userspace daemon).
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
  Phase 3/8 deliverable (`scripts/bench-idle.sh`) once the server runs.
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

## Phases 3–8 — not started

Everything below is `designed` (architecture approved) until implemented:

- Phase 3 Limits & accounting: delta accounting, quota/expiry, first-connection activation
  (MarkActivated), tc shaper, samples/rollups, scheduler
- Phase 4 REST API: full surface, idempotency, durable webhooks, OpenAPI (`serve` reuses
  internal/boot for bring-up)
- Phase 5 Web UI: design system, shell, i18n/RTL, dashboard, users, plans, interfaces
- Phase 6 Backup/ops: archives (plain + optional password), schedules, Telegram, restore wizard,
  settings UI, admins/tokens/audit screens, doctor, rotation trigger
- Phase 7 Deployment: image, installer (Docker default), shim, update/uninstall
- Phase 8 Hardening: security review, benchmarks vs budgets, VPS matrix

## Requires real VPS (carried forward)

- Kernel-module load + `awg show dump` format against the kernel backend (fields may
  default-fill differently than userspace); kernel-backend parity of the Phase 2 setconf facts
  (explicit-zero rejection, persisting params)
- Kernel link bring-up end-to-end (`ip link add type amneziawg` on a module-capable host)
- nftables + NAT behavior with real traffic and firewall coexistence (ufw/firewalld) on a
  production host; `wg-guard reconcile` on a clean VPS
- PPA on Ubuntu 22.04 / Debian 12; arm64 end-to-end
- ACME issuance on a public host; installer end-to-end on clean VPS images

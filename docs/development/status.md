# Status

Feature-level verification matrix, maintained with every phase (AGENTS.md rule: never claim
more than this table says). Statuses: `designed` → `implemented` → `unit tested` →
`integration tested` → `production verified`; items that fundamentally need real hardware stay
marked `requires real VPS`.

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

## Phases 2–8 — not started

Everything below is `designed` (architecture approved) until implemented:

- Phase 2 AWG backend & networking: exec wrapper, interface lifecycle, syncconf peers, dump
  parsing (29-field format), nftables, sysctls, firewall coexistence, reconcile-on-boot wiring
- Phase 3 Limits & accounting: delta accounting, quota/expiry, first-connection activation
  (MarkActivated), tc shaper, samples/rollups, scheduler
- Phase 4 REST API: full surface, idempotency, durable webhooks, OpenAPI
- Phase 5 Web UI: design system, shell, i18n/RTL, dashboard, users, plans, interfaces
- Phase 6 Backup/ops: archives (plain + optional password), schedules, Telegram, restore wizard,
  settings UI, admins/tokens/audit screens, doctor, rotation trigger
- Phase 7 Deployment: image, installer (Docker default), shim, update/uninstall
- Phase 8 Hardening: security review, benchmarks vs budgets, VPS matrix

## Requires real VPS (carried forward)

- Kernel-module load + `awg show dump` format against the kernel backend (fields may
  default-fill differently than userspace)
- nftables + NAT behavior and firewall coexistence (ufw/firewalld) on a production host
- PPA on Ubuntu 22.04 / Debian 12; arm64 end-to-end
- ACME issuance on a public host; installer end-to-end on clean VPS images

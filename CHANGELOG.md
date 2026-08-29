# Changelog

All notable changes to WG-Guard are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning is
[SemVer](https://semver.org/). The REST API (`/api/v1`) is a compatibility contract from its
first release — see [docs/architecture/api.md](docs/architecture/api.md).

## [Unreleased]

### Added
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
- Terminology consistency: "management/REST API" (never "public API"); backup password
  documented as strictly optional; ports/MTU/DNS/interface cap framed as recommended
  configurable defaults pending the Phase 8 VPS matrix.
- `tunnel_interfaces` contract gains `public_key` + `private_key_encrypted`; `users` gains
  `deleted_at` (soft delete); package `internal/iface` (renamed from planned
  `internal/interface`, a Go keyword).

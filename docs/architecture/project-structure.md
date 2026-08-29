# Project structure

Module: `github.com/Sir-Adnan/wg-guard` (Go ≥ 1.22, `CGO_ENABLED=0`).

## Layout

```
cmd/wg-guard/            CLI entry: version, reconcile (boot bring-up), serve (Phase 4), later:
                         status, doctor, backup, restore, update, admin reset-password,
                         uninstall helpers (single binary, hand-rolled arg parsing — no CLI
                         framework)
internal/
  config/                boot config (TOML + env overrides), validation, exposure modes
  database/              SQLite open (WAL, busy_timeout, FK, txlock=immediate), migrations runner, tx helpers
  domain/                shared types: IDs (UUIDv7), statuses, disable reasons, machine error codes
  settings/              typed runtime settings registry (validators, categories, defaults)
  user/ device/ plan/    domain packages: model + service + repository
  iface/                 tunnel interface/profile service (ports, subnets, params, rotation);
                         package name `iface` — `interface` is a Go keyword
  admin/ auth/           owner/admin accounts, argon2id, sessions, permission registry
  token/                 API tokens (hash storage, scopes, CIDR allowlists)
  secrets/               master key ring, AES-256-GCM cipher, crash-safe master-key rotation
  tunnel/                TunnelBackend interface + types + key generation (standard X25519)
    amneziawg/           pinned AWG implementation: exec-driven backend, conf renderer,
                         29-field dump parser, verify-after-apply, capability probe
                         (see ../integrations/amneziawg.md)
    fake/                in-memory backend for tests/dev (no root required)
  reconcile/             boot-time + continuous DB↔kernel reconciliation
                         (per-interface error collection; mode transitions recreate links)
  boot/                  bring-up orchestration: tooling probe → sysctl → reconcile →
                         firewall → coexistence (used by `serve` and `wg-guard reconcile`)
  audit/                 audit log (never secrets)
  firewall/              namespaced nftables table `wgguard` (rendered-state apply),
                         ufw/firewalld coexistence (Phase 2 ✅)
  network/               ip link/addr wrappers, sysctls (Phase 2 ✅)
  subprocess/            the single exec choke point: explicit argv, timeouts, structured
                         exit errors; output is never logged (Phase 2 ✅)
  shaper/                tc (HTB) speed limiting, deterministic rebuildable rendering;
                         per-user class + per-device-IP filters, egress-only (Phase 3 ✅)
  accounting/            delta counters, persistence, quota/expiry enforcement, first-
                         connection activation, samples/rollups (Phase 3 ✅)
  scheduler/             one centralized scheduler (due-heap): expiry, accounting, webhooks,
                         backups, housekeeping — no per-user goroutines (Phase 3 ✅; jobs
                         composed into serve in Phase 4)
  webhook/               durable event delivery: events table, worker, HMAC signing (Phase 4)
  backup/                archive builder/restorer (tar.gz, optional age password), sinks (Phase 6)
  metrics/               healthz/readyz + optional hand-written /metrics (Phase 4)
  i18n/                  fa/en catalogs (embedded), locale helpers, Jalali dates (Phase 5)
  api/                   REST /api/v1: handlers, middleware, errors, pagination, idempotency (Phase 4)
  web/                   admin UI handlers, session middleware, template rendering (Phase 5)
web/                     templates/, static/ (embedded via go:embed; prebuilt, no Node runtime)
migrations/              numbered SQL migrations (embedded)
packaging/               systemd unit, sysctl fragment, Dockerfile, compose template
scripts/                 dev helpers (bench-idle.sh, fixtures)
docs/                    this documentation tree
.github/workflows/       CI
```

## Rules

- **One responsibility per package**; domain-specific names, no `utils`/`common`/`helpers`.
- **File organization**: split by responsibility (`users_handler.go`, `users_service.go`,
  `users_repository.go`, `users_test.go`); no 5,000-line files or handlers.
- **Dependency direction** (enforced by review): `api`/`web` → services → infra packages. The
  domain must not import `api`, `web`, or exec details; everything AWG-specific lives behind
  `tunnel.TunnelBackend`.
- **Errors**: domain error values preserve identity for API mapping; wrap with context; never
  expose DB/subprocess internals to clients.
- **Naming**: domain-precise (`trafficLimitBytes`, `expiresAt`, `DisableReasonTrafficExceeded`);
  comments explain why/invariants/security constraints, not the next line.
- **Tests** live next to code; integration tests carry the `integration` build tag; goldens for
  AWG parsing come from [../integrations/fixtures/](../integrations/fixtures/).

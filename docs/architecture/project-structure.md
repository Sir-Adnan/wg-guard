# Project structure

Module: `github.com/Sir-Adnan/wg-guard` (Go ≥ 1.22, `CGO_ENABLED=0`).

## Layout

```
cmd/wg-guard/            CLI entry: serve, version, status, doctor, backup, restore, update,
                         admin reset-password, uninstall helpers (single binary, hand-rolled arg
                         parsing — no CLI framework)
internal/
  config/                boot config (TOML + env overrides), validation, exposure modes
  database/              SQLite open (WAL, busy_timeout, FK), migrations runner, tx helpers
  domain/                shared types: IDs, statuses, disable reasons, machine error codes
  settings/              typed runtime settings registry (validators, categories, defaults)
  user/ device/ plan/    domain packages: model + service + repository
  interface/             tunnel interface/profile service (ports, subnets, params, rotation)
  admin/ auth/           owner/admin accounts, argon2id, sessions, CSRF, permission registry
  token/                 API tokens (hash storage, scopes, CIDR allowlists)
  tunnel/                TunnelBackend interface + types
    amneziawg/           pinned AWG implementation: exec wrapper, conf renderer, dump parser,
                         capability detection (see ../integrations/amneziawg.md)
    fake/                in-memory backend for tests/dev (no root required)
  firewall/              namespaced nftables table `wgguard`, NAT/forward rules
  network/               ip link/addr wrappers, sysctls, interface detection
  shaper/                tc (HTB) speed limiting, deterministic rebuildable rendering
  accounting/            delta counters, persistence, quota enforcement, rollups
  scheduler/             one centralized scheduler (due-heap): expiry, accounting, webhooks,
                         backups, housekeeping — no per-user goroutines
  webhook/               durable event delivery: events table, worker, HMAC signing
  audit/                 audit log (never secrets)
  backup/                archive builder/restorer (tar.gz, optional age password), sinks
  reconcile/             boot-time + continuous DB↔kernel reconciliation
  metrics/               healthz/readyz + optional hand-written /metrics
  i18n/                  fa/en catalogs (embedded), locale helpers, Jalali date conversion
  api/                   REST /api/v1: handlers, middleware, errors, pagination, idempotency
  web/                   admin UI handlers, session middleware, template rendering
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

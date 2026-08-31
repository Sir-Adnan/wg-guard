# Architecture overview

One Go binary (`wg-guard`), one process: HTTP server (panel + REST API), authentication,
scheduler, quota manager, accounting, webhook dispatcher, and AWG management. SQLite for
persistence. AmneziaWG is driven through its pinned CLI as a subprocess. Docker is the default
deployment; native systemd is fully supported. Decisions and their rationale live in
[../decisions/](../decisions/); this document describes the shape.

## Component diagram

```
                ┌────────────────────────── wg-guard (single process) ──────────────────────────┐
 admin browser ─▶ web/ (session auth, i18n fa/en, templates+HTMX)  ┐                            │
 external bots ─▶ api/ /api/v1 (token auth, scopes, idempotency)   ├▶ domain services           │
                  webhook/ (durable delivery, HMAC)                │   user/device/plan/        │
                  scheduler/ (one goroutine, due-heap)             │   interface/admin          │
                  accounting/ (delta pipeline)                     │        │                   │
                  backup/ (UI+CLI only)                            ▼        ▼                   │
                  reconcile/ (DB vs kernel state)              database/  tunnel.TunnelBackend │
                                                               (SQLite WAL) ├ amneziawg (exec awg)
                                                                            └ fake (tests/dev)
                  firewall/ (nftables table `wgguard`) ◀── network/ (ip/sysctl)   shaper/ (tc)
                └──────────────────────────────────────────────────────────────────────────────┘
                                    │ exec (argv-only, timeouts)         host Linux
                                    ▼                                    ▼
                              awg / awg syncconf                kernel module amnezia (or
                                                                userspace amneziawg-go), nftables, tc
```

Dependency direction: `api`/`web` → domain services → `tunnel`/`firewall`/`network`/`database`.
No cycles; no `utils` packages. Package responsibilities:
[project-structure.md](project-structure.md).

## Key decisions (ADRs)

| Decision | ADR |
|---|---|
| Control AWG via pinned CLI subprocess, never library import | [0001](../decisions/ADR-0001-tunnel-subprocess-cli.md) |
| One obfuscation profile per tunnel interface (`awg0…awg7`) | [0002](../decisions/ADR-0002-profile-per-interface.md) |
| Kernel module primary, userspace fallback | [0003](../decisions/ADR-0003-kernel-first-userspace-fallback.md) |
| Namespaced nftables table; never touch foreign rules | [0004](../decisions/ADR-0004-namespaced-nftables.md) |
| Pure-Go SQLite (modernc), CGO_ENABLED=0 | [0005](../decisions/ADR-0005-pure-go-sqlite.md) |
| Docker-default deployment, native secondary | [0006](../decisions/ADR-0006-docker-default-deployment.md) |
| Backup/restore excluded from the REST API (panel + CLI only) | [0007](../decisions/ADR-0007-no-backup-rest-api.md) |
| Optional backup password via standard age encryption | [0008](../decisions/ADR-0008-optional-backup-password.md) |
| Vanilla-JS frontend (no Alpine), HTMX | [0009](../decisions/ADR-0009-vanilla-js-frontend.md) |
| Bilingual fa/en panel with full RTL | [0010](../decisions/ADR-0010-bilingual-fa-en-rtl.md) |
| Built-in TLS/ACME without a reverse proxy | [0011](../decisions/ADR-0011-builtin-tls.md) |
| stdlib net/http ServeMux routing | [0012](../decisions/ADR-0012-net-http-mux.md) |

## Runtime (`serve`)

`wg-guard serve` composes the whole node (internal/serve): boot config → DB + migrations →
master key → settings → domain services → boot bring-up → HTTP(S) listener → the central
scheduler. All periodic work runs on the one scheduler goroutine: accounting cycle + expiry
(`accounting.interval_seconds`, live-reloadable), sample flush, webhook delivery pass (5 s),
housekeeping (10 min prunes + rate-limit reload). Reconcile passes are serialized behind one
mutex shared by boot, the accounting/enforcement paths and API-triggered reconciles —
concurrent AWG operations on one interface are the race verify-after-apply exists to catch.
Graceful shutdown drains HTTP (both the TLS listener and, in ACME mode, the port-80 challenge
sidecar), lets the running job finish, then closes the DB. TLS: manual cert, proxy, loopback
dev, and ACME (`autocert`; HTTP-01 sidecar + certificate cache under the data dir) — all four
implemented, ACME verified against a public domain in Phase 7.

## Reconciliation (DB is the source of truth)

On boot and continuously, kernel state is verified against the database: missing interfaces are
created and configured; mismatched ports/params are corrected (audited); missing peers are
re-applied; unknown peers follow `drift_policy` (`report` default | `adopt` | `remove`). The
30 s accounting cycle triggers a reconciliation pass whenever enforcement changes who may hold
peers (quota trips, expiry, first-connection activations). `wg-guard doctor [--fix]` performs
the same repairs on demand plus environment checks (permissions, upstream pin, module, nft,
sysctls, shaper, disk, endpoint DNS, cert expiry, DB integrity).

## Failure & recovery posture

- **Restart**: everything re-derives from SQLite + reconciliation; accumulated traffic and
  `activated_at` survive restarts; in-flight webhook deliveries are durable rows.
- **Partial applies**: interface mutations are staged (render config → validate → apply →
  verify); failures roll back to the last known-good state and are reported, never silent.
- **DB**: WAL + `busy_timeout`, forward-only migrations with a pre-migration backup; corruption
  detection surfaces honest errors (see [database.md](database.md)).
- **Clock skew**: UTC stored everywhere; doctor warns about NTP skew affecting expiry.

## Resources

See the budgets and design levers in
[archive/ARCHITECTURE_V2_PROPOSAL.md §8](../archive/ARCHITECTURE_V2_PROPOSAL.md) — single
scheduler goroutine (due-heap, no busy loops), one dump per interface per cycle with one SQLite
transaction, bounded queues/caches, cursor pagination everywhere, capped SQLite page cache,
dashboard auto-refresh paused on hidden tabs. Measured by `scripts/bench-idle.sh` and Go
benchmarks; results recorded in [../development/status.md](../development/status.md).

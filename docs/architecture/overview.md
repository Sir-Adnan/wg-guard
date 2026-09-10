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
                  telemetry/ (10 s bounded live ring)              │        │                   │
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
| Scheduler-owned live telemetry and mode-native bounded logs | [0013](../decisions/ADR-0013-operational-observability.md) |

## Runtime (`serve`)

`wg-guard serve` composes the whole node (internal/serve): boot config → DB + migrations →
master key → settings → domain services → boot bring-up → HTTP(S) listener → the central
scheduler. All periodic work runs on the one scheduler goroutine: accounting cycle + expiry
(`accounting.interval_seconds`, live-reloadable), sample flush, webhook delivery pass (5 s), live
telemetry (10 s), and housekeeping (10 min prunes + rate-limit reload). The telemetry source runs
one bounded `/proc` pass and one aggregate SQLite statement; API, metrics, and browser readers
consume immutable ring copies and never sample the host. Runtime reconcile passes are serialized
behind one mutex shared by accounting/enforcement and API/web mutations. The canonical pass owns
the complete network state—AWG links/peers, rendered firewall/NAT, supported manager coexistence
and shaping—so a post-start interface cannot exist without its route policy. A failure makes the
readiness endpoint unready until a complete pass succeeds; concurrent AWG operations on one
interface remain the race verify-after-apply exists to catch.
Graceful shutdown drains HTTP (both the TLS listener and, in ACME mode, the port-80 challenge
sidecar), lets the running job finish, then closes the DB. TLS: manual cert, proxy, loopback
dev, and ACME (`autocert`; HTTP-01 sidecar + certificate cache under the data dir) — all four
implemented, ACME verified against a public domain in Phase 7.

DB/key openers retain a shared kernel lease on the persistent data-volume lock inode;
rotation and pair replacement/recovery require exclusive ownership. Startup consumes any
approved restore exclusively, then converts to shared lifetime ownership with admission
closed. The separate host lifecycle lock still owns deployment orchestration, so installed
CLI subprocesses do not inherit or reacquire it. See the exact protocol and older-binary
boundary in [lifecycle recovery](../operations/lifecycle-recovery.md).

Structured runtime records cross one recursive redaction handler and carry a closed component
label before reaching deployment-native storage. Docker owns a compressed local-driver ring;
native mode owns a scoped journal namespace; neither is copied into SQLite or exposed over HTTP.
The host-side `wg-guard logs` command normalizes both. Fixed lifecycle outcomes that occur outside
the service manager use a separate size/time-bounded private JSONL journal under the data directory.

## Reconciliation (DB is the source of truth)

On boot and continuously, kernel state is verified against the database: missing interfaces are
created and configured; mismatched ports/params are corrected (audited); missing peers are
re-applied; unknown peers follow `drift_policy` (`report` default | `adopt` | `remove`). The
30 s accounting cycle triggers a reconciliation pass whenever enforcement changes who may hold
peers (quota trips, expiry, first-connection activations). Structural web/API changes use that same
full pass immediately. `wg-guard doctor [--fix]` performs the repairs on demand and checks the
effective legacy `FORWARD` policy/manager path in addition to permissions, upstream pin, module,
nft, sysctls, shaper, disk, endpoint DNS, cert expiry and DB integrity.

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
transaction, bounded queues/caches, cursor pagination everywhere, capped SQLite page cache, one
preallocated 180-point telemetry ring, and dashboard auto-refresh paused on hidden tabs. Measured
by `scripts/bench-idle.sh` and Go
benchmarks; results recorded in [../development/status.md](../development/status.md).

Archive staging streams database members instead of allocating their declared size. This does
not eliminate standard age/scrypt's transient crypto cost: the pinned writer uses factor18
(approximately 256 MiB for the KDF), and the Phase 8.1 reader caps acceptance at that same factor
before expensive derivation. Idle-process budgets are not peak encrypted-backup/restore budgets.
Keep this cost and concurrent operation limits in Phase 11 resource certification; see
[backup limits and recovery](../operations/backup-restore.md).

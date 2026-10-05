# Architecture overview

One Go binary (`wg-guard`), one process: HTTP server (panel + REST API), authentication,
scheduler, quota manager, accounting, webhook dispatcher, and AWG management. SQLite for
persistence. AmneziaWG is driven through its pinned CLI as a subprocess. Current main uses Docker-only production deployment; the historical v0.1.9 preparation release
retains its earlier deployment contract. Decisions and their rationale live in
[../decisions/](../decisions/); this document describes the shape.

## Component diagram

```
                ┌────────────────────────── wg-guard (single process) ──────────────────────────┐
 admin browser ─▶ web/ (session auth, i18n fa/en, templates+HTMX)  ┐                            │
 external bots ─▶ api/ /api/v1 (token auth, scopes, idempotency)   ├▶ domain services           │
                  webhook/ (durable delivery, HMAC)                │   user/device/template/        │
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

The reviewed [refactor target](deployment-refactor.md) and
[Phases 15–20](../development/refactor-program.md) track implemented safety/responsibility/
Docker/domain/TLS source changes separately from physical acceptance. They prioritize fail-closed existing-data migration,
readiness and bounded slow-work isolation before packaging/native retirement.
Integrated panel/subscription certificate management is specified in
[domains and TLS](../operations/domains-and-tls.md).

## Key decisions (ADRs)

| Decision | ADR |
|---|---|
| Control AWG via pinned CLI subprocess, never library import | [0001](../decisions/ADR-0001-tunnel-subprocess-cli.md) |
| One obfuscation profile per tunnel interface (`awg0…awg7`) | [0002](../decisions/ADR-0002-profile-per-interface.md) |
| Kernel module primary, userspace fallback | [0003](../decisions/ADR-0003-kernel-first-userspace-fallback.md) |
| Namespaced nftables table; never touch foreign rules | [0004](../decisions/ADR-0004-namespaced-nftables.md) |
| Pure-Go SQLite (modernc), CGO_ENABLED=0 | [0005](../decisions/ADR-0005-pure-go-sqlite.md) |
| Docker-only runtime, verified artifacts and separated host authority | [0015](../decisions/ADR-0015-docker-only-runtime.md) |
| Backup/restore excluded from the REST API (panel + CLI only) | [0007](../decisions/ADR-0007-no-backup-rest-api.md) |
| Optional backup password via standard age encryption | [0008](../decisions/ADR-0008-optional-backup-password.md) |
| Vanilla-JS frontend (no Alpine), HTMX | [0009](../decisions/ADR-0009-vanilla-js-frontend.md) |
| Bilingual fa/en panel with full RTL | [0010](../decisions/ADR-0010-bilingual-fa-en-rtl.md) |
| Built-in TLS/ACME without a reverse proxy | [0011](../decisions/ADR-0011-builtin-tls.md) |
| stdlib net/http ServeMux routing | [0012](../decisions/ADR-0012-net-http-mux.md) |
| Scheduler-owned live telemetry and mode-native bounded logs | [0013](../decisions/ADR-0013-operational-observability.md) |

## Runtime (`serve`)

`wg-guard serve` composes the whole node (internal/serve): boot config → exclusively owned
DB inspection + required pre-migration archive → migrations → master key → settings → domain
services → boot bring-up → HTTP(S) listener → the central scheduler. Unknown/incomplete migration
history or a failed existing-data archive stops startup before schema changes.
One scheduler goroutine runs accounting + expiry (`accounting.interval_seconds`, live-reloadable),
sample flush, live telemetry (10 s), and housekeeping (10 min prunes + rate-limit reload).
It signals two fixed in-process workers: webhook delivery every 5 s and backup schedules every
minute. Each worker has one coalesced pending signal, no overlapping pass and a cancellation
deadline (4 min for delivery, 15 min for archives). Durable due rows remain the retry source;
signals do not carry per-account tasks. The telemetry source runs
one bounded `/proc` pass and one aggregate SQLite statement; API, metrics, and browser readers
consume immutable ring copies and never sample the host. Runtime reconcile passes use
`internal/runtimeapply`'s single cancellation-aware gate shared by accounting/enforcement,
runtime repair and API/web mutations. Process-local desired/applied revisions keep older success
from marking newer canceled/queued changes applied; persisted DB state remains the restart/retry
source. No durable queue/table is added. The canonical pass owns
the complete network state—AWG links/peers, rendered firewall/NAT, supported manager coexistence
and shaping—so a post-start interface cannot exist without its route policy. A failure makes the
readiness endpoint unready until a complete pass succeeds; concurrent AWG operations on one
interface remain the race verify-after-apply exists to catch.
Explicit userspace profiles use one pin-checked foreground daemon per interface under the node;
the scheduler checks the bounded process/socket set and retries failed members through the same
canonical pass. Kernel profiles retain direct link creation; active unowned daemons are refused.
Graceful shutdown marks readiness false, drains HTTP (both the TLS listener and, in ACME mode,
the port-80 challenge sidecar), stops scheduling, cancels/drains both slow workers, stops owned
userspace children, then closes the DB. A failed handler/worker drain retains the DB and data
lease for a later shutdown attempt or process cleanup. TLS: manual cert, proxy, loopback
dev, and ACME (`autocert`; HTTP-01 sidecar + certificate cache under the data dir) — all four
implemented, ACME verified against a public domain in Phase 7.

DB/key openers retain a shared kernel lease on the persistent data-volume lock inode;
rotation and pair replacement/recovery require exclusive ownership. Startup consumes any
approved restore exclusively, then converts to shared lifetime ownership with admission
closed. The separate host lifecycle lock still owns deployment orchestration, so installed
CLI subprocesses do not inherit or reacquire it. See the exact protocol and older-binary
boundary in [lifecycle recovery](../operations/lifecycle-recovery.md).
`internal/nodestate` owns shared data opening and key/settings composition; database-only
owner/token operations never create a key. View/CLI parsing and host admission stay outside
the session. `internal/layout` owns current default/managed paths; concrete deployment adapters
remain under the one host lifecycle coordinator.

Lifecycle completion requires both `/healthz` liveness and `/readyz` data/network readiness.
ACME sidecars expose readiness only to an actual loopback socket peer; forwarded headers do
not establish locality. Older retained sidecars that redirect are probed on the fixed local TLS
listener with recorded SNI instead. Certificate trust/identity remains a separate exposure gate.

Structured runtime records cross one recursive redaction handler and carry a closed component
label before reaching deployment storage. Docker owns a compressed local-driver ring;
these raw logs are not copied into SQLite or exposed over HTTP.
The host-side `wg-guard logs` command normalizes that stream. Fixed lifecycle outcomes that occur outside
the service manager use a separate size/time-bounded private JSONL journal under the data directory.

The Web Panel's version workflow crosses the container/host boundary through `internal/updatequeue`,
not through a Docker socket or general privileged agent. An `update.manage` request publishes one
0600 schema-checked stable-release/core-catalog identity to the shared data directory. A fixed
root-owned systemd path and oneshot claim it atomically and invoke only the existing bounded
panel/core lifecycle commands. The host revalidates the selection; public status contains no
subprocess error text. One active request, a service timeout and an expiry lease bound failures.

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
Phase 11 resource measurements cover the steady-state node, not peak backup KDF memory;
see [backup limits and recovery](../operations/backup-restore.md).

Archive creation/crypto holds a nonblocking data-volume claim across host/container processes.
Scheduled passes hold a separate claim from the due query through conditional advancement,
reading at most eight rows per pass. Cancellation or contention leaves due work for retry.
A crash between archive publication and schedule advancement can repeat the archive; this is
at-least-once execution, not exactly-once delivery. Slow I/O isolation is regression-tested;
production accounting/enforcement lag and peak crypto resource budgets still need measurement.

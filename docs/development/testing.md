# Testing strategy

## Active installer verification

[Phase 8.1](phase8.1.md) adds GitHub acquisition/error fixtures, bootstrap execution tests,
host-operation failure injection, 48/80/120-column fa/en terminal checks, and dedicated Ubuntu
24.04 Docker/native lifecycle drills. The evidence levels are separate:

| Area | Recorded evidence | Still required |
|---|---|---|
| GitHub delivery | Real source acquisition/build/help; executable release/error fixtures | Exact final one-command setup/rerun and published-release installation |
| Terminal | Real read-only Linux PTY at 48/80/120 columns, fa/en, hidden-input cancellation and nonTTY refusal on `234f067` | Final M5 forms and integrated setup/lifecycle; universal Persian client shaping is not claimed |
| Runtime image | Actual builder/image identity and isolated binary/AWG package checks on `234f067` | Deployment startup, fresh host kernel provisioning and broader OS/architecture cells |
| Backup/recovery | Actual SQLite/archive/encryption/HTTP fixtures and both-mode Host/CLI boundaries on `281b607`; scoped Linux race/integration pass; [CI passed on the following docs revision `c46f313`](https://github.com/Sir-Adnan/wg-guard/actions/runs/34005864073) | Independent review fixes, actual managed restore, Telegram delivery and production scheduler tick |

Exact revisions and sanitized records live in [status.md](status.md) and
[phase8.1.md](phase8.1.md). Earlier Phase 7/8 deployment evidence does not certify new lifecycle
code. These checks do not replace Phase 11's full OS/architecture certification or Phase 12's
final published-artifact gate.

The repeatable
[`verify-phase8.1-synthetic-backup.py`](../integrations/fixtures/verify-phase8.1-synthetic-backup.py)
fixture runs a private fake-backend node beside an installed node without reading or mutating the
installed deployment. Its local regression suite covers candidate/credential preflight,
same-descriptor validation and private candidate pinning, collision refusal, workspace
device/inode replacement refusal, owned-child-only shutdown, bounded noisy-service capture,
error-resilient cleanup and capture redaction. A candidate run covers
encrypted create/list/retention and schedule CRUD plus an accelerated actual central-scheduler
tick. Without the explicit private-credential opt-in it records Telegram as **unverified**; even
with that opt-in it remains synthetic fixture evidence rather than native/Docker or restore
acceptance. See [backup and restore](../operations/backup-restore.md#isolated-acceptance-helper)
for the bounded invocation and evidence limits.

During implementation, run focused regressions for the changed risk; run full build/unit/vet
and relevant race/integration gates at coherent milestones. Prose-only changes do not justify
repeating unchanged expensive suites. Root/service/data recovery paths need failure injection
before real-host execution, not only a happy-path test after the entire installer is assembled.

Nothing is "complete" without tests matching its risk. The majority of tests run without root
and without a real VPN interface.

## Layers

| Layer | Scope | Environment |
|---|---|---|
| Unit | parsers, validators, crypto, scheduling logic, i18n parity (fa/en key sets), Jalali conversion | plain `go test` |
| Repository | SQLite migrations, constraints, cursor pagination, delta accounting invariants | temp-file DB, real migrations |
| Service/API | authz matrix, quotas, expiration, first-connection, device-limit races, idempotency replay, webhook signatures, error envelope | `httptest` + fake tunnel backend |
| Tunnel adapter | conf renderer + dump parser against golden fixtures captured from the pinned upstream ([../integrations/fixtures/](../integrations/fixtures/)), exec wrapper against a scripted fake `awg` | plain `go test` |
| Deployment | the whole install/update/uninstall/rollback flow against an in-memory `Host` seam (fs map + recorded commands), incl. health-checked rollback with real probe endpoints on loopback | plain `go test` (`internal/install`) |
| Integration (`integration` build tag) | real interface lifecycle, syncconf, reconcile, nftables, sysctls — userspace backend in WSL2/CI | WSL2 Ubuntu / CI runner |
| Real VPS matrix | kernel module, netlink dump format, NAT/NAT-less paths, firewall coexistence, install/update/uninstall | Ubuntu 22.04/24.04, Debian 12, amd64/arm64 (Phase 11; the 24.04/amd64 slice is Phase-7 drill-verified) |

## Invariants with dedicated tests

- Accounting: counter reset, negative-delta prevention, restart persistence, no double counting
  (delta pipeline is property-tested).
- Concurrency: duplicate IP allocation, device-limit races (run under `-race`).
- Idempotency: replayed `Idempotency-Key` returns the original response, no side effects.
- Webhooks: signature scheme, replay window, restart-safe delivery (kill mid-flight), backoff.
- Drift: unknown peer / missing interface / changed port → reconcile per policy.
- Configuration text boundaries: endpoint and I1–I5 newline/control injection is rejected before
  persistence; reconciliation refuses an invalid stored profile before backend mutation, and I1–I5
  differences participate in exact drift correction.
- Peer replacement: `SyncPeers` carries forward the validated complete live `[Interface]`
  section and byte-verifies it after `syncconf`; a missing/malformed private key or any interface
  mutation fails closed. This prevents the pinned kernel backend from clearing the interface key
  when given a peers-only configuration.
- Client configuration: one literal full-field golden covers exact section placement, ordering,
  range preservation, spacing, and final newline; REST/admin/subscription bytes and headers are
  compared against the direct renderer. Failures report only length/digest/offset metadata and
  never configuration contents or key material.
- QR delivery: PNGs are decoded with an independent test-only implementation and compared with
  the canonical config for direct, REST, admin, and public-subscription paths. Geometry/color,
  empty, UTF-8, full, near-capacity, oversized, and deterministic-output cases are covered.
- Upgrade/restore: real archives from schema 0006 and the current schema are staged, migrated,
  applied, reopened, and compared for exact H/keepalive ranges, rollback mirrors, settings,
  interface review data, and unrelated foreign-key relationships. A separate boot test consumes
  the pending snapshot before database open and verifies the restore audit event.
- Pinned userspace runtime: the `integration` suite applies, dumps, and reapplies all supported
  H/u16 interval fields plus peer PersistentKeepalive and requires exact equality, preventing a
  lossy conversion from appearing as perpetual drift. This is a privileged WSL2/CI test; it does
  not substitute for the real kernel/client traffic gate.
- Phase 8 real-host gate: `docs/integrations/fixtures/verify-phase8-vps.sh` creates only
  collision-checked, harness-owned namespaces/interfaces inside a mode-0700 work directory. It
  compares normalized API, database, runtime, client-config, and independently decoded QR state;
  exercises recommended/randomized kernel traffic plus recommended userspace traffic; checks all
  three delivery surfaces and secret-free diagnostics; and can hold the isolated panel for browser
  QA. The exact commit-stamped Ubuntu 24.04 amd64 run passed on 2026-09-05, including cleanup;
  sanitized evidence is in
  `docs/integrations/fixtures/verify-phase8-vps-2026-09-05.txt`. A physical optical-camera scan was
  unavailable and is not claimed; actual HTTP PNG decoding and import of those bytes into real AWG
  clients passed.

## Benchmarks (measured, not guessed)

Accounting cycle cost, bulk create 100/1000, user list at 1000 peers, idle RSS/CPU
(`scripts/bench-idle.sh`, 10-minute soak at 0/100/1000 fake peers). Budgets and results are
recorded in [status.md](status.md); regressions are treated as bugs.

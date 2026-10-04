# Phase 15 — Preparation release evidence

Scope: reliable portable data, fail-closed automatic migration, lifecycle readiness,
bounded slow-work execution, complete/resumable secret rotation and an owner-ready
export/verify drill. The owner authorized the next preparation release on 2026-10-04,
then full Phase 16 source work without another release. The preparation release is
v0.1.9; publication follows exact main CI and the release workflow.

## Engineering closure

Implemented source gates and exact CI for archive/migration/readiness/worker,
secret-storage and domain-admission changes are recorded in [status](status.md) and
[release readiness](release-readiness.md). Current source additionally corrects
accounting freshness: unknown, stale, future or zero-window observations are unavailable.
The freshness window is at least five minutes and at least two configured accounting
cadences. Active-interface telemetry reports `accounting_unavailable`; an empty private
node does not claim a missing tunnel-accounting requirement. Public account payloads,
archive schema 1 and the existing installation layout are preserved.

## Actual crypto and slow delivery

`internal/serve/backup_load_integration_test.go` exercises production node/scheduler,
archive/scrypt, scheduled ownership, webhook delivery, accounting/expiry and peer
reconciliation in one process. Data is temporary, receivers are local HTTP servers,
credentials are synthetic and the tunnel provider is fake. No installed deployment,
host network or real Telegram/webhook recipient is touched. The completed age archive
is independently verified. A bounded 90 s test context and 15 s accounting cadence
bound the drill; RSS is read from `/proc` and worker/DB ownership drains before exit.

Fresh Linux evidence: WSL Ubuntu, kernel `6.18.33.1-microsoft-standard-WSL2`,
Linux/amd64, Go 1.26.0, GOMAXPROCS=1, 512 devices/three accounts and one IPv4 profile:

| Measurement | Ordinary run | Race run |
|---|---|---|
| Quota enforcement during observed KDF allocation | 4 ms | 125 ms |
| Expiry cycle while local delivery is deliberately delayed | 4 ms | 60 ms |
| Accounting cadence/budget | 15 s | 15 s |
| Process RSS baseline / observed peak | 79.7 / 334.8 MiB | 231.2 / 1031.2 MiB |
| Goroutines baseline / after shutdown | 9 / 3 | 9 / 3 |

Commands:

```bash
GOMAXPROCS=1 go test -tags integration ./internal/serve -run TestEncryptedBackupLoadPreservesEnforcementCadence -count=1 -v
GOMAXPROCS=1 go test -race -tags integration ./internal/serve -run TestEncryptedBackupLoadPreservesEnforcementCadence -count=1 -v
```

The ordinary peak includes the real factor-18 scrypt working set. Race instrumentation
is not a production memory budget. These are production-code/control-plane measurements,
not real-kernel/client or VPS certification. Actual-server CPU/RSS/lag remain Phase 20
evidence; no universal latency or memory promise is inferred.

## Publication and owner checkpoint

Owner workflow is release → update existing server → download encrypted backup →
independent verification → later approved rebuild/fresh install/restore. The
[preparation drill](../operations/migration-preparation.md) covers password recovery,
checksums, inventory and target TLS/core/network review. The owner's off-host copy
is a post-publication checkpoint and remains mandatory before rebuilding the only server.

Engineering preparation enables Phase 16 source work after the preparation release.
It does not authorize live-host mutation/rebuild or certify the later Docker/TLS
refactor. Native removal remains mandatory Phase 17 scope; this preparation release
retains current deployment support. Publish no further release after Phase 16 under
the current authorization.

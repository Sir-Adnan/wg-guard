# Phase 16 — Responsibility boundaries

Scope: behavior-preserving source refactor after the owner-authorized v0.1.9
preparation release. The owner explicitly requested full Phase 16 and no additional
release. Native removal/deployment movement remain Phase 17; remote engines/nodes
are outside this implementation. Go/SQLite/HTMX and one node/scheduler remain.

## Inventory and final ownership

| Concrete duplication/risk | Final owner and callers |
|---|---|
| DB/key lifetime, original-schema migration gate and initialization repeated in startup, backup/settings, token, owner and reconcile | `internal/nodestate` session; CLI/view parsing still resolves configuration and authority outside the opener |
| Mutation apply/logging/timeout repeated in web and REST; serialized networking embedded in serve | `internal/runtimeapply` attempt/coordinator, shared by request adapters, accounting and runtime repair |
| Private/PSK provisioning repeated in web and API | `internal/device.GenerateKeys`; sealed material only, no storage/render/host side effects |
| Default/managed paths repeated in config, state/journal and restore | `internal/layout`; no directory move, validated managed replacement and host authority outside writable node data |
| Deployment execution mixed into install/update coordination | `deployment_install.go` and `service_runtime.go`; concrete Docker/native adapters use the existing single lifecycle lock/journal |
| Host diagnostics mixed into settings CLI | `cmd/wg-guard/doctor.go`; shared services retained, flags/English presentation remain adapters |

Existing user/device/template services already share domain/storage rules between REST
and web. `plan.ApplyToUser` remains the shared template policy; edge-specific selection
errors belong to those adapters. Purchase/successor operations retain their principal-scoped
durable transaction journal. Client config/QR still use the canonical renderer. No generic
repository, deployment plugin, protocol blob, placeholder table or dependency is added.

## Persistence and runtime consistency

Node sessions expose database-only, shared services, stopped-node exclusive services and
startup entry points. Database-only operations never create key material. Startup consumes
approved restore before a live handle; existing data needs exclusive original-schema backup
before DDL, then keys/settings and shared lifetime ownership. Failed opening closes handles
and retains required recovery markers. Session close is serial/retryable and does not release
the lease if DB closure fails. HTTP/background-drain ordering stays in serve.

SQLite and durable retired-peer intent remain desired/recoverable state. The runtime coordinator
tracks only bounded process-local desired/applied revisions and observation state. On restart
the boot pass reconstructs actual networking from the persisted model. No extra scheduler,
worker queue, per-account goroutine or durable table is introduced.

Concurrent apply calls serialize through one finite gate; a canceled waiter returns promptly.
Its committed desired mutation remains pending. An older successful pass cannot restore ready
state when a newer invocation arrived during it. Runtime repair therefore re-applies current
DB state. Partial reports/errors remain pending and degrade readiness. Web/REST callers retain
their existing persistence/retry and response/audit semantics; nil runtime is explicitly
unconfigured in the operation result. No public account payload or OpenAPI change is implied.

## Host boundaries and deployment

Configuration/host-state/artifacts stay under the current `/etc/wg-guard` ownership contract;
only the individual boot file and approved certificates mount read-only. Node data stays
`/var/lib/wg-guard`. The host binary/cache/journal/deployment files remain outside writable
node mounts. Current fixed paths are central constants, with derived DB/key filenames and a
closed managed-restore check. Manual validated node-data layouts remain supported.

Path movement is not required to establish ownership. The proposed alternative host-state
directory is not implemented here: current state/journal/artifacts are already outside the
node writable mount. Phase 17 may move deployment assets with explicit ownership/recovery
tests. No new orchestrator/lock or Docker socket mount is introduced. Native adapters are
identified/preserved for their scheduled complete removal rather than replaced by a plugin
framework. Kernel/userspace/fake providers remain the existing typed WireGuard-family seam.

## Verification and limits

Focused node-session tests cover no-key database access, excluded initialization/rotation,
canceled-open admission release and complete service composition. Existing automatic-opener,
restore/rotation/failed-backup, retained-lease/drain, API tenant and client-byte/config tests
exercise the shared paths. Runtime tests cover partial reports and canceled newer revisions
without overlap/false readiness. Managed-path validation rejects arbitrary replacement targets.
Existing lifecycle service-manager failure/recovery fixtures cover the extracted adapters.

Full local Go tests, vet/build passed; applicable unchanged-package Go cache was reused.
Fresh Linux race integration with 512 devices, actual archive crypto and delayed local receivers
passed after the refactor: quota lag 105 ms, expiry cycle 59 ms against 15 s cadence,
RSS baseline/peak 227.9/1034.6 MiB under race, goroutines 9 baseline/3 after shutdown.
Ordinary load on the final refactor also passed: quota lag/expiry cycle 3/3 ms, RSS baseline/
peak 75.4/332.8 MiB and goroutines 9/3 at the same 512-device/15 s/GOMAXPROCS=1 point.
Race memory is not a production budget. Exact-source CI remains the delivery gate.
No new real-host, browser, deployment-layout or protocol support is
inferred; changed Go boundaries preserve the existing presentation/assets/public account contract.

The next phase is mandatory Docker-only distribution/native cleanup. No post-Phase-16 release
is authorized by the current instruction. The owner still verifies/retains the off-host backup
before rebuilding the only server; actual host/kernel/client/resource acceptance remains gated.

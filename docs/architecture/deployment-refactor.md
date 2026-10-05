# Docker deployment refactor target

Owner-selected product scope, revised after critical review on 2026-10-04.
Phase 17 implements the source deployment boundary; shipped v0.1.9 retains the preparation
contract. Domain/TLS and real-host acceptance remain later gates. Phase
order, milestones and exit gates are in the [Phases 15–20 program](../development/refactor-program.md).
The owner operates one server
and will export a verified backup, rebuild it, install the new distribution and
restore. Live-host rebuilds and public publication remain separately authorized.

## Product boundary

Keep WG-Guard an AmneziaWG node panel: target Docker-only production deployment,
host kernel backend by default, and the pinned userspace backend as an explicit
advanced choice. Its binary does not run for kernel profiles. Preserve fake-backend
development without Docker or host-network mutation.

The owner explicitly confirmed complete native production cleanup on 2026-10-04.
It is a mandatory Phase 17 outcome; gates determine its safe delivery, not whether
to keep a second production profile. [Removal inventory and acceptance](docker-only-cleanup.md)
distinguish native server execution from host CLI/systemd/DKMS and userspace.

Keep the Go/SQLite/HTMX modular monolith, one node process and its bounded scheduler.
Preserve public account/device/template/automation behavior. Remote nodes, native
WireGuard, Xray, sing-box and OpenVPN are deferred; extension points are not a
support claim. Do not add generic protocol tables or new databases/services now.

## Host layout and ownership

Use `/opt/wg-guard` for deployment assets where it simplifies distribution, while
retaining stable configuration and node-data paths. Moving directories is not a
performance/security improvement. The earlier `/etc/opt` and `node/` relocation
proposal is withdrawn; only a demonstrated ownership/recovery need justifies a move.
Phase 17 makes the host-authority split explicit with schema-4 state. This is a fresh-layout
boundary, not an in-place migration of the owner's existing installation.

| Host path | Responsibility | Container access |
|---|---|---|
| `/opt/wg-guard/compose.yaml` | Versioned deployment manifest | none |
| `/usr/local/bin/wg-guard` | Active verified host command | none |
| `/etc/wg-guard/wg-guard.toml` | Host-specific boot configuration | one read-only file |
| `/etc/wg-guard/tls/` | Managed certificate/key pair | approved read-only material |
| `/var/lib/wg-guard/` | DB, master key, backups and ACME cache | node data only |
| `/var/lib/wg-guard-host/` | Private host state/journal/recovery artifacts | none |
| `/var/cache/wg-guard/` | Bounded acquisition/manager cache | none |
| `/var/log/wg-guard/` | Bounded private installer log | none |

Expose the command through `/usr/local/bin/wg-guard`. Docker owns image storage;
DKMS source, modules-load configuration, broker units, certificate hooks and locks
remain in their required system locations. Centralize validated host/container
paths rather than duplicating absolute strings. Do not mount host executables,
journals or recovery/cache material as writable node storage. Preserve the narrow
update request/status bridge.

Current source host state uses `/var/lib/wg-guard-host`; current/previous binaries and Compose
snapshots are private beneath it. Legacy state/paths refuse mutation rather than changing JSON
fields or guessing ownership. Configuration/TLS and node data stay in their established paths. Native retirement
is gated on verified image distribution, recovery and host acceptance; it does
not make a systemd deployment inherently less correct or less secure.

Phase 16 now centralizes the current constants/managed-data admission in `internal/layout`
and live DB/key/migration/initialization in `internal/nodestate`. The existing host state,
artifact directory and lifecycle journal are already under `/etc/wg-guard`, outside the writable
data mount; the earlier proposed alternative host-state directory is not required or moved
in Phase 16. Phase 17 subsequently separates the mutable host state from configuration under
its own versioned fresh-install contract. [Phase 16 evidence](../development/phase16.md) records the boundary.

Remove native branches, renderers, state/artifact fields, flags, error hints and
current native-only acceptance cells together. Preserve one operation coordinator
and the concrete Docker path; do not retain a deployment-plugin framework solely
for a removed backend or delete host systemd tasks by a text search.

An `.env` file is optional and contains only necessary non-secret deployment
selection. Runtime settings remain in SQLite; boot-only paths/listener/TLS remain
in boot configuration. Do not create three competing sources for one setting.

## Responsibilities and artifacts

- Deployment owns Compose rendering, immutable image identity and container lifecycle.
- Host platform owns prerequisites, Ubuntu/kernel checks, DKMS and certificates/proxies.
- Lifecycle owns one lock, durable stages, compatibility, rollback and recovery.
- Distribution binds release/image digest, binary identity, reviewed core and protocols.
- Application retains domain services, accounting, rendering and web/REST adapters.

The central scheduler owns due-time decisions and short enforcement work. Slow
archive/delivery work needs finite in-process workers/queues so quota and expiry
do not wait behind remote I/O or crypto. Desired DB state and runtime application
outcome remain distinguishable and recoverable. Existing pre-migration backup
ordering and readiness gates are Phase 15 priorities, before packaging changes.

The host manager must work when the container cannot start. Data commands normally
execute in the container; offline verification needs no installation. Keep one
local process instead of adding a permanent host agent. Do not mount the Docker
socket into the panel, execute arbitrary requests, flush foreign network policy,
unload active tunnels or introduce automatic kernel-to-userspace fallback.

Build tested runtime images in CI and publish only under explicit authorization.
Production installs should fetch a digest-bound image rather than compile Go/AWG
tooling on the VPS. DKMS may still build against the host's running kernel. One
manifest binds image digest, binary hash/commit, core bundle, data contract and
lifecycle protocol. An offline image/core package can use the same verification.

Stage acquisition before service stop; then backup, deploy, check process/readiness/
TLS separately and commit. Artifact rollback requires compatible data; otherwise
restore the recorded original DB/key pair. Keep cache/log/image retention bounded.
Packaging changes do not certify more architectures or operating systems.

## Backup before rebuild

Preserve archive schema 1 and logical members. Check the exact snapshot against
the archived key, including every encrypted interface/device key, optional PSK,
customer capability, webhook secret and secret setting. Check references, stream
bounded values and clear plaintext. A previous key left on the source cannot make
an archive portable. Refuse invalid pairs before publication and replacement;
older checksum-only approvals cannot bypass the apply-time check.

Provide an offline `backup verify --archive PATH` with hidden-password/private-file
options. It opens no active node, applies nothing, needs neither Docker nor AWG,
uses private temporary storage and reports counts/backend inventory without secrets.
Verification is not proof of target TLS/network/client operation.

Migration acceptance preserves client config bytes, customer tokens, quotas,
expiry, charged usage, addresses, templates and ownership. Keep target deployment
configuration; archived boot settings remain report-only. Reissue/import TLS and
review panel/subscription/VPN endpoint addresses independently. Download a backup
off-host before rebuilding. The new distribution needs supported old archive
readers, not permanent native runtime or old directory aliases.

## Future engines and nodes

The existing `tunnel.Backend` is a WireGuard-family seam, not a generic proxy API:
its interfaces, peers, keys, addresses, obfuscation and handshakes are specialized.
Native WireGuard requires a separate reviewed `wg`/kernel provider; disabling AWG
obfuscation is not evidence of that provider.

Keep narrow provisioning, metering, entitlement-enforcement and rendering boundaries.
Add typed capabilities when an actual second provider needs them. Xray, sing-box
and OpenVPN each require specific authentication/configuration, statistics, quota,
revocation and client-delivery acceptance. Do not replace typed data with an untyped
blob or expose unsupported controls merely to make all engines look alike.

Remote AmneziaWG nodes are the closer next step. A future controller owns accounts/
entitlements; a node owns local runtime and durable observations. Keep an in-process
local executor for one-server use; add the remote executor/agent only when implemented.
The private node protocol is separate from the bot API, versioned, identity-bound
and restricted to typed operations. Prefer existing Go/HTTPS facilities until a
measured need justifies another transport or dependency.

Before claiming multi-node support, define idempotent desired revisions, restart-aware
metering with durable acknowledgements, stale/unreachable states, key ownership,
admission/revocation and offline enforcement. Copying the entire shared quota to
every offline node cannot enforce one exact global limit. Use bounded local budgets/
leases or explicitly document weaker semantics; unavailable statistics are not zero.

## Delivery sequence

1. Close Phase 15 backup ordering, readiness and measured production-code slow-job isolation
   before declaring engineering preparation complete. The owner authorized the preparation
   release on 2026-10-04, then Phase 16 without another release. Publication precedes the
   owner's update/export checkpoint; retain its verified off-host copy before rebuilding.
   Synthetic Linux load evidence is distinct from the actual VPS Phase 20 measurements.
2. Refactor shared application/runtime/host operations in Phase 16 while preserving
   public APIs, archive import and actual behavior. Test failure and recovery boundaries.
3. Deliver verified distribution and gated Docker-only production in Phase 17,
   integrated domains/TLS in 18, and common panel/terminal workflows in 19. Keep
   stable paths unless ownership/recovery evidence warrants a change; refuse unknown
   installation state before mutation. Owner migration is fresh install/restore.
4. Phase 20 adds exact-source/artifact gates and real Ubuntu install, restore,
   kernel/userspace, reboot, renewal, update/recovery, resource and client acceptance.
   Historical native evidence cannot certify changed deployment or TLS boundaries.
5. Native WireGuard and remote AWG nodes require a new implementation scope.

The sequence above is subordinate to the detailed [phase gates](../development/refactor-program.md):
Phase 15 reliability/migration, 16 internal boundaries, 17 distribution/deployment,
18 integrated domains/TLS, 19 operational UX and 20 real-host acceptance/publication.
Certificate management in the web panel is a planned feature, with the current
limits and target behavior in [domains and TLS](../operations/domains-and-tls.md).

References: [deployment](../operations/deployment.md),
[recovery](../operations/lifecycle-recovery.md), [backup](../operations/backup-restore.md),
[resources](overview.md#resources), [upstream](../integrations/amneziawg.md),
[PasarGuard node API](https://docs.pasarguard.org/fa/node/api/),
[Xray API](https://xtls.github.io/en/config/api.html),
[FHS state directory](https://refspecs.linuxfoundation.org/FHS_3.0/fhs/ch05s08.html).

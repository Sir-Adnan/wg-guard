# Docker deployment refactor target

Owner-selected scope, 2026-10-04. This is a target design and acceptance plan,
not the currently shipped deployment contract. The owner operates one server
and will export a verified backup, rebuild it, install the new distribution and
restore. Live-host rebuilds and public publication remain separately authorized.

## Product boundary

Keep WG-Guard an AmneziaWG node panel: Docker-only production deployment,
host kernel backend by default, and the pinned userspace backend as an explicit
advanced choice. Its binary does not run for kernel profiles. Preserve fake-backend
development without Docker or host-network mutation.

Keep the Go/SQLite/HTMX modular monolith, one node process and its bounded scheduler.
Preserve public account/device/template/automation behavior. Remote nodes, native
WireGuard, Xray, sing-box and OpenVPN are deferred; extension points are not a
support claim. Do not add generic protocol tables or new databases/services now.

## Proposed host layout

Use `/opt/wg-guard` for the Docker deployment bundle and `/var/lib` for persistent
application state. This is an operational Docker layout, not a claim that the
whole application is packaged as an FHS `/opt` software tree.

| Host path | Responsibility | Container access |
|---|---|---|
| `/opt/wg-guard/compose.yaml` | Versioned deployment manifest | none |
| `/opt/wg-guard/bin/` | Verified host manager | none |
| `/etc/opt/wg-guard/wg-guard.toml` | Host-specific boot configuration | one read-only file |
| `/etc/opt/wg-guard/tls/` | Managed certificate/key pair | approved read-only material |
| `/var/lib/wg-guard/node/` | DB, master key, backups and ACME cache | node data only |
| `/var/lib/wg-guard/host/` | Host state, journal and recovery artifacts | none |
| `/var/cache/wg-guard/` | Bounded acquisition/manager cache | none |
| `/var/log/wg-guard/` | Bounded private installer log | none |

Expose the command through `/usr/local/bin/wg-guard`. Docker owns image storage;
DKMS source, modules-load configuration, broker units, certificate hooks and locks
remain in their required system locations. Centralize validated host/container
paths rather than duplicating absolute strings. Do not mount host executables,
journals or recovery/cache material as writable node storage. Preserve the narrow
update request/status bridge.

An `.env` file is optional and contains only necessary non-secret deployment
selection. Runtime settings remain in SQLite; boot-only paths/listener/TLS remain
in boot configuration. Do not create three competing sources for one setting.

## Responsibilities and artifacts

- Deployment owns Compose rendering, immutable image identity and container lifecycle.
- Host platform owns prerequisites, Ubuntu/kernel checks, DKMS and certificates/proxies.
- Lifecycle owns one lock, durable stages, compatibility, rollback and recovery.
- Distribution binds release/image digest, binary identity, reviewed core and protocols.
- Application retains domain services, accounting, rendering and web/REST adapters.

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

1. Ship preparation on the existing layout. Verify an owner's backup off-host;
   this public release needs its own approval.
2. Implement Docker-only packaging, centralized layout/ownership and simplified
   terminal workflows as the next deployment change. Refuse unsupported direct
   upgrades from the old layout before mutation; migration is fresh install/restore.
3. Preserve public account APIs and archive import. Exercise interrupted operations
   and rollback, not only clean installation.
4. Run source/CI/artifact gates and a real Ubuntu drill: install, migrated restore,
   kernel and explicit userspace, reboot, renewal, update/recovery and preserved
   client/customer access. Historical native evidence cannot certify this layout.
5. Reassess native WireGuard or remote AWG nodes under a new implementation scope.

References: [deployment](../operations/deployment.md),
[recovery](../operations/lifecycle-recovery.md), [backup](../operations/backup-restore.md),
[resources](overview.md#resources), [upstream](../integrations/amneziawg.md),
[PasarGuard node API](https://docs.pasarguard.org/fa/node/api/),
[Xray API](https://xtls.github.io/en/config/api.html),
[FHS state directory](https://refspecs.linuxfoundation.org/FHS_3.0/fhs/ch05s08.html).

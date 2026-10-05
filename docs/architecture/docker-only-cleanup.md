# Docker-only deployment cleanup

Decision confirmed by the owner on 2026-10-04. Native production deployment will
be removed as an explicit Phase 17 deliverable. Correctness/artifact gates determine
when the change is deliverable, not whether native is retained as a second profile.
This specification is implemented in current unreleased source. Published v0.1.9 remains the
preparation build with its previous deployment contract; [Phase 17](../development/phase17.md)
records source/image and remaining physical-host acceptance separately.

## Meaning of native

Native here means running the WG-Guard production server directly as
`wg-guard.service` on the host. It does not mean the AmneziaWG kernel backend,
the host CLI, Linux tools, systemd host tasks or a future native WireGuard provider.
Kernel remains the default and explicit managed userspace remains supported.
Fake-backend developer execution is not a second production install mode.

## Critical assessment

Source inspection confirms mode-dependent execution in installation, artifact
staging/deployment, update/rollback, status, logging, restart, certificate/access
changes, restore, prerequisites, ownership validation and CLI routing. Many of
these branches share files and the same transaction engine; they are not entirely
duplicate implementations. The cost is nevertheless recurring alternative code,
state shapes, tests and documentation. One production path removes that cost.

Deleting a deployment option is not sufficient cleanup: remove its renderers,
artifacts, flags, state fields, error guidance and current acceptance cells together.
Conversely, a search-and-delete for the words `native` or `systemd` is unsafe.
Keep shared data/recovery/host behavior and historical evidence deliberately.
Do not replace the removed second backend with a generic deployment-plugin framework.
Retain narrow Host/process/filesystem seams for actual fault-injection boundaries.

## Source removal inventory

These paths identify responsibilities in the inspected baseline, not mandatory
future filenames. Package boundaries should follow behavior, not directory fashion.

| Area | Native-specific removal | Docker/shared behavior retained |
|---|---|---|
| `internal/install/plan.go`, CLI/wizard | `ModeNative`, production mode selection, native unit path/settings and help | One Docker deployment plan and immutable selected artifact |
| `internal/install/run.go` | `installNative`, panel service enable/start path | Prerequisites, owner/data preparation, transactional Compose install |
| `internal/install/update.go` | Native binary/unit deployment and native service stop/start/inspection branches | One coordinator, image/manager identity, backup, data compatibility, journal and recovery |
| `internal/install/render.go` | `RenderUnit`, native journal namespace/drop-in rendering | Compose, broker/retention ownership and required host tasks |
| `internal/install/logs.go` | Native panel `journalctl`/namespace selection | Docker service logs, private installer log and safe lifecycle outcomes |
| State/artifacts/validation | Configurable deployment mode and native `UnitPath`/retained-unit fields | Versioned Docker state, owned paths, hashes, previous/candidate image and host manager |
| Status/restart/health/doctor | Selecting a native panel service/backend environment | Docker runtime identity, liveness/readiness/TLS, host kernel/network observations |
| Restore/exposure/certificate sync | Native panel unit snapshot/restart/apply paths | Exclusive DB/key ownership, stable data, atomic cert pair and Docker activation |
| Core/prerequisites | AWG host-tool install/probe required only by native production | Host DKMS/module/headers, network utilities actually used on host, tools/daemon inside image |
| Uninstall | Native server unit/log-policy removal path in the new product | Owned Docker/core/exposure/broker cleanup, explicit data/package purge and recovery |
| Tests/fixtures | Current mode cross-products and native-only renderer/lifecycle tests | Equivalent Docker failures, generic locks/data/security, broker/host and fake-backend tests |
| Docs/menus/bootstrap | New-product native install/update/rollback guidance and flags | Current/historical release truth, fresh Docker install, old archive import and host recovery |

Do not blindly delete binary fields or `--binary` semantics: Docker also binds a
verified host-manager/panel binary to its image and may accept an explicit checked
local candidate. Evaluate those inputs against the remaining artifact contract.
Likewise, test fakes for the host are not native deployment support.

## New state and admission

- Use a versioned Docker-specific installed-state/artifact contract. Any deployment
  identity retained for reporting is descriptive, not a configurable mode switch.
- New CLI/bootstrap/wizards provide no native installation, mode switch, deprecated
  native alias or hidden fallback when Docker is unavailable. Validate unsupported
  flags before deployment mutation and as early as acquisition permits.
- Unknown/unreadable legacy state blocks mutation. Recognizing old state solely to
  explain migration is acceptable; executing a native lifecycle is not.
- The selected route for an existing native installation is backup with its supported
  old manager, off-host verification, fresh Docker install and data restore. No new
  live native-to-Docker converter or permanent native runtime is required.
- Refuse collision with an active/unowned old service/container or foreign files.
  Do not stop/delete them just because their names match a legacy convention.
- Previous/candidate automatic rollback artifacts are Docker-compatible. Restoring
  an old data archive is separate from executing an old native service artifact.
- Preserve archive readers and existing encrypted account/device state. Keep the
  original source build/backup available outside the rebuilt host for recovery.
- Existing managed Docker transitions need a supported explicit state/ownership
  migration when its layout changes; do not bypass validation by accepting arbitrary paths.

## Host functions that stay

The local manager continues to own prerequisites, image acquisition, status/logs,
core/DKMS, certificate activation, update/rollback, backup recovery and uninstall.
It must operate when the container is stopped or broken. Keep its verified cache
independent from the active artifact; do not add a second always-running local agent.

Systemd broker path/oneshot/timer, modules-load, temporary-file retention and
supported certificate hooks may remain. These are host infrastructure for Docker,
not a native WG-Guard server. The web container receives no Docker socket,
systemd control or arbitrary root executor.

## Milestones within Phase 17

| Milestone | Output |
|---|---|
| 17.0 | One image build/provenance recipe and verified candidate distribution |
| 17.1 | Native install/server renderer and mode branches removed; shared coordinator retained |
| 17.2 | Docker-specific state/admission and clear old-state migration refusal |
| 17.3 | Single-path update/rollback/log/status/restart/health/restore/exposure operations |
| 17.4 | Native-only tests/current docs removed; Docker/shared failure coverage preserved |
| 17.5 | Bounded hardening, offline recovery, supported-host/client acceptance and cleanup audit |

Publication of an official image needs approval, but local candidate construction
and cleanup tests are independent work. Native removal does not weaken the required
Phase 15 backup/readiness/slow-work gates or Phase 20 final certification.

## Completion criteria

- [x] New production parsing/wizards/recipes/state contain no native selection or
  executable native lifecycle; no `ModeNative`, `installNative` or panel `RenderUnit`
  remains in current production implementation.
- [x] The image is actually the supported server runtime; direct fake-backend
  development remains explicitly separate and does not certify host networking.
- [x] Source search results for `native`, `ModeDocker`, `systemd`, `UnitPath` and
  artifact/unit fields are reviewed semantically. Legitimate host tasks, native
  WireGuard terminology and historical fixtures are explained, not silently deleted.
- [x] One install/update/rollback/restart/restore/exposure/uninstall implementation
  covers Docker operation, pending jobs, interruption, disk pressure and failed health.
- [x] Wrong image/binary/data identities, old native state, unsupported flags and
  unowned paths fail before active mutation. No implicit pull/build/serve fallback exists.
- [x] Shared leases, encryption, idempotency, backup/restore, key/config/token/IP/
  quota preservation and network ownership retain equivalent or stronger coverage.
- [x] Host manager works offline and with a stopped container; broker/renewal/owned
  retention survives restart/rollback and is cleaned up without touching foreign resources.
- [ ] Supported Docker kernel and explicit userspace client traffic, reboot, update,
  restored backup and scoped removal pass real-host acceptance; other cells stay unverified.
- [x] Current living docs describe Docker-only only after implementation; old
  published docs/fixtures remain explicitly historical. Public API/OpenAPI changes
  follow affected contracts, not the absence of a deployment choice alone.

This plan authorizes repository refactor scope; it does not authorize a new public
release, registry publication or destructive operation on the owner's live server.
Dependencies and delivery gates: [Phases 15–20](../development/refactor-program.md).


Completed checkboxes record current source, host-seam and exact image/fake-container CI evidence,
not physical networking certification. That real-host checkbox remains open; the implementation,
initial fixture failure/correction and downloaded candidate identity are in [Phase 17](../development/phase17.md).

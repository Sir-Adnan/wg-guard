# Phase 20 — Refactor acceptance evidence

Status on 2026-10-05: **partially observed on an owner-operated host; not certified**.
The [execution program](refactor-program.md#phase-20--certification-migration-and-publication)
still owns the remaining acceptance requirements. Phases 15–19 source/CI evidence
remains in its existing records and is not replayed or relabelled here.

## Owner report

The owner reports a successful fresh installation of current main followed by
restoring an archive from the prior installation. The restored administrator login
uses the source archive's credentials, as designed; restored subscribers connect
successfully in their client applications. The owner has also confirmed that there
are no older deployments that require an in-place v0.1.9 conversion. The old-manager
bootstrap compatibility issue is addressed operationally by selecting the new manager
directly with `install.sh --commit main`; no old-state converter is added.

The subsequent Update Center screenshot identifies:

| Observation | Reported value |
|---|---|
| Panel/manager source | `e81e4a6efc61436f051d2348f83fb67fb7ab102a` |
| Version | `0.0.0-dev.e81e4a6efc61` |
| Deployment | Docker |
| Host | Ubuntu 26.04, amd64 |
| Kernel | `7.0.0-30-generic` |
| Recorded core bundle | `awg-2026-09` |
| Configured backends | Kernel: 2; Go: 0 |
| Observed module identity | Active build matches disk |
| Tools identity | `amneziawg-tools v3.1.20260812` |

This is an owner report and screenshot observation, not an agent-run server drill.
The backup creator/version and byte-for-byte before/after comparison were not supplied.
No credentials, customer capability URLs, hostnames or raw configurations are retained
in this record. Ubuntu 26.04 success in this scope does not certify either that host
version or the new deployment on the historical Ubuntu 24.04 support target.

## Open acceptance

The owner's subsequent [incident/recovery report](runtime-recovery.md) adds a real
source update, pre-update backup, failed readiness/preserved recovery on kernel
`7.0.0-38`, one-time previous-kernel boot, module loading, completed recorded rollback
and successful panel upgrade to `31de1d587b52537f3f6564af9563fc8e84917f5c` on
`7.0.0-30`. It does not certify the failed kernel or the entire failure/resource/TLS
matrix. Disk-backed acquisition also completed within this update journey.

The exact release candidate and its downloaded artifacts still need their release
gates. Physical reboot, upgrade/rollback, unavailable-container/offline recovery,
interrupted operation, explicit userspace, real CA issuance/renewal/replacement,
separate-origin routing, shaping and measured load/resource/enforcement checks have
not been established by this report. Subscriber connectivity is useful evidence,
but does not substitute for the complete restore inventory/access comparison.

The owner conditionally requested **v0.1.10 after the remaining phases and work are
complete**. This supersedes the earlier no-further-release instruction for that version
only, subject to these gates. No registry publication or agent-run live-host mutation
is authorized by that request. Latest public stable remains v0.1.9 until publication
actually completes.

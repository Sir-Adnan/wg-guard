# Phase 20 — Refactor acceptance evidence

Status on 2026-10-05: **partially observed on an owner-operated host; not certified**.
The [execution program](refactor-program.md#phase-20--certification-migration-and-publication)
still owns the remaining acceptance requirements. Phases 15–19 source/CI evidence
remains in its existing records and is not replayed or relabelled here.

## Authorized isolated drill in progress

The owner subsequently provided a raw dedicated VPS and explicitly authorized
Phase 20 installation/testing, documentation updates and v0.1.10 publication after
completion. This supersedes the earlier absence of live-host authorization for
this isolated target only. The host is Ubuntu 24.04.4 amd64, kernel
`6.8.0-138-generic`, two vCPUs and about 3.8 GiB RAM. No credentials or hostnames
are retained in the repository.

Exact `57f48eb99f20411b04d33c048c9d6b7da8792014` installed through the pinned Bash
entry on the empty host; reviewed tools/module/source-built Docker runtime and
readiness passed. Actual owner login, template terms, three API/panel config
comparisons and independently decoded QR payloads passed. Two kernel profiles and
one explicit userspace profile passed gateway/bidirectional and public DNS/HTTPS/NAT
traffic on real host network-namespace clients. These are Linux client drills,
not physical mobile-app verification. Initial HTTP-01 HTTPS and an independent
subscription certificate were issued and admitted. Further recovery/resource/TLS
and exact delivery gates remain in progress; this section is not phase completion.

The multiple-pool drill exposed a diagnostic defect: Doctor loaded only primary
CIDRs and rejected the complete Docker allow chain containing overflow CIDRs.
Current source decodes the same ordered pool inventory used by reconciliation.
Focused regression checks accept complete primary/overflow paths and still reject
missing overflow paths. The correction must also pass on the real updated target.
The owner's additional installer request adds an explicit domain/SSL menu shortcut,
guided first HTTPS setup, separate panel/subscription acquisition/replacement and
due renewal actions; source/terminal/bootstrap checks remain separate from CA evidence.

On exact `5f20efc81697b03ec624c3ac3e960bb44343fcfd`, the updated real Doctor accepts
all primary/overflow forwarding scopes. Encrypted create/verify/coordinated restore
passed; a separately staged original archive matched 18 logical table inventories,
master key, canonical config bytes, owner/reseller login and API credentials. The
comparison excludes live accounting observation baselines/timestamps and sessions,
not charged usage, quotas, expiry, key material, identities or access ownership.
Actual scoped Certbot renewal issued a new subscription leaf and its deploy hook
admitted the replacement. Changing the panel hostname preserved the separate public
origin, denied the old private origin and retained config/customer access. The public
origin denied login/dashboard/API/readiness and SNI/Host mismatch while matching
canonical config bytes under system-trusted HTTPS.

The switched managed-certificate policy exposed another Doctor defect: the legacy
manual check expected a single `tls.cert_file` after the SNI policy had become the
certificate authority. Current source validates the approved bounded policy/pairs,
keeps delegated built-in/external checks separate and fails missing/invalid material.
Interactive SSL status now shows readable address, ownership, expiry and renewal
guidance instead of policy JSON and zero dates. Real corrected-target checks and
final delivery are still pending.

The real reboot moved from `6.8.0-138-generic` to the already installed
`6.8.0-146-generic`. The initial header-meta package transaction preceded reviewed
source registration, leaving DKMS installed only for the original running kernel.
The new boot had no module/kernel links and readiness correctly returned 503.
Current source builds the selected reviewed module for the running kernel and a
bounded inventory of already bootable, header-ready installed kernels. It does not
build unrelated DKMS modules or claim compatibility with failed future headers.
Focused tests cover the additional boot target, unavailable headers/build failure
and inventory bounds. Real corrected reinstall/reboot must pass before publication.

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

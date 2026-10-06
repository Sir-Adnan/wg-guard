# Phase 20 — Refactor acceptance evidence

Status on 2026-10-06: **complete within the matrix below; published as v0.1.10**.
The [execution program](refactor-program.md#phase-20--certification-migration-and-publication)
owns the acceptance requirements; excluded cells below remain unverified.
Phases 15–19 source/CI evidence remains in its existing records and is not
replayed or relabelled here.

## Publication

Exact released source `4f78adcc4add323035e43f5404f75392938207ee` passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37460729245) and the
[manual release workflow](https://github.com/Sir-Adnan/wg-guard/actions/runs/37461855976). All seven public
assets were independently downloaded: tag→commit, six checksums, release/runtime metadata,
SBOM root and runtime image config identity matched; binary SHA-256
`53675169480beb3ff41b7b8d43f5a76be1c9553643834e263a06e59dcf845e9b`, image
`sha256:d3645ff85c36d62efd0d99fddf4c908f4b54894798664e517b399a586476bff6`.
v0.1.10 is Latest stable (published 2026-10-06). No registry image was published.

## Completed host scope

Final real-host candidate `707b92828b1ac745359617b973e9450d90ad959d` includes
the corrected DB/key target derivation and canonical installed-kernel inventory.
After deliberate removal of the prior-kernel DKMS build, the installer restored
its reviewed `6.8.0-138` build while running `6.8.0-146`. Fresh install from the
independently verified encrypted archive passed before listener start; all 18
logical table inventories, source key/configs, owner/reseller login and API access
matched the staged original. A subsequent real `6.8.0-146` reboot automatically
returned readiness and all three real kernel/userspace DNS/HTTPS/NAT client paths.

| Gate | Observed scope/result |
|---|---|
| Empty-host source install | Ubuntu 24.04.4 amd64, reviewed tools/module, Docker image/readiness passed |
| Kernel / explicit userspace | Recommended and randomized kernel plus recommended userspace; gateway, bidirectional counters, DNS/HTTPS/NAT passed |
| Config / QR / access | Three API/panel byte comparisons and independent QR decodes; source owner, reseller permissions/template assignments, scoped purchase and foreign-owner denial passed |
| Crypto / restore | Encrypted create/verify/apply, staged-original logical comparison, separately downloaded off-host verification and fresh-target initialization passed |
| Domains / CA | Actual HTTP-01 panel/public issuance, one scoped live renewal and admitted replacement, panel hostname change, old private-origin denial and SNI/Host/public-route isolation passed |
| Lifecycle failures | Actual source update with pre-backup, SIGKILL at journal `started`, retained artifacts and completed rollback/recovery passed |
| Offline / image availability | Owned container removed; cached image restart with unavailable download proxies returned readiness without registry/GitHub access |
| Corrected boot | Installed/header-ready older kernel covered; reboot on `6.8.0-146`, readiness and all client profiles passed |
| Admission failures | Wrong encrypted-archive password rejected; no restore approval/apply was requested |

Measured two-vCPU/about-3.8-GiB host windows: the initial small fixture averaged
64,201,933 B RSS, peaked at 64,339,968 B and used 0.183% of one CPU over 60 s.
With 1000 synthetic idle devices/ten synthetic accounts, a 60 s window averaged
94,265,822 B RSS, peaked at 95,563,776 B and used 0.167%, with nine peak threads.
This is control-plane load, not 1000 active clients. Real 524,288-byte transfers
at 1000 Kbps took 4.192 s down and 4.520 s up. During actual encrypted panel
backup, process RSS peaked at 372,908,032 B; eight concurrent write probes peaked
at 0.0136 s. Quota removal was observed at 22.310 s after the reviewed setup and
expiry at 22.849 s since the workload start; both ineligible peers were absent.
Those are fixture measurements including request/scheduling timing, not universal
one-cadence latency promises. The expiry-aware eligibility-blind Doctor peer-count
warning remains a documented heuristic; authoritative reconciliation/readiness and
traffic gates were checked separately.

The support claim here is Ubuntu 24.04 amd64 Docker on the listed generic kernels
and pinned kernel/userspace builds. Native deployment, Ubuntu 26.04/`7.0.0-38`,
IPv6, DNS-01, external proxy/firewall coexistence, physical mobile apps, simultaneous
1000-client handshakes, multi-day soak and an observed long-interval CA renewal
remain excluded/unverified. The isolated target was explicitly owner-authorized;
no original customer server was rebuilt. Exact main/release workflow and independent
public version/tag/asset verification passed (see Publication).

## Post-acceptance source change

After the final host candidate, the interactive terminal renewal entries were
routed by the recorded certificate owner: an owned automatic lineage runs the due
renewal, the built-in issuer runs a live certificate check, and manual/external
certificates name their owner instead of attempting issuance. A legacy panel
policy keeps `exposure renew`. This is a terminal-menu selection change covered by
focused unit tests and the release source gates; it does not alter the CA/renewal
engines exercised on the host and was not separately replayed there.

## Correction sequence

This section preserves the drill order and the defects it exposed; each correction
is closed by the final candidate in [Completed host scope](#completed-host-scope).

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
and exact delivery gates followed on the corrected candidates below.

The multiple-pool drill exposed a diagnostic defect: Doctor loaded only primary
CIDRs and rejected the complete Docker allow chain containing overflow CIDRs.
Current source decodes the same ordered pool inventory used by reconciliation.
Focused regression checks accept complete primary/overflow paths and still reject
missing overflow paths. The real updated target then passed (below).
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
guidance instead of policy JSON and zero dates. The corrected target passed Doctor
with the managed SNI policy.

The real reboot moved from `6.8.0-138-generic` to the already installed
`6.8.0-146-generic`. The initial header-meta package transaction preceded reviewed
source registration, leaving DKMS installed only for the original running kernel.
The new boot had no module/kernel links and readiness correctly returned 503.
Current source builds the selected reviewed module for the running kernel and a
bounded inventory of already bootable, header-ready installed kernels. It does not
build unrelated DKMS modules or claim compatibility with failed future headers.
Focused tests cover the additional boot target, unavailable headers/build failure
and inventory bounds. The corrected reinstall/reboot passed at the final candidate.

The fresh archive install also exposed omitted derived DB/key paths in
`Plan.BootConfig`: runtime config loading completed them, while initial offline
archive application received the incomplete value and refused the paired rename.
The plan now completes dependent paths before passing it to the archive engine;
the initial-data ordering test asserts those exact targets. The failed target
never opened a managed listener, and the independently verified off-host archive
was retained throughout retry/recovery. Corrected fresh-target acceptance then passed.

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

## Earlier owner recovery report

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
only, subject to these gates. The later live-host authorization covers only the
dedicated isolated VPS above; registry publication remains unauthorized.

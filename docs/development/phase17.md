# Phase 17 — Docker-only distribution and native removal

Source implementation on 2026-10-05, following verified Phase 16. The owner asked to continue
without another public release. Latest stable remains the preparation v0.1.9. No live-server
mutation, rebuild or registry publication is performed. This record distinguishes source,
image/fake-container, physical host/client and publication acceptance.

## Final boundaries

- Production has one Docker plan. Removed `ModeNative`, native server install/start/stop,
  `RenderUnit`, journal namespace policy, native artifact/state unit fields and mode flags/choices.
  Logs/status/doctor/restart/restore/exposure/update/rollback/uninstall use that one path.
- Explicit fake development remains direct; real `serve` outside the runtime is refused.
  Kernel/DKMS, module persistence, host diagnostics, broker/renewal/retention and the independent
  manager remain host responsibilities. Container tools/userspace are not host installations.
- Host state schema 4, journal schema 2 and installer contract revision 3/deployment schema 4
  define the new boundary. Legacy state/paths, retired/unknown fields and ambiguous records fail
  closed. Legacy service/container collision probes are read-only, never adoption/removal.
- `/opt/wg-guard/compose.yaml` separates deployment; boot/TLS and node data retain their existing
  paths. Host state/journal/retained artifacts are private `/var/lib/wg-guard-host`, not a mount.
  Backup import remains logical archive schema 1, preserving its existing data/key checks.
- One embedded recipe replaces root Dockerfile versus installer recipe divergence. Image
  construction consumes the already verified binary and embedded legal inventory. Its record
  binds immutable image ID/archive bytes, binary SHA/commit, exact reviewed engines/kernel list,
  deployment/data/maintenance contracts, notices and the linked-Go-module SBOM.
- New releases require `runtime-metadata.json` and `runtime_linux_amd64.tar.gz`; missing old-format
  images never trigger production compilation. Explicit source acquisition uses the same recipe.
  Offline `image-import` hashes before loading and rechecks platform/provenance. A verified cached
  release image needs no network/build/load, and retained artifacts can be inspected without a
  container. Candidate preparation retains the verified release descriptor with its private receipt.
- Generated deployment pins local image IDs, forbids implicit pulls, drops capabilities except
  NET_ADMIN/NET_BIND_SERVICE, uses no-new-privileges/read-only root and bounded run/tmp mounts.
  No Docker socket, privileged mode, module loading inside the container or silent backend switch.
- Update acquisition/contract/hash checks precede stop. Backup, stop/deploy, readiness/TLS,
  persistence, cancellation and independent recovery retain the shared coordinator/journal.
  Shared DB/key leases and durable peer removal are unchanged.
- Telegram multipart/probe staging uses a private disk directory in the node volume; cleanup
  remains on normal completion/cancellation. It does not consume the small AWG tmpfs. Large
  independent archive verification normally runs on the host, separate from the installed node.

The new recipe uses reviewed engine Git commits; Ubuntu build/runtime package inputs are not
claimed bit-for-bit reproducible. Published image/archive hashes freeze the actual result.
The bound SBOM covers the main binary's linked Go modules, not an invented complete inventory
of Ubuntu packages or the userspace daemon's transitive dependencies. Broader image SBOM and
base-image lock policy can be added with a concrete maintenance owner.

## Checks and evidence

Fresh focused Go checks cover distribution metadata/archive integrity and cleanup; wrong binary,
platform, data/deployment/core/label identities; offline verified image reuse; strict old state,
retired fields, trailing records and unowned legacy unit/container refusal. Native-only cells
were removed while Docker fault/recovery, stop-before-delete, lock, backup, owner-before-listener,
key/tenant/accounting, core/reboot, broker and retained-artifact tests remain.

The resulting source is checked with the full Go suite, vet/build, formatting/diff inspection and
Linux bootstrap fixtures. A fresh selected encrypted-archive enforcement load check covers the
changed disk-delivery path; historical Phase 16 numbers are not relabeled as a fresh result.
Exact CI also builds the manager/canonical Docker image, verifies image/binary/core provenance,
checksums and an isolated fake-node readiness/DB/key/restart/read-only-root smoke test. CI retains
unpublished candidate assets for seven days; it does not publish a release or registry image.

Local Windows Go checks cannot supply Linux race evidence. WSL has no enabled Docker engine;
actual image/fake-container evidence must come from the new exact CI job. The gate has now passed at the exact source recorded below; no local Docker result is inferred.

Fresh ordinary Linux encrypted-archive load after disk-spool changes: 512 fake devices,
GOMAXPROCS=1, quota lag/expiry cycle 3/3 ms against a 15 s cadence; RSS baseline/peak
76.0/333.8 MiB; goroutines 9 at baseline and 3 after drain. This proves the isolated production-
code path, not a VPS/kernel/client limit. Fresh local full Go tests, focused final image/CLI/
host checks, vet/build and WSL bootstrap fixtures passed. Unchanged packages used applicable
Go cache. The pre-commit bootstrap artifact fixture still freezes the prior HEAD; exact CI
must additionally test the new committed artifact.

## Remaining acceptance

First exact CI candidate `5ba43fe9641e23a2ac9f3eb6d4d6e9b8719fcfa8` built and verified the
image/archive but its fake readiness fixture failed. The 0700 runner-owned bind directory did
not match production's root-owned volume under dropped DAC_OVERRIDE. The fixture now uses the
correct owner, with bounded safe diagnostics; a fresh exact gate must confirm the correction.
Offline import and manager promotion additionally hash the actual manager before contract
execution; a wrong-byte regression ensures the candidate is never probed.

Phase 17 source removal/distribution is implemented; the entire production acceptance program
is not complete merely because source/image/fake tests pass. Physical Ubuntu Docker kernel and
explicit userspace traffic, reboot, restored archives, update/failure/offline-manager recovery,
certificate behavior and idle/load/backup resource ceilings remain the authorized Phase 20
host/client gate. Current/previous recorded file artifacts are bounded; Docker images/build
cache are deliberately not globally pruned because they can be shared/needed for recovery.
An explicit owned-image retention/recovery policy needs its own Docker/shared-use test before
automatic deletion is claimed.

The owner still needs the independently verified off-host v0.1.9 backup before any rebuild.
Next source work is integrated domain/TLS ownership (Phase 18), then operational UX (19).
No later release is authorized by this record. Design rationale: [ADR-0015](../decisions/ADR-0015-docker-only-runtime.md).


## Exact delivery result

`5952d97bb07b656db4d931230a4f4c671be9d3e4` passed [main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37247158064) on 2026-10-05.
Both Go race matrices (1.25.x/stable), selected encrypted-archive load, vet/build, bootstrap/
synthetic fixtures and vulnerability scan passed. The runtime job built the canonical image,
checked actual binary equality and tools/daemon VCS identity, verified all artifact checksums,
removed/re-imported the compressed image without registry/source access, then passed fake-node
readiness, persistent DB/key, restart and read-only-root checks. Correct root ownership confirmed
the earlier fixture diagnosis; the first failure is retained above rather than hidden.

Unpublished Actions artifact `11319178954` (67,176,808 bytes), ZIP SHA-256
`f16abb5e351449909930245dca8f7cf79ed114a36e785f83598cbbb6500dac66`, was independently downloaded.
All six checksum entries passed and metadata/binary identify this exact source; the downloaded
Linux binary also ran its version probe in WSL. Runtime image config ID:
`sha256:10a25f257cc114eea9b004b9ae9c27c1fa0703e5938244d211b13ff79771b4cf`.
Runtime metadata SHA-256: `fe218455b186b86ce103db47f5e9d3598f365ba0efe745522d3569f9d2eaf972`.
This is a checked unpublished candidate, not a public release/registry or physical-host claim.

Phase 17 source/distribution/removal gates are complete within this evidence scope. The physical
milestone 17.5/client/resource portions remain open under Phase 20. Current published v0.1.9,
the owner's off-host backup checkpoint and the no-further-release instruction remain unchanged.

# Phase 11 — Production certification

Status: **complete for the certified Ubuntu 24.04 amd64 scope** (2026-09-28). Phase 10 is
frozen; Phase 12 and public release have not started.

## Objective

Certify the exact feature-frozen product against realistic security, concurrency, performance,
networking, recovery, supported-Ubuntu, backend and deployment risks.

## Scope and deliverables

- Threat-model/security review of auth, permissions, sessions/tokens, secrets, subprocesses,
  configs/QR/subscriptions, webhooks, backups, database, host networking and deployment.
- Race, soak, fuzz, failure-injection, dependency-vulnerability and resource-leak testing.
- Idle/load RSS and CPU, API latency, SQLite, scheduler/accounting, telemetry, webhook, binary,
  frontend and install-footprint measurements at 0/100/1000 users/devices.
- Real 1000-shaped-peer tc/IFB workload and documented degradation/operational guidance.
- Extend Phase 8.3's verified Ubuntu 24.04 Docker public-egress baseline across native,
  firewalld/non-default policies, later supported Ubuntu releases and long-running traffic.
- Ubuntu 24.04 and later supported releases on amd64; Docker/native; kernel/userspace matrix.
- Backup/restore, key rotation, restart/reboot, update/automatic and interrupted rollback,
  uninstall/reinstall, corrupt/missing state, disk pressure, and log growth drills.
- Explicit purge ownership (AUD-040): exclude independent admitted data commands and prevent
  admission during whole-directory deletion; verify lock-inode lifecycle and interruption.
  Current operator-managed quiescence is not evidence of concurrent purge safety.
- Re-certify runtime-safe interface deletion across the supported matrix. AUD-050 implementation
  and exact Docker create/delete evidence are complete in Phase 10 and are not reopened without a regression.
- ACME/manual/proxy/dev TLS behavior, cache/reissuance and renewal paths.

## Milestones

1. Freeze the candidate behavior and review/triage every open audit finding.
2. Complete security, race, fuzz, soak and resource-leak work; fix and retest findings.
3. Run 0/100/1000 performance and shaping/network coexistence workloads.
4. Execute destructive recovery, deployment, disk/log and TLS drills.
5. Execute and record the supported-Ubuntu/backend/deployment matrix.
6. Re-run affected certification cells, synchronize docs/status, and close the phase gate.

## Verification policy

- Unit/integration/emulation evidence never upgrades a real-host matrix cell.
- The dedicated Ubuntu 24.04 amd64 VPS supplies its cells; a later Ubuntu release needs a
  genuine host before it is advertised as verified.
- Unavailable cells remain unverified and are removed from support claims rather than inferred.
- No critical/high finding may remain unresolved. Medium/low deferrals require impact and reason.
- ACME renewal uses deterministic automated tests and practical real challenge/cache/reissuance
  drills; an unobserved 60-day production interval remains labeled honestly.

## Current certification evidence

- Managed, source-checked userspace profiles passed real Ubuntu 24.04 Docker and native client
  HTTPS, daemon recovery and reboot alongside kernel profiles. The native userspace client
  sustained 120 requests over two minutes; a separate kernel client passed public HTTPS.
  Docker-aware doctor probes the owning namespace. No REST/OpenAPI shape changed.
- A real kernel upgrade and Docker restart exposed missing generic headers and lost native
  forwarding rules. Header-meta provisioning, native unit ordering and bounded policy repair
  passed subsequent reboot, deliberate rule-loss and client-traffic drills. The owned path
  repaired within seven seconds after deliberate jump removal.
- Encrypted backup/restore, key rotation, wrong/missing-key refusal, corrupt/missing-state
  refusal, update/rollback, interrupted update recovery, disk-full cleanup and concurrent purge
  refusal passed targeted automated and Ubuntu 24.04 drills. Full purge removed a real kernel
  link with a peer as well as the userspace link; only the admission-lock tombstone remained.
- The 0-peer control-plane sample measured 33 MB RSS/0.00% CPU. Ten-minute 100/1000-user+device
  windows measured 41/52 MB average RSS, 45/57 MB peak RSS and 0.03/0.04% CPU, within the
  documented runtime budgets. Real tc/IFB accepted 1000 classes/filters per direction in 134 ms
  on WSL and 405 ms on the dedicated VPS. This does not claim 1000 simultaneous handshakes.
- Full WSL race, 4.75 million dump-parser fuzz executions and Go 1.27.1 reachable-vulnerability
  scanning passed. Docker pins the patched Go toolchain. Focused Chromium/WebKit checks passed
  Persian numeric alignment without replaying the completed Phase 10 browser matrix.
- Active firewalld is refused for enabled tunnels because its zone verdicts have no certified
  owned allow path. Exact `fd64502` Docker/native smoke on Ubuntu 24.04 restored userspace and
  kernel links, reported healthy forwarding and then removed both links and owned rules. A
  read-only simulated firewalld CLI made Doctor fail explicitly. The full Windows test/build/vet
  and WSL `-race ./...` gates passed on this code; CI on integrated `main` is the final check.

The production claim is limited to the listed Ubuntu 24.04 amd64 paths. Later Ubuntu releases,
active firewalld, real-host UFW coexistence, 1000 simultaneous handshakes, a multi-day traffic
soak and an observed long-interval ACME renewal are unverified. UFW's scoped route behavior and
renewal logic have automated coverage; no result is inferred for those real-host cells.

## Documentation

Update security/threat model, testing/benchmarks, compatibility, AWG/networking, deployment,
runbook/recovery, status/release tracker, requirements, and CHANGELOG with sanitized evidence.

## Completion criteria

RB-007 closes for Ubuntu 24.04 amd64 Docker/native kernel/userspace and the explicitly listed
network paths: recovery and traffic drills pass, resource budgets are met, no critical/high
finding remains open, and the feature-frozen code passes the complete local automated suite.

## Deferred to Phase 12

Artifact/version metadata, final documentation and repository freeze, release-pipeline dry run,
candidate artifact installation, and the final readiness report.

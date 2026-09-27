# Phase 11 — Production certification

Status: **in progress** on `codex/phase11-certification`; Phase 10 is frozen. No public release.

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

- Initial audit triage covers AUD-019/037/040/055/056/057. A service-owned, source-checked
  userspace daemon now follows the configured backend mode. Pinned-source WSL integration and
  exact-image Ubuntu 24.04 Docker creation, public HTTPS client traffic, crash/restart recovery,
  and encrypted backup/restore with peer reconciliation pass. Read-only doctor mode inspection
  now probes the container namespace in Docker; its real-host correction gate remains pending.
  This activates the existing `backend_mode` contract; no REST/OpenAPI shape changed.
- Malformed legacy rotation flags and short noninteractive owner names fail before data/service
  mutation in focused tests. Explicit data purge now excludes admitted readers, retains a lock
  tombstone against new admission, and requires a clean volume before reinstall; Windows and WSL
  lock tests pass. Legacy token commands now reject incomplete flags before opening data.
  Real-host purge interruption/concurrency drills remain pending.
- Command output now has bounded capture with explicit truncation failure; a rejected userspace
  version cannot trigger link rollback, and native offline doctor does not claim to repair a
  daemon that its short-lived process cannot own.
- The owner-reported Persian numeric-card alignment correction passed affected Chromium/WebKit
  user and public-subscription compositions; the full Phase 10 matrix is not being replayed.
- The 0-peer synthetic control-plane sample measured 33 MB RSS/0.00% CPU over 30 seconds on WSL2.
  Ten-minute 100/1000-user+device windows measured 41/52 MB average RSS, 45/57 MB maximum RSS,
  and 0.03/0.04% CPU, below the 50/80 MB and 0.5% budgets. A real Linux tc/IFB test applied
  1000 classes/filters in each direction in 134 ms and verified idempotence; the dedicated VPS
  applied the same real-kernel workload in 405 ms. Traffic under 1000 real handshakes is unclaimed.
- WSL full `-race ./...`, 4.75 million dump-parser fuzz executions, and `govulncheck@v1.7.0`
  with Go 1.27.1 pass (zero reachable findings; three module advisories are not called). Go 1.26.0
  reported standard-library findings that the patched toolchain removes. Docker now pins
  Go 1.27.1 for the image build.

## Documentation

Update security/threat model, testing/benchmarks, compatibility, AWG/networking, deployment,
runbook/recovery, status/release tracker, requirements, and CHANGELOG with sanitized evidence.

## Completion criteria

RB-007 closes: supported matrix cells have real evidence, recovery and traffic drills pass,
budgets are met or professionally revised with rationale, no unresolved critical/high finding
remains, and the feature-frozen revision passes the complete automated suite.

## Deferred to Phase 12

Artifact/version metadata, final documentation and repository freeze, release-pipeline dry run,
candidate artifact installation, and the final readiness report.

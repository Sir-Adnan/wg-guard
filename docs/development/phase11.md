# Phase 11 — Production certification

Status: **planned; not implemented**. Starts after Phase 10 feature and UI freeze.

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
- Interface deletion ordering (AUD-050): tear down the still-owned runtime link before final DB
  removal, recover safely on backend/DB failure, and verify API, web and real-kernel behavior.
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

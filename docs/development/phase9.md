# Phase 9 — Operational observability

Status: **active; milestone 9.2 in progress**. The metric/log contracts, ADR and resource budgets
were accepted on 2026-09-05 after Phase 8 closed RB-001 through RB-004. Phases 8.1 and 8.2 then
completed the delivery/lifecycle and secure-access prerequisites. Execution began from clean
`main` revision `cb728945a348944dc86d3d485babbc1123bf4492` on 2026-09-10. Deterministic expanded
`/proc` fixtures are part of milestone 9.1 rather than completed 9.0 evidence.

Detailed dependency-ordered execution plan:
[`../superpowers/plans/2026-09-05-phase9-operational-observability.md`](../superpowers/plans/2026-09-05-phase9-operational-observability.md).

## Objective

Make node health and failures quickly understandable from the dashboard and one CLI workflow,
with bounded resource use, bounded log storage, and no secret disclosure.

## Scope and deliverables

- One scheduler-driven telemetry sampler shared by all administrators; no per-browser sampler.
- Bounded in-memory history for CPU, RAM, disk, host/interface RX/TX and rates, process/node/AWG
  health, online users, active peers, and relevant tunnel traffic.
- Functional live dashboard cards/graphs with sensible refresh and hidden-tab pause. Phase 10
  changes presentation, not the data contract.
- Authorized API/OpenAPI telemetry changes where external node statistics benefit.
- Mode-aware `wg-guard logs` with bounded tail, follow, since/time and component filters.
- Docker, native/systemd, installer, update/rollback, AWG, and networking diagnostic sources
  where the platform provides them.
- Seven-day default retention plus disk-size bounds, using OS/container mechanisms when they
  provide the safer implementation.
- Central redaction/safety rules: never expose passwords, tokens, private keys, raw configs,
  webhook/backup secrets, or capability URLs.

## Milestones

- [x] 9.0 — Freeze metric/log/retention contracts, ADR, and resource budgets.
- [x] 9.1 — Implement host/network/process collectors and the bounded telemetry ring.
- [ ] 9.2 — Compose one sampler into the central scheduler and health/metrics surfaces. **Active.**
- [ ] 9.3 — Add the authorized REST/OpenAPI telemetry contract.
- [ ] 9.4 — Move the dashboard to shared snapshots and add functional live graphs.
- [ ] 9.5 — Install central structured-log redaction and component classification.
- [ ] 9.6 — Implement the mode-aware `wg-guard logs` workflow.
- [ ] 9.7 — Enforce native/Docker/operation-log retention and disk bounds.
- [ ] 9.8 — Run real failure/resource drills and close RB-005 with evidence.

## Verification

- Fixture tests for `/proc`, network counters, unavailable/partial metrics, counter resets, and
  bounded histories.
- Fake-clock tests for retention and deterministic sampling.
- CLI routing/follow/cancellation tests through the install `Host` seam.
- Secret-corpus tests over errors and output; race and resource-overhead benchmarks.
- Real Ubuntu 24.04 Docker and native drills: service crash, bad AWG apply, firewall/network
  error, installer/update failure, follow behavior, retention policy, and traffic graphs.

Milestone 9.1 uses deterministic Linux `/proc` fixtures for host/default-route/owned-interface
counters and process RSS. Counter continuity, reset/replacement/gap handling, partial sources,
fixed ring capacity, chronological copy snapshots, health transitions and concurrent readers pass
focused tests and the race detector in WSL2 Ubuntu amd64. This is implementation evidence, not the
real-VPS telemetry gate.

## Documentation

Update architecture overview/project structure, deployment/runbook/security, API/OpenAPI,
testing/status/release tracker, and CHANGELOG in the same changes as behavior.

## Completion criteria

RB-005 closes: one dashboard and one CLI workflow explain real failures, log growth is bounded,
secrets stay absent, tests/race pass, and measured idle/live overhead fits documented budgets.

## Deferred to Phase 10

Final dashboard styling and the complete visual-system/page migration. Long soak, load, and later
supported Ubuntu amd64 certification remain Phase 11.

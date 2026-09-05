# Phase 9 — Operational observability

Status: **active; milestone 9.0 complete, 9.1 next**. Started 2026-09-05 after Phase 8 closed
RB-001 through RB-004 with dedicated real-VPS and browser evidence.

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

- [x] 9.0 — Freeze metric/log/retention contracts, ADR, fixtures, and resource budgets.
- [ ] 9.1 — Implement host/network/process collectors and the bounded telemetry ring.
- [ ] 9.2 — Compose one sampler into the central scheduler and health/metrics surfaces.
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

## Documentation

Update architecture overview/project structure, deployment/runbook/security, API/OpenAPI,
testing/status/release tracker, and CHANGELOG in the same changes as behavior.

## Completion criteria

RB-005 closes: one dashboard and one CLI workflow explain real failures, log growth is bounded,
secrets stay absent, tests/race pass, and measured idle/live overhead fits documented budgets.

## Deferred to Phase 10

Final dashboard styling and the complete visual-system/page migration. Long soak, load, and
cross-platform certification remain Phase 11.

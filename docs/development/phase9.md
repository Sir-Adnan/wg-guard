# Phase 9 — Operational observability

Status: **planned; design branch paused for Phase 8.1**. Initial design began 2026-09-05 after
Phase 8 closed RB-001 through RB-004. The design-only `codex/phase9-observability` branch is
preserved. Implementation follows [Phase 8.1](phase8.1.md) installation/lifecycle verification.

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

1. Define the metrics, sampling cadence, bounded history, health semantics, and resource budget.
2. Implement/test host, network, process, node, and AWG collectors.
3. Expose dashboard/API data and efficient refresh behavior.
4. Implement Docker/native/owned-operation log adapters and the CLI contract.
5. Implement/test retention, rotation, disk bounds, and redaction.
6. Run real failure drills, measure overhead, and synchronize docs/status.

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

# Phase 9 — Operational observability

Status: **active; milestone 9.7 in progress**. The metric/log contracts, ADR and resource budgets
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
- [x] 9.2 — Compose one sampler into the central scheduler and health/metrics surfaces.
- [x] 9.3 — Add the authorized REST/OpenAPI telemetry contract.
- [x] 9.4 — Move the dashboard to shared snapshots and add functional live graphs.
- [x] 9.5 — Install central structured-log redaction and component classification.
- [x] 9.6 — Implement the mode-aware `wg-guard logs` workflow.
- [ ] 9.7 — Enforce native/Docker/operation-log retention and disk bounds. **Active.**
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

Milestone 9.2 composes exactly one sampler into the existing scheduler, takes an initial sample
before `Start` returns, queries activity/interface state in one aggregate SQLite statement, tracks
recent accounting failure/recovery, and exposes only aggregate health/cadence/rates from the
optional Prometheus endpoint. Focused unit and WSL2 race suites pass; real traffic/overhead remains
the milestone 9.8 VPS gate.

Milestone 9.3 adds `GET /api/v1/node/telemetry` under `stats.read`, with a 60-point default,
180-point hard cap, chronological samples, nullable unavailable values, non-secret health codes,
and no topology. Handler/auth/bounds/serialization tests and bidirectional route/OpenAPI coverage
pass. The contract is additive; no existing V1 field or meaning changed.

Milestone 9.4 removes per-browser host sampling and the duplicate online-device query. The live
fragment reads the shared ring every 10 seconds, distinguishes online users from active peers,
and renders health, CPU, memory, VPN rates, activity and host/process details with bounded CSP-safe
SVG sparklines. Unit/i18n tests cover healthy/degraded/unavailable/stale states, gaps, flat series,
copy-only reads and topology secrecy. Asset budgets pass; manual 1440×900 and 390×844 browser
smoke passed in fa/RTL and en/LTR with no horizontal overflow. This is local fake-backend browser
evidence, not the milestone 9.8 real-VPS traffic gate.

Milestone 9.5 adds one recursive, bounded `slog.Handler` safety boundary before every production
text/JSON sink and classifies composition logs with the closed component set used by the planned
CLI filter. Text and JSON secret corpora cover messages, errors, groups, maps, URL userinfo/query
credentials and pre-bound attributes while preserving safe operational metadata. Handler
delegation/metadata/error semantics, representative component output and WSL2 race tests pass.
Real Docker/native failure-log scanning remains milestone 9.8 evidence.

Milestone 9.6 adds the host-side `wg-guard logs` command and the same recent-log action to the
local manager. Validated install state chooses Docker `docker logs` or the dedicated native
journal namespace; tail defaults to 200 and caps at 10,000, since defaults to 24 hours and caps at
seven days, follow honors process cancellation, and the closed component filter is applied to
complete lines locally with a 64 KiB bound. Exact argv/no-shell routing, split writes, text/JSON
matching, false positives, oversized/partial lines, source/output failures and real subprocess
cancellation are automated-test verified. Retention policy installation and real host behavior
remain milestones 9.7–9.8.

## Documentation

Update architecture overview/project structure, deployment/runbook/security, API/OpenAPI,
testing/status/release tracker, and CHANGELOG in the same changes as behavior.

## Completion criteria

RB-005 closes: one dashboard and one CLI workflow explain real failures, log growth is bounded,
secrets stay absent, tests/race pass, and measured idle/live overhead fits documented budgets.

## Deferred to Phase 10

Final dashboard styling and the complete visual-system/page migration. Long soak, load, and later
supported Ubuntu amd64 certification remain Phase 11.

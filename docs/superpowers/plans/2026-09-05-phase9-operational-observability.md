# Phase 9 operational observability — implementation plan

> Status: approved roadmap gate; execution started 2026-09-05 from Phase 8 verified head
> `2f756b9`. Implement test-first, commit at the checkpoints below, and do not mix Phase 10
> visual-system work into this phase.

## Outcome

An administrator can answer “is the node healthy, is it under resource pressure, is VPN traffic
abnormal, and what failed?” from the dashboard and one `wg-guard logs` command. Sampling and log
storage remain bounded, one scheduler owns periodic work, and secret-bearing values are removed at
the logging boundary.

## Architecture decisions

### Live telemetry

- Add `internal/telemetry` as the bounded runtime snapshot owner. It has no goroutine: `serve`
  invokes it from the existing central scheduler.
- Default cadence is 10 seconds. The in-memory ring holds 180 points (30 minutes), capped below
  128 KiB. Live telemetry is deliberately not persisted; the existing accounting rollups remain
  the durable 24-hour/7-day/30-day traffic history.
- The sampler reads host CPU/memory/disk/load/uptime, process RSS/heap/goroutines, default-route
  network counters, owned AWG-interface counters, database activity counts, and current node/AWG
  health. It performs bounded `/proc` reads and one small database aggregate transaction per run.
- Network rates are derived only from monotonic counter deltas. First samples, counter resets,
  interface replacement, unreasonable elapsed intervals, and partial sources produce an explicit
  unavailable value, never a spike or a fabricated zero.
- “Online users” is `COUNT(DISTINCT user)` inside the configured handshake window; “active peers”
  is the enabled-device count in that window. This corrects the current dashboard label/count
  ambiguity.
- Health states are `healthy`, `degraded`, and `unavailable`, with stable non-secret issue codes.
  Missing enabled AWG links, a recent accounting error, stale sampling, or failed readiness degrade
  the node. Raw subprocess/database errors remain in protected logs, not the API payload.
- Dashboard and API readers receive immutable copies from the ring under a short `RWMutex`; browser
  requests never touch `/proc`, AWG, or the sampling database queries.

### API and dashboard

- Add the additive `GET /api/v1/node/telemetry?points=N` endpoint under `stats.read`; `N` defaults
  to 60 and is capped at 180. It returns cadence, latest state, and oldest-to-newest points with
  nullable unavailable metrics. Update OpenAPI and bidirectional route/schema tests in the same
  commit.
- The dashboard live fragment refreshes every 10 seconds and reads only the shared telemetry
  snapshot plus existing durable totals. Hidden-tab pause remains mandatory.
- Phase 9 adds compact functional CPU, memory, VPN RX/TX-rate, activity, and health cards/graphs
  using the existing server-rendered SVG/CSS system. Phase 10 owns the final shadcn-style visual
  redesign and complete route/state browser matrix.

### Logs and retention

- Wrap every production `slog.Handler` with one `internal/logsafe` redaction handler. It redacts
  sensitive attribute names and recognized secret-bearing text (authorization headers, WG-Guard
  tokens, subscription capabilities, private/PSK/HPK directives, passwords, webhook/Telegram
  secrets). Redaction is recursive for groups and applies to messages, string attributes, and
  errors before the underlying text/JSON handler sees them.
- Add stable `component` attributes at composition boundaries (`serve`, `http`, `scheduler`,
  `accounting`, `webhook`, `backup`, `awg`, `network`). Do not log raw configs or subprocess output.
- `wg-guard logs` is a host-side command in both install modes:
  - Docker: `docker logs` for the owned container, with bounded `--tail`, `--since`, follow, and
    cancellation. The generated compose file selects Docker's `local` driver with 16 MiB × 8 files
    and compression, so one container cannot silently exhaust disk.
  - Native: `journalctl --namespace=wg-guard -u wg-guard.service` with the same CLI semantics. The
    unit uses `LogNamespace=wg-guard`; an installer-owned journald namespace drop-in sets
    `MaxRetentionSec=7day`, `MaxFileSec=1day`, `SystemMaxUse=128M`, and `RuntimeMaxUse=64M` without
    changing the host's global journal policy.
- `--tail` defaults to 200 and is capped at 10,000; `--since` defaults to 24 hours and accepts a
  bounded duration or RFC3339 instant; `--follow` exits on SIGINT/SIGTERM; `--component` accepts a
  closed documented set and filters complete lines with a bounded buffer.
- Installer/update/rollback steps continue to print live. In addition, safe milestone/outcome
  records are written through the same redaction policy to an installer-owned seven-day operation
  journal under the data directory. It contains fixed action/result metadata, never prompts,
  stdin, command argv carrying user data, or subprocess output. The logs command includes or
  isolates it by component. Daily files and an aggregate byte cap make retention independently
  testable and prevent disk growth even when no service manager is available.
- Upstream basis is pinned in the implementation docs: systemd journal namespaces provide scoped
  retention; Docker's `local` driver provides efficient automatic size rotation. Exact time-based
  retention is guaranteed for native and the owned operation journal; Docker service logs have a
  seven-day default query horizon plus the hard size cap because Docker's built-in local driver has
  no age option. This limitation must remain explicit.

## Dependency order and commit checkpoints

### Task 1 — Freeze contracts and test fixtures

Files:

- Modify `docs/development/phase9.md`
- Add `docs/decisions/ADR-0013-operational-observability.md`
- Add representative `/proc` fixtures under `internal/hoststats/testdata/`

Work:

1. Record metric definitions, units, nullable semantics, health codes, cadence/history bounds,
   log CLI grammar, retention behavior, and Docker time-retention limitation.
2. Capture sanitized deterministic fixtures for `/proc/stat`, `meminfo`, `loadavg`, `uptime`,
   `net/dev`, `net/route`, `/proc/self/status`, and counter-reset cases.
3. Mark only Task 1 active in the phase checklist.

Verification: Markdown links, fixture secret scan, `go test ./internal/hoststats` baseline.

Checkpoint: `docs(phase9): define observability contracts`.

### Task 2 — Build the bounded telemetry core

Files:

- Modify `internal/hoststats/hoststats.go`
- Modify `internal/hoststats/hoststats_linux.go`
- Modify `internal/hoststats/hoststats_other.go`
- Modify `internal/hoststats/*_test.go`
- Add `internal/telemetry/telemetry.go`
- Add `internal/telemetry/source.go`
- Add `internal/telemetry/telemetry_test.go`
- Add `internal/telemetry/bench_test.go`

Tests first:

1. Parse default-route and per-interface counters, process RSS, malformed/partial fixtures, and
   unavailable non-Linux state.
2. Prove first-sample/reset/gap rate semantics, ring wrap/order/copy isolation, concurrent readers,
   fixed capacity, online-user/peer distinctions, and health transitions.
3. Benchmark one append/snapshot and assert allocations/capacity in a resource test.

Implementation: a source seam returns one raw bounded sample; the sampler derives rates and health,
stores value-only points in a preallocated ring, and exposes copy snapshots.

Verification: focused tests, `go test -race ./internal/telemetry ./internal/hoststats`, benchmark.

Checkpoint: `feat(telemetry): add bounded node sampler`.

### Task 3 — Compose telemetry into the one scheduler

Files:

- Modify `internal/serve/serve.go`
- Modify `internal/serve/serve_test.go`
- Modify `internal/metrics/metrics.go`
- Add/modify metrics tests
- Modify `docs/architecture/overview.md`
- Modify `docs/architecture/project-structure.md`

Tests first:

1. Start creates an initial point and registers exactly one `telemetry` job; shutdown adds no
   goroutine and blocks no reader.
2. Accounting success/error and enabled-link presence produce the documented health transition.
3. Metrics output exposes only aggregate health/cadence/latest-rate gauges and no topology.

Implementation: create the reader/source/store once, sample synchronously after composition,
schedule it every 10 seconds, and live-re-anchor only through `Scheduler.SetInterval` if cadence
becomes configurable later.

Verification: serve/metrics tests, full unit suite, race suite.

Checkpoint: `feat(serve): schedule shared live telemetry`.

### Task 4 — Add the REST telemetry contract

Files:

- Modify `internal/api/server.go`
- Modify `internal/api/handlers_node.go`
- Modify `internal/api/handlers_test.go`
- Modify `internal/api/api_test.go`
- Modify `internal/api/openapi.json`
- Modify `docs/architecture/api.md`

Tests first:

1. `stats.read` is required; unauthorized/incorrect scope is rejected.
2. Default, min, max, malformed, and capped `points` behavior is deterministic.
3. Unavailable metrics serialize as `null`; timestamps/units/order and health issue codes match the
   OpenAPI schema; no interface names or raw errors escape.
4. Route↔OpenAPI coverage stays bidirectionally green.

Checkpoint: `feat(api): expose bounded node telemetry`.

### Task 5 — Replace per-browser host reads with shared dashboard telemetry

Files:

- Modify `internal/web/web.go`
- Modify `internal/web/dashboard.go`
- Modify `internal/web/dashboard_test.go`
- Add/modify `internal/web/svgchart.go` and tests
- Modify `web/templates/partial_dash_live.html`
- Modify `web/static/css/app.css`
- Modify `internal/i18n/catalogs/en.json`
- Modify `internal/i18n/catalogs/fa.json`

Tests first:

1. Dashboard requests never invoke the source; repeated/concurrent requests render the same store
   point without sampling work.
2. CPU/memory/VPN-rate/activity/health unavailable, healthy, warning, and stale states render in
   both locales without raw keys or direction errors.
3. Sparkline SVG geometry is deterministic, escaped, bounded, and correct for gaps/flat series.
4. Live fragment retains 10-second refresh and hidden-tab pause.

Verification: web/i18n tests, assets budget, browser smoke on desktop/mobile and fa/en.

Checkpoint: `feat(web): add live operational dashboard telemetry`.

### Task 6 — Install the central log-safety boundary

Files:

- Add `internal/logsafe/handler.go`
- Add `internal/logsafe/handler_test.go`
- Modify `cmd/wg-guard/serve.go`
- Modify composition logger wiring in `internal/serve/serve.go`
- Modify logging tests where necessary
- Modify `docs/operations/security.md`

Tests first:

1. Secret corpus is absent from both text and JSON output, including grouped attrs, errors, URLs,
   and message text; benign IDs, durations, counts, and paths remain useful.
2. `Enabled`, `WithAttrs`, `WithGroup`, source PC, levels, and cancellation semantics match the
   wrapped handler.
3. Every expected component appears in representative failure logs.

Checkpoint: `feat(logging): enforce centralized redaction`.

### Task 7 — Implement mode-aware `wg-guard logs`

Files:

- Add `cmd/wg-guard/logs.go`
- Add `cmd/wg-guard/logs_test.go`
- Add `internal/install/logs.go`
- Modify `internal/install/host.go`
- Modify `internal/install/render.go`
- Modify `internal/install/install_test.go`
- Modify `cmd/wg-guard/main.go`

Tests first:

1. Parse every valid option and reject extra args, negative/oversized tail, unbounded since values,
   unknown components, and follow with an invalid source.
2. Assert exact Docker/native argv, no shell, host-side Docker routing, bounded line filtering,
   source failures, broken pipes, and context cancellation.
3. Prove no credentials or free-form filter values enter argv unexpectedly.

Checkpoint: `feat(cli): add unified operational logs`.

### Task 8 — Enforce retention and record safe operation milestones

Files:

- Modify `internal/install/render.go`
- Modify `internal/install/run.go`
- Modify `internal/install/update.go`
- Modify `internal/install/uninstall.go`
- Add `internal/install/oplog.go` and tests
- Modify install-state cleanup tests
- Modify `deploy/compose.yaml`
- Modify `docs/operations/deployment.md`
- Modify `docs/operations/runbook.md`

Tests first:

1. Compose uses the local driver with the exact cap; native unit uses its own namespace; the
   namespace retention drop-in is installer-owned and removed on uninstall.
2. Seven-day and aggregate-size pruning work with a fake clock, never cross the owned directory,
   preserve 0600 permissions, and tolerate corrupt/partial lines.
3. Install/update/rollback/uninstall success and failure write only fixed redacted metadata.

Verification: install suite and rendered-config goldens; real host inspection in Task 9.

Checkpoint: `feat(install): bound operational log retention`.

### Task 9 — Real failure drills, resource gate, and phase close

Files:

- Add `docs/integrations/fixtures/verify-phase9-vps.sh`
- Add sanitized dated evidence after execution
- Modify `docs/development/phase9.md`
- Modify `docs/development/release-readiness.md`
- Modify `docs/development/status.md`
- Modify `docs/development/testing.md`
- Modify `README.md`, `ROADMAP.md`, `CHANGELOG.md`, and `AGENTS.md` only if references changed

Drills on the dedicated Ubuntu 24.04 VPS:

1. Native: verify journal namespace, seven-day/size settings, tail/since/component/follow and
   cancellation; induce service crash, bad AWG apply, and firewall/network error and recover.
2. Docker: verify local driver/caps and the same CLI contract; induce container/service failure and
   update/rollback failure safely.
3. Generate real tunnel traffic and CPU/memory/network load; compare API, dashboard, `/proc`, and
   runtime state within cadence/tolerance; verify counter reset and unavailable behavior.
4. Run actual-secret corpus scans over captured diagnostic output.
5. Measure idle CPU/RSS, sample latency/allocations, scheduler delay, API latency, and live-fragment
   size. Record exact environment and unsupported cells without inference.

Final gate: format, vet, unit, race, privileged integration, OpenAPI, asset budgets, module verify,
amd64/arm64 builds, vulnerability scan, clean repository, coherent commits, push, and CI green.
Close RB-005 only with linked evidence, then make Phase 10 active. Do not publish a public release.

## Explicit deferrals

- Complete shadcn-style visual redesign, page-by-page accessibility, wording, and responsive matrix
  remain Phase 10.
- Long soak, 1000-peer load, all OS/architecture cells, automatic userspace daemon lifecycle, and
  recovery certification remain Phase 11.
- Release artifacts, tags, registry publication, and final release remain Phase 12 and owner gated.

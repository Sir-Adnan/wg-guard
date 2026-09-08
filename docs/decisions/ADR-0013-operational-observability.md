# ADR-0013 — Scheduler telemetry and mode-native log retention

Status: accepted · Date: 2026-09-05

## Context

The dashboard currently reads `/proc` once per browser refresh and has no bounded live history.
Operational logs require different manual commands in Docker and native installs, and the generated
Docker deployment does not pin a rotating log driver. WG-Guard must add useful live telemetry and a
single CLI workflow without adding a metrics daemon, a frontend runtime, one sampler per browser,
unbounded memory, or unbounded disk use.

## Decision

Run one 10-second sampler as a job on the existing central scheduler. Keep at most 180 value-only
points (30 minutes) in a preallocated in-memory ring shared by the panel, REST API, and lightweight
Prometheus renderer. Browser requests read immutable snapshots and never perform host/runtime
sampling. Durable traffic history continues to use the existing accounting rollups; live telemetry
is intentionally ephemeral across restart.

Aggregate host and process pressure, default-route and owned-AWG network counters, online-user/
active-peer counts, observed enabled interfaces, readiness, and accounting health. Preserve source
absence as nullable/unavailable state. Derive rates only from valid monotonic deltas and never turn a
counter reset into a spike.

Use the deployment's native log owner instead of creating another service:

- native systemd uses a `wg-guard` journal namespace with an installer-owned seven-day and size
  policy;
- Docker uses the per-container `local` logging driver with compression and an explicit size/file
  cap;
- a small owned operation journal retains only fixed, redacted install/update/rollback milestones
  for seven days, covering work that does not run inside the service manager.

`wg-guard logs` normalizes tail, since, follow, cancellation, and component filtering across those
sources. A central `slog.Handler` wrapper redacts secrets before text/JSON handlers or platform log
storage receive a record.

## Consequences

- Sampling adds no goroutine and its memory ceiling is independent of browser count.
- Restart loses only the short live graph; durable traffic totals/history remain unchanged.
- Native retention is precisely time- and size-bounded without changing global journald policy.
- Docker service logs are hard size-bounded and the CLI defaults to the seven-day query horizon.
  Docker's built-in `local` driver has no age-based option, so it cannot promise exact seven-day
  physical deletion; this limitation is documented rather than hidden.
- Logs remain local/root-operational data. No panel/API endpoint exposes raw logs.
- New code uses only the standard library and existing project packages.

## Upstream contracts

- systemd documents per-namespace journal configuration and `MaxRetentionSec`, `SystemMaxUse`, and
  `RuntimeMaxUse` in
  [journald.conf](https://www.freedesktop.org/software/systemd/man/latest/journald.conf.html).
- systemd assigns unit output to a named journal namespace with
  [`LogNamespace=`](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html#LogNamespace=).
- Docker recommends the efficient rotating `local` driver and documents `max-size`, `max-file`, and
  compression in the
  [local logging driver reference](https://docs.docker.com/engine/logging/drivers/local/).

## Alternatives rejected

- Prometheus/Influx/Grafana or a frontend chart runtime: unnecessary dependencies and operational
  cost for a small self-hosted node.
- Per-browser sampling: duplicate `/proc`/database work and inconsistent histories.
- Persisting every 10-second telemetry point in SQLite: needless write amplification; existing
  accounting rollups already own durable traffic history.
- Modifying global journald limits: surprising impact on unrelated host services.
- Reading Docker's internal log files directly: unsupported by Docker and unsafe around rotation.

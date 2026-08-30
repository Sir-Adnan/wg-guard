# Deployment

**Docker is the default installation method** (clean, declarative, easily managed); a native
systemd mode is fully supported for administrators who prefer it. Both modes share identical
data paths, so backups and mode-switching are layout-independent.

## Docker mode (default)

- **Official image** (`wgguard/wg-guard`, multi-arch amd64/arm64): Ubuntu 24.04 base + pinned
  `amneziawg-tools` from `ppa:amnezia/ppa` + nftables + ca-certificates + the WG-Guard binary.
- **Run profile**: `network_mode: host`, `CAP_NET_ADMIN`, `restart: unless-stopped`, volumes
  `/etc/wg-guard` (boot config, TLS material) and `/var/lib/wg-guard` (DB, master key, backups).
- **Why this split**: the AmneziaWG kernel module and forwarding run on the **host** — the VPN
  data plane never traverses the container, so Docker adds zero hot-path overhead. The panel and
  AWG tooling run in the container with host networking (interfaces appear on the host, nftables
  edits the host's tables through the shared netns). Rejected alternatives (host agent process;
  privileged module-loading container) are recorded in [ADR-0006](../decisions/ADR-0006-docker-default-deployment.md).
- **Host `wg-guard` shim**: a tiny wrapper execs into the container so every CLI command is
  identical in both modes (`wg-guard status|doctor|backup|restore|update|uninstall|user …`).

## Native mode (secondary, fully supported)

Same binary as a hardened systemd service (`NoNewPrivileges`, `ProtectSystem=strict`,
`PrivateTmp`, ambient `NET_ADMIN`), same `/etc/wg-guard` + `/var/lib/wg-guard` layout.
Spec compliance: Docker is never *required*.

## Listening & TLS (built-in; no reverse proxy required)

`wg-guard serve` loads boot config from `/etc/wg-guard/wg-guard.toml` (override with
`-config PATH` or the `WGG_*` environment variables listed in `internal/config`). Every
runtime-tunable knob (accounting cadence, rate limits, node identity, webhooks…) lives in the
Settings registry and hot-applies; only paths, the listener and the TLS mode require a restart.

| Mode | Behavior | Status |
|---|---|---|
| Domain + ACME | Built-in `autocert`: HTTP-01 on port 80, TLS served on the configured panel port — any port works (e.g. `https://sub.example.com:34562`); port 80 must stay reachable for issuance/renewal | designed (ADR-0011); lands with the installer (Phase 7) — serve rejects the mode with a clear message today |
| Manual certs | `tls.mode = "manual"` with `cert_file`/`key_file`; TLS 1.2 minimum | implemented |
| Behind reverse proxy | `tls.mode = "proxy"`: HTTP bound to loopback/private interface (explicit, documented choice) | implemented |
| Development | `tls.mode = "dev"`: loopback-only HTTP with loud warnings | implemented |

The installer never silently exposes plaintext management to the public internet.

### Scheduler & background work

All periodic work runs on ONE scheduler goroutine: the accounting delta cycle +
expiry pass (every `accounting.interval_seconds`, live-reloadable), traffic-sample flush
(`accounting.sample_flush_seconds`), webhook delivery pass (5 s), and housekeeping (10 min:
idempotency-key, session, traffic-history and webhook-event pruning + rate-limit reload).
Graceful shutdown drains in-flight HTTP requests before stopping jobs and closing the DB.

### Dev/benchmark backend

`wg-guard serve -backend fake` substitutes the in-memory tunnel backend: no root, no AWG
tooling, no host networking changes. Intended for development and the resource measurements in
`scripts/bench-idle.sh` — it logs a loud warning and never touches tunnels or firewall.

### Metrics

`GET /metrics` (Prometheus text: uptime, request classes, accounting cycle stats, goroutines,
heap) is **off by default**; enable with `[metrics] enabled = true` (or `WGG_METRICS_ENABLED=1`)
when your monitoring stack needs it — it exposes topology signals and belongs behind an
operator's decision, ideally not on a public listener.

## Ports & networking defaults

All values below are **recommended defaults** (sensible starting points chosen from upstream
constraints), fully editable after install — not verified optima; guidance is revisited after
the Phase 8 VPS matrix.

| Setting | Recommended default | Editable later |
|---|---|---|
| Panel HTTP | 8080 | yes (boot config + Settings) |
| Panel TLS | 443 (any port supported) | yes |
| AWG listen port (per interface) | random 30000–50000; low ports prompt in installer | yes (per interface, hot-applied) |
| MTU | 1420 | yes (global default + per interface) |
| VPN pool | `10.8.0.0/16` carved per interface (`10.8.N.0/24`) | yes (per interface, validated) |
| Client DNS (generated configs) | `1.1.1.1, 1.0.0.1` | yes (Settings) |
| Max tunnel interfaces | 8 (`awg0…awg7`) | yes (Settings) |

## Updates

`wg-guard update` (CLI or UI): pre-upgrade backup → pull new image / atomic binary replace →
restart → health check → rollback instructions. Never auto-updates.

## Uninstall

Stops services; `--dry-run` lists every artifact; preserves data/backups optionally; removes
only WG-Guard-owned resources (files, units, nftables table, recorded sysctls, packages the
installer itself installed).

## Host requirements

Ubuntu 22.04/24.04 or Debian 12 (amd64/arm64); root; for kernel-module mode: DKMS build
prerequisites (`build-essential`, kernel headers). Userspace fallback exists when DKMS is
unavailable ([ADR-0003](../decisions/ADR-0003-kernel-first-userspace-fallback.md)).

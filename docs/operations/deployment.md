# Deployment

**Docker is the default installation method** (clean, declarative, easily managed); a native
systemd mode is fully supported for administrators who prefer it. Both modes share identical
data paths, so backups and mode-switching are layout-independent.

## Docker mode (default)

- **Official image** (`wgguard/wg-guard`): Ubuntu 24.04 base + pinned `amneziawg-tools` from
  `ppa:amnezia/ppa` + nftables + ca-certificates + the WG-Guard binary
  ([Dockerfile](../../Dockerfile), amd64/arm64). The registry publication of versioned
  multi-arch tags is part of the Phase 12 release pipeline; until then build locally
  (`docker build -t wgguard/wg-guard:<tag> .`) and pass `--image` to the installer, which is
  also what `wg-guard update` consumes.
- **Run profile**: `network_mode: host`, `CAP_NET_ADMIN`, `restart: unless-stopped`, volumes
  `/etc/wg-guard` (boot config, TLS material) and `/var/lib/wg-guard` (DB, master key, backups,
  ACME cache). The generated compose file adds a TLS-mode-aware healthcheck.
- **Why this split**: the AmneziaWG kernel module and forwarding run on the **host** — the VPN
  data plane never traverses the container, so Docker adds zero hot-path overhead. The panel and
  AWG tooling run in the container with host networking (interfaces appear on the host, nftables
  edits the host's tables through the shared netns). Rejected alternatives (host agent process;
  privileged module-loading container) are recorded in [ADR-0006](../decisions/ADR-0006-docker-default-deployment.md).
- **Host `wg-guard` shim**: the same binary, mode-aware — panel/data commands exec into the
  container; `install`, `update`, `uninstall`, `status`, `doctor`, `version` run on the host;
  `serve` is refused with compose hints. Every CLI command is identical in both modes.
- **Kernel module**: the installer writes `/etc/modules-load.d/wg-guard.conf` (boot
  persistence) and, when the DKMS module is registered for a different kernel series than the
  running one (typical after an unattended kernel upgrade), installs the matching headers and
  rebuilds via `dkms autoinstall`.

## Native mode (secondary, fully supported)

Same binary as a hardened systemd service (`NoNewPrivileges`, `ProtectSystem=strict`,
`PrivateTmp`, ambient `NET_ADMIN`), same `/etc/wg-guard` + `/var/lib/wg-guard` layout.
Spec compliance: Docker is never *required*.

## Interactive installer

`wg-guard install` walks a short wizard; **pressing Enter everywhere is a complete,
sensible install** (Docker, no domain → loopback HTTP for a reverse proxy; with a domain →
ACME on 443). `--yes` skips all prompting and uses flags + defaults only — an explicit
`--tls` flag is never overridden by a prompt default.

1. Mode (Docker default / native), domain, TLS mode, panel + ACME challenge ports.
2. *Optional* — **VPN network defaults** (Enter keeps the recommended defaults):
   AWG listen-port allocation range (`network.port_min/port_max`), the VPN pool offered to
   the first interface (`network.default_pool`), client MTU (`network.mtu`), client DNS
   resolvers (`network.dns_servers`).
3. *Optional* — **Telegram backups** (skipping leaves the panel defaults): bot token
   (input is hidden on terminals and travels via stdin — never argv, logs or state),
   chat ID, and a daily UTC backup time that creates an enabled `installer-daily` schedule.
4. Container image (Docker mode) and a final plan confirmation.

Every choice lands in the Settings registry / backup schedules and stays editable in the
panel afterwards; values equal to the registry defaults are not persisted.

**When seeding happens matters**: the collected settings are applied through the installed
CLI (`wg-guard settings set`, `wg-guard backup schedule-add`) **before the service first
boots** — the registry caches values in memory, so post-boot CLI writes would stay invisible
until a restart. The panel domain is also seeded as `node.endpoint`, so the first exported
client config carries a working `Endpoint` line. In Docker mode this runs before the state
file exists, so the shim executes host-direct against the bind-mounted data dir (same DB the
container will use).

## Listening & TLS (built-in; no reverse proxy required)

`wg-guard serve` loads boot config from `/etc/wg-guard/wg-guard.toml` (override with
`-config PATH` or the `WGG_*` environment variables listed in `internal/config`). Every
runtime-tunable knob (accounting cadence, rate limits, node identity, webhooks…) lives in the
Settings registry and hot-applies; only paths, the listener and the TLS mode require a restart.

| Mode | Behavior | Status |
|---|---|---|
| Domain + ACME | Built-in `autocert`: HTTP-01 on the challenge port (`tls.acme_http_port`, default 80 — must stay reachable for issuance/renewal), TLS served on the configured panel port — any port works (e.g. `https://sub.example.com:34562`); certificates cached under `<data_dir>/acme`; the challenge listener also redirects plain-HTTP visitors to the real HTTPS URL | implemented + verified on a public domain ([phase7.md](../development/phase7.md)) |
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
the Phase 11 production matrix.

| Setting | Recommended default | Editable later |
|---|---|---|
| Panel HTTP | 8080 | yes (boot config + Settings) |
| Panel TLS | 443 (any port supported) | yes |
| AWG listen ports | allocated randomly from `network.port_min`–`port_max` (30000–50000); the range is promptable at install | yes (Settings, hot-applied) |
| MTU | 1420 (promptable at install) | yes (global default + per interface) |
| VPN pool | first interface honors `network.default_pool` (promptable at install; empty = `10.8.0.0/24`), later interfaces continue the `10.8.N.0/24` ladder | yes (Settings + per interface, validated) |
| Client DNS (generated configs) | `1.1.1.1, 1.0.0.1` (promptable at install) | yes (Settings) |
| Max tunnel interfaces | 8 (`awg0…awg7`) | yes (Settings) |

## Updates

`wg-guard update` (CLI): pre-upgrade backup → compose image switch + pull + recreate (docker)
or staged binary replace with the previous kept at `<bin>.pre-update` (native) → restart →
health check → automatic rollback on failure. Interrupted updates recover with
`wg-guard update --rollback` (re-deploys the state-recorded last healthy artifact). Never
auto-updates. Full procedures: [runbook.md](runbook.md).

## Uninstall

`wg-guard uninstall --dry-run` first: stops services and removes only the state-recorded
WG-Guard-owned artifacts; data/backups and installer-installed packages are preserved unless
`--purge-data` / `--purge-packages` is passed.

## Host requirements

Compatibility targets are Ubuntu 22.04/24.04 and Debian 12 on amd64/arm64; only Ubuntu 24.04
amd64 has completed the current real-host drills. Root is required. Kernel mode needs DKMS build
prerequisites (`build-essential`, matching kernel headers). The userspace fallback architecture
is accepted, and its config/runtime adapter is integration-tested, but WG-Guard does not yet
supervise the daemon automatically. Until Phase 11 closes AUD-019, managed production tunnels
require the kernel module ([ADR-0003](../decisions/ADR-0003-kernel-first-userspace-fallback.md)).

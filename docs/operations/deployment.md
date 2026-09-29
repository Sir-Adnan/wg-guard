# Deployment

**Docker is the default installation method** (clean, declarative, easily managed); a native
systemd mode is fully supported for administrators who prefer it. Both modes share identical
data paths, so backups and mode-switching are layout-independent.

## Docker mode (default)

- **Verified local runtime image**: the installer builds an Ubuntu 24.04 amd64 image from the
  selected verified binary and exact-source AmneziaWG tools/userspace components.
  No public registry image is required or published. The repository
  [Dockerfile](../../Dockerfile) builds the runtime composition verified in the v0.1.0
  host gate; v0.1.3 does not change its tunnel data-plane behavior. An operator may supply
  an explicitly checked local image via `--image`.
- **Run profile**: `network_mode: host`, `CAP_NET_ADMIN`, `restart: unless-stopped`, volumes
  `/etc/wg-guard` (boot config, TLS material) and `/var/lib/wg-guard` (DB, master key, backups,
  ACME cache). The generated compose file adds a TLS-mode-aware healthcheck.
- **Host layout**: no `/opt` application directory is needed. Docker owns immutable image layers
  in its configured engine data root (commonly `/var/lib/docker`, but operator-configurable).
  WG-Guard's stable host artifacts are `/etc/wg-guard/compose.yaml`, `/etc/wg-guard`,
  `/var/lib/wg-guard`, `/usr/local/bin/wg-guard`, and the verified manager under
  `/var/cache/wg-guard`.
- **Why this split**: the AmneziaWG kernel module and forwarding run on the **host** — the VPN
  data plane never traverses the container, so Docker adds zero hot-path overhead. The panel and
  AWG tooling run in the container with host networking (interfaces appear on the host; nftables
  and the iptables-nft compatibility CLI edit host policy through the shared netns). Rejected alternatives (host agent process;
  privileged module-loading container) are recorded in [ADR-0006](../decisions/ADR-0006-docker-default-deployment.md).
- **Host `wg-guard` shim**: the same binary, mode-aware — panel/data commands exec into the
  container; `manage`, `install`, `update`, `uninstall`, `restart`, `owner-bootstrap`, `core`,
  `status`, `doctor`, `version` run on the host;
  `serve` is refused with compose hints. The host-owned `doctor` delegates only AWG tool/interface
  inspection to the running container; system and network-policy checks stay on the host. Its
  explicit `--fix` uses the managed Docker restart so container startup performs canonical
  reconciliation before the host verifies the result. Every CLI command is identical in both modes.
- **Kernel module**: the installer writes `/etc/modules-load.d/wg-guard.conf` (boot
  persistence). On supported Ubuntu it checks out the exact catalogued upstream kernel tag and
  commit, registers versioned DKMS source, installs matching running-kernel headers when needed,
  loads the module, and can rebuild only that recorded identity. It never unloads active tunnels.

`internal/install.BuildRuntimeImage` can build a local Ubuntu 24.04 runtime image directly from
a checksum-verified acquired panel binary plus the exact catalogued tools source, `iproute2`,
`nftables`, `iptables`, `procps` (`sysctl`), CA roots and curl. It executes no candidate installer and returns
only an immutable Docker image ID. Its private build context is removed after success/failure;
the caller's staging parent is preserved. Acquisition-to-lifecycle plumbing and recording that
ID as the active/previous artifact are implemented by the shared lifecycle engine. No official
public image publication is implied.

Private mode uses the config's loopback-only `dev` transport because no reverse proxy terminates
TLS; this permits its session cookie over the documented local SSH tunnel. Managed Nginx and
operator-proxy modes use `proxy` transport and keep `Secure` cookies because the browser-facing
connection is HTTPS.

## Native mode (secondary, fully supported)

Same binary as a hardened systemd service (`NoNewPrivileges`, `ProtectSystem=strict`,
`PrivateTmp`, ambient `NET_ADMIN`), same `/etc/wg-guard` + `/var/lib/wg-guard` layout.
The unit starts after Docker when that service is also part of boot, without requiring Docker
for native operation. Runtime policy checks restore an owned forwarding path if Docker later
rebuilds its chains.
Spec compliance: Docker is never *required*.

## Interactive installer

The verified GitHub entry stores the independent manager and private receipt under
`/var/cache/wg-guard`, places the fresh-host command at `/usr/local/bin/wg-guard`, and opens the
manager. Selecting **Install** runs `wg-guard install` with that exact build; a canceled
or failed setup resumes locally through `sudo wg-guard` without another acquisition. Defaults are
Docker, no domain → private loopback HTTP, or a usable domain → safe automatic HTTPS. `--yes`
uses flags and defaults; legacy explicit `--tls` remains compatible but cannot conflict with the
new `--exposure`/`--certificate` choices.

1. Optional domain and a conservative access recommendation based on actual 80/443 ownership.
2. *Optional advanced settings* — Docker/native mode, certificate strategy, panel/public/challenge
   ports and **VPN network defaults** (Enter keeps the recommendation):
   AWG listen-port allocation range (`network.port_min/port_max`), the VPN pool offered to
   the first interface (`network.default_pool`), client MTU (`network.mtu`), client DNS
   resolvers (`network.dns_servers`).
3. *Optional* — **Telegram backups** (skipping leaves the panel defaults): bot token
   (input is hidden on terminals and travels via stdin — never argv, logs or state),
   chat ID, and a daily UTC backup time that creates an enabled `installer-daily` schedule.
4. Container image (Docker mode) and a final plan confirmation.
5. Create the first local owner before starting the public listener. Username defaults to `admin`;
   a blank hidden password generates a 24-character value shown once after final success, while
   invalid/mismatched manual input is retried. Reuse an existing owner without changing its
   credentials. Fresh `--yes`
   setup requires a private `--owner-password-file`. See [terminal management](terminal-management.md)
   for menus, locale, input bounds, automation and the manual-serve posture.

Every choice lands in the Settings registry / backup schedules and stays editable in the
panel afterwards; values equal to the registry defaults are not persisted.

**When seeding happens matters**: the collected settings are applied through the installed
CLI (`wg-guard settings set`, `wg-guard backup schedule-add`) **before the service first
boots** — the registry caches values in memory, so post-boot CLI writes would stay invisible
until a restart. The domain, explicit public-IP candidate, or eligible interface IP is seeded as
`node.endpoint`, independently of the loopback panel listener. Endpoint-less installation is
refused with `--public-ip` guidance. No third-party IP echo service is queried implicitly.
IPv4-mapped addresses use IPv4 classification. Private/shared space (including CGNAT
`100.64.0.0/10`), documentation, benchmarking, reserved and other non-public special-use ranges
are excluded. The classifier follows the [IANA IPv4](https://www.iana.org/assignments/iana-ipv4-special-registry/)
and [IPv6](https://www.iana.org/assignments/iana-ipv6-special-registry/) special-purpose registries
(checked 2026-09-06), preserving their globally reachable exceptions within protocol-assignment
blocks. Deprecated compatible/site-local IPv6 ([RFC 4291](https://www.rfc-editor.org/rfc/rfc4291.html))
and conditional/deprecated transition ranges are also excluded. Passing classification does not
establish address assignment, routing, NAT forwarding, firewall access or actual reachability;
operators must verify those separately.
Negative Telegram group IDs are accepted as signed nonzero integers. In Docker mode this runs before the state
file exists, so the shim executes host-direct against the bind-mounted data dir (same DB the
container will use).

## Panel exposure and TLS

`wg-guard serve` loads boot config from `/etc/wg-guard/wg-guard.toml` (override with
`-config PATH` or the `WGG_*` environment variables listed in `internal/config`). Every
runtime-tunable knob (accounting cadence, rate limits, node identity, webhooks…) lives in the
Settings registry and hot-applies; only paths, the listener and the TLS mode require a restart.

The runtime still has `acme`, `manual`, loopback `proxy`, and loopback-only development modes.
The installer adds an ownership-aware exposure layer so operators choose the product outcome:

| Exposure | Listener | Certificate owner | Verified use |
|---|---|---|---|
| Private | `127.0.0.1` HTTP | none | SSH tunnel; default without a domain |
| Direct | public HTTPS | built-in domain ACME, managed/manual files, or short-lived public-IP Certbot | free required ports |
| Managed Nginx | `127.0.0.1` HTTP backend | standard host Nginx + webroot/DNS/manual certificate | multi-service host already using 80/443 |
| External proxy | `127.0.0.1` HTTP backend | operator | custom Nginx/Caddy/Apache/Traefik/container proxy |

Automatic domain selection uses direct built-in ACME only when the required ports are free. A
standard active Ubuntu Nginx with the normal `conf.d` include can instead receive one atomic
WG-Guard virtual host and shared HTTP-01 webroot. Exact-host conflicts and nonstandard proxies are
never edited. Cloudflare DNS-01 needs no validation port and stores its scoped token only in a
root-private file; it does not request a wildcard. Cloudflare Origin CA is accepted only as a
manual proxied-origin certificate and is not browser-trusted when Cloudflare is bypassed.

Public-IP HTTPS uses official Certbot 5.4+ with Let's Encrypt's `shortlived` profile (160 hours),
standalone HTTP-01 and public TCP 80. It is explicit, not the blank-domain default. When a public
proxy already owns the ports, use a hostname through that proxy or DNS-01; WG-Guard does not stop
the owner or expose public HTTP as a fallback.

Managed Certbot material is copied to `/etc/wg-guard/tls` (certificate 0644, key 0600). A fixed
0700 deploy hook accepts only the recorded deterministic lineage, refreshes both files under the
lifecycle lock, reloads Nginx or restarts the correct Docker/native service, then proves health and
certificate identity. `wg-guard exposure status`/`doctor` report SAN, issuer class, expiry, timer,
hook, credential permissions, Nginx drift and listener drift without printing secrets.

Process liveness and certificate readiness remain distinct. A new certificate path starts as
`pending`; a trusted handshake persists `verified`. Built-in ACME can be retried with
`wg-guard tls-check`; managed paths use `wg-guard exposure renew`. Every public response is HTTPS,
and HSTS is emitted only for direct TLS or HTTPS asserted by a trusted private/loopback proxy peer.
See [terminal management](terminal-management.md#panel-access--https) and
[lifecycle recovery](lifecycle-recovery.md).

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

### Operational logs

`wg-guard logs` is always a host command. Validated install state selects `docker logs` for the
owned container or `journalctl --namespace=wg-guard -u wg-guard.service` for native mode. It
defaults to the latest 200 records from 24 hours, caps tail at 10,000 and since at seven days, and
supports cancellable follow plus a closed structured-component filter. The filter processes only
complete lines with a 64 KiB per-line bound and never places the filter value in subprocess argv.
Docker container stdout and stderr are merged into this one redirectable log stream because the
application logger writes structured records to stderr; native journal records already arrive on
stdout. Source command failures still return a nonzero CLI status.
`--source operations` instead reads the fixed, private lifecycle journal without requiring install
state; follow/component apply only to service logs. Raw logs remain local; there is no panel/API
log endpoint.

Docker Compose selects the efficient `local` driver with compression, `max-size=16m` and
`max-file=8`. This hard-bounds the owned container near 128 MiB, but Docker has no age option: the
seven-day CLI query horizon is not a claim of exact physical age deletion. Native service output
uses `LogNamespace=wg-guard`; the installer-owned
`/etc/systemd/journald@wg-guard.conf.d/retention.conf` sets `MaxRetentionSec=7day`,
`MaxFileSec=1day`, `SystemMaxUse=128M` and `RuntimeMaxUse=64M` without changing global journald
policy. Install/update reload the namespace; rollback restores the previous unit/policy and
uninstall removes the owned drop-in.

Lifecycle work outside the service manager writes only fixed action/outcome/mode metadata through
the same redaction boundary under `/var/lib/wg-guard/operations`. At most seven UTC daily JSONL
files and 8 MiB are retained; oldest recognized owned files are pruned on writes and readers skip
invalid, oversized or interrupted lines. The installer-owned
`/etc/tmpfiles.d/wg-guard-operations.conf` uses an mtime-only seven-day rule serviced by Ubuntu's
existing `systemd-tmpfiles-clean.timer`, so idle old files do not wait for another lifecycle write.
The installer applies the file explicitly, refuses an unowned conflict, records ownership, and
rolls it back if the state commit fails. Purging node data intentionally removes these records.
The platform policies, an expired-file cleanup and both deployment modes passed the
[Phase 9 VPS gate](../integrations/fixtures/verify-phase9-vps-2026-09-10.txt).

## Ports & networking defaults

All values below are **recommended defaults** (sensible starting points chosen from upstream
constraints), fully editable after install — not verified global optima. Phase 11 certified
the documented Ubuntu 24.04 paths, not a universal performance optimum.

| Setting | Recommended default | Editable later |
|---|---|---|
| Loopback panel/backend | TCP 8080 | yes; remains private behind SSH/proxy |
| Public panel HTTPS | TCP 443 | yes; only with a valid certificate path |
| HTTP-01 validation | TCP 80 | yes; external CA validation still reaches public port 80 |
| AWG listen ports | allocated randomly from `network.port_min`–`port_max` (30000–50000); the range is promptable at install | yes (Settings, hot-applied) |
| MTU | 1420 (promptable at install) | yes (global default + per interface) |
| VPN pool | first interface honors `network.default_pool` (promptable at install; empty = `10.8.0.0/24`), later interfaces continue the `10.8.N.0/24` ladder | yes (Settings + per interface, validated) |
| Client DNS (generated configs) | `1.1.1.1, 1.0.0.1` (promptable at install) | yes (Settings) |
| Max tunnel interfaces | 8 (`awg0…awg7`) | yes (Settings) |

## Updates

`wg-guard update` explicitly selects the latest published stable release; `--commit main`
explicitly selects development source and resolves its immutable SHA. Acquisition/pull and
contract verification precede active mutation, followed by a recorded local backup, swap,
restart, health check and durable commit. `--binary` supplies a local candidate; Docker image
overrides also require the matching host binary and `--local-image` explicitly disables pulling.
Both modes retain a previous artifact; `--rollback` requires proven data compatibility and
`--recover` replays an interrupted journal. Unknown compatibility requires coordinated DB/key
restoration, not just restarting old code. See [lifecycle-recovery.md](lifecycle-recovery.md).

The authenticated Web Panel exposes the same stable panel and reviewed core transitions to
`update.manage`. It writes a fixed 0600 request/status envelope under `/var/lib/wg-guard`; the
installer-owned `wg-guard-update.path` starts a root oneshot that revalidates the catalog identity
and invokes only bounded existing lifecycle arguments. The container receives no Docker socket,
systemd control or general host agent. One request may be active, the oneshot is time-bounded, and
public status persists only a safe outcome code. Fresh Docker/native installs enable the bridge;
`sudo wg-guard update-broker-install` repairs it for an existing installation. Uninstall disables
and removes only recognized WG-Guard-owned bridge artifacts.

## Uninstall

`wg-guard uninstall --dry-run` first: stops services and removes only the state-recorded
WG-Guard-owned artifacts; data/backups and installer-installed packages are preserved unless
`--purge-data` / `--purge-packages` is passed. `--purge-all` implies both and additionally removes
the fixed WG-Guard manager/cache, log and remaining configuration/lifecycle directories. It does
not remove unrelated proxy sites, shared certificate lineages or unowned packages.
After service stop, uninstall removes and verifies database-recorded kernel AmneziaWG links;
an unattributed live tunnel blocks removal rather than silently surviving a data purge.
An explicit data purge removes data members while retaining one locked, purged-marker inode in
the empty data directory; deleting that inode by hand would defeat concurrent-admission safety.

## Host requirements

Prerequisites and selection are implemented and real-host verified on the dedicated Ubuntu 24.04
amd64 node. Later Ubuntu releases still need their own host certification:

```bash
wg-guard core installed
wg-guard core recommended
wg-guard core latest-compatible
wg-guard core exact awg-2026-09
wg-guard install --mode native --yes --public-ip PUBLIC_IP --prerequisites auto --core recommended
```

Replace `PUBLIC_IP` with the server's real public address; documentation-only addresses are rejected.
The core commands print bounded JSON metadata. The host terminal is English-only; shared web-panel
messages retain fa/en catalog parity.

Preflight requires Ubuntu 24.04 or newer on amd64/x86_64 and inspects the running kernel, init,
endpoint and TCP ports before package/deployment writes. Other distributions, older Ubuntu and
other architectures stop before acquisition or deployment. Ubuntu 24.04 is the verified target.
Native mode needs `ip`, `tc`, `nft`, `iptables`, `sysctl`, matching `awg` and systemd. Docker mode checks the
engine, Compose and daemon while keeping host module management separate. A missing engine uses
Ubuntu's `docker.io`; a missing plugin uses `docker-compose-v2` with recommendations and removals
disabled, preserving an existing Docker CE engine. An inactive supported systemd daemon/socket is
reloaded and started before readiness is retried. Existing dependencies are not blanket-upgraded.

Docker's iptables backend may keep the host-wide `FORWARD` policy at `DROP`. WG-Guard deliberately
does not relax it. For each enabled tunnel it renders NAT in `table inet wgguard` and a narrow
source/interface plus established-return allow path in its own `WGGUARD-FORWARD` child chain,
attached by one tagged jump at Docker's documented `DOCKER-USER` extension point. Runtime
interface mutations refresh both layers before readiness is healthy; `doctor` reports a missing
or partial path. Uninstall removes only the owned table, jump and child chain.

The recommended `awg-2026-09` bundle does not depend on PPA retention. It clones only the exact
catalogued official tools/kernel tags, verifies both full commits and clean trees, builds the
native `awg` tool/runtime layer, and registers `amneziawg/1.0.0-wgguard.20260906` with DKMS.
Versioned source and a bounded installer-owned cache make retry deterministic. The package-backed
`awg-2026-08` identity remains for legacy installed-state/update compatibility and fails closed if
its historical exact packages are unavailable; it is not the recommended fresh-install path.
For managed `-generic` kernels, installation also records `linux-headers-generic` alongside
headers for the running kernel. This keeps DKMS headers available when Ubuntu later upgrades
that kernel stream; a custom kernel flavor still requires operator-provided matching headers.

APT commands wait for Ubuntu's dpkg lock rather than racing `unattended-upgrades`. The lifecycle
journal records package intents and observed ownership before runtime mutation. A safe first-run
failure closes as `aborted`, keeps the local manager, and carries observed package/repository
ownership into the next attempt so later `--purge-packages` remains complete. Explicit package
purge stops installer-owned Docker service/socket before removing Docker and source-core assets.

`--prerequisites check` requires operator-provisioned prerequisites and makes no package/module
mutations. Native tools must report the catalogued version, and managed modules need observable
matching loaded/disk build identity. `--skip-module` explicitly delegates host module lifecycle
to the operator, but still checks required native AWG tools. It does not silently select
userspace. A normal managed-core installation fails if the module is absent, different from disk,
or its loaded build identity cannot be established.

The installer accepts Ubuntu 24.04 or newer on amd64; production certification currently covers
Ubuntu 24.04. Newer releases remain unverified and fail closed when the pinned bundle is unavailable.
An active firewalld is not a certified forwarding policy: boot, runtime readiness and doctor
fail closed for enabled tunnels until it is disabled. Docker's scoped forwarding extension
and test-backed UFW-managed routes are the supported coexistence paths. Root is required. Kernel mode needs
DKMS build prerequisites (`build-essential`, matching kernel
headers). Explicit userspace profiles now have a service-owned daemon lifecycle: the Docker
image carries the reviewed daemon and maps `/dev/net/tun`; native nodes need that exact daemon
installed from a clean pinned Git checkout, with Go VCS build metadata, and a usable TUN device.
The daemon's stale `--version` text is not proof of the source revision. New profiles fail before persistence when those
prerequisites are absent. The kernel remains the installed default; Docker and native managed
userspace client traffic passed on Ubuntu 24.04 ([ADR-0003](../decisions/ADR-0003-kernel-first-userspace-fallback.md)).

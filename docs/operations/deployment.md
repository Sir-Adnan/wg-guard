# Deployment

v0.1.10 and later implement the Docker-only deployment contract; v0.1.9 and earlier
retain their previous deployment/layout. The [Phase 20](../development/phase20.md)
record holds the corrected Ubuntu 24.04 host acceptance and v0.1.10 publication evidence. Existing installations
must export and independently verify their backup using the original manager, then use the
approved fresh-install/restore route. This is not an in-place layout converter.

## Docker production runtime

- One reviewed recipe: [`internal/install/runtime/Dockerfile`](../../internal/install/runtime/Dockerfile),
  embedded/rendered by the manager. CI and source builds consume the exact precompiled manager;
  they do not independently recompile a different panel into the image.
- New-format releases provide a checksummed compressed Docker image archive and
  `runtime-metadata.json`; no registry is required. The manifest binds the image config digest,
  archive bytes, binary hash/commit, reviewed tools/userspace/kernel inventory, deployment/data/
  maintenance contracts, notices and Go-module SBOM. Missing or inconsistent image assets fail
  closed. Explicit development commits use the same recipe in a private build context.
- Docker's classic image store names a loaded release image by that config digest; the
  containerd image store (the default on newer Docker Engine installs) names it by the
  archive's OCI manifest digest. The manager reads that manifest from the checksum-verified
  archive, confirms it references the config digest, and admits either local ID only with
  matching platform and provenance labels. Without the archive only the config digest is
  knowable, so a containerd-store host re-acquires the verified archive instead of reusing
  an image found by labels.
- The image uses host networking and only `NET_ADMIN`/`NET_BIND_SERVICE` after dropping other
  capabilities. Root is read-only, with temporary `/run` (32 MiB) and `/tmp` (64 MiB). Boot config
  and approved TLS material are individual read-only mounts; node data is the one writable host
  mount. There is no privileged container, Docker socket, module-loading capability or silent
  userspace fallback. Host networking is not full host-network isolation.
- Large archive staging uses the node-data disk. Telegram multipart bodies use a private
  `backup-delivery/` directory there and retain the existing upload limit; they do not compete
  with AWG's small `/tmp`. Independent `backup verify` is normally a host command with private
  disk staging; an explicit container invocation must provide adequate temporary disk space.
- No arbitrary CPU/memory limit is guessed. The listed two-vCPU/about-3.8-GiB
  Ubuntu 24.04 resource, crypto and kernel/userspace traffic measurements are in Phase 20;
  they are not universal workload budgets.
- The host owns kernel/DKMS, headers, module load/persistence, diagnostic/network utilities,
  certificate/proxy tasks, the narrow update broker, acquisition and recovery. AWG tools and
  the explicit userspace daemon live in the image; native WG-Guard server execution is removed.
- Deployments pin immutable local image IDs and use `pull_policy: never`. Acquisition/verification
  occurs before stopping an existing service; backup, stop, deploy, readiness/TLS proof and state
  commit share one coordinator. Retained images can be inspected without a running container.

## Host paths and configuration

| Path | Responsibility | Container access |
|---|---|---|
| `/opt/wg-guard/compose.yaml` | Generated deployment manifest | none |
| `/etc/wg-guard/wg-guard.toml` | Boot paths, listener and TLS | one read-only file |
| `/etc/wg-guard/tls/` | Managed TLS material | approved read-only files |
| `/var/lib/wg-guard/` | DB/key, backups, archive/delivery staging, ACME | node data only |
| `/var/lib/wg-guard-host/` | Private installed state, journal and retained artifacts | none |
| `/var/cache/wg-guard/` | Independent manager and acquisition cache | none |
| `/var/log/wg-guard/` | Private bounded installer logs | none |
| `/usr/local/bin/wg-guard` | Active verified host command | none |

This is a service-oriented split, not the full FHS `/opt` package family. Strict `/opt` packages
use `/etc/opt` and `/var/opt` ([FHS](https://refspecs.linuxfoundation.org/FHS_3.0/fhs/ch03s13.html)).
WG-Guard retains the established configuration/node-data paths and separates mutable host
authority from them. Renaming data paths alone provides no measured performance benefit.

There is deliberately no required `.env`. TOML owns boot configuration, SQLite owns runtime
settings, and the generated Compose manifest owns immutable deployment identity. Existing
`WGG_*` environment overrides remain available; the program does not auto-read `.env` files.
Compose `.env` interpolation is a separate feature and does not automatically populate container
environment ([Docker](https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/)).
Do not duplicate one setting across these stores or put private keys/passwords in a deployment
environment file. A future operator-managed Compose workflow can use a small non-secret env file
only when it has a concrete purpose; the managed workflow needs none.

The host CLI routes data commands into Docker; lifecycle/status/logs/doctor and independent archive
verification stay on the host. Direct `serve` with a real backend is refused outside the runtime;
`--backend fake` remains development-only. Ubuntu 24.04 amd64 remains the verified historical
target; new-layout host/client certification is still pending, not inferred from image tests.

## Interactive installer

The verified GitHub entry stores the independent manager and private receipt under
`/var/cache/wg-guard`, places the fresh-host command at `/usr/local/bin/wg-guard`, and opens the
manager. Selecting **Install** runs `wg-guard install` with that exact build; a canceled
or failed setup resumes locally through `sudo wg-guard` without another acquisition. Defaults are
Docker, no domain → private loopback HTTP, or a usable domain → safe automatic HTTPS. `--yes`
uses flags and defaults; legacy explicit `--tls` remains compatible but cannot conflict with the
new `--exposure`/`--certificate` choices.

1. Optional domain and a conservative access recommendation based on actual 80/443 ownership.
2. *Optional advanced settings* — certificate strategy, panel/public/challenge
   ports and **VPN network defaults** (Enter keeps the recommendation):
   AWG listen-port allocation range (`network.port_min/port_max`), the VPN pool offered to
   the first interface (`network.default_pool`), client MTU (`network.mtu`), client DNS
   resolvers (`network.dns_servers`).
3. *Optional* — **Telegram backups** (skipping leaves the panel defaults): bot token
   (input is hidden on terminals and travels via stdin — never argv, logs or state),
   chat ID, and a daily UTC backup time that creates an enabled `installer-daily` schedule.
4. Optional explicitly verified container image and a final plan confirmation.
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
lifecycle lock, reloads Nginx or restarts the Docker service, then proves health and
certificate identity. `wg-guard exposure status`/`doctor` report SAN, issuer class, expiry, timer,
hook, credential permissions, Nginx drift and listener drift without printing secrets.

Process liveness and certificate readiness remain distinct. A new certificate path starts as
`pending`; a trusted handshake persists `verified`. Built-in ACME can be retried with
`wg-guard tls-check`; managed paths use `wg-guard exposure renew`. Every public response is HTTPS,
and HSTS is emitted only for direct TLS or HTTPS asserted by a trusted private/loopback proxy peer.
See [terminal management](terminal-management.md#panel-access--https) and
[lifecycle recovery](lifecycle-recovery.md).

Install/update/recovery now wait for both local `/healthz` and an exact bounded `/readyz`
response before committing runtime success. A responsive but unready node cannot complete the
lifecycle transition. In ACME mode the sidecar answers only actual loopback readiness; for older
retained artifacts its redirect triggers a fixed local TLS probe with the recorded SNI, never a
request to an arbitrary redirect target. This runtime check does not certify public DNS, TLS trust
or a physical client's tunnel; exposure verification retains its separate certificate proof.

### Scheduler & background work

ONE scheduler goroutine runs the accounting delta cycle +
expiry pass (every `accounting.interval_seconds`, live-reloadable), traffic-sample flush
(`accounting.sample_flush_seconds`), telemetry and housekeeping (10 min:
idempotency-key, session, traffic-history and webhook-event pruning + rate-limit reload).
Delivery (5 s) and backup (1 min) callbacks signal two fixed workers with one coalesced pending
signal each; slow I/O/crypto runs outside the scheduler. Delivery passes have a 4 min deadline,
backup passes 15 min. Archive work is claimed across data-volume processes; scheduled scans
are also serialized and capped at eight due rows. On cancellation/contention due rows remain
eligible. Crashes can repeat a published archive before its schedule advances.
Graceful shutdown first marks readiness false, drains HTTP, stops scheduling and cancels/drains
the workers. A timed-out handler/worker keeps the DB/key lease and DB open until a successful
retry or process exit; it does not admit restore/rotation while readers remain.

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

`wg-guard logs` is host-owned and selects Docker logs from validated state. It defaults to
200 records over 24 hours, caps tail at 10,000 and since at seven days, and supports cancellable
follow and a bounded structured-component filter. Container stdout/stderr are merged into one
redirectable stream; command failures remain nonzero.
`--source operations` instead reads the fixed, private lifecycle journal without requiring install
state. `--source installer` reads the current and rotated root-private host command log without
install state and can follow new lines by name through rotation. Follow applies to service and
installer logs; component filtering applies only to service logs. Raw logs remain local; there is no panel/API
log endpoint.

Docker Compose selects the efficient `local` driver with compression, `max-size=16m` and
`max-file=8`. This hard-bounds the owned container near 128 MiB, but Docker has no age option: the
seven-day CLI query horizon is not a claim of exact physical age deletion.

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
| VPN pools | first profile prefers `network.default_pool` (empty = `10.8.0.0/24`); blank creation searches the available `10.8.N.0/24` ladder when a default conflicts. Up to 15 overflow pools expand a profile without moving existing devices. | yes (Settings + per interface, validated; see [address planning](cleanup.md)) |
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
public status persists only a safe outcome code. Fresh Docker installs enable the bridge;
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
wg-guard core exact awg-2026-10
wg-guard install --yes --public-ip PUBLIC_IP --prerequisites auto --core recommended
```

Replace `PUBLIC_IP` with the server's real public address; documentation-only addresses are rejected.
The core commands print bounded JSON metadata. The host terminal is English-only; shared web-panel
messages retain fa/en catalog parity.

Preflight requires Ubuntu 24.04 or newer on amd64/x86_64 and inspects the running kernel, init,
endpoint and TCP ports before package/deployment writes. Other distributions, older Ubuntu and
other architectures stop before acquisition or deployment. Ubuntu 24.04 is the verified target.
The host needs systemd, `ip`, `nft`, `iptables` and `sysctl`, plus the Docker
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

The recommended `awg-2026-10` bundle does not depend on PPA retention. It clones only the exact
catalogued official kernel tag, verifies its full commit and clean tree, applies the reviewed
[Ubuntu `7.0.0-38` signature correction](../integrations/amneziawg.md) to the DKMS source copy, and
registers `amneziawg/1.0.0-wgguard.20260906.1` with DKMS, retiring a superseded `awg-2026-09` registration.
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
mutations. Container tools must match their reviewed image provenance; managed modules need observable
matching loaded/disk build identity. `--skip-module` explicitly delegates host module lifecycle
to the operator, while still requiring Docker and host diagnostic/network tools. It does not silently select
userspace. A normal managed-core installation fails if the module is absent, different from disk,
or its loaded build identity cannot be established.

The installer accepts Ubuntu 24.04 or newer on amd64; production certification currently covers
Ubuntu 24.04. Newer releases remain unverified and fail closed when the pinned bundle is unavailable.
An active firewalld is not a certified forwarding policy: boot, runtime readiness and doctor
fail closed for enabled tunnels until it is disabled. Docker's scoped forwarding extension
and test-backed UFW-managed routes are the supported coexistence paths. Root is required. Kernel mode needs
DKMS build prerequisites (`build-essential`, matching kernel
headers). Explicit userspace profiles now have a service-owned daemon lifecycle: the Docker
image carries the reviewed daemon and maps `/dev/net/tun`, with embedded Go VCS build provenance.
The daemon's stale `--version` text is not proof of the source revision. New profiles fail before persistence when those
prerequisites are absent. The kernel remains the installed default; historical Docker/kernel and userspace
client traffic passed on Ubuntu 24.04; the new profile needs its own host gate ([ADR-0003](../decisions/ADR-0003-kernel-first-userspace-fallback.md)).

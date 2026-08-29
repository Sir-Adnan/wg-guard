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

| Mode | Behavior |
|---|---|
| Domain + ACME | Built-in `autocert`: HTTP-01 on port 80, TLS served on the configured panel port — any port works (e.g. `https://sub.example.com:34562`); port 80 must stay reachable for issuance/renewal |
| Manual certs | Administrator-provided cert/key paths |
| Behind reverse proxy | HTTP bound to loopback/private interface (explicit, documented choice) |
| Development | Loopback-only HTTP with loud warnings |

The installer never silently exposes plaintext management to the public internet.

## Ports & networking defaults

| Setting | Default | Editable later |
|---|---|---|
| Panel HTTP | 8080 | yes (boot config + Settings) |
| Panel TLS | 443 (any port supported) | yes |
| AWG listen port (per interface) | random 30000–50000; low ports prompt in installer | yes (per interface, hot-applied) |
| MTU | 1420 | yes (global default + per interface) |
| VPN pool | `10.8.0.0/16` carved per interface (`10.8.N.0/24`) | yes (per interface, validated) |

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

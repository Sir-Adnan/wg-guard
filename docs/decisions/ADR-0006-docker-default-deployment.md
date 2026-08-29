# ADR-0006 — Docker-default deployment, native secondary

Status: accepted · Date: 2026-08-29

## Context

The product needs a clean, polished, easy-to-manage install experience. The VPN data plane runs
in the host kernel (AmneziaWG kernel module + forwarding), which a container cannot own without
privileges that defeat container isolation.

## Decision

Docker is the default installation: one official multi-arch image (Ubuntu base + pinned
amneziawg-tools + WG-Guard), `network_mode: host` + `CAP_NET_ADMIN`, generated compose file,
host `wg-guard` shim execing into the container. The kernel module is a host-installed
component (installer handles it). Native systemd mode remains fully supported with identical
data paths (`/etc/wg-guard`, `/var/lib/wg-guard`), so backups and mode switches are trivial.

## Consequences

- Zero hot-path overhead: traffic is forwarded by the host kernel module, never through the
  container.
- Declarative upgrades (`compose pull` + health-checked restart) and clean removal.
- Native path keeps the spec's "Docker never required" guarantee.

## Alternatives rejected

- Panel container + privileged host agent: second long-running process + IPC attack surface for
  zero data-plane gain.
- Privileged container loading DKMS itself: ≈ host root with worse isolation; module must match
  host kernel headers anyway.

# Phase 8.3 — Data-plane forwarding integrity

Status: **active (2026-09-10); root cause reproduced, implementation not yet verified**.

## Objective

Make a successful client handshake mean that routed IPv4 traffic can actually leave and return
through the supported Ubuntu 24.04 amd64 node, including the default Docker deployment. Close the
test gap that previously exercised only the tunnel gateway or namespace-local traffic.

## Confirmed failure

Docker's iptables backend can set the host `FORWARD` policy to `DROP` and jump through
`DOCKER-USER` before WG-Guard's independent nftables chain at priority `filter + 10`. A drop is
terminal across nftables base chains, so the later WG-Guard accept rules cannot restore the
packet. Handshake packets target the host and therefore continue to work, which masks the routed
data-plane failure. The dedicated VPS and an isolated three-namespace reproduction both exhibit
this ordering.

## Scope and milestones

- [x] **8.3.0** — Reproduce the failure, trace the hook ordering, review official Docker,
  nftables and pinned AmneziaWG contracts, and freeze this corrective boundary.
- [ ] **8.3.1** — Test-first implementation of scoped, idempotent Docker forwarding coexistence;
  preserve the namespaced nftables NAT table and never change the global FORWARD policy.
- [ ] **8.3.2** — Make unresolved forwarding blockers visible to boot readiness and `doctor`;
  keep UFW handling effective and avoid silently claiming a working VPN.
- [ ] **8.3.3** — Add the required Ubuntu/container runtime tooling and remove only WG-Guard-owned
  compatibility state when no interface remains or the node is uninstalled.
- [ ] **8.3.4** — Extend integration and real-VPS gates through an AWG client default route to
  public IP, DNS and HTTPS; verify counters, restart/reconcile idempotency and cleanup.
- [ ] **8.3.5** — Run final focused/full CI gates, synchronize evidence/status, commit and push.

## Safety and compatibility

- Product support remains Ubuntu 24.04 or newer on amd64; no arm64 work is added.
- Existing AWG parameter, config, QR and REST/OpenAPI contracts do not change.
- Docker integration uses its documented user-policy extension point and rules are limited to
  WG-Guard interface names and their exact private subnets.
- WG-Guard does not set global `FORWARD ACCEPT`, disable Docker firewalling, restart Docker, flush
  foreign tables, or overwrite administrator rules.
- A foreign or unsupported blocking policy fails with an actionable diagnosis rather than an
  unsafe global mutation.

## Completion criteria

Automated regressions fail on the old ordering and pass with scoped coexistence; `doctor` detects
both healthy and blocked paths; fresh default Docker installation on the dedicated Ubuntu 24.04
amd64 VPS passes real AWG handshake, public IP, DNS and HTTPS traffic, service restart and
reconcile; owned rules clean up without changing unrelated firewall state; the complete Go/CI
gate is green and evidence is linked from status/release readiness.

## Deferred

Interface-delete transaction ordering (AUD-050), generic firewalld policy management, later
Ubuntu certification, userspace-daemon lifecycle, soak/load/security certification and the full
UI redesign remain Phases 10–11. Phase 8.3 may diagnose unsupported foreign policies but does not
take ownership of them.

# Phase 8.3 — Data-plane forwarding integrity

Status: **complete (2026-09-10)**.

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
- [x] **8.3.1** — Test-first implementation of scoped, idempotent Docker forwarding coexistence;
  preserve the namespaced nftables NAT table and never change the global FORWARD policy.
- [x] **8.3.2** — Make unresolved forwarding blockers visible to boot readiness and `doctor`;
  keep UFW handling effective and avoid silently claiming a working VPN.
- [x] **8.3.3** — Add the required Ubuntu/container runtime tooling and remove only WG-Guard-owned
  compatibility state when no interface remains or the node is uninstalled.
- [x] **8.3.4** — Extend integration and real-VPS gates through an AWG client default route to
  public IP, DNS and HTTPS; verify counters, restart/reconcile idempotency and cleanup.
- [x] **8.3.5** — Run final focused/full CI gates, synchronize evidence/status, commit and push.

## Implementation

- The running node now uses one serialized `boot.RuntimeReconciler` after structural API, web and
  accounting changes. It updates AWG state, rendered nftables NAT/forwarding, manager coexistence
  and shaping together; runtime failure drops `/readyz` until a successful canonical pass.
- Docker's iptables backend is handled through a fixed `WGGUARD-FORWARD` child chain and one tagged
  jump from `DOCKER-USER`. Rules admit only source traffic arriving on an owned AWG interface and
  established return traffic to that interface's exact subnet. Desired rules are added before
  stale ones are removed, so live updates do not flush the chain or open a forwarding blackout.
- The global `FORWARD` policy remains untouched. A DROP without complete Docker or UFW coverage is
  a boot error. Missing manager executables are correctly classified as absent, not as failed
  repairs.
- `doctor` distinguishes the nftables table from the effective forwarding path and understands
  iptables' canonical rule ordering. Docker/runtime images contain Ubuntu's `iptables-nft`
  compatibility tool; native prerequisite checks require it.
- Empty desired state and uninstall remove the WG-Guard nftables table, tagged jump and child
  chain only. Docker's own chains and host-wide policy are preserved.

## Verification results

- Test-first regressions cover Docker's earlier DROP, missing/partial/canonical rules, live
  no-flush reconciliation, post-start interface creation, UFW repair errors and real missing-tool
  error shapes, runtime readiness degradation/recovery, doctor status, runtime-image tooling and
  uninstall ownership.
- The closing gate passed `gofmt`/diff hygiene, fixture shell syntax, `go test ./...`, `go vet
  ./...`, race tests for firewall/boot/serve/doctor, and the root-only nftables integration test.
- Exact data-plane candidate `1c7cb032b29d05a14aeea2db94b1cb31d988804c` passed a fresh local-image Docker
  install on Ubuntu 24.04.4 amd64 with Docker 29.1.3 and kernel 6.8.0-138. Interfaces were created
  only after the node was already serving. Downloaded plain, recommended and randomized configs
  each passed handshake, gateway, public IPv4, DNS, HTTPS, public NAT identity and bidirectional
  transfer counters while the host policy remained `FORWARD DROP`.
- Restart retained readiness and public HTTPS for all three clients with exactly six child rules
  and one tagged jump. `doctor` reported interfaces, nftables, forwarding and sysctl as pass.
  Uninstall removed the table, child chain and jump while preserving Docker's DROP policy; test
  namespaces, links, container and secret-bearing files were removed. Sanitized evidence:
  [verify-phase8.3-vps-2026-09-10.txt](../integrations/fixtures/verify-phase8.3-vps-2026-09-10.txt).
- Closing fail-safe regressions reject an unparseable effective policy, unexpected rules inside
  WG-Guard's owned child chain and nftables permission failures. They do not change the verified
  normal data path and are covered by the final automated gate.

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

RB-011 is closed: automated regressions catch both root causes, the exact default-Docker candidate
passes the real public-egress matrix, diagnostics agree with effective state, and owned rules
clean up without changing unrelated firewall state.

## Deferred

Interface-delete transaction ordering (AUD-050), generic firewalld policy management, later
Ubuntu certification, userspace-daemon lifecycle, soak/load/security certification and the full
UI redesign remain Phases 10–11. Phase 8.3 may diagnose unsupported foreign policies but does not
take ownership of them.

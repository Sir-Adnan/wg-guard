# Networking

WG-Guard owns its tunnel interfaces, firewall rules, and shaping — deterministically,
idempotently, and without ever endangering administrator access (SSH lockout is the classic
self-inflicted outage).

## Tunnel interfaces

- Interfaces `awg0…awg7` (15-char name limit applies), one per obfuscation profile; the count
  cap is a configurable WG-Guard default, not an upstream limit.
- WG-Guard brings interfaces up itself (`ip link add awgN type amneziawg`, `awg setconf`,
  address, MTU) — **not** `awg-quick`, whose firewall/routing mutations are global and outside
  our ownership model.
- Per interface: listen port (recommended default: random 30000–50000, low-port prompt
  available), IPv4 pool (recommended default `10.8.N.0/24`, validated: RFC1918, no overlap with
  host routes or other interfaces, minimum size warnings), MTU (recommended default 1420 —
  Phase 0 pinned the upstream constraints; transport-level overhead on real hardware is
  confirmed in the Phase 8 VPS matrix before any guidance change), endpoint override
  (host[:port]) for generated client configs.
- Bulk peer changes use `awg syncconf` (diff-apply without resetting active sessions);
  single-peer ops use `awg set`. One `awg show awgN dump` per interface per accounting cycle
  feeds stats, handshakes, and drift detection.

## Firewall ownership (nftables)

- Everything lives in one namespaced table: `table inet wgguard`. Rules are commented
  `wgguard:managed`. WG-Guard **never** flushes or edits foreign tables (Docker, ufw, admin
  rules) and only ever *adds* scoped rules — never global policies.
- Contents: forward accept for `awgN` subnets (priority `filter + 10` so host firewall chains
  evaluate first), srcnat masquerade (or explicit SNAT) for tunnel egress.
- **Firewall coexistence**: doctor/installer detect ufw/firewalld. With ufw's default forward
  DROP policy, forwarded tunnel traffic dies before our accept rules run — so we add the
  required allow rule *through that framework* (`ufw route allow in on awgN`) or print the
  exact commands. This is the most common "installed fine, no traffic" failure and is handled
  explicitly.
- Uninstaller removes exactly the `wgguard` table and nothing else.

## Sysctls & system state

`net.ipv4.ip_forward=1` (and IPv6 forwarding when used) set idempotently; prior values recorded
under `/var/lib/wg-guard` for clean restore on uninstall. Recorded in the runbook.

## Shaping (speed limits)

Linux `tc` (HTB) per device IP on the interface egress; unlimited/preset/custom Mbps; separate
upload/download designed-for-later. Rules are deterministic (rendered from DB state), rebuilt on
restart/reconcile, and cleaned up on delete. Thousands of tc classes cost CPU — benchmarked at
1000 shaped peers in Phase 8 with a documented graceful-degradation policy if needed.

## Addressing

- One IPv4 per device per interface pool, UNIQUE-constrained, transaction-allocated.
- IPv6: schema-ready (nullable per-interface pool); enabled later without redesign.

## Safety invariants

1. DB is the source of truth; kernel state is reconciled to it (see [overview.md](overview.md)).
2. No shell interpolation, ever — `exec` with explicit argv and timeouts.
3. Interface mutations staged: render → validate → apply → verify → audit; failures roll back.
4. Networking changes must never lock out the administrator: scoped rules only, coexistence
   handled, `--dry-run` uninstall.

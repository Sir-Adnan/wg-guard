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
  confirmed in the Phase 11 production matrix before any guidance change), endpoint override
  (host[:port]) for generated client configs.
- Bulk peer changes use `awg syncconf` (diff-apply without resetting active sessions);
  single-peer ops use `awg set`. One `awg show awgN dump` per interface per accounting cycle
  feeds stats, handshakes, and drift detection.

## Firewall ownership (nftables)

- Everything lives in one namespaced table: `table inet wgguard`. Rules are commented
  `wgguard:managed`. WG-Guard **never** flushes or edits foreign tables (Docker, ufw, admin
  rules) and only ever *adds* scoped rules — never global policies.
- The table is applied as **rendered state**: its full content is a pure function of the
  enabled interfaces' device pools, applied atomically with `nft -f` (probe → when present,
  one transaction deletes and recreates it, so re-applying never duplicates rules; with zero
  enabled interfaces the desired state is "no table"). Forward chain priority 10 (after the
  standard `filter` chains evaluate), postrouting priority 100 (`srcnat`); chain policies are
  `accept` because a base chain needs one and `drop` here would be a global policy.
- Contents: forward accept for `awgN` (both directions), and per-interface masquerade
  `oifname != "awgN" ip saddr <pool> masquerade` — the wg-quick pattern, so multi-WAN hosts and
  interface renames need no re-config. Tunnel-to-tunnel traffic is masqueraded too (same as
  wg-quick); per-profile isolation stays a possible future setting.
- **Firewall coexistence**: doctor/installer detect ufw/firewalld. With ufw's default forward
  DROP policy, forwarded tunnel traffic dies before our accept rules run — so bring-up adds the
  required allow rule *through that framework* (`ufw route allow in on awgN`, idempotent) and
  reports findings (active managers, whether the routed policy blocks forwarding, and the exact
  remedy commands). This is the most common "installed fine, no traffic" failure and is handled
  explicitly.
- Uninstaller removes exactly the `wgguard` table and nothing else.

## Sysctls & system state

`net.ipv4.ip_forward=1` (and IPv6 forwarding when used) set idempotently at bring-up (the read
value is checked first; only `sysctl -w` when off); prior values are recorded under
`/var/lib/wg-guard` for clean restore on uninstall. Recorded in the runbook.

## Reconciliation of mode changes

The pinned runtime cannot switch an interface between plain and obfuscated states with
`setconf` (explicit zeros are rejected; omitted keys persist — see
[../integrations/amneziawg.md](../integrations/amneziawg.md)). The reconcile engine therefore
recreates the link (remove → create → peer re-sync) when it observes an obfuscation-mode
transition; same-mode parameter drift applies via `setconf`. Peer reconciliation of a freshly
(re)created interface re-adds desired devices wholesale; non-WG-Guard peers are lost by
recreation (their PSKs are unknowable) and the drift report says so.

## Shaping (speed limits)

Implemented in Phase 3 (egress) and Phase 4 (ingress) in `internal/shaper`. Limits are
**independent per direction** and stored per user (and per plan) as
`speed_limit_down_kbps` / `speed_limit_up_kbps`; NULL/0 means unlimited for that direction,
and setting one direction never touches the other's qdisc tree.

- **Download (server→client) = egress**: Linux `tc` HTB on the tunnel interface itself. One
  class per (user, interface) carries the user's download limit; one u32 filter per device IP
  selects the class — aggregate enforcement across a user's devices. Users without limits pass
  via HTB's direct service (`default 0`), so shaping one user never degrades another.
- **Upload (client→server) = ingress**: the tunnel's ingress qdisc mirrors packets into an IFB
  device `ifb-<iface>` (`mirred egress redirect`), where the same HTB design applies to packet
  **source** addresses (client IPs). IFB is created on demand and torn down with the tree when
  the last upload limit on the interface is removed. An IFB-unavailable kernel (module missing)
  fails **upload** shaping with an explicit error ("ifb device … unavailable") while download
  limits remain enforced — direction independence degrades independently, and a failure never
  looks like an enforced limit.

Rules are deterministic (rendered from DB state), rebuilt on restart/bring-up and re-ensured by
the accounting cycle (change detection: identical desired state costs zero subprocesses), and
cleaned up per direction when a limit is removed. A rebuild deletes the affected root qdisc(s)
and recreates the tree(s) in one `tc -b` batch (`qdisc add` — `qdisc replace` is rejected by
HTB with "Change operation not supported", verified against iproute2 in WSL2 2026-08-30).
tc failures at bring-up are non-fatal findings with a remedy; with limits configured and tc
missing, ensure fails loudly — an unenforced limit must never look enforced.

Live-pinned ingress facts (WSL2, kernel 6.18.33.1-microsoft-standard, 2026-08-30): `ip link add
X type ifb` works; `tc qdisc add dev X handle ffff: ingress` + `mirred egress redirect` filters
work; HTB on ifb works; `tc qdisc del dev X ingress` exits 0 even when absent (tolerated in the
rebuild). Thousands of tc classes cost CPU — the per-user class design keeps the class count at
users-with-limits, not devices. The 1000-shaped-peer tc benchmark and production degradation
policy remain the Phase 11 certification pass.

## Addressing

- One IPv4 per device per interface pool, UNIQUE-constrained, transaction-allocated.
- IPv6: schema-ready (nullable per-interface pool); enabled later without redesign.

## Safety invariants

1. DB is the source of truth; kernel state is reconciled to it (see [overview.md](overview.md)).
2. No shell interpolation, ever — `exec` with explicit argv and timeouts.
3. Interface mutations staged: render → validate → apply → verify → audit; failures roll back.
4. Networking changes must never lock out the administrator: scoped rules only, coexistence
   handled, `--dry-run` uninstall.

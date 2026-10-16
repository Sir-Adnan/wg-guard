# ADR-0016 — Kernel-native egress routing instead of a proxy engine

Status: accepted · Date: 2026-10-16

## Context

Operators want to send chosen destinations through another exit and leave the rest direct.
Typical cases are AI services, Google Play or IP-lookup sites, sent through WARP, a provider's
WireGuard/AmneziaWG server or another local IP. Some also want several exits grouped behind a
balancer.

Xray-style panels do this with a userspace proxy core that sniffs SNI and routes by domain. That
approach brings three problems:

- a second large runtime to pin and update;
- per-packet userspace copies, or a tun2socks hop for a WireGuard data path;
- a configuration language that does not match WG-Guard's DB-is-truth reconciliation model.

## Decision

Egress routing uses only kernel facilities that WG-Guard already depends on:

- **Exits.**
  - Each tunneled outbound is an extra AmneziaWG/WireGuard link (`wgoN`), managed through the
    existing `tunnel.Backend`.
  - Each exit has a policy-routing table selected by an fwmark in a reserved mark field.
  - `source_ip` exits use SNAT.
- **Classification.** Traffic is classified in one namespaced nftables table,
  `ip wgguard_egress`. Connections keep their exit through the conntrack mark.
- **Balancers.** Balancers are nftables maps: `jhash` for per-device `sticky`, `numgen` for
  `round_robin`/`random`/`least_conn`, and a single jump for `failover`. `least_conn` shares are
  recomputed from conntrack counts on each health pass.
- **Domain rules.** Domain rules are resolved at DNS time by a small built-in forwarding resolver
  on the tunnel gateways. It feeds answer addresses into per-rule nftables timeout sets. The
  routing decision stays in the kernel; the resolver only learns addresses.
- **Geodata.** v2fly `geosite.dat`/`geoip.dat` are read by an in-tree protobuf reader with no
  new dependency.

## Consequences

- No new runtime or service. The data path stays in the kernel, and the extra cost is one DNS hop
  plus one set update per newly seen name.
- Domain matching only works for names resolved through the node. The design accounts for this:
  - port-53 traffic is redirected to the resolver;
  - DoH/DoT is blocked by default, with an explicit operator opt-out;
  - IP and geoip matchers do not depend on DNS.
- Matching is by destination address, so a CDN address shared with an unmatched site follows the
  rule while the address is in the set. This is documented and limited by the TTL clamp.
- IPv6 egress is out of scope. The resolver suppresses AAAA answers for matched names so clients
  fall back to IPv4.
- Teardown is exact: one table, owned rules and routing tables, and links attributed in the
  database.

## Alternatives rejected

- **Xray or sing-box core.** Large dependency and a second configuration model, and it breaks the
  "no proxy engine" product constraint.
- **SNI sniffing in nftables or a userspace queue.** Fragile (ECH, QUIC, TLS 1.3 fragments) and
  needs NFQUEUE plus a userspace hot path.
- **`ip rule` per destination prefix.** Does not scale to geosite-sized lists, has no conntrack
  stickiness and no load balancing.

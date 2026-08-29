# ADR-0004 — Namespaced nftables table + firewall coexistence

Status: accepted · Date: 2026-08-29

## Context

VPN forwarding needs NAT and forward rules. Hosts run other firewall managers (ufw, firewalld,
Docker) whose chains and policies interact with ours; blindly flushing global state is the
classic way panels lock administrators out of SSH.

## Decision

All rules live in one namespaced table: `table inet wgguard`, rules commented `wgguard:managed`.
WG-Guard never flushes or modifies foreign tables and never sets global policies — it only adds
scoped forward-accept (priority `filter + 10`) and srcnat rules. Firewall-manager coexistence is
explicit: doctor/installer detect ufw/firewalld and add the required forward allow-rule through
that framework (ufw's default forward DROP would otherwise shadow our rules).

## Consequences

- Predictable uninstall (`delete table inet wgguard`); no collateral damage.
- One documented failure mode (ufw forward policy) handled proactively.

## Alternatives rejected

- Using `awg-quick up` firewall behavior: global, unowned mutations outside our model.
- Raw iptables: deprecated path, worse interaction with nftables-native hosts.

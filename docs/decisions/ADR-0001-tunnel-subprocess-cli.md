# ADR-0001 — Tunnel control via the pinned `awg` CLI subprocess

Status: accepted · Date: 2026-08-29

## Context

WG-Guard must manage AmneziaWG interfaces/peers. Options: import amneziawg-go as a library
(MIT, technically importable), speak netlink/UAPI directly, or execute the pinned `awg` CLI.

## Decision

Execute the pinned `awg` CLI exclusively, behind `internal/tunnel.TunnelBackend`: explicit
argv, timeouts, no shell interpolation; bulk peer ops via `awg syncconf`, single ops via
`awg set`, stats/handshakes via one `awg show <iface> dump` per interface per accounting cycle.

## Consequences

- Decouples the panel from a fast-moving fork's device stack (a userspace panic cannot take
  the panel down); every existing panel uses this pattern.
- GPLv2 tools are cleanly executed, not linked/vendored.
- Requires a strict dump parser (AWG v3.1 dump format is documented + fixture-pinned in
  docs/integrations/amneziawg.md) and version pinning/capability detection.

## Alternatives rejected

- Library import: couples process lifetime to upstream fork churn; beta-quality device stack.
- Direct netlink/UAPI: reimplements upstream tools; high maintenance, easy to get subtly wrong.

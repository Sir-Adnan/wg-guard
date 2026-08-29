# ADR-0003 — Kernel module primary, userspace fallback

Status: accepted · Date: 2026-08-29

## Context

AmneziaWG ships a kernel module (DKMS, best performance, no resident process) and a userspace
daemon (amneziawg-go, one process per interface, needs only TUN). DKMS needs headers +
build toolchain, which some VPS images lack.

## Decision

Prefer the kernel module; fall back to the userspace daemon when DKMS prerequisites are
unavailable. Both are transparent to WG-Guard because the `awg` CLI talks to either (netlink
vs UAPI socket — see docs/integrations/amneziawg.md). The active backend is reported via
`/api/v1/node` and `doctor`.

## Consequences

- Best throughput and lowest overhead in the common case; deployment still works on hosts
  without DKMS.
- Userspace mode inherits upstream userspace bug history (arm64 H4, RandomTrailers panic) —
  mitigated by avoiding the buggy feature surface and treating userspace as fallback only.
- Doctor distinguishes backends (kernel module presence vs UAPI socket).

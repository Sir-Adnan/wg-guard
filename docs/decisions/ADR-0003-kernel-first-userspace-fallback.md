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

## Implementation status

The decision remains the target architecture, but the Phase 8 audit found that daemon lifecycle
orchestration has not landed: `backend_mode` is stored/reported while boot and reconciliation
always take the kernel-link path. The adapter has been exercised against a manually started
pinned daemon; that proves wire/config compatibility, not automatic fallback. Phase 11 owns a
bounded supervisor, restart/failure drills, and observed backend reporting (AUD-019). Until then,
production-managed tunnels require the kernel module and status values describe configured intent.

## Consequences

- Best throughput and lowest overhead in the common case. Once AUD-019 is implemented, hosts
  without DKMS retain a managed fallback; until then they require manual daemon operation and
  are not certified production targets.
- Userspace mode inherits upstream userspace bug history (arm64 H4, RandomTrailers panic) —
  mitigated by avoiding the buggy feature surface and treating userspace as fallback only.
- Doctor distinguishes backends (kernel module presence vs UAPI socket).

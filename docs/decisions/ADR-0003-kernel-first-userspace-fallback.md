# ADR-0003 — Kernel module primary, userspace fallback

Status: accepted · Date: 2026-08-29

## Context

AmneziaWG ships a kernel module (DKMS, best performance, no resident process) and a userspace
daemon (amneziawg-go, one process per interface, needs only TUN). DKMS needs headers +
build toolchain, which some VPS images lack.

## Decision

Prefer the kernel module; offer an explicit userspace mode when the pinned daemon and TUN are
available. Both use the `awg` CLI (netlink vs UAPI socket; see
docs/integrations/amneziawg.md). Configuration is never silently switched between backends.
The configured mode is reported via `/api/v1/node`; `doctor` compares it with observed runtime.

## Implementation status

Phase 11 implements one foreground daemon per userspace interface under the node process. The
supervisor verifies the pinned daemon identity, owns its child and UAPI socket, notices failure,
repairs through canonical reconciliation, and stops it on removal/shutdown. A pre-existing active
unowned daemon fails closed. The exact pinned source passed WSL TUN/UAPI integration; Docker and
native real-host certification is still required before this mode is production-certified.
Native installs need the separately installed reviewed daemon; the managed installer continues
to require the kernel module by default. Automatic selection on DKMS failure is not implemented.

## Consequences

- Best throughput and lowest overhead in the common kernel case. Explicit userspace profiles can
  run without DKMS after the pinned daemon and TUN are provisioned; no silent fallback is claimed.
- Userspace mode inherits upstream userspace bug history (arm64 H4, RandomTrailers panic) —
  mitigated by avoiding the buggy feature surface and treating userspace as fallback only.
- Doctor distinguishes backends (kernel module presence vs UAPI socket).

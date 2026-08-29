# ADR-0002 — One obfuscation profile per tunnel interface (`awg0…awg7`)

Status: accepted · Date: 2026-08-29

## Context

AmneziaWG obfuscation parameters (Jc/Jmin/Jmax/S1–S4/H1–H4, optional I1–I5) live in the
`[Interface]` section and are shared by **all peers** on an interface — per-peer profiles are
impossible upstream. The product requires per-user/per-plan obfuscation profiles.

## Decision

Manage multiple tunnel interfaces (`awg0…awg7` — a cap of 8 is the WG-Guard default, not an
upstream limit; administrator-configurable in Settings), one per obfuscation profile, each with
its own listen port and IPv4 pool. A user's profile selects the interface its
devices' peers live in. Changing a profile's parameters is a guided **rotation** (new interface
→ migrate users/devices → retire old), never a silent in-place edit (existing clients embed the
old parameters).

## Consequences

- Per-user profiles work within upstream constraints; "Plain WG" compat profile (all-zero
  params) supports stock WireGuard clients.
- Costs: more interfaces (each trivial for the kernel), IP pool bookkeeping, and the
  documented rotation workflow; interfaces/ports/subnets validated for collisions.

## Alternatives rejected

- Single global profile: violates the product requirement (spec §6/§14).
- Per-peer params via cleverness: does not exist upstream; inventing protocol behavior is
  forbidden.

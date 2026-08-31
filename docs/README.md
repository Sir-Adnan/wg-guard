# WG-Guard Documentation

WG-Guard is a lightweight, self-hosted AmneziaWG VPN node management panel for Linux VPS
servers: one Go binary, SQLite, a premium bilingual (fa/en) web panel, and a stable REST API for
external management systems (Telegram bots, billing panels, VPN platforms).

This directory is the authoritative documentation. Nothing important lives only in code comments
or chat history.

## Reading map

| Document | Purpose |
|---|---|
| [product/requirements.md](product/requirements.md) | What WG-Guard is: product scope, user/device/profile model, lifecycle, non-goals |
| [product/ui-ux.md](product/ui-ux.md) | Design system: tokens, components, i18n/RTL, themes, motion, budgets, QA gates |
| [architecture/overview.md](architecture/overview.md) | How it works: process model, components, key decisions, resource design |
| [architecture/project-structure.md](architecture/project-structure.md) | Repository and package layout, naming conventions, dependency rules |
| [architecture/database.md](architecture/database.md) | SQLite schema, invariants, allocation, retention, migration policy |
| [architecture/api.md](architecture/api.md) | REST `/api/v1` contract: auth, errors, pagination, idempotency, versioning |
| [architecture/networking.md](architecture/networking.md) | Tunnel interfaces, nftables ownership, sysctls, firewall coexistence, shaping |
| [integrations/amneziawg.md](integrations/amneziawg.md) | Pinned AmneziaWG upstream: verified facts, CLI behavior, fixtures, verification log |
| [integrations/webhooks.md](integrations/webhooks.md) | Event catalog, HMAC signature scheme, durable delivery semantics |
| [operations/deployment.md](operations/deployment.md) | Docker (default) and native installation, TLS modes, ports, updates |
| [operations/backup-restore.md](operations/backup-restore.md) | Backup archives, schedules, Telegram delivery, restore and server migration |
| [operations/runbook.md](operations/runbook.md) | Operational procedures: install/update/uninstall, DR, doctor, incidents |
| [operations/security.md](operations/security.md) | Threat model, secrets inventory, panel hardening, subprocess safety |
| [development/workflow.md](development/workflow.md) | Build, test, lint, CI, release process |
| [development/testing.md](development/testing.md) | Testing strategy: layers, fake backend, integration tags, benchmarks |
| [development/status.md](development/status.md) | Feature matrix: designed / implemented / unit tested / integration tested / needs real VPS |
| [development/release-readiness.md](development/release-readiness.md) | Active Phase 8–12 program: requirement ownership, blockers, audit findings, compatibility state |
| [development/phase8.md](development/phase8.md) | Current Phase 8 execution checklist and verification log |
| [development/phase9.md](development/phase9.md) through [phase12.md](development/phase12.md) | Planned observability, UI/UX, production-certification, and release-candidate gates |
| [decisions/](decisions/) | Architecture Decision Records (ADRs) |

## Archived sources (frozen, provenance only)

- [archive/wg-guard_SPEC.md](archive/wg-guard_SPEC.md) — the original 3,461-line specification.
  Superseded by the docs above; kept as the historical source of truth.
- [archive/INITIAL_DELIVERABLE.md](archive/INITIAL_DELIVERABLE.md) — original upstream research
  into the AmneziaWG ecosystem (licenses, CLI surface, known bugs). Still the reference for
  upstream facts not yet reproduced in `integrations/amneziawg.md`.
- [archive/ARCHITECTURE_V2_PROPOSAL.md](archive/ARCHITECTURE_V2_PROPOSAL.md) — the approved
  architecture proposal (v2 + final-direction revisions).

## Rules

- Every document stays under ~400 lines and never duplicates another.
- When implementation changes behavior, the corresponding doc is updated in the same change.
- Anything not verified against a real environment is labeled explicitly
  (designed / needs real VPS verification), never implied.

# Release-readiness program

Living tracker for the approved Phase 8–12 program. `ROADMAP.md` owns phase order and gates;
this document owns cross-phase requirement coverage, release blockers, audit findings, and
verification state. Phase execution details live in the active phase document.

Last updated: 2026-08-31. Active phase: **8 — Audit & configuration integrity**.

## Program status

| Phase | State | Exit dependency |
|---|---|---|
| 8 — Audit & configuration integrity | active | Lossless config + decoded QR + real handshake/traffic evidence |
| 9 — Operational observability | planned | Useful live metrics/logs with bounded cost and retention |
| 10 — Product UI/UX redesign | planned | Every route/state passes complete bilingual responsive QA |
| 11 — Production certification | planned | Material findings closed; supported compatibility cells verified |
| 12 — Release candidate | planned | Clean, reproducible candidate ready for owner-approved publication |

Phases execute sequentially. A discovery may be assigned to a future phase, but unrelated
implementation does not cross the active phase boundary.

## Requirement ownership

| Requirement area | Owning phase | Verification gate |
|---|---|---|
| Comprehensive project audit and discoveries | 8 starts; all phases maintain; 12 closes | Every material finding resolved, assigned, or explicitly deferred with reason |
| QR rendering and content identity | 8 | Decode generated PNG and byte-compare with downloadable config on every surface |
| Client config correctness and AWG parity | 8 | DB/runtime/download/QR/subscription equality plus real handshake and traffic |
| H1–H4 and other range semantics | 8 | Lossless migration, validation, API, setconf, dump, reconcile, and backup round trips |
| Recommended and randomized profiles | 8 | Relationship-aware generation, property tests, runtime acceptance, client use |
| Live CPU/RAM/network/peer/node monitoring | 9; visual finish in 10 | Real-load graphs, hidden-tab pause, measured sampler overhead |
| Unified CLI operational logs | 9 | Docker/native failure drills, follow/cancel behavior, no secret disclosure |
| Seven-day log retention and disk bounds | 9; soak in 11 | Fake-clock tests, platform policy inspection, disk-growth drill |
| Complete shadcn-style visual system | 10 | All routes and states migrated; no legacy visual surface |
| Mobile, desktop, large-screen responsiveness | 10 | Browser matrix from ~320 px through ultrawide with no overflow |
| Settings information architecture | 10 | Normal flow simplified; advanced controls remain reachable and explained |
| fa/en localization and RTL/LTR | 10 | Catalog parity, no raw keys, copy review, correct technical-data direction |
| Token scope, admin permission, webhook event wording | 10 | Localized human labels/descriptions; stable machine identifiers retained |
| Security, race, soak, resource and performance hardening | 11 | Recorded tests/benchmarks with no unresolved critical/high finding |
| OS/architecture/backend/deployment matrix | 11 | Real-host evidence per supported cell; unverified cells not advertised |
| Backup/update/rollback/recovery/ACME drills | 11 | Repeatable evidence using the feature-frozen candidate |
| API/OpenAPI synchronization | Every affected phase; 12 final | Bidirectional route/schema coverage green |
| Documentation and repository hygiene | Every phase; 12 final | Living docs agree; no secrets or inappropriate artifacts tracked |
| Release artifacts and publication workflow | 12 | Checksums/multi-arch metadata verified; publication remains manually gated |

## Release blockers

| ID | Blocker | Owner | State | Evidence required to close |
|---|---|---|---|---|
| RB-001 | QR images do not reliably render/scan on panel and subscription surfaces | Phase 8 | open | Automated decode equality + browser/mobile scan evidence |
| RB-002 | Generated client configuration has unverified/lossy parameter paths | Phase 8 | open | Canonical pipeline and real default/randomized tunnel traffic |
| RB-003 | H1–H4 ranges are reduced to scalar integers in current models | Phase 8 | open | Lossless full-chain round trip and migration evidence |
| RB-004 | Complete pinned-version parameter/client compatibility is not classified | Phase 8 | open | Source/runtime/client capability matrix with gated unsupported fields |
| RB-005 | Operational troubleshooting and log retention are incomplete | Phase 9 | planned | Unified log workflow and bounded retention verified in both modes |
| RB-006 | Existing UI is not the requested complete design and QA baseline | Phase 10 | planned | Full route/state/browser matrix completed |
| RB-007 | Production compatibility and hardening matrix is incomplete | Phase 11 | planned | Supported cells and recovery/performance evidence recorded |
| RB-008 | Versioned checksummed artifacts and official multi-arch workflow are absent | Phase 12 | planned | Clean candidate pipeline dry run and artifact install verification |

No release blocker may be silently downgraded. A blocker can close only with linked evidence or
be explicitly waived by the project owner with the residual risk recorded.

## Audit findings

Severity: critical (secret loss/exposure or unusable release), high (major correctness/security),
medium (material product/operations weakness), low (polish/maintainability). State values are
`open`, `in progress`, `verified`, or `deferred`.

| ID | Severity | Finding | Owner | State |
|---|---|---|---|---|
| AUD-001 | critical | QR raster path has no decode/content-equivalence test; current regression can pass an unusable image | Phase 8 | open |
| AUD-002 | critical | H1–H4 are stored as `uint32`; observed `low-high` values lose the upper bound during dump parsing | Phase 8 | open |
| AUD-003 | high | OpenAPI exposes only a subset of current AWG profile fields and models H1–H4 as integers | Phase 8 | open |
| AUD-004 | high | Random profile generation is split between browser and server paths, weakening canonical validation | Phase 8 | open |
| AUD-005 | high | No single CLI workflow aggregates operational logs across deployment modes | Phase 9 | planned |
| AUD-006 | high | Application/deployment log retention is not documented or enforced as one bounded policy | Phase 9 | planned |
| AUD-007 | medium | Human-facing token scopes, admin permissions, and webhook events expose machine identifiers | Phase 10 | planned |
| AUD-008 | low | `project-structure.md` says Go 1.22 while `go.mod`, workflow, and CI require 1.25 | Planning update | in progress |

Add only evidence-backed findings. Do not use this table as an idea backlog.

## Compatibility certification

The Phase 11 matrix starts from the honest state below. “Planned” is not support evidence.

| OS | Arch | Docker | Native | Kernel backend | Userspace fallback | State |
|---|---|---|---|---|---|---|
| Ubuntu 24.04 | amd64 | drill verified | drill verified | drill verified | integration only | partial |
| Ubuntu 24.04 | arm64 | planned | planned | planned | planned | unverified |
| Ubuntu 22.04 | amd64 | planned | planned | planned | planned | unverified |
| Ubuntu 22.04 | arm64 | planned | planned | planned | planned | unverified |
| Debian 12 | amd64 | planned | planned | unavailable/unknown | planned | unverified |
| Debian 12 | arm64 | planned | planned | unavailable/unknown | planned | unverified |

Containers, cross-compilation, and emulation can validate packaging but do not upgrade a
real-host kernel or architecture cell to verified.

## Discovery workflow

1. Reproduce or cite concrete evidence; do not log or paste secrets/config contents.
2. Record impact, severity, affected surface, and likely owning phase.
3. Fix critical/high findings in the earliest dependency-safe phase.
4. Add a regression test before the fix and attach real-host evidence when required.
5. Update this tracker, the active phase document, status matrix, and behavior documentation in
   the same coherent change.

## Publication boundary

Phase 12 may build, checksum, install, upgrade, and inspect candidate artifacts and may prepare a
manual publication workflow. It must stop before any final public tag, release, or registry image
is published. Publication requires explicit project-owner approval.

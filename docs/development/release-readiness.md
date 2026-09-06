# Release-readiness program

Living tracker for the approved Phase 8–12 program. `ROADMAP.md` owns phase order and gates;
this document owns cross-phase requirement coverage, release blockers, audit findings, and
verification state. Phase execution details live in the active phase document.

Last updated: 2026-09-06. Active phase: **8.1 — GitHub delivery & lifecycle**.

## Program status

| Phase | State | Exit dependency |
|---|---|---|
| 8 — Audit & configuration integrity | complete | Lossless config + decoded QR + real handshake/traffic evidence |
| 8.1 — GitHub delivery & lifecycle | active; M1–M4 reviewed, M5 implemented under review | One-command installation and safe lifecycle verified on the dedicated VPS |
| 9 — Operational observability | planned; existing design branch paused | Useful live metrics/logs with bounded cost and retention |
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
| GitHub bootstrap, release/commit selection, terminal installer/manager | 8.1 | Checksummed acquisition, exact source identity, width/locale QA, real installation |
| Prerequisites, compatible AWG bundle selection, domain/IP and TLS setup | 8.1; broad matrix in 11 | Explicit supported combinations; real kernel/tools and certificate evidence |
| CLI backup/Telegram schedules and transactional lifecycle | 8.1; certification repeated in 11 | Failure injection, bounded restore, real update/rollback/backup/restore |
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
| Release artifacts and publication workflow | Acquisition contract/dry-run artifacts in 8.1; final workflow/freeze in 12 | Checksums/multi-arch metadata verified; publication remains manually gated |

## Release blockers

| ID | Blocker | Owner | State | Evidence required to close |
|---|---|---|---|---|
| RB-001 | QR images do not reliably render/scan on panel and subscription surfaces | Phase 8 | verified | Direct/REST/admin/subscription PNGs independently decode to exact config bytes; real desktop/mobile fa/en light/dark presentation passed; those decoded bytes imported into real clients. Physical optical camera unavailable and explicitly unperformed. |
| RB-002 | Generated client configuration has unverified/lossy parameter paths | Phase 8 | verified | Canonical typed paths and three delivery surfaces are unit tested; recommended and randomized decoded configs passed real kernel client handshake and bidirectional traffic. |
| RB-003 | H1–H4 ranges are reduced to scalar integers in current models | Phase 8 | verified | Storage/apply/dump/drift/API/forms/config/QR/backup paths preserve both bounds; userspace integration and exact kernel runtime/client equality passed. |
| RB-004 | Complete pinned-version parameter/client compatibility is not classified | Phase 8 | verified | Pinned source/runtime matrix is frozen; supported generated subsets passed real kernel clients, and the recommended subset passed the exact pinned userspace daemon. Unsupported/client-specific fields remain gated. |
| RB-005 | Operational troubleshooting and log retention are incomplete | Phase 9 | planned | Unified log workflow and bounded retention verified in both modes |
| RB-006 | Existing UI is not the requested complete design and QA baseline | Phase 10 | planned | Full route/state/browser matrix completed |
| RB-007 | Production compatibility and hardening matrix is incomplete | Phase 11 | planned | Supported cells and recovery/performance evidence recorded |
| RB-008 | Versioned checksummed artifacts and official multi-arch workflow are absent | Phase 12 | planned | Clean candidate pipeline dry run and artifact install verification |
| RB-009 | Installation lacks GitHub acquisition and a complete, reliably recoverable terminal lifecycle | Phase 8.1 | in progress | Source/version integrity; usable CLI; prerequisites; real Docker/native install and recovery evidence |

No release blocker may be silently downgraded. A blocker can close only with linked evidence or
be explicitly waived by the project owner with the residual risk recorded.

Phase 8 completion (2026-09-05): the exact commit-stamped candidate passed the isolated Ubuntu
24.04 kernel/userspace gate, three-surface QR/config equality, real client traffic, browser QA,
actual-secret diagnostics scanning, and cleanup. Sanitized evidence:
[`../integrations/fixtures/verify-phase8-vps-2026-09-05.txt`](../integrations/fixtures/verify-phase8-vps-2026-09-05.txt).

## Audit findings

Severity: critical (secret loss/exposure or unusable release), high (major correctness/security),
medium (material product/operations weakness), low (polish/maintainability). State values are
`open`, `in progress`, `verified`, or `deferred`.

| ID | Severity | Finding | Owner | State |
|---|---|---|---|---|
| AUD-001 | critical | QR raster path has no decode/content-equivalence test; current regression can pass an unusable image | Phase 8 | verified |
| AUD-002 | critical | H1–H4 are stored as `uint32`; observed `low-high` values lose the upper bound during dump parsing | Phase 8 | verified |
| AUD-003 | high | OpenAPI exposes only a subset of current AWG profile fields and models H1–H4 as integers | Phase 8 | verified |
| AUD-004 | high | Random profile generation is split between browser and server paths, weakening canonical validation | Phase 8 | verified |
| AUD-005 | high | No single CLI workflow aggregates operational logs across deployment modes | Phase 9 | planned |
| AUD-006 | high | Application/deployment log retention is not documented or enforced as one bounded policy | Phase 9 | planned |
| AUD-007 | medium | Human-facing token scopes, admin permissions, and webhook events expose machine identifiers | Phase 10 | planned |
| AUD-008 | low | `project-structure.md` said Go 1.22 while `go.mod`, workflow, and CI require 1.25 | Planning update | verified |
| AUD-009 | high | Fixed preset headers and equality-only validation violate recommended/non-overlapping H semantics | Phase 8 | verified |
| AUD-010 | high | Interface form numeric parse errors can silently become valid zero values | Phase 8 | verified |
| AUD-011 | medium | API JSON decoding accepts unknown fields and trailing values, hiding configuration typos | Phase 8 | verified |
| AUD-012 | high | Backup restore may allocate up to 4 GiB per allowlisted member instead of enforcing the product memory budget | Phase 8.1; recertify in 11 | M5 streaming/member/total bounds and unsafe archive regressions pass in `281b607`; independent review pending |
| AUD-013 | medium | CLI `settings set ... -stdin` reads without a size bound | Phase 8.1 | M5 4096-byte stdin bound and secret-argv refusal regressions pass in `281b607`; independent review pending |
| AUD-014 | medium | Direct-TLS HSTS and reverse-proxy ownership are not defined or tested | Phase 11 | planned |
| AUD-015 | low | Third-party inventory still labels implemented age encryption as planned | Phase 12 | planned |
| AUD-016 | high | A successful kernel `setconf` had been treated as `AdvancedSecurity` support even though the pinned setter ignores it, userspace rejects it, ordinary dump cannot observe it, and kernel `showconf` synthesizes a phantom peer line | Phase 8 | verified |
| AUD-017 | high | Client rendering placed AWG interface fields after `[Peer]`, ignored the selected interface MTU, silently omitted corrupt keepalive, and a REST test could print raw key-bearing configs | Phase 8 | verified |
| AUD-018 | medium | Restore environment review queried a nonexistent `interfaces` table, silently omitting every staged tunnel interface from the operator report | Phase 8 | verified |
| AUD-019 | high | `backend_mode="userspace"` is stored/reported but boot and reconciliation ignore it; no userspace daemon lifecycle implements the advertised fallback | Phase 11 | planned |
| AUD-020 | medium | `awg show interfaces` space-separated multiple names were parsed as one combined name, producing false runtime/reconciliation state | Phase 8 | verified |
| AUD-021 | high | Endpoint overrides and I1–I5 could contain line breaks/control text and inject extra directives into exported client configurations | Phase 8 | verified |
| AUD-022 | high | Reconciliation neither loaded nor compared I1–I5, so configured signature packets were omitted from apply and their drift was invisible | Phase 8 | verified |
| AUD-023 | medium | The panel trusted a hidden generated-profile label without proving values came from its preview; policy validation was broader than generation and S2 used an unbounded retry | Phase 8 | verified |
| AUD-024 | medium | The first real-host harness draft could delete pre-existing resources after partial setup and compared only config shape, not exact config/API state | Phase 8 | verified |
| AUD-025 | critical | Peer-only `awg syncconf` clears the live interface private key on the pinned kernel backend, preventing all client handshakes | Phase 8 | verified |
| AUD-026 | high | Docker update treats failed pulls as success candidates and lacks automatic recovery on compose-up failure; native restart failure also bypasses rollback | Phase 8.1 | M3 implemented and fault-tested in `4b72243`; independent review closed after `fc2c537`; actual deployment drills pending M6 |
| AUD-027 | high | Installer assumes prerequisites; native installation never ensures AWG tools/module, and SkipModule is not consumed | Phase 8.1 | M2 implemented, unit tested and independently reviewed; fresh prerequisite/runtime deployment verification remains M6 |
| AUD-028 | medium | IP-only summary advertises a server URL although listener is loopback; explicit TLS port 8080 is overwritten by defaults | Phase 8.1 | M2 fixed, unit tested and independently reviewed; integrated real-host setup remains M6 |
| AUD-029 | medium | Backup schedule CLI can panic on missing flag values; installer rejects negative Telegram group IDs | Phase 8.1 | M2 installer parsing and M5 missing-flag/signed-chat/interval validation regressions pass; M5 independent review pending |
| AUD-030 | high | Uninstall trusts unchecked state paths and continues removal after service-stop errors, risking deletion while the node is running | Phase 8.1 | M3 state/path, stop-failure and absent-unit retry regressions pass; independent review closed; actual removal/recovery drill remains M6 |
| AUD-031 | high | Telegram delivery wraps HTTP transport errors containing the token-bearing request URL and echoes remote descriptions without token redaction | Phase 8.1 | M5 actual net/http URL-error/token-echo refusal regressions pass in `281b607`; independent review pending |
| AUD-032 | high | Restore verification metadata is optional at apply; database/key replacement is not recovered as a pair on failure and boot may continue after partial apply | Phase 8.1 | M5 private preview/approval, complete hashes, recoverable pair and fail-closed boot regressions pass in `281b607`; review and real-host recovery pending |
| AUD-033 | high | Fresh public installation starts before an owner exists; anonymous onboarding can claim the node, and owner creation uses non-atomic count-then-insert | Phase 8.1 for installer/atomic creation; manual-deployment posture reviewed in 11 | M4 implemented and unit/PTY tested in `e21a87f`/`4c8f07e`; independent task review closed, real deployment gate pending M6 |
| AUD-034 | high | Backup creation ignores stored-password read/decryption errors and may silently create a plaintext archive instead of the intended encrypted backup | Phase 8.1 M5 | Fail-closed secret loading with intentionally unset plaintext preserved; error-path regression passes in `281b607`, review pending |
| AUD-035 | high | Pinned age reader defaults to accepting scrypt factor22 (~4 GiB transient memory), although WG-Guard writes factor18 (~256 MiB); an archive can request disproportionate KDF work before extraction limits apply | Phase 8.1 M5; resource certification in 11 | Pinned age v1.2.1 source verified; factor18 acceptance cap and specific early-refusal regression pass in `281b607`, review pending |
| AUD-036 | medium | Two backups created in one second reuse the same archive name and can overwrite the previous archive | Phase 8.1 M5 | Nonce filenames and actual same-second preservation regression pass in `281b607`; independent review pending |

Detailed evidence and reviewed no-finding areas are in [phase8-audit.md](phase8-audit.md).
Add only evidence-backed findings. Do not use this table as an idea backlog.

## Compatibility certification

The Phase 11 matrix starts from the honest state below. “Planned” is not support evidence.

| OS | Arch | Docker | Native | Kernel backend | Userspace fallback | State |
|---|---|---|---|---|---|---|
| Ubuntu 24.04 | amd64 | drill verified | drill verified | config/client traffic verified | manual-daemon config/client traffic verified; product lifecycle planned | partial |
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

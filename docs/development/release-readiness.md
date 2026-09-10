# Release-readiness program

Living tracker for the approved Phase 8–12 program. `ROADMAP.md` owns phase order and gates;
this document owns cross-phase requirement coverage, release blockers, audit findings, and
verification state. Phase execution details live in the corresponding phase document.

Last updated: 2026-09-10. Phases 8, 8.1, 8.2 and **9 — Operational observability** are complete.
Corrective **Phase 8.3 — Data-plane forwarding integrity** is complete. Phase 10 is active again
at milestone 10.0; no visual migration is yet claimed.

## Program status

| Phase | State | Exit dependency |
|---|---|---|
| 8 — Audit & configuration integrity | complete | Lossless config + decoded QR + real handshake/traffic evidence |
| 8.1 — GitHub delivery & lifecycle | complete | One-command installation and safe lifecycle verified on the dedicated VPS |
| 8.2 — Secure access & persistent manager | complete | Offline local retry, update-aware independent manager/Update Center, honest secure exposure, certificate renewal and proxy rollback verified |
| 9 — Operational observability | complete | Useful live metrics/logs with bounded cost and retention |
| 8.3 — Data-plane forwarding integrity | complete | Effective Docker forwarding plus public DNS/HTTPS egress on Ubuntu 24.04 amd64 |
| 10 — Product UI/UX redesign | active; milestone 10.0 | Every route/state passes complete bilingual responsive QA |
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
| GitHub bootstrap, release/commit selection, terminal installer/manager | 8.1 | Checksummed acquisition, exact source identity, English/narrow-width QA, real installation |
| Prerequisites and compatible AWG bundle selection | 8.1; supported-Ubuntu matrix in 11 | Explicit supported combinations and real kernel/tools evidence |
| Persistent manager, domain/IP TLS, DNS-01 and reverse-proxy coexistence | 8.2; certification repeated in 11 | Zero-network retry, protected DNS credential, certificate identity/renewal, rollback and real VPS evidence |
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
| Supported-Ubuntu/backend/deployment matrix | 11 | Real-host evidence per supported cell; unverified later Ubuntu releases not advertised |
| Backup/update/rollback/recovery/ACME drills | 11 | Repeatable evidence using the feature-frozen candidate |
| API/OpenAPI synchronization | Every affected phase; 12 final | Bidirectional route/schema coverage green |
| Documentation and repository hygiene | Every phase; 12 final | Living docs agree; no secrets or inappropriate artifacts tracked |
| Release artifacts and publication workflow | Acquisition contract/dry-run artifacts in 8.1; final workflow/freeze in 12 | Checksums and amd64 metadata verified; publication remains manually gated |

## Release blockers

| ID | Blocker | Owner | State | Evidence required to close |
|---|---|---|---|---|
| RB-001 | QR images do not reliably render/scan on panel and subscription surfaces | Phase 8 | verified | Direct/REST/admin/subscription PNGs independently decode to exact config bytes; real desktop/mobile fa/en light/dark presentation passed; those decoded bytes imported into real clients. Physical optical camera unavailable and explicitly unperformed. |
| RB-002 | Generated client configuration has unverified/lossy parameter paths | Phase 8 | verified | Canonical typed paths and three delivery surfaces are unit tested; recommended and randomized decoded configs passed real kernel client handshake and bidirectional traffic. |
| RB-003 | H1–H4 ranges are reduced to scalar integers in current models | Phase 8 | verified | Storage/apply/dump/drift/API/forms/config/QR/backup paths preserve both bounds; userspace integration and exact kernel runtime/client equality passed. |
| RB-004 | Complete pinned-version parameter/client compatibility is not classified | Phase 8 | verified | Pinned source/runtime matrix is frozen; supported generated subsets passed real kernel clients, and the recommended subset passed the exact pinned userspace daemon. Unsupported/client-specific fields remain gated. |
| RB-005 | Operational troubleshooting and log retention are incomplete | Phase 9 | verified | Unified log workflow, bounded retention, real traffic/load, failure recovery and secret scans passed in both modes; [evidence](../integrations/fixtures/verify-phase9-vps-2026-09-10.txt) |
| RB-006 | Existing UI is not the requested complete design and QA baseline | Phase 10 | planned | Full route/state/browser matrix completed |
| RB-007 | Production compatibility and hardening matrix is incomplete | Phase 11 | planned | Supported cells and recovery/performance evidence recorded |
| RB-008 | Versioned checksummed amd64 artifacts and official publication workflow are absent | Phase 12 | planned | Clean candidate pipeline dry run and artifact install verification |
| RB-009 | Installation lacks GitHub acquisition and a complete, reliably recoverable terminal lifecycle | Phase 8.1 | verified | Source/version integrity, terminal QA, Telegram/scheduler, and real Docker/native install/update/rollback/restore/recovery evidence are linked from [phase8.1.md](phase8.1.md) |
| RB-010 | Busy public ports, IP-only HTTPS and post-install TLS changes lack one safe installer-owned workflow | Phase 8.2 | verified | Cached manager/zero-network retry and public-HTTP refusal pass automated gates; real Docker Nginx/webroot and short-lived IP issuance/renewal, occupied-port refusal, rollback-safe transitions and cleanup are recorded in [Phase 8.2 evidence](../integrations/fixtures/verify-phase8.2-vps-2026-09-09.txt). Cloudflare DNS-01 is automated-test verified; real issuance awaits a scoped token and is not claimed |
| RB-011 | Docker's earlier `FORWARD` DROP can allow AWG handshake while blocking all routed client traffic | Phase 8.3 | verified | Scoped `DOCKER-USER` coexistence, fail-closed readiness/doctor diagnostics, and exact data-plane candidate plain/recommended/randomized public-IP/DNS/HTTPS traffic with cleanup [evidence](../integrations/fixtures/verify-phase8.3-vps-2026-09-10.txt) |

No release blocker may be silently downgraded. A blocker can close only with linked evidence or
be explicitly waived by the project owner with the residual risk recorded.

Phase 8 completion (2026-09-05): the exact commit-stamped candidate passed the isolated Ubuntu
24.04 kernel/userspace gate, three-surface QR/config equality, real client traffic, browser QA,
actual-secret diagnostics scanning, and cleanup. Sanitized evidence:
[`../integrations/fixtures/verify-phase8-vps-2026-09-05.txt`](../integrations/fixtures/verify-phase8-vps-2026-09-05.txt).

Phase 8.2 completion (2026-09-09): the exact candidate passed the Ubuntu 24.04.4 amd64 Docker
secure-exposure gate, including real domain and public-IP certificates, renewal hook, Nginx
coexistence, state migration, narrow terminal QA and original-node restoration. Sanitized evidence:
[`../integrations/fixtures/verify-phase8.2-vps-2026-09-09.txt`](../integrations/fixtures/verify-phase8.2-vps-2026-09-09.txt).
The same record contains the corrective exact-source fresh-install drill: the vanished PPA pin,
APT lock contention and stale Docker socket/retry ownership defects are closed without changing
REST/OpenAPI.

Phase 9 completion (2026-09-10): one bounded sampler fed the dashboard, metrics and the
`stats.read` REST contract; central redaction, unified host logs and deployment-native retention
passed real Native/Docker failure, traffic, resource and cleanup drills on Ubuntu 24.04.4 amd64.
Sanitized evidence:
[`../integrations/fixtures/verify-phase9-vps-2026-09-10.txt`](../integrations/fixtures/verify-phase9-vps-2026-09-10.txt).

Phase 8.3 corrective completion (2026-09-10): runtime mutations now reconcile the complete
network state and Docker's terminal `FORWARD DROP` is crossed only through its scoped user-policy
extension point. The exact Ubuntu 24.04.4 amd64 Docker candidate passed post-start plain,
recommended and randomized AWG clients through public IPv4, DNS and HTTPS, then restart,
diagnostics and owned-rule cleanup while the global DROP remained unchanged. Sanitized evidence:
[`../integrations/fixtures/verify-phase8.3-vps-2026-09-10.txt`](../integrations/fixtures/verify-phase8.3-vps-2026-09-10.txt).

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
| AUD-005 | high | No single CLI workflow aggregates operational logs across deployment modes | Phase 9 | verified: bounded Docker/native service and operation sources, component filtering and follow cancellation passed the real VPS gate; Docker stderr unification regression closed before final acceptance |
| AUD-006 | high | Application/deployment log retention is not documented or enforced as one bounded policy | Phase 9 | verified: Docker 16 MiB × 8 compressed local rotation, scoped native 7-day/size policy, operation journal 7-day/8 MiB and real tmpfiles expiry passed on the VPS; Docker physical age deletion remains an explicit platform limitation |
| AUD-007 | medium | Human-facing token scopes, admin permissions, and webhook events expose machine identifiers | Phase 10 | planned |
| AUD-008 | low | `project-structure.md` said Go 1.22 while `go.mod`, workflow, and CI require 1.25 | Planning update | verified |
| AUD-009 | high | Fixed preset headers and equality-only validation violate recommended/non-overlapping H semantics | Phase 8 | verified |
| AUD-010 | high | Interface form numeric parse errors can silently become valid zero values | Phase 8 | verified |
| AUD-011 | medium | API JSON decoding accepts unknown fields and trailing values, hiding configuration typos | Phase 8 | verified |
| AUD-012 | high | Backup restore may allocate up to 4 GiB per allowlisted member instead of enforcing the product memory budget | Phase 8.1; recertify in 11 | M5 streaming/member/total bounds and unsafe archive regressions pass in `281b607`; independent review closed |
| AUD-013 | medium | CLI `settings set ... -stdin` reads without a size bound | Phase 8.1 | M5 4096-byte stdin bound and secret-argv refusal regressions pass in `281b607`; independent review closed |
| AUD-014 | medium | Direct-TLS HSTS and reverse-proxy ownership are not defined or tested | Phase 8.2 ownership/header implementation; recertify in 11 | verified; HSTS is emitted only for proven HTTPS, proxy metadata is trusted only from private/loopback peers, and real direct/Nginx HTTPS returned one canonical header |
| AUD-015 | low | Third-party inventory still labels implemented age encryption as planned | Phase 8.1 inventory correction; full distribution review in 12 | Active age/ACME and terminal/system pins/imports/license files checked; Go/runtime-image distinction corrected. Complete transitive/frontend notices and release-source obligations remain Phase 12 |
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
| AUD-026 | high | Docker update treats failed pulls as success candidates and lacks automatic recovery on compose-up failure; native restart failure also bypasses rollback | Phase 8.1 | M3 implemented and fault-tested in `4b72243`, review closed after `fc2c537`; Docker/native rollback and failed-start recovery passed on the dedicated VPS |
| AUD-027 | high | Installer assumes prerequisites; native installation never ensures AWG tools/module, and SkipModule is not consumed | Phase 8.1 + 8.2 correction | exact source-backed tools/kernel/DKMS/runtime readiness passed on Ubuntu 24.04 amd64; unsupported OS/architecture combinations fail early. Later supported Ubuntu amd64 source-build certification remains Phase 11 |
| AUD-028 | medium | IP-only summary advertises a server URL although listener is loopback; explicit TLS port 8080 is overwritten by defaults | Phase 8.1 | M2 fixed, unit tested and reviewed; terminal and real domain/ACME deployment gates passed |
| AUD-029 | medium | Backup schedule CLI can panic on missing flag values; installer rejects negative Telegram group IDs | Phase 8.1 | M2 installer parsing and M5 missing-flag/signed-chat/interval validation regressions pass; M5 independent review closed |
| AUD-030 | high | Uninstall trusts unchecked state paths and continues removal after service-stop errors, risking deletion while the node is running | Phase 8.1 | M3 state/path, stop-failure and absent-unit retry regressions pass; review closed; safe uninstall and node recovery passed in both VPS deployment modes |
| AUD-031 | high | Telegram delivery wraps HTTP transport errors containing the token-bearing request URL and echoes remote descriptions without token redaction | Phase 8.1 | M5 actual net/http URL-error/token-echo refusal regressions pass in `281b607`; independent review closed |
| AUD-032 | high | Restore verification metadata is optional at apply; database/key replacement is not recovered as a pair on failure and boot may continue after partial apply | Phase 8.1 | M5 private preview/approval, complete hashes, recoverable pair and fail-closed boot regressions pass in `281b607`; review closed and coordinated real Docker/native recovery passed |
| AUD-033 | high | Fresh public installation starts before an owner exists; anonymous onboarding can claim the node, and owner creation uses non-atomic count-then-insert | Phase 8.1 for installer/atomic creation; manual-deployment posture reviewed in 11 | M4 implemented and unit/PTY tested in `e21a87f`/`4c8f07e`; review closed and owner-before-start passed in both real deployment modes |
| AUD-034 | high | Backup creation ignores stored-password read/decryption errors and may silently create a plaintext archive instead of the intended encrypted backup | Phase 8.1 M5 | Fail-closed secret loading with intentionally unset plaintext preserved; error-path regression passes in `281b607`, review closed |
| AUD-035 | high | Pinned age reader defaults to accepting scrypt factor22 (~4 GiB transient memory), although WG-Guard writes factor18 (~256 MiB); an archive can request disproportionate KDF work before extraction limits apply | Phase 8.1 M5; resource certification in 11 | Pinned age v1.2.1 source verified; factor18 acceptance cap and specific early-refusal regression pass in `281b607`, review closed |
| AUD-036 | medium | Two backups created in one second reuse the same archive name and can overwrite the previous archive | Phase 8.1 M5 | Nonce filenames and actual same-second preservation regression pass in `281b607`; independent review closed |
| AUD-037 | medium | Legacy `secrets rotate --config` indexes a missing value and panics before configuration/database access | Phase 11 legacy CLI hardening; final installer review triage | Reproduced safely on the exact `53f55e2` Linux candidate with no node data opened; require bounded standard flag parsing and malformed-argument regressions. No implementation fix claimed |
| AUD-038 | high | An already admitted native CLI retains its database/key across managed restore; resuming paused key rotation can make restored encrypted settings unreadable | Phase 8.1 final correction | Reproduced on `14d4a19`; persistent lifetime ownership, safe admission/initialization/shutdown and split-layout refusal implemented in `0578dcc`. Automated/review gates and real Docker lease/restore exclusion passed |
| AUD-039 | medium | Core `recovery-required` remains blocked after a transient unknown module identity is repaired; the suggested update recovery also refuses core operations | Phase 8.1 final correction; Phase 8.2 update-center maintenance | Operation-specific retry, fresh observation and interrupted/state-write regressions pass. Recorded legacy-package to reviewed-source native transition is now automated-test covered; unknown/unowned combinations still fail closed and no real cross-bundle transition is claimed. The dedicated VPS reaffirmed the current source bundle as matches-disk |
| AUD-040 | medium | Explicit destructive `uninstall --purge-data` removes the data directory without excluding independent admitted data commands | Phase 11 destructive maintenance certification | Existing `uninstall.go` whole-directory removal is outside the final ownership correction; default data-preserving uninstall is distinct. Require all data commands stopped and no concurrent admission before explicit purge; concurrent purge safety is not certified |
| AUD-041 | high | Real Go-based GitHub source acquisition rejects codeload's standard PAX global metadata as an unsafe filesystem path | Phase 8.1 M6 acceptance correction | Fixed in `d30894a`: one exact commit-bound global metadata record is consumed without materialization while archive safety bounds remain enforced. Focused tests, exact-revision CI and real source install/update through the corrected extractor passed |
| AUD-042 | high | A first interactive bootstrap keeps its verified build only in a temporary directory and immediately enters setup; cancellation/failure requires another acquisition instead of reopening a durable local manager | Phase 8.2 | verified; the checked build and bounded receipt are atomically persisted before the menu and failed/canceled setup resumes locally. The offline daily command makes no acquisition; the GitHub entry now performs a bounded identity check |
| AUD-043 | high | Domain ACME currently fails whenever 80/443 are owned by another service, and no transactional standard-Nginx/shared-webroot or DNS-01 route exists | Phase 8.2 | verified; standard-Nginx/webroot passed real issuance and no-mutation conflict refusal; DNS-01 uses the protected official plugin path and passes automated secret/command tests, with real issuance unclaimed without a scoped token |
| AUD-044 | high | Trusted public-IP certificates are now available but the installer exposes only private SSH/manual files; short-lived renewal and reload are absent | Phase 8.2 | verified; staging and production Certbot short-lived IP certificates, SAN/expiry diagnostics, automatic timer, deploy-hook Docker reload and trusted HTTPS passed on the dedicated VPS |
| AUD-045 | high | Fresh installation depends on a retired PPA core package and retries can fail on Ubuntu maintenance locks or a stale Docker socket while losing package ownership | Phase 8.2 corrective hardening | verified; recommended exact GitHub-source tools/kernel bundle, APT lock wait, safe aborted-state ownership carry, Docker service/socket lifecycle regressions and Ubuntu 24.04.4 Docker install/purge acceptance passed |
| AUD-046 | high | The installed-node bootstrap fast path opens `/usr/local/bin/wg-guard` before resolving `--commit main`, so a repeated one-line command can keep presenting an old manager after GitHub advances | Phase 8.2 maintenance | verified; simulated `main` advancement proves one build then metadata-only reuse, while real exact-commit Docker acceptance proved independent manager advancement with an unchanged service hash, heartbeat, current-build skip and full update backup/health/core closure. Invalid selection and strict refresh fail; only transport/compiler failure can use a verified cache. Published-release update remains unclaimed because no stable release exists |
| AUD-047 | high | An interrupted uninstall is presented as generic recovery, reads an already removed boot config, and dispatches `update --recover`, trapping the operator in a recovery loop | Phase 8.2 maintenance | fixed from the user-provided transcript: uninstall has a dedicated config-independent view and resumes its own removal with explicit keep-data/full-reset choices. Focused regressions and a real Ubuntu 24.04.4 amd64 synthetic-journal Full reset passed; API/OpenAPI is unchanged |
| AUD-048 | high | Interactive install silently selects username `owner`; a short password is rejected only after deployment/data preparation, leaving an initial-install journal without a useful retry/reset path and causing apparent successful credentials to fail as `admin` | Phase 8.2 maintenance | verified: explicit `admin` default, shared backend validation with in-place retry, blank-password secure generation and post-completion show-once handoff, guided cleanup for every interrupted initial install, and distinct node-reset/complete-removal ownership pass automated gates. Real Docker confirmed default `admin`, short-password retry and complete removal; generated credentials were intentionally not captured |
| AUD-049 | high | Recommended private Docker setup records loopback HTTP as proxy TLS, making the session cookie `Secure`; valid credentials are accepted but the browser cannot return the cookie over the documented SSH-tunnel HTTP URL and falls back to login | Phase 8.2 maintenance | verified: reproduced on Ubuntu 24.04.4 amd64, then fixed by resolving private exposure to loopback-only dev transport while retaining proxy mode for real HTTPS proxies. Exact final-candidate Docker login finished at `/` with HTTP 200 and one session cookie |
| AUD-050 | high | Deleting a tunnel interface removes its DB ownership record before reconciliation, so the still-live kernel link is classified as foreign and may remain after the operator believes it was deleted | Phase 11 interface lifecycle hardening | open; reproduced during the Phase 9 VPS gate. Implement teardown-before-final-delete with failure recovery and API/web/backend regressions; the Phase 9 fixture removes only its collision-checked owned link during cleanup |
| AUD-051 | critical | Docker's iptables backend installs an earlier `FORWARD` policy DROP; WG-Guard's later nftables accept chain cannot override that terminal verdict, while prior real-host gates stopped at the tunnel gateway | Phase 8.3 | verified: scoped owned child chain and tagged `DOCKER-USER` jump preserve the global DROP; all supported generated profiles passed real public egress |
| AUD-052 | critical | API/web runtime reconciliation updated interfaces and peers but not the firewall/NAT rendered state, so an interface created after startup could handshake without routed traffic | Phase 8.3 | verified: one serialized runtime reconciler now reapplies tunnel, firewall/NAT, coexistence and shaping; regression and exact fresh-install public-egress gate pass |
| AUD-053 | high | Host-owned `doctor` used the host PATH for Docker AWG inspection; source-backed Docker installs therefore reported missing `awg` and falsely classified every enabled interface as absent even while client traffic worked; its direct offline fix path could not use container-only tools | Phase 8.3 maintenance | verified: Docker AWG probes/dumps use the runtime container, host system/network checks remain local, non-not-found errors cannot recommend recreation, and Docker fix uses managed restart/startup reconciliation; automated tests plus exact Ubuntu 24.04.4 amd64 healthy/unavailable/recovered and real missing-link repair acceptance pass |

Detailed evidence and reviewed no-finding areas are in [phase8-audit.md](phase8-audit.md).
Add only evidence-backed findings. Do not use this table as an idea backlog.

## Compatibility certification

The Phase 11 matrix starts from the honest state below. “Planned” is not support evidence.

| OS | Arch | Docker | Native | Kernel backend | Userspace fallback | State |
|---|---|---|---|---|---|---|
| Ubuntu 24.04 | amd64 | public egress + lifecycle drills verified | lifecycle drill verified; expanded firewall recertification planned | kernel config/client/public traffic verified | manual-daemon config/client traffic verified; product lifecycle planned | partial |
| Ubuntu >24.04 | amd64 | planned per release | planned per release | planned per release | planned | unverified |

Containers and emulation can validate packaging but do not upgrade a real-host Ubuntu release
or backend cell to verified. Non-Ubuntu systems and non-amd64 architectures are out of scope.

## Discovery workflow

1. Reproduce or cite concrete evidence; do not log or paste secrets/config contents.
2. Record impact, severity, affected surface, and likely owning phase.
3. Fix critical/high findings in the earliest dependency-safe phase.
4. Add a regression test before the fix and attach real-host evidence when required.
5. Update this tracker, the current phase document, status matrix, and behavior documentation in
   the same coherent change.

## Publication boundary

Phase 12 may build, checksum, install, upgrade, and inspect candidate artifacts and may prepare a
manual publication workflow. It must stop before any final public tag, release, or registry image
is published. Publication requires explicit project-owner approval.

# Product and verification status

**Backup workbench follow-up (2026-10-05; current main, unreleased):**

The [workbench follow-up](backup-workbench.md) adds non-destructive panel verification,
resumable bounded review reports, enabled-source-owner preflight, conditional cancellation,
cursor archive pages and separated native sections. Crypto/review/apply/purge claims and
shared flash navigation have failure/concurrency coverage. Fresh local Go/web/vet/build,
focused Linux race and Chromium/WebKit workbench matrices passed within the recorded
scope; exact final main CI is a separate gate. No REST/OpenAPI contract, archive schema,
new release or real-host acceptance is implied. Phase 20 remains required.

**Phase 19 source (2026-10-05; unreleased):** fresh-target install from validated
archive before managed listener start, source access/settings preservation and
shared CLI/panel operation receipts are implemented. Focused portability/failure/
terminal/queue/catalog checks, full ordinary suite, vet/build, Linux race and both
48-cell operational browser matrices passed within the [recorded source scope](phase19.md).
Exact source `e589b2a5fc1ad4d92afb689c1c5dfea45e91d862` passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37298179907), including
both full Linux race/load jobs, build, vulnerability and runtime-image checks.
The independently checked unpublished candidate is recorded in Phase 19. No owner-server/rebuild/real CA claim or new release
is made; latest stable remains v0.1.9.


**Phase 18 source (2026-10-05; unreleased):** independent panel/public origins,
SNI and resumed TLS admission, per-request role isolation, owner-authorized
certificate staging/host mailbox, CA renewal/retirement and snapshot recovery are
implemented on main. Focused source/local HTTPS tests and Chromium/WebKit fa/en
responsive/native-fallback checks, the full ordinary Go suite, focused Linux race,
vet/build and bootstrap/asset checks passed. Exact implementation
`c5da79abcda3839b7e01f7ef5b397e8f1d1bc429` passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37258161422), including
both full Linux race/load jobs, build, vulnerability and fake runtime-image checks.
The unpublished image and independently checked artifact are recorded in
[Phase 18](phase18.md); physical CA/forwarding/reboot/client acceptance remains
Phase 20. Latest stable v0.1.9 and the no-further-release instruction are unchanged.


This is the current capability matrix. A feature is not production-verified merely because it
builds, passes a unit test, or ran in WSL/container emulation. Detailed phase evidence remains in
the linked phase records; release blockers and audit findings live in
[release-readiness.md](release-readiness.md).

**Current gate (2026-10-05):** Phases 0–14 are complete within their documented scopes.
The [refactor program, Phases 15–20](refactor-program.md), is in progress:
Phase 15 engineering preparation is complete and v0.1.9 is public;
the [Phase 15 record](phase15.md) separates synthetic Linux evidence from host certification.
The owner authorized preparation publication followed by full Phase 16 source work without
another release. Owner-server update/export is a post-publication checkpoint before rebuild.
Phase 16 is complete within its documented source scope with exact CI passed.
[Phase 17](phase17.md) implements Docker-only source, schema-4 private host state, one runtime
recipe and verified offline image distribution. Its exact image/CI gate passed; real-host
acceptance remains separate. [Phase 18](phase18.md) now implements independent
domain/certificate management on main; physical acceptance remains Phase 20. The
[domain/TLS guide](../operations/domains-and-tls.md) separates unreleased main
implementation from published v0.1.9 and physical acceptance.
[Phase 19](phase19.md) adds the installer/operator source journey; Phase 20 remains planned.

The owner explicitly selected complete native production removal in Phase 17.
The [cleanup inventory](../architecture/docker-only-cleanup.md) covers lifecycle,
state/artifacts, flags, renderers, logs, tests and current docs. Host CLI, kernel/
DKMS, required systemd broker/renewal/retention and fake development remain.
Current main removes Native production execution; the published v0.1.9 preparation release
retains the prior deployment contract. No later release or registry publication is authorized.
The [Phase 16 record](phase16.md) describes the completed source refactor: shared node-data
sessions, bounded runtime application with desired/applied observations, sealed device-key
provisioning, centralized managed paths and separate deployment/diagnostic adapters. Full local
Go tests, vet/build and ordinary/race Linux load checks passed. Exact implementation
`8c11b87efa48bffe8afe3bed7823242aaac50f76` passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37229319774), including both
Linux race jobs, encrypted-archive load and build/vulnerability/bootstrap checks.
Account/API/renderer/archive/deployment contracts remain preserved, and no post-Phase-16 release
is authorized. Ordinary/race synthetic 512-device load remains within 15 s enforcement budget
and drains from 9 baseline goroutines to 3; these checks do not certify the owner's live host.
[v0.1.9](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.9) is the latest stable
preparation release. Its exact `dd034fe5aa1d2f327998bc88a6229756449077c2` source passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37221761826) and
[release workflow](https://github.com/Sir-Adnan/wg-guard/actions/runs/37222439063), including
both Linux race jobs, isolated encrypted-archive load gates, build/image identity, attestations
and downloaded artifact verification. All five public assets were independently downloaded;
four manifest checksums, binary/metadata/SBOM and annotated tag matched that SHA. Metadata
SHA-256: `d77c0b8ee31e28d140da4f4ba45c2463bd0e9872ac9d362347203969cd0f7c88`.
The owner's update/export checkpoint follows publication and still gates rebuilding the only
host; actual host/kernel/client/resource acceptance remains separate.
The earlier v0.1.8 exact `cf718a926f81202b7ed8060cac27bb5322dbba82` source passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37157707988) and the
[release workflow](https://github.com/Sir-Adnan/wg-guard/actions/runs/37158268158),
including both Go race jobs, checksummed amd64 assets, image/binary identity, attestations and
downloaded draft-asset verification. The v0.1.0 release had a successful
latest-release install on the dedicated Ubuntu 24.04 VPS.
The owner's panel spot-check complements the Phase 10 browser matrix and Phase 11 certification;
no untested host/browser cell is inferred from it.

**v0.1.8 publication (2026-10-04; owner-authorized):** The account/address maintenance and
presentation changes below are public. All five anonymous public assets were independently
downloaded; the four manifest checksums, binary/metadata/SBOM identities and annotated tag
matched the exact released SHA above. The public metadata SHA-256 is
`6713500f4eb5f6422e53616323104a5a3c8057e1cdc1c92b3a68d6d97fc71d41`.
Browser evidence is reused from the applicable cleanup/presentation implementation at
`99c7140` and final interface correction at `1e60dc4`; publication adds fresh exact-source
and artifact gates, not new real-host or physical-device acceptance.

**v0.1.8 account/address maintenance (2026-10-04):** Ordinary user deletion now cascades
permanently to devices/customer access/successors and frees IPs, retaining durable public-peer
removal intent. Profiles accept ordered overflow IPv4 pools, available defaults and observed-host
overlap checks; the API includes an advisory capacity snapshot. The panel cleanup workspace has
sealed state-checked account/history previews, explicit owner/date selection and manual SQLite
space maintenance. Existing pool addresses and soft-deleted records survive migration 0015.
`schema15-ipv4-pools-v1` blocks pool-unaware release selection/rollback over the upgraded data.
Full local Go tests, vet/build and focused deletion/allocation/migration/API checks passed.
Chromium and WebKit each checked 16 fa/en Light/Dark/320–1440px cleanup/interface cells with
accessibility and historical calendar interactions. The existing Chromium foundation regression
passed after the calendar extension. The final buffered-retired-device correction passed
focused accounting tests, and exact `b2ac3a1f56b4edc7632fe8913e5139deb34b90e5` source passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37145012644), including both
Linux race jobs, build and vulnerability checks. No new
real-host multi-pool forwarding, production compaction or physical-device claim is made, and
these changes ship in the published v0.1.8 asset.

**v0.1.8 presentation follow-up (2026-10-04):** Cleanup now combines eligible data kinds,
statuses and owners in searchable multi-selection menus with Select all and one sealed,
state-checked transaction. Account cascades are excluded from duplicate history counts.
The shared presentation layer adds question-mark guidance, top-layer help/selection popovers,
keyboard form tabs, restrained surfaces and an interface pool editor with read-only prepared
CIDR/capacity/conflict suggestions. Appearance offers Latin-default or Persian number display,
independent of language/preset, with owner defaults and personal overrides (migration 0016).
Technical/input/copy/API values remain canonical. The full local Go suite passed, with focused
follow-up checks and vet/build on the resulting source. Chromium and WebKit each passed 16
fa/en, light/dark, 320/390/768/1440px presentation cells and 16 cleanup-menu cells, including
accessibility, prepared CIDRs, help/tab interactions, number preference, search, Select all
and date menus. The shared Chromium foundation regression passed. Exact presentation source
`99c71403ee4fd186f105f4339af6c004b326aa12` passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37155855102), including both
Linux race jobs. The interface tab/card spacing correction then passed fresh web tests/build
and both 16-cell browser matrices, including card geometry, expanded packet parameters and
two no-JavaScript fallback cells per engine. Exact correction source
`1e60dc41eefdd12cf03551c210d30189165b86e3` passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37156961921); the frozen publication
source passed the separate gates linked above. No new physical-device or real-host acceptance
is claimed for these follow-ups.

**Migration preparation on main (2026-10-04; not yet published):** Archive creation,
restore preview and pre-replacement apply now check every known encrypted carrier
against the archived master key plus foreign-key integrity. Missing keys are allowed
only for data with no encrypted values; exports depending on an unarchived rotation
key are refused. The independent `backup verify` command needs no installed state,
Docker or AWG and reports safe stored-record/backend counts. Archive schema and
public account API remain unchanged. Local full Go tests passed, with applicable
unchanged-package cache reuse; final focused backup/CLI/web/catalog checks and
vet/build passed after the last bounded-inspection correction. Regression coverage
includes second-record corruption, PSKs, webhook text envelopes, settings, incorrect/
missing keys, rotation, broken references, old approved previews and fresh-layout
restore with unchanged config bytes, customer token and charged usage. Exact source
`00c41618b4384b2ac60d297123d265955a292204` passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37165992240), including
both Linux race jobs, build, bootstrap/fixture and vulnerability checks. No new
real-host or browser-engine result is claimed; publication remains separate.
The owner-selected
[Docker refactor target](../architecture/deployment-refactor.md) is a design plan:
the current installation layout and native runtime support have not changed yet.

**Phase 15 safety implementation on main (2026-10-04; not yet published):** Automatic
live openers now fail closed on broken/unknown migration history or failed required
pre-migration archives, preserving the original schema/key. Install/update/recovery
require local data/network readiness independently of liveness and certificate proof.
Two fixed coalescing workers move archive/delivery I/O outside the central scheduler;
cross-process archive/scheduled-pass claims, conditional schedule advancement and
retryable shutdown protect data ownership. Focused local migration, readiness,
contention, cancellation and shutdown tests passed. Final local `go test ./...`,
`go vet ./...` and `go build ./...` passed; unchanged packages reused applicable
Go cache. Exact safety source `39bfb0a4a5f27e4fc80bb27529cccd30d91cdaec` passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37200471956), including both
Linux race jobs, build, fixtures and vulnerability scan. No new host/client lag, browser or
release claim is made.
Production lag/resource measurement, broader restored semantics/export review and an
owner-reviewed off-host backup remain Phase 15 gates. Phases 16–20 cover modular responsibility
boundaries, verified images/deployment, two-domain TLS/SNI and route isolation,
operational UI/terminal flows and real-host acceptance. Directory movement is
conditional on ownership/recovery benefit; existing config/node data paths are
retained in the revised plan. These later changes are planned, not implemented by
the safety changes above, and no new release is implied.

**Phase 15 secret-storage follow-up (2026-10-04; not yet published):** Startup sampling,
full archive inspection and node rotation now share the concrete encrypted-field inventory.
The old rotation omitted customer-link/webhook fields; the corrected bounded sweep includes
them and optional PSKs, verifies all values with the current key and checkpoints SQLite before
dropping the predecessor. Interrupted retries reuse the retained current/previous pair;
invalid predecessor keys are refused. Private atomic key writes are fsynced on Linux.
Verification cancellation/timeouts report incomplete and cannot publish restore previews.
Focused local tests passed all encrypted fields, 129-device paging, mixed-key interruption,
foreign/oversized/invalid keys and portable post-rotation archives. Actual accounting/expiry
and fake peer removal completed in 1 ms while two simulated worker operations stalled, within
a 15 s cadence budget; this does not measure actual KDF/I/O or real-host enforcement.
Final local full Go tests, vet/build and focused affected-package checks passed; applicable
unchanged-package results reused Go cache. Exact follow-up source
`7c9d1908dedfb04dec55239b3a5db44d67f0a558` passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37202081662), including both
Linux race jobs, build, fixtures and vulnerability scan. API/OpenAPI
and archive schema remain unchanged. The [owner preparation drill](../operations/migration-preparation.md)
reviews export/password recovery and memory/disk limits. Restored domain/core semantics,
actual host lag/resource measurement and the owner's off-host backup still gate the rest
of Phase 15.

**Phase 15 domain-data admission (2026-10-04; not yet published):** Immutable archive
inspection now checks known contiguous schema history, matching canonical interface/device
keys and PSKs, supported AWG parameter/profile values, disjoint pools and assignable device
IPs, subscription/accounting/lifecycle/date values, customer token/hash consistency and current
setting definitions. Creation, preview, standalone verification and pre-replacement apply share
this gate; original-schema recovery reads version-aware fields without changing its bytes.
Expired/disabled/legacy-deleted and legitimate over-limit records plus unknown historical
settings remain portable. Fixed fa/en errors contain no raw rows or secret values.
Focused corruption, old-preview and legacy/current-schema checks plus final local full Go tests,
vet/build and format/diff inspection passed; unchanged packages reused applicable Go cache.
Exact source `bb60bfd06696fcb3355debc76a0982bd39b22a1f` passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37207442547), including both
Linux race jobs, build, bootstrap/synthetic fixtures and vulnerability scan.
This closes the documented stored-domain subset, not target
core/module/daemon provenance or real-host connectivity. Actual host lag/resource measurement
and a reviewed off-host owner backup still gate completion of Phase 15.

**v0.1.7 maintenance workspace (2026-10-03; owner-authorized and published):** Update Center now separates installed
component identity, release selection, readiness/prepared artifacts, scheduled windows, safe
stage/history reports and recovery. `update.read` is distinct from execution; queued new-format
accounts are reauthorized under shared data ownership. The generated Docker runtime includes
the pinned Go engine, and bridge/timer capabilities follow installed artifact contracts across
rollback. See [the implementation contract](../operations/update-center.md).
Fresh local delivery checks passed `go test ./...`, `go vet ./...` and `go build ./...`; unchanged
packages reused their applicable Go cache. Chromium and WebKit each passed 16 fa/en,
light/dark, 320/390/768/1440px cells with accessibility scanning and workflow interactions.
The existing browser foundation regression passed. Local Firefox launch was blocked by
`spawn UNKNOWN`; local race was unrun because CGO is disabled. Exact-revision CI and any
new public release remain separate. New timer/queue/generated-runtime paths have no new
real-host claim. Public metadata/checksums independently identify the released source above;
the metadata SHA-256 is `246d0e70b56558460256134c7462961c31aca25727a656a9278e6e9343d9410f`.
The implementation and final UI source at `0197f2d9c2e81594b5209e835e2284aabe8857e2` passed
[exact main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37127008947), including both
Linux race jobs. The preparation and v0.1.7 publication gates also passed at the exact released SHA.

**v0.1.6 backup/startup correction:** An owner Docker upgrade report exposed
a valid-webhook text envelope being misread as binary by startup/offline key validation. This
could falsely reject the master key, block pre-update backups and prevent service startup after
a webhook was added. The stored-webhook regression reproduces the old failure and passes with
the corrected text decoder; missing/wrong keys and malformed text remain denied. CLI backup and
guarded compatible-helper retry tests cover the upgrade path from an affected old Docker binary.
The owner's failed upgrade is incident evidence, not a successful real-host test of the fix.

**v0.1.5 installer update:** The host manager now separates service/component,
lifecycle and private installer logs, with a 200-line recent view and service/installer live
follow. Bootstrap and Go install/update paths show named stages and bounded elapsed-time progress;
pre-update backup and health waits are included. Local Go tests/vet and the offline bootstrap
fixture cover the change. No new Ubuntu VPS installation or live log-follow drill is claimed.

**v0.1.5 API provisioning follow-up:** Purchase requests now accept `device_count` (1–100,
default one), within the account/template cap. Account, independently keyed devices, customer
link and a result listing every device ID commit in one transaction. The count is part of the
idempotency fingerprint; service/API tests cover rollback, concurrent replay, result recovery,
owner/reseller policy and per-device config delivery. OpenAPI and `/docs` also clarify the user
form mapping, panel-only actions and field-specific PATCH semantics, with missing bulk fields
and stats response schemas added. New real-host or physical-device verification is not claimed.

**v0.1.4 template and automation follow-up:** Selecting a technical template on the panel,
REST user creation or bulk creation now copies its saved entitlement terms; edits change the
reference without resetting an existing subscription. The pre-installation catalog contract is
`/templates`, `template_id` and `templates.read/write`, with a matching fresh SQLite schema;
plan-named aliases were not shipped. Owner-scoped integrations can create an account, first
device and customer link atomically with explicit finite terms without cataloging each external
SKU. Reseller-bound purchases still require owner-assigned enabled templates. Quota top-up has a
90-day recoverable, non-secret before/after result; panel Add data raises allowance, and Reset
Usage remains independent. Local full Go/vet, contract/migration checks and focused Chromium and
WebKit template-form checks passed at 320/390/1440px in fa/en; main and release CI ran Linux race
tests. New real-host, physical-device and Firefox follow-up checks were not claimed.

**v0.1.1 follow-up (Phase 13):** The public subscription QR visibility and
mixed-direction usage defects are corrected; its desktop/phone composition and the ten-source
visual preset layer are implemented. Fresh/upgrade migration, preference/owner boundaries,
local font serving and the existing Go suite pass. Chromium and WebKit checked 80 preset × mode ×
locale × viewport representative cells each, plus public subscription/QR flows and the eight-cell
subscription composition set. Firefox could not launch in this Windows test environment, so its
new Phase 13 cells remain unverified. No Phase 13 VPS or physical-device result is claimed;
the prior Ubuntu production certification is not silently extended. Phase 14 integration work
shipped in v0.1.3 under the separate source/release gates below.

**v0.1.3 subscription delivery update:** Browser downloads now send `.conf` files as
attachments with a binary media type, while the REST API keeps its text response. Shared
filenames have a short, stable device-specific stem; Settings selects one of three responsive
public layouts for all existing links. The public theme menu is aligned and operable across
RTL/LTR and responsive widths, and shared button elevation is restrained. Focused Go and
Chromium/WebKit browser checks cover the changed routes, 320–1440px layouts, fa/en and Light/Dark.
Actual Android/iOS downloads and a fresh VPS deployment of this revision remain unverified.

**Phase 14 integration release:** The 14.0 idempotency replay correction shipped in v0.1.2. v0.1.3
adds owner-managed reseller accounts, live permission ceilings, owner-assigned plan access,
principal-bound API tokens, scoped customer/device reads and dedicated reseller panel routes.
The purchase API commits user, first device, customer link and a 90-day non-secret operation
result together; key replay and lookup survive token rotation within the principal. Separate
scopes read/rotate customer links; rotation replaces every device key. Owner/reseller service,
API and panel tests cover cross-principal denial, rollback, replay and old credentials.

The owner replaced the proposed four-policy renewal with independent Reset Usage and one queued
Next Plan per customer. Queued terms are frozen; the bounded scheduler activates at the first
time/quota boundary, resets charged usage in one transaction and records before/after state.
Manual blocks remain effective, and incompatible edits move the queue to review. Service/API,
fresh-migration and focused Chromium layout checks passed. Existing time-only
renewal remains unchanged.

Phase 14.4 adds typed webhook payload schemas and examples, tenant-scoped event fanout and worker
delivery, explicit owner opt-in for cross-reseller node-wide fanout (legacy destinations default
off), an owned reseller webhook panel/API, a public-HTTPS egress policy for reseller receivers,
and bounded non-secret delivery receipts. Local webhook/API/panel tests cover owner, node-operator
and reseller boundaries, cross-tenant poison rows, signature/retry behavior and receipt paging.
The applicable Go package tests and local legacy-row upgrade through migration 0014 passed.
The exact PR source passed [CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/36557270951);
the squashed main commit passed [main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/36557949421)
and the [v0.1.3 release gate](https://github.com/Sir-Adnan/wg-guard/actions/runs/36558677000).
The reseller webhook panel also passed focused Chromium checks at 320/390/1440 px in fa/en and
Light/Dark. Physical-device and new real-host behavior of v0.1.3 are not claimed.
Batch-by-ID and post-activation financial
reversal were not added because the evidenced workflows use cursor/operation/activation recovery
and no safe reversal contract has been approved.

| Capability | Implemented and automated | Real-host / browser evidence | Current limit |
|---|---|---|---|
| Go/SQLite foundation, auth, encrypted secrets, reconciliation | Unit, integration and race coverage | Exercised in Docker/native recovery on Ubuntu 24.04 | Only documented deployment modes certified |
| Pinned AmneziaWG kernel and managed userspace backends | Config/apply/dump/drift and lifecycle tests | Kernel/userspace client HTTPS, reboot and recovery on Ubuntu 24.04 | arm64 and uncatalogued upstream builds unsupported |
| Users, devices, templates, quota/expiry, accounting and speed shaping | Service/API/web tests; 1000-class tc/IFB integration | Live client traffic and 1000-class shaping on dedicated VPS | 1000 simultaneous handshakes untested |
| REST API, scoped tokens, webhooks and OpenAPI | Contract, permission, pagination and delivery tests, including Phase 14 tenant fanout/receipt and multi-device purchase tests | Exact v0.1.6 source passed main and release gates; earlier panel/API workflows served published OpenAPI | Direct purchase, quota top-up and multi-device provisioning have no new real-host claim |
| Complete bilingual panel and public subscription | Catalog parity, accessibility and browser suites | Chromium/Firefox/WebKit route/state/viewport matrix plus targeted real TLS workflows | Physical-device testing unavailable |
| Backup/restore, settings, administrators, audit and schedules | Atomic save, recovery, encryption and error-path tests | Real disk pressure, migration, rollback and backup drills | Long-interval ACME renewal unobserved |
| GitHub bootstrap, terminal manager, Docker/native lifecycle | Acquisition, integrity, rollback, interrupted-state tests | Fresh installs, update/rollback, reboot, data-preserving/full removal; public latest-release Docker install on Ubuntu 24.04 | Later Ubuntu and non-amd64 hosts unverified/unsupported |
| Firewall, forwarding and diagnostics | Owned-rule, fail-closed and Docker coexistence tests | Real Docker and native client public DNS/HTTPS; scoped Docker policy | Active firewalld refused; real-host UFW cell unverified |
| Metrics, charts and bounded logs | Telemetry, retention and redaction tests | Real client/load and browser presentation checks | Multi-day traffic soak unperformed |
| Security and resource certification | Full Linux race, 4.75M parser fuzz cases, reachable-vulnerability scan | 0-peer control sample: 33 MB RSS; 100/1000-user ten-minute windows: 41/52 MB average RSS and ≤0.04% CPU; recovery drills | Certification applies to listed Ubuntu 24.04 paths |

## Release-critical workflows

| Workflow | Verification level and lasting result |
|---|---|
| Profile creation and edit | Unit/integration tested across advanced values, default/generated presets, validation and clearing; supported generated subsets completed real kernel/client traffic. Uncatalogued parameters remain gated. |
| Client delivery | Direct, API, admin, public-subscription and QR outputs use the canonical config renderer; decoded QR bytes matched downloads and real clients connected. Physical optical camera testing was unavailable. |
| User and device changes | CRUD, limits, bulk actions, permissions, key rotation and failure recovery are automated-test covered; scoped real traffic and dashboard workflows passed. |
| Subscription access replacement | Automated atomic token/all-device key replacement and former-peer removal; the exact Phase 10 Docker gate proved the old URL returned 404, old peer disappeared and replacement access worked. |
| Settings and operator permissions | Submitted Settings saves are atomic with safe input redisplay; administrators, API tokens, webhooks, audit and backup permission states passed targeted tests and bilingual browser checks. |
| Backup and migration | Encrypted archive, preview, staged restore, missing-key refusal, data-preserving uninstall and full purge passed automated and exact Ubuntu 24.04 drills; a matching master key is required to recover encrypted device material. |
| Update and rollback | Source/release acquisition, independent manager refresh, exact component identity, interrupted update recovery and native/Docker rollback passed targeted and real-host tests; the public v0.1.0 bootstrap selected latest and installed the published asset on a real Docker host. |
| Responsive and accessible panel | Full inventoried route/state matrix passed Chromium, Firefox and WebKit across fa/en, light/dark and the documented viewport set; real physical-device testing was unavailable. |
| Host diagnostics | Doctor separates container AWG inspection from host network inspection, checks effective forwarding and fails closed on active firewalld; real healthy and repair cases passed on Ubuntu 24.04. |

Phase records: [configuration parity](phase8.md), [delivery](phase8.1.md),
[secure access](phase8.2.md), [forwarding](phase8.3.md),
[observability](phase9.md), [UI/UX](phase10.md), and
[production certification](phase11.md). The historical Phase 0–7 completion record remains
in [ROADMAP.md](../../ROADMAP.md), [phase6.md](phase6.md), and [phase7.md](phase7.md).

## Production compatibility

| Host | Docker | Native | Kernel backend | Userspace backend | Status |
|---|---|---|---|---|---|
| Ubuntu 24.04 LTS amd64 | Verified client, public HTTPS, restart, backup/restore and purge | Verified client, reboot, update/rollback, key rotation, TLS and purge | Verified client/public traffic and upgraded-kernel DKMS | Verified managed daemon and recovery in both modes | Supported for the listed paths; active firewalld excluded |
| Ubuntu newer than 24.04 amd64 | No genuine host | No genuine host | No genuine host | No genuine host | Unverified; no production claim |

Non-Ubuntu hosts and non-amd64 architectures are outside scope. WSL and container-only
results never upgrade a real-host cell. Phase 11's scoped gate, risks and resource evidence
are in [phase11.md](phase11.md); exact published-artifact evidence is in
[phase12.md](phase12.md).


Phase 17 exact implementation `5952d97bb07b656db4d931230a4f4c671be9d3e4` passed [main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37247158064) on 2026-10-05:
both Linux race matrices and encrypted-archive load, vet/build, vulnerability/bootstrap,
canonical image/binary/core identity and offline-load/fake-node persistence/hardening checks.
The unpublished seven-day Actions artifact was independently downloaded: ZIP digest and all
six file checksums passed; binary/runtime metadata identify that SHA. No new real-host/client,
public release or registry result is implied. Latest stable remains v0.1.9.

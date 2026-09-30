# Product and verification status

This is the current capability matrix. A feature is not production-verified merely because it
builds, passes a unit test, or ran in WSL/container emulation. Detailed phase evidence remains in
the linked phase records; release blockers and audit findings live in
[release-readiness.md](release-readiness.md).

**Current gate (2026-10-01):** Phases 0–14 are complete within their documented scopes.
[v0.1.5](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.5) is the latest stable
release. Its exact `05869315e840f8d2beb4225bf49ffacd31ee9943` source passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/36781442855) and the
[release workflow](https://github.com/Sir-Adnan/wg-guard/actions/runs/36782237324),
including both Go race jobs, checksummed amd64 assets, image/binary identity, attestations and
downloaded draft-asset verification. The v0.1.0 release had a successful
latest-release install on the dedicated Ubuntu 24.04 VPS.
The owner's panel spot-check complements the Phase 10 browser matrix and Phase 11 certification;
no untested host/browser cell is inferred from it.

**Post-v0.1.5 corrective source work (not yet released):** An owner Docker upgrade report exposed
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
| REST API, scoped tokens, webhooks and OpenAPI | Contract, permission, pagination and delivery tests, including Phase 14 tenant fanout/receipt and multi-device purchase tests | Exact v0.1.5 source passed main and release gates; earlier panel/API workflows served published OpenAPI | Direct purchase, quota top-up and multi-device provisioning have no new real-host claim |
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

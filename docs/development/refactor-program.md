# WG-Guard refactor program — Phases 15–20

Planning baseline: 2026-10-04. This program follows completed Phases 0–14 and the
critical architecture review. It is an execution plan, not evidence that its target
features are implemented. Current support remains in [status](status.md).

## Scope and decisions

Keep the Go/SQLite/HTMX modular monolith, AmneziaWG kernel default, explicit pinned
userspace, one node server process and one central scheduler. Improve correctness,
recovery, resource bounds and delivery before changing packaging or aesthetics.
Remote nodes, native WireGuard, Xray, sing-box and OpenVPN remain outside this program.
Keep their possible implementation boundaries clear without adding placeholder
tables, generic protocol blobs, remote agents or production dependencies.

Docker-only is the owner-confirmed production target. Removing native deployment
is a mandatory Phase 17 deliverable, not a future optional choice. Verification
gates govern safe delivery; current support is not removed in the preparation release.
The [cleanup specification](../architecture/docker-only-cleanup.md) identifies
all native branches/artifacts/flags/tests/docs and the host functions that remain.
Native is not intrinsically inferior; the decision reduces recurring deployment
duplication and acceptance cost. Host CLI recovery remains container-independent.

Use `/opt/wg-guard` for deployment assets when helpful. Retain `/etc/wg-guard`
configuration/TLS and `/var/lib/wg-guard` node data unless a demonstrated ownership
or recovery requirement justifies a move. Host lifecycle state/artifacts must be
outside writable node mounts. Centralized validated paths matter more than renaming
directories. Phase 17 now separates Compose under `/opt/wg-guard` and mutable private host authority under
`/var/lib/wg-guard-host`. Legacy state is explicitly refused; node configuration/data paths remain
stable. [ADR-0015](../decisions/ADR-0015-docker-only-runtime.md) explains ownership and the `.env` decision.

## Dependency and completion rules

| Phase | Purpose | State | Required predecessor |
|---|---|---|---|
| 15 | Operational safety and migration preparation | Engineering complete; v0.1.9 public | Existing release contracts |
| 16 | Application/runtime/host responsibility boundaries | Complete within documented source scope; no release | 15 safety gates |
| 17 | Verified Docker distribution and complete native removal | Source/image/CI gates passed; physical acceptance in 20; unreleased | 16 boundaries; registry approval for publication |
| 18 | Integrated panel/subscription domains and TLS | Source implemented; local/browser gates; physical acceptance in 20; unreleased | 16 host operation model; 17 deployment ownership |
| 19 | Cohesive installer and operational panel UX | Source implemented; focused/browser/race gates; unreleased | Implemented 15–18 services |
| 20 | Real-host acceptance and migration/release readiness | Planned | 15–19 evidence |

Design work may clarify a later phase; unrelated implementation does not silently
cross the active phase. Every phase records exact source/environment, fresh/reused
checks, failures and remaining limits. It ends with coherent commits and matching
living docs. Unit/fixture, browser, real-host and release evidence are separate.
No dates, performance promises, release numbers or completion percentages are inferred.

## Phase 15 — Operational safety and migration preparation

- [x] Inspect all archived encrypted carriers, foreign-key integrity and stored
  inventory; reject wrong/missing keys and unarchived rotation-key dependence.
- [x] Provide independent `backup verify`; recheck old approved previews before
  replacement; test fresh-layout restore preserving config bytes/token/charged usage.
  Implemented at `00c41618b4384b2ac60d297123d265955a292204`, with local checks and
  [exact main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37165992240).
  These changes are on main, not a new public release or real-host certification.
- [x] Correct pre-migration backup ordering/wiring. Distinguish empty initial setup
  from existing data. An existing-node migration must not continue after a failed
  required backup. No CLI/settings/data opener may silently migrate first and call
  its later backup "pre-migration".
  Shared `MigrateNode` now archives the original schema before live DDL/settings/key
  initialization; regression tests cover every opener, broken history, backup failure
  and reader contention. This is local source/test evidence, not a host upgrade drill.
- [x] Separate liveness, data/network readiness and certificate proof. Install,
  update and recovery must not commit based only on `/healthz` or a challenge redirect.
  Handle no-interface/private installations honestly; do not invent a client test.
  Lifecycle now requires an exact bounded local readiness response; responding/unready,
  malformed/oversized/redirected responses are tested. ACME loopback handling and fixed
  local TLS fallback retain certificate proof as a separate gate.
- [x] Keep scheduler callbacks short. Dispatch backup and slow webhook work to
  bounded in-process workers with finite queues, one concurrent archive, cancellation,
  duplicate suppression, durable retry/claims and deterministic shutdown.
  Accounting/expiry must not wait behind slow delivery or archive crypto.
  Two fixed workers each coalesce one pending signal. Data-volume claims serialize
  archives and scheduled due scans; cancellation/contention preserves due rows,
  concurrent schedule edits are protected and shutdown retains active DB/key ownership.
  Local regressions cover stalled work and cross-process admission. Crash retry remains
  at-least-once; the following production lag/resource measurement is still open.
- [x] Test actual accounting/expiry/peer removal while both worker operations are
  deliberately stalled, with an additional-lag budget of one configured accounting
  cadence. The two-account fake-backend Windows fixture completed in 1 ms against
  15 s; the test checks state and removed peers, not merely dispatch.
- [x] Measure production-code enforcement lag during actual backup crypto/delivery; define
  and test a budget relative to configured accounting cadence. Unknown/stale metering
  is reported, not converted to zero or a false enforcement-success claim.
  Linux integration uses the real node/scheduler, factor-18 encrypted archive, local
  delayed webhook/Telegram receivers and 512 fake-backend devices at GOMAXPROCS=1.
  Quota enforcement and expiry stay within the 15 s cadence; peak RSS includes KDF
  memory. Stale/future accounting observations are unavailable rather than healthy.
  Actual VPS/kernel/client lag and peak-resource certification remain Phase 20 gates.
- [x] Consolidate encrypted-storage definitions for startup, archive inspection and
  bounded node rotation, with secret-settings parity. Rotation now covers customer
  links/webhooks and safely completes an interrupted window without replacing its
  required keys; full current-key verification/checkpoint precedes predecessor removal.
  Local tests cover 129 devices/all encrypted fields, mixed-key retries, invalid keys
  and independent post-rotation archive verification.
- [x] Distinguish invalid data from incomplete/timed-out archive verification;
  cancellation retains its cause and cannot publish a restore preview.
- [x] Validate relevant restored domain semantics and supported core requirements;
  cryptographic decryption alone is insufficient.
  The shared immutable-snapshot gate now verifies migration history, matching keys/PSKs,
  supported AWG profiles, pool/IP assignment, entitlement/accounting/lifecycle/timestamp
  values, customer lookup hashes and current settings. It reads original schema versions
  without modifying them and rechecks older approved previews before replacement.
  Stored backend inventory identifies reviewed kernel/userspace requirements; actual
  installation, module/daemon provenance, networking and clients remain host gates.
- [x] Review export encryption, bounded memory/disk and off-host password recovery.
  Keep local pre-update recovery policy distinct from downloadable/off-host archives.
  The [preparation drill](../operations/migration-preparation.md) records optional
  age export encryption, the independent password-recovery requirement, streaming/
  admission limits, transient KDF memory and remaining real-host resource gates.
- [x] Produce an owner-usable backup/export/verify/recovery drill linked above.
- [ ] Owner checkpoint after the preparation release: download and independently verify
  the off-host copy before any rebuild. This is not a pre-publication or Phase 16
  source-refactor dependency: on 2026-10-04 the owner explicitly requested release
  first so the installed server can be updated and backed up, then full Phase 16
  implementation without another release. The rebuild remains separately gated.

Implementation evidence: migration/readiness/worker safety at
`39bfb0a4a5f27e4fc80bb27529cccd30d91cdaec` passed
[exact CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37200471956).
Shared storage/rotation, incomplete verification and the actual-service stalled-pass regression
at `7c9d1908dedfb04dec55239b3a5db44d67f0a558` passed local full Go tests/vet/build and
[exact CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37202081662), including both
Linux race jobs. Local unchanged-package evidence used applicable Go cache. No new real-host,
browser, public release or registry publication is inferred from these gates.
Stored-domain admission at `bb60bfd06696fcb3355debc76a0982bd39b22a1f` passed local full Go
tests, vet/build and [exact CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37207442547),
including both Linux race jobs. Original/current layouts, readable invalid data, old approved
previews and supported backend inventory have regression evidence; real-host requirements
and the owner's verified off-host copy remain open.

Preparation exit: failure-injection/race coverage for changed boundaries; actual offline
fixture restore with preserved identity/data; measured real crypto/slow-delivery isolation;
owner-ready export/verify drill and exact-source/artifact publication gates. The owner
authorized the next preparation release and Phase 16 in sequence on 2026-10-04.
Owner-server backup and actual VPS resource/kernel/client acceptance remain explicit
post-publication/migration checkpoints, never inferred from synthetic fixture results.

## Phase 16 — Modular responsibilities and runtime consistency

- [x] Inventory concrete duplication in CLI, panel, API and lifecycle. Keep one
  application operation for each supported action and explicit operation state.
- [x] Separate request/view parsing, application orchestration, domain policy,
  persistence, runtime adapters, host platform and lifecycle coordination.
  Do not force an interface/repository framework around every table.
- [x] Make desired/persisted/applied/failed states distinguishable. Preserve durable
  peer removal, transaction boundaries and idempotent reconciliation; runtime
  failure after a committed account change must have a recoverable outcome.
- [x] Centralize validated host/container paths and ownership. Keep the host journal,
  executable and retained artifact set outside node-writable mounts.
- [x] Split `internal/install` and large CLI responsibilities by actual behavior,
  retaining one coordinator/lock instead of competing installers for core/TLS/panel.
- [x] Identify shared host/data responsibilities before native removal. Keep
  concrete Docker execution rather than introducing a deployment-plugin abstraction
  for a second backend which the product will no longer support.
- [x] Retain the typed WireGuard-family backend and canonical client renderer.
  Preserve device keys/IPs, principals/scopes, units and public API semantics.
- [x] Audit backup, leases, pending restores and concurrent commands across the
  refactored boundaries; keep fake-backend development independent of host changes.

Exit: behavior-equivalence and persistence/concurrency/failure tests; coherent
dependency direction; measured resource baseline. Update API/OpenAPI only if an
actual public contract changes, never merely for internal package movement.

Implementation/inventory and local Go/failure/ordinary+race Linux load evidence:
[Phase 16 record](phase16.md). `nodestate`, `runtimeapply`, device credential
provisioning and `layout` own shared rules; concrete host adapters retain one
lifecycle coordinator and the current deployment contract. No post-Phase-16 release.
Exact implementation `8c11b87efa48bffe8afe3bed7823242aaac50f76` passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37229319774), including both
Linux race jobs, build/vulnerability/bootstrap checks and the actual encrypted-archive load gate.

## Phase 17 — Docker-only distribution and native cleanup

Mandatory milestones 17.0–17.5 and removal inventory:
[Docker-only cleanup](../architecture/docker-only-cleanup.md).

- [x] One reviewed runtime build recipe for release/install. Remove divergence
  between the repository Dockerfile and installer-generated runtime composition.
- [x] Bind image digest, panel binary hash/commit, AWG tools/userspace source,
  kernel bundle, data contract, lifecycle protocol, notices and SBOM in release metadata.
  Digests establish content identity; publisher authentication needs its own policy.
- [ ] Build/test runtime images in CI. Publish only with owner authorization.
  Support bounded cache/local verified artifacts so outage recovery does not require
  a functioning registry, internet connection or running container.
- [x] Remove native production install/update/rollback/server rendering and all
  mode-specific status/log/restart/health/restore/exposure paths. Keep one Docker
  execution path under the shared lifecycle coordinator and preserve host/fake roles.
- [x] Remove native choices/flags/aliases, native unit/journal artifacts and state
  fields. Version the Docker state contract; legacy state is a migration refusal,
  not permission to execute a retained native runtime.
- [x] Replace native mode-cross-product tests/current guidance while preserving
  Docker-equivalent failure, data/security/lease and host broker/kernel coverage.
  Historical recorded native evidence remains historical.
- [x] Preserve host kernel/DKMS ownership, loaded/on-disk identity and reviewed
  compatibility. No `privileged: true`, Docker socket mount, foreign firewall flush
  or silent switch to userspace.
- [x] Review minimal capabilities, private/read-only mounts, temporary writable
  paths, `no-new-privileges`, log bounds and resource limits against real operations.
  Do not claim host-network containers fully isolate the host network.
- [x] Stage acquisition before stop, verify backup, deploy, prove readiness/TLS,
  commit state and retain a bounded known-good recovery set. Check data compatibility
  before artifact rollback. Do not make host reinstall the normal update workflow.
- [x] Decide layout changes from demonstrated ownership needs. Recognize only
  explicitly supported old managed layouts for migration review; refuse unknown
  state before mutation. Fresh install plus restore remains the owner's chosen route.

Exit: exact artifact verification plus isolated/real Docker install, failure/recovery,
rollback, unavailable-container and offline-manager drills. Supported Ubuntu/amd64
scope does not expand just because the runtime is in Docker. The explicit native
cleanup checklist must pass; merely removing its menu option is incomplete.

Source scope and fresh/reused/unrun checks are in [Phase 17](phase17.md). A checkbox above
records source implementation/review, not physical host certification. Exact Docker image/CI
evidence passed; registry/release publication is not authorized. The required physical
client/reboot/restore/failure/resource matrix remains open under Phase 20. Global Docker cache
or shared-image pruning is not automatic; current/previous file recovery identities are bounded.

## Phase 18 — Domains and TLS inside the panel

Specification: [domains and certificates](../operations/domains-and-tls.md).

- [x] Model panel origin, public subscription origin and VPN endpoint independently.
  Manage one panel hostname and at most one distinct subscription hostname initially.
- [x] Offer panel/terminal automatic issuance and renewal, manual certificate/key
  import or controlled file paths, and existing external proxy mode.
- [x] For same-host direct HTTPS, use one owned listener with SNI certificate
  selection and hostname-role routing. Both domains may use port 443; separate
  daemons competing for the same address/port are not required.
- [x] Separate enrollment, certificate storage/activation, hostname authorization
  and public/private route policy. Never enroll domains from an arbitrary request
  Host or from the subscription link setting alone; removing a name must also
  deny existing cached-certificate access.
- [x] Give certificate/path/import work a dedicated bounded owner-authorized host
  operation contract. The existing update bridge remains identity-only; do not
  turn it into a raw command/config/key transport or root file browser.
- [x] Validate pairs, chain/SAN, validity and permissions before activation;
  atomically retain the working pair, activate with the minimum required restart/
  recreate/reload and roll back on failure. Manual import does not imply automatic renewal.
- [x] Show separate requested/issued/active/verified/expiring/failed states and safe
  receipts. Changes to panel origin must not rotate customer links or device keys.
- [x] Deny admin/API/login and path traversal on a dedicated public subscription
  hostname; allow the required public page/config/QR/assets and GET preferences only.
- [x] Keep proxy/remote-host operation explicit. GUI fields cannot configure another
  server's DNS, forwarding or TLS merely by changing a local URL.

Source implementation/evidence: [Phase 18](phase18.md). Checked items describe
implemented source; actual CA issuance/renewal/reboot and remote forwarding remain
Phase 20 physical acceptance. No release is authorized.

Exit: TLS/ownership/failure tests, fa/en and keyboard/touch browser coverage, actual
two-hostname HTTPS issuance/renewal/import/replacement and route-isolation proof.
No public backup/bot certificate API is added by this plan.

## Phase 19 — Installer and operational UX

- [x] Keep bootstrap small: acquire/verify the manager; Go owns product operations.
  Terminal remains English-only with actual stages and recent/live component logs.
- [x] Expose a short fresh-install flow plus advanced access/core choices. Provide
  install-from-backup before exposing the public listener; review target deployment
  separately and restore the source owner/access data rather than guessing credentials.
- [x] Use the same operation model in CLI, Update Center, Backups and domain/TLS pages.
  Present progress, queued/running/recovery-needed states and specific next actions.
- [x] Remove duplicated prompts, misleading "Done" states and manual Compose/Nginx
  editing from supported standard workflows. Never stop an unknown port owner.
- [x] Reuse fields, help, tabs, dialogs, receipt/status cards and responsive layouts.
  Preserve fa/en, RTL/LTR, numeral preferences, technical Latin values, CSP and
  meaningful fallback paths. Do not rewrite unrelated pages for visual novelty.
- [x] Keep acquisition, broker recovery and logs usable without a healthy web panel.

Implementation and fresh/reused/unrun evidence: [Phase 19](phase19.md). These
checks cover source/isolated terminal/browser behavior; actual owner migration
and physical host/network/client acceptance remain Phase 20.

Exit: actual terminal cancellation/redirect/secret/failure fixtures, affected browser
matrix and no-JS fallback, and a documented operator journey with no hidden steps.

## Phase 20 — Certification, migration and publication

- [ ] Freeze an exact source/artifact candidate; run applicable source/security/race gates.
- [ ] Use an explicitly authorized isolated Ubuntu 24.04 amd64 host or an owner-coordinated
  maintenance drill. Do not rebuild the user's only host merely to gain evidence.
- [ ] Test new install, restore, kernel and explicit userspace, reboot, upgrade,
  certificate renewal/replacement, service/image/registry failure and interrupted recovery.
- [ ] Compare all relevant IDs, keys/config bytes, customer access, IPs, quotas,
  expiry, usage, templates/reseller ownership and API credentials against the backup.
  Validate actual client DNS/HTTPS traffic, shaping and old/public hostname behavior.
- [ ] Measure idle/load/backup memory, CPU, writer contention and enforcement lag.
  Resource/compatibility claims use measured evidence, not universal assumed optima.
- [ ] Refresh deployment, security, networking, backup, terminal, UI, API when affected,
  status/readiness and release notes together. Keep historical fixtures/results unchanged.
- [ ] Obtain scope-specific release/registry approval, pass publication gates and
  independently download/verify public artifacts before reporting publication.

Exit: explicit supported matrix and honest unverified cells; owner backup and
recovery path retained; exact public-source/artifact identity proved. No version
number is reserved by this plan.

## Deferred work

Remote AmneziaWG nodes and native WireGuard need a new scope/acceptance program.
Node transport must be private, identity-bound, versioned and limited to typed
operations, with durable metering/desired revisions and offline quota policy.
Xray/sing-box/OpenVPN remain independent product decisions; shared account concepts
do not make their credential, stats or revocation contracts interchangeable.

## Required review outputs

Each phase maintains a short implementation report: problem, final behavior,
changed boundaries, tested source/environment, failures, remaining risks and exit
evidence. A new dependency needs a resource/maintenance justification. A plan checkbox
becomes complete only with implementation and the evidence its acceptance claim requires.


Phase 17 exact implementation `5952d97bb07b656db4d931230a4f4c671be9d3e4` passed [main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37247158064) on 2026-10-05:
both Linux race matrices and encrypted-archive load, vet/build, vulnerability/bootstrap,
canonical image/binary/core identity and offline-load/fake-node persistence/hardening checks.
The unpublished seven-day Actions artifact was independently downloaded: ZIP digest and all
six file checksums passed; binary/runtime metadata identify that SHA. No new real-host/client,
public release or registry result is implied. Latest stable remains v0.1.9.

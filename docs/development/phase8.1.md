# Phase 8.1 — GitHub delivery & lifecycle

State: **active / M1–M5 reviewed, integrated acceptance pending**, updated 2026-09-06. Owner approval to implement and push was supplied
in the installer request. Final public release publication remains separately approval-gated.

## Objective and placement

Make a clean Linux VPS installable from GitHub with one entry command, then manageable through
the same polished terminal experience. Extend the existing Phase 7 engine and shared application
services. Phase 8 remains complete. Phase 9's design-only branch is paused; its telemetry and
seven-day unified-log work follows this phase. Phase 10 still owns the web redesign, Phase 11 the
feature-frozen compatibility/security/load matrix, Phase 12 the final release candidate.

The insertion avoids renumbering the accepted 9–12 requirement mappings and keeps deployment
feature development ahead of certification. Artifact format/acquisition work is pulled forward
from Phase 12 because installing releases requires a real artifact contract. No release is
invented or silently substituted with development code.

## Design decisions

1. A small Bash bootstrap acquires the selected build; the existing Go binary owns installation
   and lifecycle mutations through `internal/install`. A shared Go distribution package handles
   catalog, immutable identity and update acquisition. Shell is limited to first acquisition and
   prerequisite tooling required to run that first binary. A giant shell installer and a second
   resident management service were considered; both duplicate ownership and recovery logic.
2. Release selection offers latest stable and a bounded release list; exact release selection
   requires published compatible assets and SHA-256 verification. Development selection resolves
   `main` or an explicit full commit SHA before fetching/building. It displays development status,
   records the exact SHA, and never silently downgrades a release request to a source build.
   Download checksums provide integrity over the trusted GitHub/TLS channel, not independent
   publisher authentication. Provenance/signing remains part of Phase 12 release engineering.
3. Build prerequisites are temporary where practical; a source install can require Go at build
   time, never a production runtime. Toolchain downloads are platform-specific and checksummed
   from official Go metadata; module verification stays enabled. Use immutable local Docker image
   identities or verified release metadata, not a mutable `latest` rollback reference.
4. The terminal UI has a branded overview, grouped numbered actions, readable steps and review
   before mutation. It must work at 48/80/120 columns, with `NO_COLOR`, `TERM=dumb`, cancellation,
   piped input and noninteractive flags. English and Persian copy uses `internal/i18n` catalogs;
   terminal bidi support is reported honestly. Secret entry is hidden on TTY and never placed in
   argv, logs or state. Input is bounded; EOF/cancel never means consent.
5. Domain setup uses existing in-process ACME and validates certificate readiness separately
   from process health. Panel TCP port is configurable; HTTP-01 needs external TCP 80 even if an
   internal challenge listener differs. Each AWG interface has its own UDP listen port; the
   installer configures its allocation range/defaults. IP-only defaults to loopback with a usable
   SSH tunnel command; manual certificates are available for public TLS. This does not claim
   that all CAs forbid IP certificates; the current `autocert` integration is domain-oriented.
6. Prerequisite adapters inspect OS, architecture, init system, Docker/Compose, kernel headers,
   tools, module and ports before mutation. Install required packages without blanket upgrades,
   preserve foreign Docker/firewall/SSH resources, and record owned resources. Ubuntu PPA suites
   are not guessed or injected into Debian. Unsupported automatic setup fails with an actionable
   manual-prerequisite route. Only evidence-backed OS/architecture cells are advertised verified.
   A clean supported host may need repository tooling and the documented PPA prepared first;
   when core packages need installation, both exact AWG versions must then be available before
   core-package/deployment mutation. An already installed, validated exact bundle satisfies this
   gate without forced repository refresh. Failed required availability is incomplete prerequisite
   preparation, never a successful installation.
7. AWG is a compatibility bundle: tools, kernel source/package and optional userspace daemon are
   separate versions. Recommended means the pinned verified contract. Latest means latest
   compatible catalog entry, not arbitrary upstream HEAD. Explicit selection is allowed only
   for a catalogued compatible bundle. Display installed/requested/loaded versions and reboot
   requirements. Do not unload active tunnels automatically. Managed userspace fallback remains
   AUD-019 in Phase 11; no installer path may claim it exists.
8. Lifecycle operations use exclusive locking, durable operation state, staged artifacts,
   pre-update backups and health-checked commit/recovery. Keep a previous healthy artifact after
   successful updates. A failed pull/build/checksum changes no active deployment. Startup failure
   and health failure both recover. Binary rollback is not a database downgrade: schema changes
   require explicit coordinated restoration from the recorded pre-update backup. Never report
   automatic recovery when state is incompatible or recovery failed.
9. Backup UI calls the shared archive/schedule/Telegram services: create/list/send, encrypted
   off-host recommendation, restore preview/confirmation, schedule list/add/enable/disable/delete,
   daily/weekly/every-N-hours or equivalent days with retention. Keep the single scheduler.
   Restore streams large allowed members with total/member bounds and rejects corrupt or unsafe
   archives before swap. No new backup REST API and no cron process.
10. Fresh installer-managed nodes create the owner locally before starting a public listener.
    Interactive setup uses hidden password and confirmation; noninteractive public setup needs
    an explicit protected password file unless an owner already exists. Reuse the admin service
    with atomic single-owner creation. Existing owners are never reset by install or update.
    This closes the first-visitor ownership race without changing the panel's visual design.

## Milestones and deliverables

| Milestone | Deliverable | Verification |
|---|---|---|
| M1 | GitHub catalog/acquisition, bootstrap, local candidate artifact builder | Fake HTTP failure/integrity tests; Linux bootstrap fixture execution; no-release behavior |
| M2 | Prerequisites, AWG catalog/policy, safe port/TLS/IP setup | Host-seam tests; pinned upstream check; clean-host prerequisite drill |
| M3 | Locked/recoverable install/update/rollback and catalogued core switch | Failure injection at swap/start/health/state; previous artifact/data recovery; explicit pending-reboot state |
| M4 | Terminal design system, setup and management navigation | fa/en parity; scripted flows, EOF/secret tests; real 48/80/120-column TTY |
| M5 | Backup/Telegram/schedule/restore management | Real service/database tests; archive security/bounds; negative group IDs; scheduled execution |
| M6 | Integrated verification, operator docs and handoff | Docker/native dedicated-VPS lifecycle, TLS, source identity, CI and clean repository |

Implementation detail and progress: [implementation plan](../superpowers/plans/2026-09-05-installer-lifecycle.md).

## Completion criteria

- Every requested major feature maps to M1–M6 and is implemented or has a concrete external
  blocker recorded; no future roadmap item is marked implemented by this phase.
- Build, unit tests, vet, race and relevant Linux integration checks pass; GitHub CI is green on
  the pushed revision. Tests cover malformed input, cancellation, download and mutation failures.
- A dedicated Ubuntu 24.04 amd64 VPS verifies Docker and native acquisition/install, exact build
  identity, AWG readiness, ACME certificate, update, successful rollback, failed-update recovery,
  backup, restore and schedule execution without harming unrelated host resources.
- Real Telegram send requires an operator-provided bot/chat; without them only service/HTTP
  integration is marked verified. No credentials or real backup contents enter Git or evidence.
- Release fixture tests and candidate artifacts do not count as an actual published-release
  installation. If no public release exists, that path remains explicitly pending publication.
- README, deployment/runbook, backup docs, architecture, testing/status and this tracker agree;
  AGENTS stays concise. Commits are coherent and pushed; no public final release is published.

## Deferred work and dependency handoff

Phase 9 consumes lifecycle diagnostic context, adding unified live logs and seven-day retention.
Phase 10 consumes stable operational semantics for web settings redesign. Phase 11 repeats
lifecycle drills across all supported OS/architecture cells and owns managed userspace lifecycle,
security/load/soak certification. Phase 12 freezes candidate artifacts, final release workflow,
provenance and documentation and requests explicit approval before public publication.

## Verification record

- Baseline: clean main `2f756b9`; isolated branch `codex/installer-lifecycle` created.
- M1 (`5ec46e0`): acquisition package, executable bootstrap and candidate builder implemented.
  Full Go tests/build/vet, WSL shell fixtures, dual-architecture local checksums and Linux amd64
  execution passed. [CI passed](https://github.com/Sir-Adnan/wg-guard/actions/runs/33995279156)
  on the exact commit. Independent review closed after output-capture hardening in `899f4e0`;
  a real subprocess regression verifies the per-stream memory cap. This does not verify Docker
  integration, clean-host provisioning or published-release installation.
- Read-only dedicated-VPS baseline:
  [sanitized record](../integrations/fixtures/verify-phase8.1-baseline-2026-09-05.txt).
  Existing Docker node, two AWG interfaces and valid cached certificate must be preserved.
- New integrated real-host verification: pending. A dedicated Telegram bot/chat was supplied;
  credential and chat reachability checks passed, while actual new-code backup delivery is pending.
- Acquisition-only VPS probe of `5ec46e0`: bootstrap checksum matched and dependency downloads
  started, but the SSH connection did not deliver a final result. Build success is **unverified**;
  the owned remote temporary directory was cleaned, original HTTPS health remained good, and
  system Go remained unchanged. Repeat with durable bounded evidence during integrated testing.
- Durable repetition on `3a02c72` **passed**: real GitHub source acquisition, temporary compiler,
  candidate build and installer help completed in 80.7 seconds; original HTTPS health and system
  Go were preserved. Latest-release selection failed closed because no release exists.
  [Sanitized evidence](../integrations/fixtures/verify-phase8.1-acquisition-2026-09-06.txt).
  [Exact-commit CI passed](https://github.com/Sir-Adnan/wg-guard/actions/runs/33996190149).
- M2 (`fa74c4a`, hardened in `da46c00`): prerequisite/core/TLS and candidate runtime-image helper
  implemented; full Go tests/build/vet passed, independent review closed. Public endpoint
  classification rejects CGNAT/mapped and relevant special-use addresses; this is not proof of
  network reachability. Read-only core CLI commands passed on the
  actual Docker node ([evidence](../integrations/fixtures/verify-phase8.1-core-readonly-2026-09-06.txt)).
  Current exact package availability is also recorded
  [separately](../integrations/fixtures/verify-phase8.1-package-metadata-2026-09-06.txt).
  These checks do not certify fresh installation, image deployment or certificate issuance.
- TLS diagnostic follow-up: M4 now retains the last classified handshake failure alongside
  timeout/cancellation, with a regression test. See the M4 review record below.
- M2 review/status revision `bd40179`: [CI passed](https://github.com/Sir-Adnan/wg-guard/actions/runs/33998640840).
  M3 was still in progress at that revision; its later review record follows below.
- Baseline schema follow-up (read-only, 2026-09-06): the existing Phase 7 VPS database contains
  migrations 0001–0006. Candidate schema 0007 is a real data-contract transition. Integrated
  drills must preserve and restore the original database/master-key pair before returning to
  the original Phase 7 image; binary-only rollback is not sufficient proof of safe recovery.
- M3 (`4b72243`): locked, journaled lifecycle/source integration and core maintenance implemented;
  full Go tests/build/vet, Linux scoped race/process-death lock/atomic-file tests and executable
  bootstrap compatibility fixtures passed. Independent review closed after the native cleanup
  fix in `fc2c537`; no real deployment drill
  is claimed. [Recovery contract and limits](../operations/lifecycle-recovery.md) distinguish
  same-contract rollback from M5's still-pending coordinated original-schema recovery. M4's
  owner-before-listener hook was provided for the later M4 provisioning implementation below.
- M3 native cleanup review fix: complete systemd load/activity observations distinguish a
  genuinely absent inactive unit from failed/ambiguous queries. Partial install cleanup and
  interrupted uninstall retry tests pass while genuine stop failures still preserve data.
  Scoped re-review found no remaining Important/Critical issue. The `2a65933` implementation/
  status revision [passed CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/34000616493);
  CI on the subsequent cleanup fix must be observed separately.
- Cleanup fix and closing status revision `e041875` subsequently
  [passed CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/34001185021).
- M4 (`e21a87f`, bootstrap rerun follow-up `4c8f07e`): bilingual terminal/setup/management,
  actual-FD hidden input, local owner-before-listener provisioning, atomic single-owner creation,
  shared locked restart and candidate capability gating implemented. Full Go suite/build/vet,
  scoped Linux race/PTY and executable bootstrap fixtures passed. Independent task review closed
  with no Important/Critical finding; M6 real-deployment certification remains pending. Bootstrap default
  interactive entry starts fresh setup with the acquired build or opens an existing node's menu
  without reinstalling. [Operator guide](../operations/terminal-management.md).
- Two minor M4 review items remain for M6 final review: show customized pool/MTU/DNS in the final
  setup review, and make recovery hints operation-specific (restart journals need restart retry,
  not update recovery). They do not waive the final product-quality gate.
- M5 (`281b607`) implements complete backup/schedule/Telegram management, streaming private
  restore preview/explicit approval and recoverable database/master-key replacement. Shared
  host-side restoration coordinates stop/apply/start in both modes; original-schema recovery
  verifies the journal's archive and retained artifact before old-code startup. Full test/build/
  vet, scoped Linux race/integration and bootstrap fixture gates pass; independent review is
  pending. Actual VPS lifecycle, Telegram delivery and scheduler-tick acceptance remain M6.
  No REST route/wire representation changed; internal web restore forms carry preview identity.
- M5 also closes implementation gaps found during execution: secret-read failure cannot
  downgrade encryption, Telegram transport errors do not echo credential URLs, same-second
  archives have unique names, and excessive age/scrypt work factors are refused before costly
  derivation. The existing factor18 crypto cost (~256 MiB transient) is documented. These are
  automated implementation results, not a completed phase or VPS recovery claim.
- M5 review identified three Important gaps: recovery markers were not checked by every manual
  database opener, managed configuration could redirect data away from the lifecycle guard,
  and new substantive safety messages bypassed Persian localization. Fix `8be8c6f` adds opener
  refusal before database access, canonical managed-path validation before preparation/stop and
  keyed safe fa/en CLI/panel conditions. Focused regressions, full test/build/vet and scoped Linux
  race/integration pass; scoped re-review is pending. Original implementation/docs revision
  `c46f313` [passed CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/34005864073);
  the fix requires its own CI result.
- Crypto-localization follow-up `8ce6f99` covers missing/short passwords, invalid or damaged
  encrypted inputs, streaming crypto failures and excessive KDF work with safe keyed fa/en
  messages while retaining internal error causes. Focused and full local gates pass; the
  scoped re-review closed the residual finding with no new Important/Critical issue. M5's
  implementation review is complete; actual Telegram, schedule and lifecycle acceptance remain M6.
- Exact revision `9db9016` [failed CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/34006644053)
  in the stable-Go download cancellation test: a canceled response was reported as a size
  mismatch. Minimum-Go tests, both architecture builds and vulnerability checks passed.
  Correction `f47d92c` checks cancellation after response/file cleanup; a deterministic body
  fixture reproduces canceled clean EOF before the fix. Focused repeated/race and full local
  test/build/vet gates pass. Scoped review and the next exact-head CI remain pending; the
  older green run does not cover this failure.
- M4 implementation/status revision `99b9338`
  [passed CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/34003509985).
  Closing-review revision `234f067` also
  [passed CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/34003707951).
- Real read-only management/input verification on candidate `234f067` passed 17 Linux PTY
  cases and three nonTTY checks; original settings, deployment identity and trusted HTTPS health
  were preserved ([sanitized evidence](../integrations/fixtures/verify-phase8.1-terminal-readonly-2026-09-06.txt)).
  This does not certify new installation, lifecycle mutation or universal Persian client shaping.
  Legacy install-state displays missing TLS/core fields as blank; final UX review should use an
  explicit unknown/not-recorded state. The bounded exact-candidate harness is retained with the evidence.
- Real runtime-image preparation on `234f067` passed after one identical bounded retry of an
  external Launchpad signing-key API 504. Actual image labels, binary digest, installer contract
  and pinned AWG tools/package were verified in network-isolated, read-only containers;
  original deployment and interfaces remained unchanged. Only the exact owned probe image and
  private staging directory were removed. [Evidence and limits](../integrations/fixtures/verify-phase8.1-runtime-image-2026-09-06.txt).
  This verifies image preparation, not node installation, clean-host DKMS provisioning or ACME.

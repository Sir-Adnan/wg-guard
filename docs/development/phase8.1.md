# Phase 8.1 — GitHub delivery & lifecycle

State: **active / design recorded**, 2026-09-05. Owner approval to implement and push was supplied
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

## Milestones and deliverables

| Milestone | Deliverable | Verification |
|---|---|---|
| M1 | GitHub catalog/acquisition, bootstrap, local candidate artifact builder | Fake HTTP failure/integrity tests; Linux bootstrap fixture execution; no-release behavior |
| M2 | Prerequisites, AWG catalog/policy, safe port/TLS/IP setup | Host-seam tests; pinned upstream check; clean-host prerequisite drill |
| M3 | Locked/recoverable install/update/rollback | Failure injection at swap/start/health/state; previous artifact and data recovery |
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
- Implementation and new real-host verification: pending.

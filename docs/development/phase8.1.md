# Phase 8.1 — GitHub delivery & lifecycle

State: **complete**, 2026-09-09. Public release/tag/registry publication remains separately
approval-gated.

## Objective and placement

Make a supported Ubuntu VPS installable from GitHub with one command and manageable through a
clear English terminal experience. The product contract is Ubuntu 24.04 or newer on amd64;
Ubuntu 24.04 is the verified baseline. This phase extended the Phase 7 installer rather than
creating a second deployment engine.

## Delivered architecture and behavior

- A small Bash bootstrap performs first acquisition. The Go binary owns install, update,
  rollback, backup, restore, core maintenance and uninstall through shared services.
- Operators can select the latest stable release, a bounded release list, `main`, or an exact
  full commit SHA. Release assets require SHA-256 metadata; source builds resolve and retain an
  immutable commit identity. A release request never falls back silently to source.
- GitHub source extraction keeps strict root, traversal, link/device, duplicate, member-count and
  size controls. Codeload's single leading PAX global header is accepted only when its sole
  `comment` record equals the selected commit SHA; it is metadata and is never materialized.
- Source builds use a temporary, checksummed Go toolchain when needed. Docker runtime images are
  built from the acquired binary and carry the immutable source/build contract; no mutable image
  tag is trusted for rollback.
- Preflight rejects non-Ubuntu, Ubuntu older than 24.04 and non-amd64 hosts before acquisition or
  deployment, then checks init, Docker/Compose, tools, exact AWG bundle, ports and TLS prerequisites
  without blanket upgrades or ownership of foreign resources.
  Recommended/latest/explicit AWG choices are limited to the compatibility catalog; unsupported
  arbitrary upstream versions are refused.
- Domain installs use the existing in-process ACME path. HTTP-01 requires externally reachable
  TCP 80; the panel has a configurable TCP/TLS port and each AWG interface owns one UDP port.
  IP-only installs default to loopback access through the displayed SSH tunnel.
- The terminal UI is English-only and uses a compact recommended path: optional domain, an
  advanced-settings gate and Enter-to-accept safe defaults. Advanced controls remain available.
  Narrow SSH terminals, `NO_COLOR`, `TERM=dumb`, cancellation, EOF and noninteractive automation
  are supported. Secrets use hidden TTY input, stdin or a protected file and never argv. Fresh
  installs create the owner before a public listener starts.
- `sudo wg-guard` is the zero-download daily entry. Re-running the GitHub one-liner downloads only
  its small bootstrap, validates the owned binary and its Phase 8.1 management contract, then opens
  that local manager. Older host CLIs acquire a compatible manager instead; neither path implies
  an update.
- Lifecycle mutations use an exclusive lock, durable journal, staged artifacts, pre-update
  backup, candidate admission/health contracts and automatic recovery. DB/master-key users hold
  a shared lifetime lease; restore or replacement requires exclusive ownership. Binary rollback
  coordinates schema/data recovery instead of pretending an old binary can read a newer schema.
- Backup management reuses the single application scheduler and archive/Telegram services:
  encrypted create/list/send, restore preview and confirmation, retention, and daily/weekly/
  interval schedules. No resident shell service, cron scheduler or new REST API was introduced.
- Default uninstall preserves data. Explicit `--purge-data` still requires operator-managed
  quiescence; concurrent destructive purge fencing is tracked as AUD-040 in Phase 11.

## Milestones

| Milestone | Deliverable | State |
|---|---|---|
| M1 | GitHub catalog/acquisition, bootstrap and local candidate artifacts | complete |
| M2 | Prerequisites, AWG compatibility policy, ports and TLS/IP setup | complete |
| M3 | Locked install/update/rollback/uninstall and core maintenance | complete |
| M4 | English terminal setup/management, recommended defaults and local-owner bootstrap | complete |
| M5 | Bounded backup/Telegram/schedule/restore management | complete |
| M6 | Integrated VPS verification, documentation and repository handoff | complete |

## Verification record

| Area | Evidence and result |
|---|---|
| Automated and review gates | Package, command, shell-fixture, PTY, failure-injection, restore-security, race and integration gates passed during M1–M5. Final PAX correction `d30894a` passed targeted acquisition tests plus `go test ./...`, build and vet. [CI passed on the exact code revision](https://github.com/Sir-Adnan/wg-guard/actions/runs/34252238598). |
| GitHub acquisition | [Real source acquisition/build/help, empty-release refusal](../integrations/fixtures/verify-phase8.1-acquisition-2026-09-06.txt) and the [one-command management rerun](../integrations/fixtures/verify-phase8.1-one-command-rerun-2026-09-06.txt) passed. The final Docker drill acquired both `6b9dd63` and `d30894a` through the corrected Go extractor. |
| Terminal UX | [17 PTY and three nonTTY checks](../integrations/fixtures/verify-phase8.1-final-terminal-2026-09-06.txt) passed across supported widths/modes without exposing secrets. The final English-only/recommended-path correction is covered by command, prompt, terminal-layout and contract-gated bootstrap fast-path regressions; it does not change deployment or wire behavior. |
| Backup and Telegram | [Isolated real Bot API and scheduler acceptance](../integrations/fixtures/verify-phase8.1-synthetic-backup-2026-09-06.txt) passed with encrypted archives, retention, schedule execution and two real sends. Credentials and backup contents are not recorded. |
| Native lifecycle | [Sequential Ubuntu 24.04 drill](../integrations/fixtures/verify-phase8.1-native-2026-09-06.txt) passed install, owner creation, encrypted backup/restore, update, rollback, failed-start recovery, safe uninstall and restoration of the original node. |
| Docker lifecycle | [Final Ubuntu 24.04 drill](../integrations/fixtures/verify-phase8.1-docker-2026-09-08.txt) passed source install/update, immutable identity, fresh ACME issuance, encrypted DB/key restore, lifetime-lease exclusion, rollback in both directions, unreachable-source preservation, failed-start recovery and safe uninstall. A legacy-fixture flag mismatch was isolated; only that remaining cross-contract cell was rerun with the historical `-stdin` spelling and passed. The original node, certificate and AWG interfaces were restored. |
| AWG/config regression | [Corrected-candidate protocol evidence](../integrations/fixtures/verify-phase8.1-protocol-2026-09-06.txt) confirms real config/QR/client behavior and traffic remained intact. Existing exact core/package/runtime-image observations are recorded in the linked integration fixtures. |

## Completion and remaining limits

All Phase 8.1 implementation, review, documentation and dedicated Ubuntu 24.04 amd64 gates are
complete. API/OpenAPI did not change. Phase 9 implementation is not included.

The following are intentionally not claimed:

- no compatible public release or registry image exists yet, so real published-release install
  remains a Phase 12 gate;
- later Ubuntu releases require exact-package availability and real-host certification in
  Phase 11; other distributions and architectures are outside product scope;
- only the installed catalogued AWG bundle could be reaffirmed; no unsupported version transition
  was invented, and managed userspace lifecycle remains AUD-019 in Phase 11;
- concurrent `--purge-data` safety remains AUD-040 in Phase 11;
- no public release was published.

Phase 9 consumes the stable lifecycle diagnostics for bounded live metrics and logs. Phase 10
owns the full web redesign. Phase 11 owns production certification and unresolved operational
findings. Phase 12 owns signed/checksummed amd64 release artifacts and approval-gated publication.

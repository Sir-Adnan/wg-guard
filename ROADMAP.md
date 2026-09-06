# Roadmap

WG-Guard is developed in sequential, independently verifiable phases. Every phase ends with
tests green, living documentation synchronized, coherent commits, and an honest verification
report that distinguishes implemented, unit tested, integration tested, real-VPS verified, and
unverified work. Detailed release-readiness tracking lives in
[docs/development/release-readiness.md](docs/development/release-readiness.md).

| Phase | Scope | Status |
|---|---|---|
| **0 — Documentation & scaffold** | Documentation, repository scaffold, CI, toolchain, and pinned upstream research | ✅ Complete |
| **1 — Core foundation** | Configuration, database, secrets, auth, domain services, tunnel abstraction, and reconciliation | ✅ Complete |
| **2 — AWG backend & networking** | Pinned CLI backend, interface/peer lifecycle, nftables, sysctls, coexistence, and boot reconciliation | ✅ Complete |
| **3 — Limits & accounting** | Delta accounting, quota/expiry enforcement, first-connection activation, shaping, and traffic rollups | ✅ Complete |
| **4 — REST API** | `/api/v1`, auth scopes, idempotency, pagination, rate limits, durable webhooks, OpenAPI, and node runtime | ✅ Complete |
| **5 — Primary web UI** | Initial design system, auth/onboarding, dashboard, users/devices, plans, interfaces, subscriptions, fa/en and RTL | ✅ Complete |
| **6 — Backup, settings & operations** | Backup/restore, schedules and Telegram, settings, administrators, tokens, webhooks, audit, and doctor | ✅ Complete |
| **7 — Deployment & installer** | Docker/native installation, ACME, host shim, update/rollback, uninstall, and deployment drills | ✅ Complete |
| **8 — Audit & configuration integrity** | Project audit; lossless AWG parameter parity; default/randomized profiles; client config and QR correctness; real handshake/traffic verification | ✅ Complete |
| **8.1 — GitHub delivery & lifecycle** | One-command acquisition, premium terminal installer/manager, prerequisites, compatible AWG versions, and verified lifecycle recovery | 🚧 Active |
| **9 — Operational observability** | Efficient live node/AWG metrics, dashboard telemetry, unified CLI logs, redaction, and bounded seven-day retention | ⬜ Planned; design branch preserved |
| **10 — Product UI/UX redesign** | Complete shadcn-style redesign of every page/state; responsive desktop/mobile; Settings IA; fa/en copy and accessibility audit | ⬜ Planned |
| **11 — Production certification** | Security, race/soak/performance, 1000-peer shaping, recovery drills, and OS/architecture/deployment compatibility matrix | ⬜ Planned |
| **12 — Release candidate** | Release pipeline, checksummed/multi-arch artifacts, repository/docs/API freeze, final regression, and publication-ready report | ⬜ Planned |

## Phase gates

### Phase 8 — Audit & configuration integrity

Eliminate the release-blocking QR and client-configuration defects before later UI work builds on
their models. Preserve every supported AmneziaWG value losslessly across database, API/OpenAPI,
forms, backend apply/dump, reconciliation, downloads, subscriptions, QR, and backup/restore.
Complete only with decoded QR equality and real default/randomized client handshake plus traffic
evidence. Completed 2026-09-05 with the exact commit-stamped dedicated-VPS gate, browser QA, and
sanitized evidence linked from [docs/development/phase8.md](docs/development/phase8.md).

### Phase 8.1 — GitHub delivery & lifecycle

Inserted after completed Phase 8 by the 2026-09-05 installer request. Extend Phase 7's engine
with a GitHub bootstrap and cohesive terminal management, source/version provenance, prerequisite
and AWG compatibility checks, safe update/rollback, and backup/restore scheduling. This is an
independent delivery phase, not a reopening of Phase 8. Phase 9's design-only branch is paused;
its metrics/log-retention implementation remains separate. Artifact acquisition contracts move
forward from Phase 12; public publication still requires owner approval. Complete only with
automated failure tests, terminal QA, and Docker/native lifecycle evidence on the dedicated VPS.
Detailed gate: [docs/development/phase8.1.md](docs/development/phase8.1.md).
M1–M5 implementation reviews are closed. M6 integrated acceptance and final review remain;
this does not mark Phase 8.1 complete or certify the latest CI revision.
M6 has verified the exact GitHub management rerun, isolated real encrypted Telegram delivery/
central-scheduler execution and sequential native lifecycle with original-node restoration.
Final-review data/key concurrency and core retry corrections remain blockers; amended-candidate
checks, managed Docker, cross-contract recovery and fresh TLS issuance remain gates.

### Phase 9 — Operational observability

Add one bounded scheduler-driven telemetry pipeline and a mode-aware diagnostic log workflow.
Complete only when live metrics and Docker/native logs are useful under real failures, secrets
remain absent, retention is bounded, and measured overhead stays within documented budgets.
Detailed gate: [docs/development/phase9.md](docs/development/phase9.md).

### Phase 10 — Product UI/UX redesign

Migrate the complete panel and public subscription experience to one accessible shadcn-style
component system without adding a production SPA runtime. Complete only after every route and
state passes fa/en, RTL/LTR, light/dark, keyboard/touch, and 320px-through-ultrawide browser QA.
Detailed gate: [docs/development/phase10.md](docs/development/phase10.md).

### Phase 11 — Production certification

Feature-freeze the product, close material security/audit findings, and test the exact release
candidate under realistic load, networking, recovery, OS, architecture, backend, and deployment
conditions. Unsupported and unavailable matrix cells must be labeled honestly, never inferred.
Detailed gate: [docs/development/phase11.md](docs/development/phase11.md).

### Phase 12 — Release candidate

Freeze behavior and documentation, build and verify release artifacts from a clean revision,
exercise installation and upgrade from those artifacts, and produce the final readiness report.
Public tags, releases, and registry images remain approval-gated and are not published in this
phase without explicit owner approval. Detailed gate:
[docs/development/phase12.md](docs/development/phase12.md).

## Verification policy

Nothing is done unless [docs/development/status.md](docs/development/status.md) records how it was
verified. WSL2 and containers do not count as real kernel/architecture verification. A planned or
implemented item is not described as production verified until its relevant real-host matrix cell
has evidence.

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
| **8.1 — GitHub delivery & lifecycle** | One-command acquisition, premium terminal installer/manager, prerequisites, compatible AWG versions, and verified lifecycle recovery | ✅ Complete |
| **8.2 — Secure access & persistent manager** | Cached local manager, state-aware terminal UX, port-safe exposure, Nginx coexistence, DNS-01, public-IP HTTPS, and certificate lifecycle | ✅ Complete |
| **9 — Operational observability** | Efficient live node/AWG metrics, dashboard telemetry, unified CLI logs, redaction, and bounded seven-day retention | ⬜ Next; implementation not started |
| **10 — Product UI/UX redesign** | Complete shadcn-style redesign of every page/state; responsive desktop/mobile; Settings IA; fa/en copy and accessibility audit | ⬜ Planned |
| **11 — Production certification** | Security, race/soak/performance, 1000-peer shaping, recovery drills, and supported-Ubuntu/deployment compatibility matrix | ⬜ Planned |
| **12 — Release candidate** | Release pipeline, checksummed amd64 artifacts, repository/docs/API freeze, final regression, and publication-ready report | ⬜ Planned |

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
independent delivery phase, not a reopening of Phase 8; Phase 9 metrics/log-retention implementation
remains separate and has not started. Artifact acquisition contracts move
forward from Phase 12; public publication still requires owner approval. Complete only with
automated failure tests, terminal QA, and Docker/native lifecycle evidence on the dedicated VPS.
Completed 2026-09-09. M1–M6 are implemented, reviewed and documented. Dedicated Ubuntu 24.04
evidence covers real GitHub source acquisition, terminal modes, Telegram scheduling, native and
Docker install/update/rollback/recovery, coordinated restore, fresh ACME, and original-node
restoration. The PAX codeload metadata correction passed the exact-revision CI gate. Published
release artifacts, the broader compatibility matrix and public publication remain Phases 11–12.
Detailed gate and limits: [docs/development/phase8.1.md](docs/development/phase8.1.md).

### Phase 8.2 — Secure access & persistent manager

Inserted before Phase 9 by the 2026-09-09 secure-installer request. Persist the first verified
GitHub acquisition as the local manager, make its menu state-aware, and let operators configure
or later change private, direct HTTPS, standard-Nginx, Cloudflare DNS-01, public-IP certificate,
and manual/external certificate paths without exposing public plaintext or stealing foreign
ports. Complete only when cached retry needs no network, proxy/certificate changes roll back
safely, short-lived IP renewal works, and the targeted Ubuntu 24.04 amd64 VPS matrix passes.
Completed 2026-09-09. The persistent manager, state migration, safe access models, protected
certificate workflows and rollback/diagnostics are automated-test verified. The dedicated Docker
VPS gate passed real Nginx/webroot domain and short-lived public-IP issuance, renewal hooks,
occupied-port refusal, narrow SSH QA and original-node restoration. Cloudflare DNS-01 is
implemented and test-verified but not real-issued because no scoped test token was available;
Native secure-exposure recertification remains Phase 11. Detailed results and evidence:
[docs/development/phase8.2.md](docs/development/phase8.2.md).

Corrective acceptance on 2026-09-09 also closed the fresh-install blocker caused by the retired
PPA package pin: the recommended `awg-2026-09` bundle now builds exact reviewed upstream source,
APT lock contention waits safely, interrupted prerequisite ownership carries into retry, Docker
service/socket recovery is automatic, and install/purge passed on Ubuntu 24.04.4 amd64. Phase 9
remains the next phase; this correction added no Phase 9 or REST/OpenAPI work.

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
candidate under realistic load, networking, recovery, supported-Ubuntu, backend, and deployment
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

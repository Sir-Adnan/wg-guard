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
| **8.2 — Secure access & persistent manager** | Independently cached/update-aware manager, unified update center, state-aware terminal UX, port-safe exposure, Nginx coexistence, DNS-01, public-IP HTTPS, and certificate lifecycle | ✅ Complete |
| **8.3 — Data-plane forwarding integrity** | Docker/UFW forwarding coexistence, effective route/NAT diagnostics, full tunnel-to-Internet verification, and owned firewall cleanup | ✅ Complete |
| **9 — Operational observability** | Efficient live node/AWG metrics, dashboard telemetry, unified CLI logs, redaction, and bounded seven-day retention | ✅ Complete |
| **10 — Product UI/UX redesign** | Complete shadcn-style redesign of every page/state; responsive desktop/mobile; Settings IA; fa/en copy and accessibility audit | ✅ Complete |
| **11 — Production certification** | Security, race/soak/performance, 1000-peer shaping, recovery drills, and supported-Ubuntu/deployment compatibility matrix | ✅ Complete for Ubuntu 24.04 amd64 scope |
| **12 — Release candidate** | Release pipeline, checksummed amd64 artifacts, repository/docs/API freeze, final regression, and publication-ready report | ✅ Complete; v0.1.0 published |
| **13 — Appearance and subscription follow-up** | Public QR/RTL fixes, responsive subscription redesign, ten source-reviewed visual presets with personal/installation defaults | Complete; v0.1.1 published after exact-source CI/release gates; Firefox/Phase 13 real host unverified |
| **14 — Integration API for automation** | Isolated reseller accounts and owner/reseller integrations; recoverable provisioning, customer delivery, usage reset, queued successor plans and webhook contracts | ✅ Complete for documented scope; security in v0.1.2, integration in v0.1.3, template/direct-entitlement follow-up in v0.1.4 after exact-source CI and release gates |
| **15 — Operational safety and migration preparation** | Complete portable backup checks, pre-migration ordering, readiness and slow-job isolation | Active; archive preparation on main, remaining gates open |
| **16 — Modular responsibility boundaries** | Shared application operations, desired/applied state, host/runtime/lifecycle separation and centralized paths | Planned |
| **17 — Docker-only distribution and native cleanup** | One image recipe, exact provenance, complete native lifecycle/flags/state/tests cleanup and independent host recovery | Planned; native removal explicitly confirmed |
| **18 — Integrated domains and HTTPS** | Panel/subscription domain roles, automatic/manual certificate management from panel and CLI, SNI and public-route isolation | Planned |
| **19 — Installer and operational UX** | Install-from-backup, common operation status/recovery, domain/TLS UI and terminal simplification | Planned |
| **20 — Refactor certification and publication** | Actual migrated restore/client traffic, reboot/renewal, resource and failure drills, exact artifact acceptance | Planned |

The [refactor program](docs/development/refactor-program.md) defines Phase 15–20
milestones, dependencies and exit evidence. Historical Phases 0–14 remain complete
within their recorded scopes; planned refactor features are not current support.
The [revised architecture target](docs/architecture/deployment-refactor.md) and
[domain/TLS specification](docs/operations/domains-and-tls.md) govern that work.
Native production removal is implemented in unreleased Phase 17 source, with
image/source checks distinct from physical acceptance. No new release, registry upload or live-server
rebuild is implied. Detailed inventory: [Docker-only cleanup](docs/architecture/docker-only-cleanup.md).

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
ports. Complete only when local retry needs no network, GitHub entry checks avoid repeat builds,
proxy/certificate changes roll back
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

A post-completion maintenance correction makes the one-line entry resolve its selected GitHub
revision before opening management. An unchanged revision uses the verified manager cache; a
changed revision atomically refreshes the independent manager without touching the active service.
The same manager now exposes component-scoped manager, panel, compatible AmneziaWG and ordered
all-component updates. That maintenance did not alter the then-planned Phase 9 or REST/OpenAPI contract.

The final terminal-maintenance correction removes the redundant `q` shortcut, makes `0` the sole
menu navigation key, and gives interrupted uninstall its own config-independent recovery view.
Installed and recovery flows now expose an explicit data-preserving removal or confirmed full
WG-Guard reset without touching unrelated host resources. At that checkpoint Phase 9 was unchanged
and unstarted.

A final account-provisioning correction before Phase 9 makes the interactive username explicit
(`admin` on Enter), retries invalid passwords before leaving the wizard, and offers a
cryptographically generated password with a show-once post-health credential card. Interrupted
initial setup now opens guided reset choices, and uninstall distinguishes recoverable node reset
from complete removal of the local manager/cache/logs. Private loopback installation also uses the
correct non-proxy transport so SSH-tunnel login can retain its session cookie. This remains Phase
8.2 maintenance; it does not add Phase 9 or API/OpenAPI scope.

### Phase 9 — Operational observability

Add one bounded scheduler-driven telemetry pipeline and a mode-aware diagnostic log workflow.
Complete only when live metrics and Docker/native logs are useful under real failures, secrets
remain absent, retention is bounded, and measured overhead stays within documented budgets.
Completed 2026-09-10. Native and Docker retention/log/failure drills, real AmneziaWG traffic,
dashboard/API telemetry, resource measurements, secret scanning, lifecycle recovery and full
cleanup passed on Ubuntu 24.04.4 amd64. Detailed gate and evidence:
[docs/development/phase9.md](docs/development/phase9.md).

### Phase 8.3 — Data-plane forwarding integrity

Inserted as a corrective release blocker after Phase 9 when real clients could handshake but
could not reach routed networks on a Docker host. Preserve the namespaced nftables NAT model,
integrate only through a firewall manager's supported extension point, fail readiness when an
earlier forwarding DROP remains unresolved, and extend the real-host gate through public DNS and
HTTPS rather than stopping at the tunnel gateway. Completed 2026-09-10: the exact Ubuntu 24.04
amd64 Docker candidate passed fresh install, post-start interface creation, plain/recommended/
randomized public egress, DNS, HTTPS, counters, doctor, restart/idempotency and owned-rule cleanup.
Phase 10 then resumed at milestone 10.0. Detailed gate and evidence:
[docs/development/phase8.3.md](docs/development/phase8.3.md).

A bounded post-completion diagnostic correction also made host-owned Docker `doctor` inspect AWG
inside the runtime container without moving kernel/firewall checks off the host. Exact candidate
verification covered healthy, unavailable-container, recovered and managed missing-link repair
states; that maintenance did not alter the Phase 10 scope.

### Phase 10 — Product UI/UX redesign

Migrate the complete panel and public subscription experience to one accessible shadcn-style
component system without adding a production SPA runtime. Complete only after every route and
state passes fa/en, RTL/LTR, light/dark, keyboard/touch, and 320px-through-ultrawide browser QA.
Completed 2026-09-13. The full Chromium/Firefox/WebKit route and state matrix, relevant exact-code
Ubuntu 24.04 amd64 Docker/TLS workflows, decoded QR/config equality, repository gates and main CI
passed. Physical-device runs were unavailable and are not claimed. Phase 11 started afterward;
public release remains owner-approval gated. Detailed gate:
[docs/development/phase10.md](docs/development/phase10.md).

### Phase 11 — Production certification

Feature-freeze the product, close material security/audit findings, and test the exact release
candidate under realistic load, networking, recovery, supported-Ubuntu, backend, and deployment
conditions. Unsupported and unavailable matrix cells must be labeled honestly, never inferred.
Completed 2026-09-28 for the documented Ubuntu 24.04 amd64 Docker/native kernel/userspace paths:
real client, reboot, recovery, TLS, update/rollback, disk pressure, full cleanup, race/fuzz and
0/100/1000 resource/shaping gates passed. Later Ubuntu releases, active firewalld, real-host UFW
and 1000 simultaneous handshakes are not certified. Phase 12 subsequently published the
owner-approved v0.1.0. Detailed gate:
[docs/development/phase11.md](docs/development/phase11.md).

### Phase 12 — Release candidate

Completed 2026-09-28: the exact `fb38c1f` source passed main CI and manual release gates;
checksummed Linux/amd64 assets, metadata, notices and attestations were published as
[v0.1.0](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.0). The public latest-release
bootstrap installed a healthy Docker node on the dedicated Ubuntu 24.04 amd64 VPS. Future
versions and official registry images remain approval-gated; the certified host matrix is
unchanged. Detailed gate:
[docs/development/phase12.md](docs/development/phase12.md).

### Phase 13 — Appearance and subscription follow-up

13.0 verifies the reported public QR and mixed-direction metrics against the actual web code and
reviews the ten named TweakCN light/dark exports. 13.1 introduces the local preset registry,
semantic CSS bridge, independent appearance preference scopes and source-aware Settings UI.
13.2 repairs the public QR state and redesigns the subscription composition for desktop/phone.
13.3 requires focused service/security tests, representative browser states for every preset in
fa/en and light/dark, subscription QR/config checks, accessible contrast/focus and responsive
review; changed cells are retested after fixes. Record any unavailable browser/physical-device
cell honestly. This is new post-release work, not a retroactive Phase 10 or v0.1.0 claim.

Completed 2026-09-28: [v0.1.1](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.1)
publishes `91f0cad` after [main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/36430328644)
and the [release gate](https://github.com/Sir-Adnan/wg-guard/actions/runs/36430987412).
Local Chromium/WebKit and Go checks passed; new Firefox, physical-device and VPS cells were
not verified for this follow-up.

### Phase 14 — Integration API for automation

The owner chose separate reseller accounts with configurable permissions. Owner integrations may
see the whole node; reseller panel sessions and API tokens must see only that reseller's customers.
The later owner clarification drops four combined renewal policies in favor of independent usage
reset and one queued successor plan. A successor is an explicitly authorized entitlement, not an
automatic purchase: the first time or quota boundary activates it once, starts a new period with
fresh charged usage, and never overrides a manual disable or suspension. Optional unused-volume
carry applies only when time ends first; time never carries. Plan terms are frozen when queued.

1. **14.0 — Contract and security:** close the idempotency replay authorization defect; specify
   principal ownership, least-privilege grants, purchase/result identity, successor activation and
   failure behavior before exposing new integration routes.
2. **14.1 — Reseller boundary:** add owner-managed reseller accounts and permission selection,
   principal-bound tokens, row ownership and denial tests across panel, API, subscriptions,
   statistics and webhook delivery. Do not expose a reseller login until every reachable route
   enforces the boundary.
3. **14.2 — Purchase delivery:** atomically provision a user and initial device, persist an
   inspectable operation result with its mutation, and add principal-scoped customer-link delivery
   and rotation. A lost HTTP response must not require guessing whether a purchase succeeded.
4. **14.3 — Entitlement lifecycle:** preserve the existing time-only renewal and independent
   Reset Usage. Add one owner/reseller-scoped Next Plan queue per customer, immutable plan terms,
   bounded scheduler activation after metering and before expiry, a transactional activation
   record, cancellation before activation and review state for incompatible account/device edits.
   A separate immediate plan-replacement and conditional correction API is considered only if
   integration use cases still require it; never infer payment from queueing.
5. **14.4 — Integration completeness:** type webhook event payloads and delivery guarantees,
   expose safe delivery/reconciliation lookup where needed, add bounded batch reads if justified,
   complete OpenAPI examples and contract tests, then run one focused compatibility/security gate.

The v0.1.3 source scopes webhook creation, management, fanout, worker delivery and
non-secret receipt lookup to owner, node operator or reseller authority. Owner-wide reseller
event fanout requires an explicit opt-in; legacy endpoints migrate with it off. A reseller
destination uses checked public HTTPS egress, without redirects. The typed OpenAPI webhook catalog and
at-least-once/retry/ordering contract are complete. A batch-by-ID read is not added: the existing
bounded user cursor (up to 500 per page), opaque resource IDs and purchase operation journal cover
the evidenced sync/recovery workflows without a second bulk lookup contract. Immediate plan
replacement or reversal after activation remains a separate product decision; cancel-before-
activation and current-state reconciliation are implemented, and no financial rollback guarantee
is implied.

The v0.1.4 pre-installation cleanup makes technical templates canonical: `/templates`,
`template_id` and `templates.read/write` replace plan-named catalog identifiers without aliases.
The owner can provision direct finite terms atomically while external products/prices stay in
the bot; reseller-bound purchases still use owner-assigned templates. Panel, REST single-create
and bulk-create paths share one template-to-user entitlement mapping. Ordinary quota top-up and
Reset Usage remain independent, optional lifecycle actions. Opaque IDs are not usernames;
private configs and capability links must not enter logs, audit metadata or operation journals.
[v0.1.4 main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/36703931653) and the
[release gate](https://github.com/Sir-Adnan/wg-guard/actions/runs/36704785746) certify the
listed source/artifact checks; no new physical-device or VPS cell is inferred from them.
Future public releases still require owner approval.

## Verification policy

Nothing is done unless [docs/development/status.md](docs/development/status.md) records how it was
verified. WSL2 and containers do not count as real kernel/architecture verification. A planned or
implemented item is not described as production verified until its relevant real-host matrix cell
has evidence.


Phase 17 source now has one verified Docker runtime recipe, strict new deployment state and
separated private host authority. Exact image/fake-container CI and the physical Phase 20
acceptance/publication gates remain distinct. See [Phase 17](docs/development/phase17.md).


Phase 18 implements independent panel/public subscription origins, SNI and resumed
TLS admission, a dedicated owner-authorized certificate mailbox, versioned imports,
CA renewal/retirement and snapshot recovery. Source and isolated browser/TLS checks
remain distinct from physical Phase 20 acceptance. See [Phase 18](docs/development/phase18.md).


Phase 19 implements archive initialization before managed listener start and
shared safe CLI/panel operation states with explicit review/restart/recovery actions.
Source owners/settings/keys/usage survive the verified archive path; target HTTPS
is reviewed separately. Source/browser/race evidence stays distinct from physical
Phase 20 migration and publication. See [Phase 19](docs/development/phase19.md).


Phase 20 completed the explicitly authorized isolated Ubuntu 24.04.4 amd64
Docker/kernel/userspace host matrix and published Phases 17–20 as **v0.1.10**
(Latest stable) from exact `4f78adc`, with independently verified public assets.
Excluded host/client cells and the next scope are in [Phase 20](docs/development/phase20.md).

# Testing strategy

Use [workflow.md](workflow.md) to decide *when* evidence is needed and whether a prior result
still applies. This page maps WG-Guard's test layers and retains completed phase evidence. The
phase-specific sections below describe their original gates; statements about a then-future
phase are historical, not instructions to rerun it now. Current claims and limits are in
[status.md](status.md).

## Current test selection

Phase 20's root-only isolated-VPS fixture is
[`verify-phase20-vps.py`](../integrations/fixtures/verify-phase20-vps.py), with the
separate archive inspector [`phase20-stage.go`](../integrations/fixtures/phase20-stage.go).
They require explicit owner authorization, private work/password/token files and
an ordinary managed installation. Secrets/configs remain private; reports expose
only counts, equality results and measured resources. The recorded correction/drill
sequence, exact revisions and excluded physical cells are in [Phase 20](../archive/development/phase20.md).

For Phase 15–20 changes, the [refactor program](../archive/development/refactor-program.md) supplies the
specific exit evidence: migration refusal and readiness, slow-worker isolation,
data/runtime state equivalence, artifact/host ownership, two-domain TLS and route
boundaries, affected browser/terminal flows and final real-host migration/resource
drills. Apply those gates when implementing the phase; do not run them to certify
a prose-only planning change. Historical results never certify a new layout or TLS role model.

Tests cover the Docker runtime only, with shared data/lease/security, kernel/userspace, host
broker and fake-backend coverage. A smaller test matrix is not permission to remove failure
checks. Captured fixtures from older layouts are evidence, not a supported implementation.

Start with the affected behavior and its dependencies, then extend to the failure mode that
matters. These are starting points, not automatic checklists:

| Affected behavior | Focused evidence | Expand when |
|---|---|---|
| Prose, links, documented claims | Inspect changed text, local links and diff against the authoritative claim | A command/contract/example changes or source evidence is needed to justify a new claim |
| Domain, API or SQLite | Relevant `internal/<service>`, `internal/api` and `internal/database` tests, including contract/permission and migration tests when affected | Shared models, schema, auth or public contract affects other consumers; verify their paths and OpenAPI coverage |
| Panel, localization or assets | `internal/web` and `internal/i18n` tests for affected routes/states; inspect fa/en and RTL/LTR; browser check for changed behavior/layout | Shared shell/components, keyboard/touch, CSP, theme or cross-viewport behavior changes; use affected browser cells, then acceptance matrix if required |
| AWG/client config, reconcile, firewall or shaping | Pinned upstream facts plus affected adapter, renderer, reconciler and network tests | Real backend/kernel, namespace or forwarded traffic changes; add integration and scoped Ubuntu host/client evidence before a production claim |
| Auth, secrets, concurrency, backup or lifecycle | Focused security/failure-injection tests, rollback/data-preservation checks and race tests where concurrent access matters | A privilege or persistence boundary, host orchestration or recovery state changes; use isolated fixtures first, then authorized real-host acceptance |
| Acquisition, release or resource use | Distribution/bootstrap fixtures, exact-source/build checks or targeted benchmark/asset measurements | Artifact identity, support matrix or performance claim changes; use the exact CI/release gate and relevant host evidence |

`go test -race` exercises the package tests again with race instrumentation, not a different
functional suite. A browser test skipped because its optional runtime is absent is not browser
evidence. WSL integration, fake-backend tests, real Ubuntu host traffic and physical devices each
prove different things. Keep the dedicated invariants and archived evidence below available for
the specific risk; do not execute their entire historical sequence for an unrelated edit.

## Phase 8.1 installer verification

[Phase 8.1](../archive/development/phase8.1.md) adds GitHub acquisition/error fixtures, bootstrap execution tests,
host-operation failure injection, narrow/wide English terminal checks, and dedicated Ubuntu
24.04 lifecycle drills. The evidence levels are separate:

| Area | Recorded Phase 8.1 evidence | Later-phase limit |
|---|---|---|
| GitHub delivery | Real source acquisition/build/help, executable release/error fixtures and exact one-command management. `d30894a` passed source install/update through strict commit-bound PAX handling and [exact-revision CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/34252238598) | Phase 12 subsequently verified a fresh v0.1.0 published-asset install on Ubuntu 24.04 amd64 |
| Terminal | Final real Linux acceptance passed 17 PTY plus three nonTTY cases across 48/80/120 columns and no-color/dumb modes. Follow-up regressions verify the current English-only contract, compact recommended path, Enter defaults and installed-node fast path | Feature-frozen terminal regression is repeated in Phase 11 |
| Runtime image | Builder/image/binary/AWG identity, real Docker deployment and fresh ACME passed on Ubuntu 24.04 amd64 | Later supported Ubuntu amd64 cells remain Phase 11 |
| Backup/recovery | Encrypted Telegram/scheduler, Docker DB/key restore, legacy-schema recovery, update/rollback/failure recovery and original-node restoration passed | Feature-frozen matrix repetition remains Phase 11 |

Exact revisions and sanitized records live in [status.md](status.md) and
[phase8.1.md](../archive/development/phase8.1.md). Earlier Phase 7/8 deployment evidence does not certify new lifecycle
code. Historical checks do not replace Phase 11's supported-Ubuntu/backend certification or
[Phase 12's final published-artifact gate](../archive/development/phase12.md).

## Phase 8.2 secure-access verification

Test-first Go coverage owns exposure derivation, state-schema migration, secret transport,
certificate identity/expiry, deterministic renewal lineages, Nginx rendering/reload rollback,
runtime rewrites, post-install rollback and HSTS/proxy trust. Bootstrap fixtures
cover atomic manager/receipt persistence and zero-network retry; PTY tests cover 40/48/80-column
English menus. The [dedicated Ubuntu 24.04.4 amd64 Docker record](../integrations/fixtures/verify-phase8.2-vps-2026-09-09.txt)
adds real webroot-domain and short-lived-IP issuance, renewal hooks, occupied-port refusal,
state migration and cleanup. Its corrective addendum also records an exact GitHub-source
AWG/DKMS Docker install, real APT-lock failure/retry, Docker socket repair, cached 40-column rerun,
identity checks and explicit source/package purge. Cloudflare DNS-01 is automated-test verified only because no scoped
test token was available.

The repeatable
[`verify-phase8.1-synthetic-backup.py`](../integrations/fixtures/verify-phase8.1-synthetic-backup.py)
fixture runs a private fake-backend node beside an installed node without reading or mutating the
installed deployment. Its local regression suite covers candidate/credential preflight,
same-descriptor validation and private candidate pinning, collision refusal, workspace
trusted-ancestry refusal for locations writable by a different non-root UID, device/inode
replacement refusal, owned-child-only shutdown, bounded noisy-service capture, error-resilient
cleanup and capture redaction. Root/same-UID compromise is outside the documented threat model.
A candidate run covers
encrypted create/list/retention and schedule CRUD plus an accelerated actual central-scheduler
tick. Without the explicit private-credential opt-in it records Telegram as **unverified**; even
with that opt-in it remains synthetic fixture evidence rather than deployment or restore
acceptance. See [backup and restore](../operations/backup-restore.md#isolated-acceptance-helper)
for the bounded invocation and evidence limits.

## Phase 9 observability verification

Deterministic `/proc` fixtures and fake clocks cover metric availability, rate continuity, ring
bounds, log retention and operation outcomes. Text/JSON secret corpora cover the central redaction
handler; the install Host seam covers exact Docker argv, split/oversized lines, broken
output, cancellation, policy ownership and transactional update rollback.

The historical [Phase 9 fixture](../integrations/fixtures/verify-phase9-vps.sh) (it predates
the current layout and does not run against it) and its
[sanitized acceptance record](../integrations/fixtures/verify-phase9-vps-2026-09-10.txt) add
Ubuntu 24.04.4 amd64 evidence: platform policy inspection, bounded/follow
CLI behavior, process crash recovery, real kernel AmneziaWG traffic, telemetry under load,
authenticated live-dashboard output, AWG/network faults, actual-secret scans, Docker lifecycle
failure recovery and full cleanup. Long soak, 1,000 peers and later supported Ubuntu amd64 cells
remain Phase 11; Docker's local driver is size-bounded but cannot promise physical age deletion.

The [Phase 8.3 public-egress fixture](../integrations/fixtures/verify-phase8.3-vps.sh) closes the
gateway-only gap. On a fresh Docker node it creates plain, recommended and randomized interfaces
after service startup, imports each downloaded config into a separate real kernel client
namespace, routes the namespace default through AWG, and requires public IPv4, DNS, HTTPS, NAT
identity and bidirectional counters while Docker retains `FORWARD DROP`. It also checks doctor,
restart/idempotency, the exact owned rule/jump count and cleanup. The exact 2026-09-10 run is
[recorded here](../integrations/fixtures/verify-phase8.3-vps-2026-09-10.txt).

For new work, apply [workflow.md](workflow.md): focused regressions during implementation and
the evidence warranted by the delivered revision and acceptance claim. Prose-only changes do not
justify replaying unchanged suites. Root/service/data recovery paths need failure injection before
real-host execution, not only a happy-path test after the installer is assembled. Behavior-changing
work needs checks matching its risk; most layers run without root or a real VPN interface.

## Layers

| Layer | Scope | Environment |
|---|---|---|
| Unit | parsers, validators, crypto, scheduling logic, i18n parity (fa/en key sets), Jalali conversion | plain `go test` |
| Repository | SQLite migrations, constraints, cursor pagination, delta accounting invariants | temp-file DB, real migrations |
| Service/API | authz matrix, quotas, expiration, first-connection, device-limit races, idempotency replay, webhook signatures, error envelope | `httptest` + fake tunnel backend |
| Tunnel adapter | conf renderer + dump parser against golden fixtures captured from the pinned upstream ([../integrations/fixtures/](../integrations/fixtures/)), exec wrapper against a scripted fake `awg` | plain `go test` |
| Deployment | the whole install/update/uninstall/rollback flow against an in-memory `Host` seam (fs map + recorded commands), incl. health-checked rollback with real probe endpoints on loopback | plain `go test` (`internal/install`) |
| Integration (`integration` build tag) | real interface lifecycle, syncconf, reconcile, nftables, sysctls — userspace backend in WSL2/CI | WSL2 Ubuntu / CI runner |
| Real VPS matrix | kernel/userspace backends, client/public traffic, scoped Docker/non-default forwarding, reboot, recovery, update/rollback, TLS and uninstall | Ubuntu 24.04 amd64 verified for listed paths; UFW is automated-test verified, while later releases and active firewalld are not production-certified |

## Invariants with dedicated tests

- Accounting: counter reset, negative-delta prevention, restart persistence, no double counting
  (delta pipeline is property-tested).
- Concurrency: duplicate IP allocation, device-limit races (run under `-race`).
- Idempotency: replayed `Idempotency-Key` returns the original response, no side effects.
- Webhooks: signature scheme, replay window, restart-safe delivery (kill mid-flight), backoff.
- Drift: unknown peer / missing interface / changed port → reconcile per policy.
- Configuration text boundaries: endpoint and I1–I5 newline/control injection is rejected before
  persistence; reconciliation refuses an invalid stored profile before backend mutation, and I1–I5
  differences participate in exact drift correction.
- Peer replacement: `SyncPeers` carries forward the validated complete live `[Interface]`
  section and byte-verifies it after `syncconf`; a missing/malformed private key or any interface
  mutation fails closed. This prevents the pinned kernel backend from clearing the interface key
  when given a peers-only configuration.
- Client configuration: one literal full-field golden covers exact section placement, ordering,
  range preservation, spacing, and final newline; REST/admin/subscription bytes and headers are
  compared against the direct renderer. Failures report only length/digest/offset metadata and
  never configuration contents or key material.
- QR delivery: PNGs are decoded with an independent test-only implementation and compared with
  the canonical config for direct, REST, admin, and public-subscription paths. Geometry/color,
  empty, UTF-8, full, near-capacity, oversized, and deterministic-output cases are covered.
- Upgrade/restore: real archives from schema 0006 and the current schema are staged, migrated,
  applied, reopened, and compared for exact H/keepalive ranges, rollback mirrors, settings,
  interface review data, and unrelated foreign-key relationships. A separate boot test consumes
  the pending snapshot before database open and verifies the restore audit event.
- Pinned userspace runtime: the `integration` suite applies, dumps, and reapplies all supported
  H/u16 interval fields plus peer PersistentKeepalive and requires exact equality, preventing a
  lossy conversion from appearing as perpetual drift. This is a privileged WSL2/CI test; it does
  not substitute for the real kernel/client traffic gate.
- Phase 8 real-host gate: `docs/integrations/fixtures/verify-phase8-vps.sh` creates only
  collision-checked, harness-owned namespaces/interfaces inside a mode-0700 work directory. It
  compares normalized API, database, runtime, client-config, and independently decoded QR state;
  exercises recommended/randomized kernel traffic plus recommended userspace traffic; checks all
  three delivery surfaces and secret-free diagnostics; and can hold the isolated panel for browser
  QA. The exact commit-stamped Ubuntu 24.04 amd64 run passed on 2026-09-05, including cleanup;
  sanitized evidence is in
  `docs/integrations/fixtures/verify-phase8-vps-2026-09-05.txt`. A physical optical-camera scan was
  unavailable and is not claimed; actual HTTP PNG decoding and import of those bytes into real AWG
  clients passed.

The corrected Phase 8.1 lifecycle candidate `6b9dd63` repeated the isolated real-host protocol
gate successfully on 2026-09-06: [evidence and limits](../integrations/fixtures/verify-phase8.1-protocol-2026-09-06.txt).
Its [final terminal evidence](../integrations/fixtures/verify-phase8.1-final-terminal-2026-09-06.txt)
records 17 PTY and three nonTTY cases. The
[final Docker evidence](../integrations/fixtures/verify-phase8.1-docker-2026-09-08.txt) separately
proves source-backed acquisition and the affected lifecycle paths after AUD-041. Keep Bash and Go
acquisition paths independently covered; PAX support must preserve traversal/link/device/root/
duplicate/member-count/size protections and bind metadata to the exact selected commit.

## Benchmarks (measured, not guessed)

Accounting cycle cost, bulk create 100/1000, user list at 1000 peers, idle RSS/CPU
(`scripts/bench-idle.sh`, 10-minute soak at 0/100/1000 fake peers). Budgets and results are
recorded in [status.md](status.md); regressions are treated as bugs.

## Phase 10 UI verification

[Phase 10](../archive/development/phase10.md) owns route/state/milestone coverage and the final browser matrix. During
implementation use targeted settings/web/i18n tests and changed interaction checks. Coherent
commits still require build/unit success; the full Phase 10 QA matrix runs at 10.7, not per edit.

### Opt-in browser tests

Each test below skips unless `WG_TEST_BROWSER_NODE` names a development Node executable;
`WG_TEST_PLAYWRIGHT` names an existing Playwright module (default `playwright`). Drivers default
`WG_TEST_BROWSER_ENGINE` to `chromium` (alternate engines such as `webkit` need their development
browser binaries); Chromium launches the `WG_TEST_BROWSER_CHANNEL` channel (default `chrome`) where
the driver supports it. Optional `WG_TEST_AXE` locates a development-only `@axe-core/playwright`
installation for WCAG-tagged scans. Each Go test hosts an isolated temporary database; ephemeral
session/subscription values travel only over child stdin and are never printed or committed.
Diagnostics omit URLs, capabilities, configs and QR pixels. No npm/Node dependency enters
production. Run one with `go test ./internal/web -run '^<Test>$' -count=1 -v`; give
`TestBrowserPhase10` a sufficient `-timeout` (for example `70m`).

| Go test | Driver (`scripts/`) | Covers | Extra env |
|---|---|---|---|
| `TestBrowserFoundation` | `test-web-ui.cjs` | focus/inert/resize, menu keys, dynamic dialogs, submitter preservation, duplicate submission, HTMX recovery, representative locale/theme layouts | — |
| `TestBrowserPhase10` | `test-web-product.cjs`, which loads `test-web-public.cjs`, `test-web-states.cjs`, `test-web-interactions.cjs` | milestone product checks: form errors and mutations, fa/en/theme/phone/desktop pages, auth/public, states, interactions | suites, groups and filters below |
| `TestBrowserPresentation` | `test-web-presentation.cjs` | appearance, digits and representative panel pages across locale/theme/viewport cells | — |
| `TestBrowserGuidanceLayout` | `test-guidance-layout.cjs` | help anchoring, paired inputs, legends/drawers, Standard/From template retention, interface CIDR/capacity, compact backup creation, chart hover/keyboard, native and touch-without-Popover fallbacks | `WG_TEST_VISUAL_PRESET` |
| `TestBrowserMaintenance` | `test-web-maintenance.cjs` | Update Center tabs across locale/theme/viewport cells | — |
| `TestBrowserBackupWorkbench` | `test-backup-workbench.cjs` | 80 fa/en Light/Dark responsive cells, cursor pagination, native verification/reload, approval and conditional cancellation | `WG_TEST_BACKUP_NATIVE_ONLY` (native flow only) |
| `TestBrowserSystemHealth` | `test-system-health.cjs` | synthetic `/system` health states, keyboard/details, read-only forms, native fallbacks | — |
| `TestBrowserCleanup` | `test-web-cleanup.cjs` | `/cleanup` selection menus plus interface list/edit pages across 16 fa/en Light/Dark responsive cells | — |
| `TestBrowserOperationalJourney` | `test-operational-journey.cjs` | domains, backups and updates journey: queued/awaiting-restart/idle states, keyboard, native fallbacks | — |
| `TestBrowserDomains` | `test-domains-ui.cjs` | 16 fa/en Light/Dark responsive cells, method/source selection, keyboard and two no-JavaScript fallbacks | `WG_TEST_DOMAIN_SCREENSHOTS` (isolated artifacts) |

`web-qa.cjs` is the shared QA helper (`web-state-actions.cjs` serves the state driver). It applies the cell filters
`WG_TEST_UI_LANG`, `WG_TEST_UI_THEME` and `WG_TEST_UI_WIDTH`, measures request/loading/layout
behavior before instrumentation, and offers diagnostics: `WG_TEST_UI_PERF_TRACE` prints performance
probes, `WG_TEST_UI_PERF_FONT_DELAY` delays `.woff2` responses by that many milliseconds and
`WG_TEST_UI_CHART_TRACE` prints traffic-chart geometry.

`TestBrowserPhase10` controls:

| Variable | Values and effect |
|---|---|
| `WG_TEST_UI_SUITE` | `10.2` (default), `10.3`, `10.3-dashboard`, `10.3-users`, `10.3-users-native` (standalone JavaScript-disabled User checks), `10.4`, `10.4-admin`, `10.4-backups`, `10.4-settings`, `10.5` (auth/public driver with a separate temporary first-run node), `10.6` (public compositions only), `appearance` (visual preset gallery), `final` (expanded viewport matrix, real-route states from temporary databases, interactions, touch, system theme and equivalent zoom; reserved for 10.7) |
| `WG_TEST_UI_GROUP` | `all` (default), `public`, `states` (seeds its own state cases outside `final`), `interactions`, `user-workspace` (Chromium/WebKit `/users` and user detail at 320/390/1440px), `calendar` (create-user page and drawer date picker), `user-template` (From template/Standard fields), `reseller-webhooks` (seeds a reseller webhook account), `next-plan` (seeds a 1 MB allowance and checks the queued-successor card; needs only this variable) |
| `WG_TEST_UI_PAGE` | limit composition reruns to one route |
| `WG_TEST_UI_STATE`, `WG_TEST_UI_STATE_FROM` | run one named state case, or resume an interrupted state matrix there |
| `WG_TEST_UI_STATE_MODE` | `preview` seeds and runs the state matrix outside the `final` suite |
| `WG_TEST_UI_PUBLIC` | one surface: `login`, `expired-login`, `limited-login`, `onboarding`, `not-found`, `subscription`, `unavailable-subscription` |
| `WG_TEST_SUB_LAYOUT` | sets `subscription.layout` before seeding; the public driver expects it (default `ticket`) |
| `WG_TEST_VISUAL_PRESET` | sets the installation preset; the public driver asserts it |

Native device/schedule checks use actual JavaScript-disabled contexts. Generated login credentials
travel only over stdin. Synthetic healthy telemetry stays fresh; stale data has its own case.
Virtual subscriber identities prevent matrix traffic from consuming one rate bucket; the production
limit and a dedicated limited-state case remain enforced. Run full acceptance with the filters
absent and a sufficient Go test timeout. These checks are not a complete panel, physical-device or
assistive-technology certification; emulation and axe imply no physical zoom or screen-reader use.

The asset script reports JS/CSS/fonts/SVG raw and gzip sizes without size ceilings; rendered HTML
and fragments are browser measurements. Investigate waste and loading/rendering regressions. Browser
geometry does not replace visual review, screen-reader use, real-device or live-TLS acceptance.

## Phase 11 certification

`go test -race ./...` runs under Linux; the dump parser has a bounded fuzz target. Use the
patched stable Go toolchain for `govulncheck@v1.7.0`: an older compiler's standard-library
advisories describe that compiler, not the candidate built with Go 1.27.2. The synthetic
`bench-idle.sh` seeds 0/100/1000 users and devices without API throttling, then measures a
ten-minute steady-state window; it does not stand in for kernel traffic. The integration-tag
1000-client-IP tc/IFB test exercises real kernel shaping in both directions, while the dedicated
Ubuntu VPS supplies separate deployment, actual peer/traffic, recovery and TLS evidence.


### Independent domains and HTTPS

`go test ./internal/domaintls ./internal/domainqueue ./internal/install ./internal/web
./internal/serve ./cmd/wg-guard` covers the domain-origin/TLS/host adapter boundary.
The real local HTTPS fixture establishes a resumable session, then retires its
name and checks existing requests and session-ticket handshake denial. Host CA
seams are synthetic, not a real issuance/renewal claim. Root lifecycle failure,
owner/lease, controlled source and queue publication checks belong to this boundary.

`TestBrowserDomains` (see [opt-in browser tests](#opt-in-browser-tests)) covers the domains panel;
there is no production browser runtime. See [Phase 18 evidence](../archive/development/phase18.md) and the Phase 20
physical gate.

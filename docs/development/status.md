# Product and verification status

What WG-Guard supports today and how each claim was verified. A feature is "verified" only at
the level a row names; unit, browser and real-host evidence are kept apart. Dated records of
earlier passes are in the [status history](../archive/development/status-history.md).

## Current release

**v0.2.0 (published 2026-10-19, Latest)** ships egress routing (Phase 21) and the design system
with subscription designs (Phase 22), with their follow-ups, built with Go 1.27.2. Exact source
`2a5255a` passed [main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/39297366895) and the
[release workflow](https://github.com/Sir-Adnan/wg-guard/actions/runs/39299359481). The seven public
assets were downloaded independently: all six checksums matched, the tag resolves to `2a5255a`,
and the binary reports v0.2.0 at that commit (SHA-256 `04ca895c…`; runtime image
`sha256:1a0018e2…`). A first attempt at `c3a628e` stopped at the release govulncheck gate on Go
1.27.1 standard-library advisories and published nothing. No registry image is published; the
[changelog](../../CHANGELOG.md) lists every change.

| Area | State |
|---|---|
| Host scope | Ubuntu 24.04 LTS amd64, Docker runtime, kernel and userspace AmneziaWG backends |
| Egress routing | Automated and namespace-tested; first real-host pass fixed three defects; client traffic through an outbound not yet re-verified |
| Design system and subscription designs | Go and local Chromium suites; WebKit, Firefox and physical phones unverified |
| Ubuntu 26.04 / kernel `7.0.0-38` | Module compile fix in bundle `awg-2026-10`; real-host load unverified ([runtime recovery](../archive/development/runtime-recovery.md)) |

## Capabilities

| Capability | Implemented and automated | Real-host / browser evidence | Current limit |
|---|---|---|---|
| Go/SQLite foundation, auth, encrypted secrets, reconciliation | Unit, integration and race coverage | Exercised in Docker recovery on Ubuntu 24.04 | Only documented deployment modes certified |
| Pinned AmneziaWG kernel and managed userspace backends | Config/apply/dump/drift and lifecycle tests | Kernel/userspace client HTTPS, reboot and recovery on Ubuntu 24.04 | arm64 and uncatalogued upstream builds unsupported |
| Users, devices, templates, quota/expiry, accounting and speed shaping | Service/API/web tests; 1000-class tc/IFB integration | Live client traffic and 1000-class shaping on dedicated VPS | 1000 simultaneous handshakes untested |
| REST API, scoped tokens, webhooks and OpenAPI | Contract, permission, pagination and delivery tests, including Phase 14 tenant fanout/receipt and multi-device purchase tests | Published OpenAPI served by real panel/API workflows | Direct purchase, quota top-up and multi-device provisioning have no new real-host claim |
| Egress routing: outbounds, balancers, rules, resolver | Plan/nft render, real-nft namespace transactions, conntrack netlink dump, resolver, store and web tests | First real-host pass (WARP registration); panel screenshots | Client traffic through an outbound not yet re-verified; see [egress routing](#egress-routing) |
| Subscription portal, app guide, `vpn://` and `/sub/{token}/info` | Web, JSON contract and AmneziaVPN key round-trip tests | Screenshots only | AmneziaVPN import reviewed from source, not run on a device |
| Complete bilingual panel and public subscription | Catalog parity, accessibility and browser suites | Chromium/Firefox/WebKit route/state/viewport matrix plus targeted real TLS workflows | Physical-device testing unavailable |
| Backup/restore, settings, administrators, audit and schedules | Atomic save, recovery, encryption and error-path tests | Real disk pressure, migration, rollback and backup drills | Long-interval ACME renewal unobserved |
| GitHub bootstrap, terminal manager, Docker lifecycle | Acquisition, integrity, rollback, interrupted-state tests | Fresh installs, update/rollback, reboot, data-preserving/full removal; public latest-release Docker install on Ubuntu 24.04 | Later Ubuntu and non-amd64 hosts unverified/unsupported |
| Firewall, forwarding and diagnostics | Owned-rule, fail-closed and Docker coexistence tests | Real Docker client public DNS/HTTPS; scoped Docker policy | Active firewalld refused; real-host UFW cell unverified |
| Metrics, charts and bounded logs | Telemetry, retention and redaction tests | Real client/load and browser presentation checks | Multi-day traffic soak unperformed |
| Security and resource certification | Full Linux race, 4.75M parser fuzz cases, reachable-vulnerability scan | 0-peer control sample: 33 MB RSS; 100/1000-user ten-minute windows: 41/52 MB average RSS and ≤0.04% CPU; recovery drills | Certification applies to listed Ubuntu 24.04 paths |

## Production compatibility

| Host | Kernel backend | Userspace backend | Status |
|---|---|---|---|
| Ubuntu 24.04 LTS amd64 (Docker) | Verified client and public traffic, reboot, upgraded-kernel DKMS | Verified managed daemon and recovery | Supported for the listed paths; active firewalld refused |
| Ubuntu newer than 24.04 amd64 | No genuine host | No genuine host | Unverified; no production claim |

Non-Ubuntu hosts and non-amd64 architectures are outside scope. WSL and container-only results
never upgrade a real-host cell. The certified host matrix, resource figures and failure drills
are in [Phase 20](../archive/development/phase20.md) and [Phase 11](../archive/development/phase11.md).

## Release-critical workflows

| Workflow | Verification level and lasting result |
|---|---|
| Profile creation and edit | Unit/integration tested across advanced values, default/generated presets, validation and clearing; supported generated subsets completed real kernel/client traffic. Uncatalogued parameters remain gated. |
| Client delivery | Direct, API, admin, public-subscription and QR outputs use the canonical config renderer; decoded QR bytes matched downloads and real clients connected. Physical optical camera testing was unavailable. |
| User and device changes | CRUD, limits, bulk actions, permissions, key rotation and failure recovery are automated-test covered; scoped real traffic and dashboard workflows passed. |
| Subscription access replacement | Automated atomic token/all-device key replacement and former-peer removal; the exact Phase 10 Docker gate proved the old URL returned 404, old peer disappeared and replacement access worked. |
| Settings and operator permissions | Submitted Settings saves are atomic with safe input redisplay; administrators, API tokens, webhooks, audit and backup permission states passed targeted tests and bilingual browser checks. |
| Backup and migration | Encrypted archive, preview, staged restore, missing-key refusal, data-preserving uninstall and full purge passed automated and exact Ubuntu 24.04 drills; a matching master key is required to recover encrypted device material. |
| Update and rollback | Source/release acquisition, independent manager refresh, exact component identity, interrupted update recovery and Docker rollback passed targeted and real-host tests; the public v0.1.0 bootstrap selected latest and installed the published asset on a real Docker host. |
| Responsive and accessible panel | Full inventoried route/state matrix passed Chromium, Firefox and WebKit across fa/en, light/dark and the documented viewport set; real physical-device testing was unavailable. |
| Host diagnostics | Doctor separates container AWG inspection from host network inspection, checks effective forwarding and fails closed on active firewalld; real healthy and repair cases passed on Ubuntu 24.04. |

## Egress routing

**Egress routing and subscription portal (2026-10-16; shipped in v0.2.0).** Kernel-native
outbounds (WARP, WireGuard/AmneziaWG import, source IP), balancers (`sticky`, `round_robin`,
`random`, `least_conn`, `failover`), ordered rules with presets, geosite/geoip and a built-in
DNS resolver are implemented; see [egress routing](../architecture/egress-routing.md) and
[ADR-0016](../decisions/ADR-0016-kernel-egress-routing.md).

| Level | Evidence |
|---|---|
| Unit / integration | Matcher, preset, config parser, store lifecycle, plan compiler, strategy rendering, least-connection shares, resolver learning/blocking, WARP request encoding, web CRUD/permissions and fa RTL tests |
| Namespace | `nft -f` transactions inside `unshare -rn`: table creation, incremental edit keeping resolver-fed timeout elements, teardown; conntrack mark-filtered netlink dump |
| Browser | Routing pages and subscription portal screenshots in fa/en, light/dark, 390 and 1440 px |
| Real host (first pass) | Ubuntu 24.04 Docker test node at `d87fe1f`: WARP registration succeeded, but `wgoN` creation failed because the userspace socket-name guard accepted only `awgN`, the failed link's route then failed the whole policy pass, and free WARP accounts were labeled WARP+. All three are fixed with regression tests; traffic is not yet re-verified |
| Second pass (2026-10-19, unit and namespace only) | Import parser against `amneziawg-tools` `ee0f0a9` key list and runtime constraints of `amneziawg-go` `b5928ef` and the kernel module `4569c4c`; kernel-to-userspace link fallback; sticky seed rendered and loaded by real `nft`; unreachable guard routes, apply retry, chained outbounds (`FwMark`), WARP renewal, HTTPS/SVCB blanking, UFW DNS rule and DNS release at shutdown |
| Third pass (2026-10-19, unit and namespace) | Site test detectors (RegionRestrictionCheck `b6d4a6f` markers), destination filtering, IPv6 site bridge (plan, IPv6 policy routes, gating, sniffing, relay and IPv4 fallback with fakes; redirect and QUIC refusal loaded by real `nft`; `SO_ORIGINAL_DST` read through a real nft redirect in a namespace), conntrack drain (the kernel mark filter removed only the marked flow in a namespace), endpoint re-resolution and WARP rotation retries with fakes, web form, card and site test page |
| Not verified | Real-host client traffic through `wgoN` (including chained links and the unreachable guard route), the IPv6 site bridge with real clients and WARP IPv6, site test verdicts against the live services, WARP renewal and rotation against the live API, AmneziaVPN import on a device (Android `vpn://`, iOS share sheet, desktop `.vpn` file), element-timeout refresh on older kernels, interaction with UFW/Docker forward chains under load |

## Design system redesign

**shadcn-anatomy design system and visual presets (2026-10-18; shipped in v0.2.0).** Every
panel, auth and public subscription surface is rebuilt on `web/styles`; 29 presets in `web/themes`
drive colors, fonts, radius, shadows, spacing and four layout axes; the subscription page has its
own owner default and a visitor choice. See the [design system](../product/design-system.md) and
[ADR-0017](../decisions/ADR-0017-design-system-and-presets.md).

| Level | Evidence |
|---|---|
| Unit / integration | Preset schema, value safety, WCAG AA contrast in both modes, CSS generation and import (`internal/web/theme`); appearance precedence, owner boundaries, subscription defaults and visitor cookie; every page's handler/markup tests; full `go test ./...`, `go vet ./...` and race tests for `internal/web/...`, `internal/i18n`, `internal/database` |
| Browser (Chromium, local) | 24 panel routes × 3 presets (WG-Guard, Linear, shadcn Zinc) × fa-light-390 / en-dark-390 / fa-dark-1440 with no horizontal overflow or console errors; per-area screenshots in fa/en, light/dark, 390/1440; live-preview checks. On the final tree every opt-in driver passed fresh: `TestBrowserFoundation`, `TestBrowserGuidanceLayout`, `TestBrowserBackupWorkbench`, `TestBrowserCleanup`, `TestBrowserDomains`, `TestBrowserMaintenance`, `TestBrowserOperationalJourney`, `TestBrowserPresentation`, `TestBrowserSystemHealth`, and `TestBrowserPhase10` suites `10.2`, `10.3`, `10.4`, `appearance` (every preset), `10.5` (auth/public, 56 cells; subscription at 320–1440 px for the pass, split, compact and portal layouts, since replaced by the four designs in the next row) and groups `states` (122 cells), `interactions` and `calendar` |
| Subscription designs (fresh) | Ticket, Membership card, Minimal and App designs with Connect / Setup guide / Details tabs: Go tests for the Appearance picker, owner-only sample preview, reset and the `0019` mapping; `TestBrowserPhase10` suite `10.5` (56 cells) and the subscription surface per design at 320–1440 px in fa/en, Light/Dark (tab switching, one-at-a-time guide accordion, isolated quantities, device-action sizing), plus suites `10.4` and `appearance` |
| Not verified | WebKit/Firefox, physical phones and touch hardware (coarse-pointer sizing is emulated), assistive technology, the `final` 16-width matrix and the axe scan (`WG_TEST_AXE` not installed) |

**Subscription device activity and design files (2026-10-18; shipped in v0.2.0).** Device
cards on the public subscription page show each device's usage and, while it is offline, when
it last connected. Each design's stylesheet lives at `web/styles/40-pages/sub-designs/<id>.css`.
CI now runs `python3 scripts/check-docs.py` to check links, anchors and named repository paths.

| Level | Evidence |
|---|---|
| Unit | `TestPublicSubscriptionDeviceActivity` (usage and last-connected rendering); `TestSubscriptionDesignsAreComplete` (every design ID has a hero template and a stylesheet bundled after `sub-base.css`) |
| CI | Docs check step in `.github/workflows/ci.yml` |
| Browser | Public subscription surface for all four designs at 320–1440px (local Chromium) |
| Not verified | WebKit/Firefox, physical phones and real hosts |

**Review fixes: panel security, REST contract and cleanup (2026-10-18; shipped in v0.2.0).**
Owner-account management is owner-only and delegated grants are capped at the granter; return
paths, flash text and the proxy visitor address are hardened; REST optional bodies, atomic
`PATCH /settings`, tag arrays, the bulk `start_index` default, webhook stats and replay
`Content-Type` match the contract; the OpenAPI document is complete; native remnants, dead code
and orphaned catalog strings are removed.

| Level | Evidence |
|---|---|
| Unit | `TestOwnerPasswordIsOwnerOnly`, `TestAdminGrantsAreCappedByTheGranter`, `TestSafeNext`, `TestVisitorIPTrustsOnlyTheManagedProxy`, `TestFlashTextNeedsServerSignature`, `TestSessionSurvivesTransientStorageError`, `TestLongPlanTimeMeterDoesNotOverflow` (`internal/web`); `TestOptionalBodiesAndDisableReason`, `TestSettingsPatchIsAtomic`, `TestUserTagsAreAlwaysAnArray`, `TestBulkCreateStartIndexDefaultsToOne`, `TestWebhookGetReportsDeliveryStats`, `TestIdempotentReplayKeepsContentType`, `TestOpenAPIDocumentIntegrity` (`internal/api`); `TestSanitizeURLValuesAndKeepTimes` (`internal/logsafe`) |
| Static | `deadcode -test` reports only an interface method; staticcheck clean apart from intentional Persian ZWNJ literals and proper-noun error strings |
| Browser | Interaction, states and maintenance suites after the `htmx:load` change (local Chromium) |
| Not verified | A real reverse-proxy deployment for the visitor-address change; real hosts |

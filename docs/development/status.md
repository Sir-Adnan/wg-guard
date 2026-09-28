# Product and verification status

This is the current capability matrix. A feature is not production-verified merely because it
builds, passes a unit test, or ran in WSL/container emulation. Detailed phase evidence remains in
the linked phase records; release blockers and audit findings live in
[release-readiness.md](release-readiness.md).

**Current gate (2026-09-28):** Phases 0–10 and the documented Ubuntu 24.04 amd64 scope of
Phase 11 are complete. Phase 12 release-candidate preparation is in progress. The owner's
panel spot-check is useful product feedback, but does not replace the final artifact/browser/VPS
gate. No public release has yet been verified.

| Capability | Implemented and automated | Real-host / browser evidence | Current limit |
|---|---|---|---|
| Go/SQLite foundation, auth, encrypted secrets, reconciliation | Unit, integration and race coverage | Exercised in Docker/native recovery on Ubuntu 24.04 | Only documented deployment modes certified |
| Pinned AmneziaWG kernel and managed userspace backends | Config/apply/dump/drift and lifecycle tests | Kernel/userspace client HTTPS, reboot and recovery on Ubuntu 24.04 | arm64 and uncatalogued upstream builds unsupported |
| Users, devices, plans, quota/expiry, accounting and speed shaping | Service/API/web tests; 1000-class tc/IFB integration | Live client traffic and 1000-class shaping on dedicated VPS | 1000 simultaneous handshakes untested |
| REST API, scoped tokens, webhooks and OpenAPI | Contract, permission, pagination and delivery tests | Exercised through exact-code panel/API workflows | Phase 12 final contract freeze pending |
| Complete bilingual panel and public subscription | Catalog parity, accessibility and browser suites | Chromium/Firefox/WebKit route/state/viewport matrix plus targeted real TLS workflows | Physical-device testing unavailable |
| Backup/restore, settings, administrators, audit and schedules | Atomic save, recovery, encryption and error-path tests | Real disk pressure, migration, rollback and backup drills | Long-interval ACME renewal unobserved |
| GitHub bootstrap, terminal manager, Docker/native lifecycle | Acquisition, integrity, rollback, interrupted-state tests | Fresh installs, update/rollback, reboot, data-preserving/full removal on Ubuntu 24.04 | Published release assets await Phase 12 |
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
| Update and rollback | Source/release acquisition, independent manager refresh, exact component identity, interrupted update recovery and native/Docker rollback passed targeted and real-host tests; published v0.1.0 artifact installation is still the Phase 12 gate. |
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
are in [phase11.md](phase11.md); Phase 12 must verify the exact release artifacts before
any public claim.

# Roadmap

WG-Guard is built in sequential, independently verifiable phases. Each phase ends with green
tests, synchronized living docs and a verification report that keeps implemented, tested and
real-host verified work apart. Support claims live in the [status matrix](docs/development/status.md);
release gates in [release readiness](docs/development/release-readiness.md). The scope and exit
evidence of each completed phase are in the [phase gate archive](docs/archive/roadmap-phases.md).

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
| **13 — Appearance and subscription follow-up** | Public QR/RTL fixes, responsive subscription redesign, ten source-reviewed visual presets (since replaced by 29 themes, ADR-0017) with personal/installation defaults | ✅ Complete; v0.1.1 published after exact-source CI/release gates; Firefox/Phase 13 real host unverified |
| **14 — Integration API for automation** | Isolated reseller accounts and owner/reseller integrations; recoverable provisioning, customer delivery, usage reset, queued successor plans and webhook contracts | ✅ Complete for documented scope; security in v0.1.2, integration in v0.1.3, template/direct-entitlement follow-up in v0.1.4 after exact-source CI and release gates |
| **15 — Operational safety and migration preparation** | Complete portable backup checks, pre-migration ordering, readiness and slow-job isolation | ✅ Complete; preparation release v0.1.9 published |
| **16 — Modular responsibility boundaries** | Shared application operations, desired/applied state, host/runtime/lifecycle separation and centralized paths | ✅ Complete; shipped in v0.1.10 |
| **17 — Docker-only distribution and native cleanup** | One image recipe, exact provenance, complete native lifecycle/flags/state/tests cleanup and independent host recovery | ✅ Complete; native removed; shipped in v0.1.10 |
| **18 — Integrated domains and HTTPS** | Panel/subscription domain roles, automatic/manual certificate management from panel and CLI, SNI and public-route isolation | ✅ Complete; HTTP-01 host-verified; shipped in v0.1.10 |
| **19 — Installer and operational UX** | Install-from-backup, common operation status/recovery, domain/TLS UI and terminal simplification | ✅ Complete; shipped in v0.1.10 |
| **20 — Refactor certification and publication** | Actual migrated restore/client traffic, reboot/renewal, resource and failure drills, exact artifact acceptance | ✅ Complete for Ubuntu 24.04 amd64 Docker scope; v0.1.10 published |
| **21 — Egress routing and client portal** | Kernel-native outbounds (WARP, WireGuard/AmneziaWG, source IP), balancers, ordered rules with presets/geodata and a routing resolver; subscription portal, app guide, AmneziaVPN `vpn://` and machine-readable info | 🚀 Shipped in v0.2.0; automated-tested; real-host verification pending ([status](docs/development/status.md#egress-routing)) |
| **22 — Design system and subscription designs** | shadcn-anatomy CSS design system, 29 themes, Appearance gallery and four selectable subscription page designs | 🚀 Shipped in v0.2.0; Go + Chromium browser suites; WebKit/Firefox/physical phones unverified ([status](docs/development/status.md#design-system-redesign)) |

## Next scope and open items

Phases 21–22 shipped in v0.2.0; nothing beyond them is scheduled. Each item needs its own owner
decision, scope and evidence; none is implied by a published support claim.

- **Egress routing on a real host:** client traffic through `wgoN` links (chained and failover
  included), the IPv6 site bridge with real clients and WARP IPv6, live site test verdicts, WARP
  renewal against the live API and AmneziaVPN import on devices
  ([status](docs/development/status.md#egress-routing)).
- **Ubuntu 26.04 / kernel `7.0.0-38`:** bundle `awg-2026-10` fixes the module build
  ([upstream issue 259](https://github.com/amnezia-vpn/amneziawg-linux-kernel-module/issues/259));
  loading it on a real host and a host acceptance pass are still needed
  ([runtime recovery](docs/archive/development/runtime-recovery.md)).
- **Unverified cells:** DNS-01 real issuance, external proxy and firewall-manager coexistence,
  active firewalld, physical phones, WebKit and Firefox for the v0.2.0 UI, 1000 simultaneous
  handshakes, a multi-day soak and an observed long-interval certificate renewal.
- **Distribution:** no official registry image; releases ship verified offline image assets.
- **Deferred products:** remote AmneziaWG nodes, native WireGuard, Xray/sing-box/OpenVPN and
  immediate plan replacement after activation
  ([refactor program](docs/archive/development/refactor-program.md#deferred-work)).

## Verification policy

Nothing is done unless [docs/development/status.md](docs/development/status.md) records how it was
verified. WSL2 and containers do not count as real kernel/architecture verification. A planned or
implemented item is not described as production verified until its relevant real-host matrix cell
has evidence.

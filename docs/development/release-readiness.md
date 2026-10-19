# Release readiness

How a WG-Guard version becomes public, what blocks the next one, and which versions are out.
The [status matrix](status.md) owns support claims. Closed Phase 8–20 blockers, audit findings
and per-release records are in the [release history](../archive/development/release-readiness-history.md).

## Release gates

A version is published only when all of these hold for one exact `main` commit:

1. The owner has authorized that version (each authorization covers one version; registry images
   need their own).
2. [CI](../../.github/workflows/ci.yml) passed on that commit.
3. The [release workflow](../../.github/workflows/release.yml) passed on that commit: source gates,
   artifact build and checksums, binary and runtime-image identity, attestations, and the
   downloaded draft check.
4. The public assets were downloaded again and their checksums, tag→commit and attestations
   verified.
5. The [changelog](../../CHANGELOG.md), [status](status.md) and this page record the release.

## Open before a broader claim

No release blocker is open. These cells stay unverified and are not implied by any release:

| Area | Missing evidence |
|---|---|
| Egress routing | Real-host client traffic through outbounds (chained, failover), IPv6 site bridge with real clients and WARP IPv6, live site test verdicts, WARP renewal against the live API ([status](status.md#egress-routing)) |
| Client apps | AmneziaVPN import on devices; physical phones; WebKit and Firefox for the v0.2 UI ([status](status.md#design-system-redesign)) |
| Hosts | Ubuntu newer than 24.04 (including kernel `7.0.0-38` module load), active firewalld, non-amd64 |
| Scale and time | 1000 simultaneous handshakes, multi-day soak, an observed long-interval certificate renewal, DNS-01 real issuance |

## Finding a defect

1. Reproduce it or cite concrete evidence; never paste secrets or config contents.
2. Record impact, severity and the affected surface.
3. Add a regression test with the fix, and real-host evidence when the claim needs it.
4. Update the status matrix and the behavior docs in the same change.

A blocker closes only with linked evidence, or with an explicit owner waiver that records the
residual risk.

## Published releases

| Version | Date | Exact source | Highlights |
|---|---|---|---|
| [v0.2.1](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.2.1) | 2026-10-19 | `3471f97` | Tests tab with IP version choice, speed and bulk outbound tests, Claude and custom-URL verdict fixes; Outbounds and Rules redesign |
| [v0.2.0](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.2.0) | 2026-10-19 | `2a5255a` | Egress routing, IPv6 site bridge and site test; design system, 29 themes, four subscription designs |
| [v0.1.11](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.11) | 2026-10-08 | `228e2a5` | Containerd image-store admission; bundle `awg-2026-10` |
| [v0.1.10](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.10) | 2026-10-06 | `4f78adc` | Docker-only runtime, domains and TLS, installer UX (Phases 16–20) |
| [v0.1.9](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.9) | 2026-10-04 | `dd034fe` | Backup verification and migration preparation (Phase 15) |
| [v0.1.8](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.8) | 2026-10-04 | `cf718a9` | Account and address maintenance, combined cleanup |
| [v0.1.7](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.7) | 2026-10-03 | `f166360` | Installer progress, multi-device provisioning, webhook key check |
| [v0.1.6](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.6) | 2026-10-01 | `ca0003e` | Backup and startup correction |
| [v0.1.5](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.5) | 2026-10-01 | `0586931` | Maintenance release |
| [v0.1.4](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.4) | 2026-09-30 | `314c94b` | Templates and direct entitlements for integrations |
| [v0.1.3](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.3) | 2026-09-29 | `4177b28` | Integration API (Phase 14) |
| [v0.1.2](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.2) | 2026-09-29 | `6eaa5ac` | Security update |
| [v0.1.1](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.1) | 2026-09-28 | `91f0cad` | Appearance and subscription follow-up (Phase 13) |
| [v0.1.0](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.0) | 2026-09-28 | `fb38c1f` | First public release (Phase 12) |

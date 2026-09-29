# Changelog

## [Unreleased]

- Correct mobile `.conf` downloads and shorten stable device filenames for VPN-client import.
- Add three selectable public subscription layouts with responsive phone and desktop compositions.
- Keep the public theme menu aligned and visible in RTL/LTR on phones and desktops; soften shared
  button shadows and touch hover treatment.

## [v0.1.1] — 2026-09-28

This update refines the bilingual web panel; the supported Ubuntu 24.04 amd64 target and
the `/api/v1` behavior remain unchanged.

- Add ten selectable visual presets with Light/Dark variants, personal choices, and an
  owner-controlled panel default. Preset colors, typography and shared components use a central
  token registry; accessibility corrections preserve readable controls and status text.
- Redesign the public subscription page for desktop and mobile. Correct RTL usage/transfer
  presentation and hide the obsolete single-device QR state when viewing all device codes.
- Self-host the optional Latin preset fonts and include their licenses. Clarify current API
  automation guarantees and record future integration work without introducing new endpoints.

## [v0.1.0] — 2026-09-28

The first public WG-Guard release provides a self-hosted AmneziaWG node panel for the
**real-host-verified Ubuntu 24.04 LTS amd64** target.

### Product

- Complete bilingual Persian/English web panel with responsive desktop/mobile layouts,
  RTL/LTR, light/dark/system themes, accessible workflows, and a public subscription page.
- Interface and advanced-profile management, plans, users, devices, per-device config/QR
  delivery, quotas, expiry, speed shaping, and subscription access replacement.
- Operational dashboard, node/AWG telemetry, traffic history, health diagnostics, and
  bounded logs.
- Scoped REST API, OpenAPI, signed webhooks, audit log, administrator permissions,
  encrypted backup/restore, schedules, and Telegram delivery.

### Operations and assurance

- Verified GitHub release acquisition, English terminal manager, Docker and native
  installation, HTTPS options, update/rollback, recovery, and owned uninstall.
- Pinned AmneziaWG tools, kernel module and managed userspace daemon; Docker forwarding
  coexistence and fail-closed unsupported firewalld policy.
- Production certification includes real kernel/userspace client traffic, reboot and
  recovery drills, resource and 1000-class shaping checks, race/fuzz/security gates.
  Release assets include checksums, metadata, notices and an SPDX Go-module inventory.

### Support boundary

Ubuntu 24.04 amd64 is the verified target. Later Ubuntu releases, active firewalld,
real-host UFW coexistence, 1000 simultaneous handshakes, a multi-day traffic soak,
and long-interval ACME renewal are **not** certified. No arm64 or non-Ubuntu claim is made.

The [documentation map](docs/README.md) and [certification record](docs/development/phase11.md)
hold details and verification limits.

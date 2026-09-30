# Changelog

## [Unreleased]

## [v0.1.4] — 2026-09-30

- Correct create-user and bulk-create forms so selecting a technical template applies its saved
  quota, duration, activation policy, device/speed limits and connection profile. Show the
  template choice before manual terms, with a responsive preview and concise form states.
- Name the pre-installation technical catalog `templates`: the panel/REST route is `/templates`,
  the wire key is `template_id`, the scopes are `templates.read/write`, and the fresh SQLite
  schema uses `templates` and `reseller_template_access`. No plan-named compatibility API remains.
- Let an owner-scoped integration atomically purchase with explicit finite entitlement terms
  instead of creating one template per external SKU. Reseller-bound tokens still require an
  owner-assigned template; pricing and product catalogs stay in the bot/storefront.
- Add a recoverable, owner/reseller-scoped quota top-up API. The allowance, webhook event and
  non-secret before/after operation result commit together; retries cannot charge the same
  entitlement twice. Charged usage and the existing `traffic/add` contract remain unchanged.
- Make the panel's Add data action increase finite allowance, expose Reset Usage and renewal
  from the user list/detail, and compact the bilingual mobile user workspace.
- Refresh API-token and reseller permission shortcuts for current integration scopes and add
  concise bot/webhook setup guidance in the panel and documentation.
- Keep the Jalali/Gregorian date picker open while changing months, including inside the
  mobile create-user drawer in WebKit, and disable navigation to unavailable past months.

Automated service/API/web tests and focused Chromium/WebKit browser checks cover these changes.
Physical-device and new real-host verification are not claimed for this release.

## [v0.1.3] — 2026-09-29

- Add owner-managed reseller accounts with live grant ceilings, isolated customer views, scoped
  API tokens, assigned plans and reseller webhook management.
- Add an atomic purchase operation that creates a user, first device and customer link together;
  committed results can be recovered for 90 days by the same owner or reseller identity. Add
  scoped customer-link retrieval and rotation of the link plus every device key.
- Add independent Reset Usage and one queued Next Plan per customer, with frozen terms,
  first-boundary activation, optional unused-volume carry on time expiry, and activation history.
- Publish typed webhook payloads, tenant-safe fanout with owner-only opt-in for reseller events,
  public-HTTPS egress for reseller receivers, non-secret paginated delivery receipts and explicit
  at-least-once/ordering guarantees. Existing endpoints do not gain reseller events on upgrade.
- Correct mobile `.conf` downloads and shorten stable device filenames for VPN-client import.
- Add three selectable public subscription layouts with responsive phone and desktop compositions.
- Keep the public theme menu aligned and visible in RTL/LTR on phones and desktops; soften shared
  button shadows and touch hover treatment.

The previous v0.1.2 security correction remains included. The support target is Ubuntu 24.04
amd64; new physical-device and real-host verification is not claimed for this revision.

## [v0.1.2] — 2026-09-29

Security update for the V1 API. Idempotent responses now pass token authentication and live
authorization before replay; keys are isolated per token, and ambiguous legacy entries fail
closed. This prevents a matching unauthenticated, revoked, or differently scoped request from
receiving a stored response. No Phase 14 reseller or purchase API work is included.

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

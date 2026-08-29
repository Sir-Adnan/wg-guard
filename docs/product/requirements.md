# Product requirements

WG-Guard is a lightweight, self-hosted **AmneziaWG VPN node management panel** for Linux VPS
servers. Each server runs its own independent node. External systems (Telegram bots, billing
systems, Guardinohub-like platforms, custom panels) manage the node through its REST API.

Source of historical truth: the original specification (archived at
[../archive/wg-guard_SPEC.md](../archive/wg-guard_SPEC.md)); this document is the distilled,
current product contract.

## Goals (in priority order)

1. Correctness — accounting, quotas, and lifecycle never lie.
2. Security — secure defaults, least privilege, honest threat model.
3. Extremely low RAM and idle CPU — the VPN traffic gets the server's resources.
4. Maintainability — clean, modular, idiomatic Go; focused docs.
5. API stability — `/api/v1` is a contract from V1.
6. Premium UI/UX — bilingual (fa/en), full RTL, light/dark, excellent on mobile and desktop.
7. Frontend performance — server-rendered, tiny payloads, no heavy runtime.
8. Ease of installation — polished interactive installer, Docker default.

## Non-goals

- No multi-node controller, reseller system, or centralized management.
- No redesign of WireGuard cryptography or custom obfuscation schemes.
- No PostgreSQL/MySQL/Redis/RabbitMQ/Node.js runtime/nginx requirement.
- No auto-updates; updates are administrator-initiated.
- Backup/restore is administrative (panel + CLI), not a REST API surface.

## Core model

- **User** — a subscription owner. Fields: id (UUIDv7), username (unique), display name, note,
  tags, status, disable reason, traffic limit/used, speed limit, device limit, obfuscation
  profile (→ tunnel interface), start policy, duration, activated_at, expires_at, last activity.
- **Device** — one VPN peer per device, never a shared key. Fields: id, user, name, interface,
  VPN IP (unique per interface), public key, encrypted private/preshared keys, status, last
  handshake, last endpoint, rx/tx (accumulated), counters for delta accounting.
- **Plan** — reusable preset (quota, duration, start policy, device limit, speed limit,
  profile). Users do not need a plan; API clients may pass limits directly.
- **Tunnel interface / profile** — `awg0…awg7` (8 by default; the cap is administrator-
  configurable, not an upstream limit); each = one obfuscation profile with its own listen port,
  IPv4 subnet pool (recommended default `10.8.N.0/24` for `awgN`), MTU (recommended default
  1420), endpoint override. Ports, MTU, subnet pools, client DNS, and the interface count are
  **recommended configurable defaults** chosen from upstream constraints — not verified optima;
  final guidance follows the Phase 8 VPS matrix. One profile per interface is an upstream
  constraint (obfuscation params live in `[Interface]` and are shared by all peers). Changing a
  profile's params is a guided **rotation** workflow.

## Lifecycle

Statuses: `active`, `disabled`, `suspended`, `expired`, `traffic_exceeded`,
`waiting_first_connection`. Disable reasons: `manual`, `expired`, `traffic_limit`,
`admin_action`.

- Start policy **immediate**: subscription starts at creation.
- Start policy **first_connection**: duration starts at the first valid handshake; must survive
  restarts (persisted `activated_at`).
- Expiration enforcement and quota enforcement run on the internal scheduler (no external cron).
- Quota exhaustion sets `traffic_exceeded`, preserves the account, and emits an audit event.
- Renewal: extend from current expiration, extend from now, or set exact date.
- Operations: create/edit/enable/disable/suspend/delete (soft delete + restore)/renew/clone,
  reset/add/remove traffic, change quota/duration/devices/speed/profile, regenerate and revoke
  device configs. Bulk create (10–100+ with shared properties), bulk actions, export (CSV/ZIP).

## Accounting

Delta-based, per-device RX/TX/total; quota applies to RX+TX by default. AWG counters reset on
restarts/recreates — accumulated usage lives in SQLite; a counter decrease is a reset
(re-baseline), never a negative delta. One dump per interface per cycle (default 30 s), one
transaction per cycle. Raw `latest-handshake` is the authoritative activity value; "online" is
derived (handshake within a configurable window, default 3 min).

## Roles & API

- **Owner** (full access, immutable) + **Admins** with centrally registered permissions
  (users.*, devices.*, configs.view, plans.manage, stats.view, audit.view, api_tokens.manage,
  webhooks.manage, server.view/manage, backup.manage, update.manage, admins.manage).
- **API tokens** separate from admin sessions: `wg_…`, hashed at rest, scopes, optional CIDR
  allowlist, expiry, revocation.
- The web panel and REST API share one business layer — no duplicated logic.

## Bilingual product

Persian (**default**) and English; full RTL; light (default) and dark themes. See
[ui-ux.md](ui-ux.md).

## Backups

Manual, scheduled, Telegram delivery, retention; **optional** single backup password (age
format); restore with environment review for server migration. Administrative surface:
panel + CLI. See [../operations/backup-restore.md](../operations/backup-restore.md).

# Project: WG-Guard

You are a senior Go, Linux networking, VPN, security, backend, API, and lightweight web application engineer.

Build a production-quality open-source project named **WG-Guard**.

WG-Guard is a lightweight, self-hosted VPN node management panel installed independently on each Linux VPS.

The project should feel as lightweight and easy to install as projects such as wg-easy or WGDashboard, but provide significantly more professional user management, traffic quota, expiration, API, automation, and anti-DPI capabilities.

The primary tunnel engine must be **AmneziaWG-compatible**, not plain WireGuard.

Do NOT redesign WireGuard cryptography or implement a new VPN cryptographic protocol.

Use the existing AmneziaWG ecosystem/engine for WireGuard-compatible anti-DPI/traffic-obfuscation capabilities.

Before writing code, inspect the repository, produce a concise implementation plan and architecture, identify risks and dependencies, and then implement the project incrementally.

## How to use this specification

This file is the product and engineering source of truth for WG-Guard.

When an AI coding agent receives this specification, it must NOT interpret it as permission to generate the entire project in one uncontrolled pass. The required workflow is:

1. Inspect the current repository and environment.
2. Validate assumptions that depend on upstream AmneziaWG behavior, supported platforms, licenses, and tooling.
3. Produce the Initial Deliverable defined later in this specification.
4. Identify contradictions, unknowns, security risks, and implementation choices that need approval.
5. Wait for explicit approval of the architecture before implementation.
6. Implement one approved phase at a time.
7. Keep the repository buildable and tested after each phase.
8. Do not silently change product decisions in this specification.

If an upstream behavior is uncertain, do not guess. Inspect the pinned upstream source/documentation or clearly mark the item as unresolved before implementing it.

---

# 1. Main goals

WG-Guard must be:

- Extremely lightweight
- Low RAM usage
- Low idle CPU usage
- Easy to maintain
- Easy to install
- Easy to update
- Suitable for small VPS servers
- Suitable for hundreds or thousands of VPN peers
- API-first
- Secure by default
- Friendly for external management systems
- Modern but lightweight UI
- Single-node by design

Every server runs its own independent WG-Guard installation.

External systems such as:

- Telegram bots
- VPN sales panels
- Central management systems
- Guardinohub-like platforms
- Custom billing systems

must be able to add WG-Guard as a node and manage it completely through its REST API.

WG-Guard itself does NOT need reseller functionality or a centralized multi-node controller.

---

# 2. Technology constraints

Prefer:

- Go
- SQLite
- net/http or Chi
- server-rendered HTML
- HTMX
- Alpine.js only where useful
- lightweight CSS
- Go embed for frontend assets
- systemd
- nftables
- Linux tc
- AmneziaWG

Avoid unnecessary runtime dependencies.

DO NOT require:

- Node.js runtime
- Next.js
- React SPA
- PostgreSQL
- MySQL
- Redis
- RabbitMQ
- Celery
- Docker
- Kubernetes
- nginx

unless there is an extremely strong technical reason.

The default installation must work as:

WG-Guard binary

- SQLite database
- AmneziaWG
- Linux networking facilities

Frontend assets may be built during development if necessary, but production installation should NOT require Node.js.

Prefer a single WG-Guard process containing:

- HTTP server
- Web UI
- REST API
- authentication
- authorization
- scheduler
- quota manager
- traffic accounting
- webhook dispatcher
- AWG management

---

# 3. Supported Linux systems

Target initially:

- Ubuntu 22.04 LTS
- Ubuntu 24.04 LTS
- Debian 12

Architect the installer so more distributions can be added later.

Support at minimum:

- amd64
- arm64

---

# 4. Tunnel architecture

The primary tunnel backend should use AmneziaWG-compatible tooling/runtime.

Do NOT modify WireGuard cryptography.

Do NOT invent custom cryptographic algorithms.

Do NOT fork and rewrite the WireGuard kernel implementation unless absolutely required.

Create a clean internal tunnel abstraction.

Example:

TunnelBackend

with an AmneziaWG implementation.

The architecture should allow adding another backend in the future without rewriting user/accounting/business logic.

For example:

internal/tunnel/
backend.go
amneziawg/

The rest of WG-Guard should not depend directly on shell commands or AWG implementation details.

Do not assume AmneziaWG command names, config fields, kernel interfaces, or client compatibility from memory. During Phase 0, inspect and pin the exact upstream version/integration method that WG-Guard will support. Record the tested upstream version and capabilities. If a desired anti-DPI capability is not supported by the pinned upstream version, report it rather than silently emulating or inventing protocol behavior.

---

# 5. Anti-DPI configuration

WG-Guard should expose user-friendly obfuscation profiles instead of forcing normal administrators to manually edit low-level AmneziaWG parameters.

Profiles:

- Automatic
- Balanced
- Strong
- Custom

Advanced/custom settings may expose AmneziaWG-specific parameters when appropriate.

Low-level obfuscation settings must be isolated inside the tunnel backend.

Do not hard-code one globally identical fingerprint/profile for every installation if AmneziaWG supports safely varying parameters.

Configuration generation must remain compatible with appropriate AmneziaWG clients.

Clearly document client compatibility.

---

# 6. Core user model

WG-Guard manages logical users.

A user may contain one or more devices.

Each device must receive its own VPN peer/key.

Never implement device limits by encouraging multiple devices to reuse the same private key.

Conceptually:

User
├── Subscription / limits
├── Device 1 -> AWG peer
├── Device 2 -> AWG peer
└── Device N -> AWG peer

Each user should have:

- UUID/internal ID
- username
- optional display name
- note
- tags if useful
- status
- created_at
- updated_at
- activated_at
- expires_at
- duration
- start policy
- traffic limit
- traffic used
- speed limit
- device limit
- obfuscation profile
- enabled/disabled state
- disable reason
- last activity
- metadata where useful

Use stable internal IDs rather than usernames as database identity.

---

# 7. User lifecycle

Support:

- Create user
- Edit user
- Enable user
- Disable user
- Suspend user
- Delete user
- Soft delete where useful
- Restore user
- Renew user
- Clone user
- Reset traffic
- Add traffic
- Remove traffic
- Set traffic limit
- Set unlimited traffic
- Change expiration
- Extend expiration
- Change duration
- Change device limit
- Change speed
- Change anti-DPI profile
- Regenerate device configuration
- Revoke device

Provide clear status values such as:

- active
- disabled
- suspended
- expired
- traffic_exceeded
- waiting_first_connection

Store the reason a user became disabled.

Example:

manual
expired
traffic_limit
admin_action

---

# 8. Subscription start policy

Support at minimum:

### Immediate

The subscription starts as soon as the user is created.

### First connection

The subscription duration begins after the first valid VPN connection/handshake.

Example:

30-day subscription created today.

The user connects 5 days later.

Expiration should be calculated 30 days from first activation, not creation.

This must work reliably across service restarts.

---

# 9. Expiration management

Support:

- unlimited
- custom duration
- exact expiration time
- minutes/hours if needed
- days
- weeks
- months

Automatic expiration enforcement must happen without external cron.

Use an internal lightweight scheduler.

Expired users must be disabled automatically.

Renewal should support:

- extend from current expiration
- extend from now
- set exact expiration

Expose remaining duration in UI and API.

---

# 10. Traffic accounting

Implement reliable per-user and per-device traffic accounting.

Track:

- RX
- TX
- total traffic

Traffic limit normally applies to:

RX + TX

Do not assume raw AWG/WireGuard counters are lifetime persistent.

Counters may reset when:

- interface restarts
- peers are recreated
- service restarts
- system reboots

Implement delta-based accounting and persist accumulated usage.

The accounting design must prevent:

- negative deltas
- double counting
- usage loss after restart
- counter-reset corruption

Avoid excessive SQLite writes.

Use safe aggregation/batching where appropriate.

Provide:

- total quota
- used traffic
- remaining traffic
- unlimited mode
- percentage used

When quota reaches its limit:

- disable access automatically
- preserve user/account
- set status to traffic_exceeded
- record audit event

Allow administrator to:

- reset usage
- add traffic
- set a new quota
- make unlimited

---

# 11. Bulk user creation

Bulk creation is essential.

Allow creating:

- 10 users
- 50 users
- 100 users
- custom number

with shared properties such as:

- username prefix
- random username option
- duration
- traffic quota
- device limit
- speed limit
- obfuscation profile
- activation policy

Example generated names:

gs-001
gs-002
gs-003

Support export of bulk-created users.

Possible formats:

- CSV
- ZIP containing configs
- structured JSON via API

Bulk actions should support:

- enable
- disable
- delete
- renew
- add traffic
- reset traffic
- change duration
- change quota
- change speed
- change device limit

Make bulk operations transactional/safe where appropriate.

---

# 12. Device management

Each logical device = separate VPN peer.

Device fields should include:

- ID
- user ID
- name
- VPN internal IP
- public key
- encrypted/private configuration handling as appropriate
- status
- last handshake
- last endpoint
- RX
- TX
- created_at
- updated_at

Operations:

- Create device
- Rename device
- Enable device
- Disable device
- Delete/revoke device
- Regenerate configuration
- Get config
- Get QR
- View last connection

Enforce per-user device limit.

Return a machine-readable error such as:

DEVICE_LIMIT_REACHED

when the limit is exceeded.

---

# 13. Speed limiting

Do not modify AmneziaWG to implement bandwidth limiting.

Use Linux networking mechanisms such as:

- tc
- nftables where appropriate

Support:

- unlimited
- preset speed limits
- custom Mbps

Prefer a design that can later support separate upload/download limits.

Rules must be:

- deterministic
- recoverable after restart
- automatically rebuilt when necessary
- cleaned up when a user/device is deleted

Networking changes must not accidentally lock administrators out of the server.

---

# 14. Plans

Support lightweight reusable plans.

A plan may contain:

- name
- traffic quota
- duration
- start policy
- device limit
- speed limit
- obfuscation profile
- enabled state

Users must NOT be required to use a plan.

External API clients must be allowed to create users by providing limits directly.

This prevents external panels from needing to duplicate/synchronize their own plans with WG-Guard.

---

# 15. Web dashboard

Build a modern, minimal, fast management UI.

The UI should feel polished but remain lightweight.

Do not create a heavy SPA.

Use server-side rendering + HTMX where suitable.

Main sections:

- Dashboard
- Users
- Plans
- Administrators
- API Tokens
- Webhooks
- Server/Node
- Settings
- Audit Logs
- Backup
- Update/About

Dashboard should show:

- total users
- active users
- online/recently active users
- expired users
- traffic-exceeded users
- users expiring soon
- total traffic
- service status
- AmneziaWG status
- basic CPU/RAM/disk/network information

WireGuard/AmneziaWG does not provide a traditional connected session state. Any UI/API field described as "online" must be defined as a derived "recently active" state based on a documented last-handshake/activity threshold. Expose the raw last-handshake timestamp as the authoritative value.

Avoid excessive live polling.

---

# 16. User table UX

The users page is one of the most important screens.

Support:

- pagination
- search
- filter
- sorting
- multi-select
- bulk actions

Useful columns:

- username
- status
- traffic
- remaining traffic
- expiration
- remaining time
- devices
- speed
- last handshake
- created at

Quick actions:

- edit
- renew
- add traffic
- config
- QR
- disable
- delete

Use confirmation dialogs for destructive actions.

---

# 17. Administrators and permissions

No reseller system is required.

Roles:

### Owner

Full access.

Owner permissions cannot accidentally be removed.

### Admin

Custom selectable permissions.

Examples:

- users.view
- users.create
- users.update
- users.delete
- users.enable
- users.disable
- users.renew
- users.bulk
- users.traffic
- devices.view
- devices.create
- devices.delete
- configs.view
- plans.view
- plans.manage
- stats.view
- audit.view
- api_tokens.manage
- webhooks.manage
- server.view
- server.manage
- backup.manage
- update.manage
- admins.manage

Design permissions centrally rather than scattering authorization checks.

All privileged operations must be authorized server-side.

Never rely on UI hiding for authorization.

---

# 18. Authentication

Implement secure administrator authentication.

Requirements:

- modern password hashing
- secure sessions
- CSRF protection
- secure cookies
- SameSite policy
- login rate limiting
- brute-force protection
- session expiration
- logout
- password change
- audit login activity

Optional architecture for future TOTP/2FA is welcome, but keep V1 simple.

Never store plaintext admin passwords.

## 18.1 HTTPS, TLS, and network exposure

Production access to the web dashboard and public REST API must use HTTPS. Plain HTTP must not be the recommended internet-facing deployment mode.

WG-Guard should support lightweight deployment modes without requiring nginx:

- built-in HTTPS with administrator-provided certificate/key
- built-in ACME/Let's Encrypt when a valid domain and networking prerequisites are available
- HTTP bound to loopback/private interface when an administrator intentionally uses an external reverse proxy
- explicit development/insecure mode only for local testing

The installer must explain the selected exposure mode and must not silently expose an unauthenticated or plaintext management API to the public internet.

API tokens may optionally be restricted by IP/CIDR allowlists. Management endpoints and sensitive configuration endpoints must have appropriate security headers, no-store caching policy where needed, request size limits, and rate limits.

Do not require a second long-running web server merely to provide TLS if the Go process can safely provide the requested mode.

---

# 19. API-first architecture

REST API is a first-class feature.

Base path:

/api/v1

The web panel and REST API must use the same internal service/business layer.

Do NOT duplicate business logic.

Concept:

Web UI
\
 UserService
/
REST API

External systems must be able to completely manage users without using the web dashboard.

---

# 20. API authentication

Use dedicated service API tokens.

Human administrator credentials must NOT be reused by external systems.

Token example concept:

wg_xxxxxxxxxxxxxxxxx

Store only securely hashed token values where possible.

Each token should support:

- name
- permissions/scopes
- created_at
- last_used_at
- expires_at
- enabled
- optional allowed IP/CIDR list

Token permissions should be separate from administrator sessions.

Example scopes:

- users.read
- users.create
- users.update
- users.delete
- users.bulk
- devices.read
- devices.create
- devices.update
- devices.delete
- configs.read
- traffic.read
- traffic.update
- plans.read
- plans.write
- stats.read
- node.read
- node.settings
- webhooks.read
- webhooks.write

---

# 21. Core REST endpoints

Implement clean RESTful APIs.

At minimum:

## Node

GET /api/v1/node
GET /api/v1/node/health
GET /api/v1/node/stats

## Users

POST /api/v1/users
GET /api/v1/users
GET /api/v1/users/{id}
PATCH /api/v1/users/{id}
DELETE /api/v1/users/{id}

POST /api/v1/users/{id}/enable
POST /api/v1/users/{id}/disable
POST /api/v1/users/{id}/renew

POST /api/v1/users/{id}/traffic/add
POST /api/v1/users/{id}/traffic/set
POST /api/v1/users/{id}/traffic/reset

## Bulk users

POST /api/v1/users/bulk

Provide bulk-action endpoints using a consistent design.

## Devices

GET /api/v1/users/{id}/devices
POST /api/v1/users/{id}/devices

GET /api/v1/devices/{id}
PATCH /api/v1/devices/{id}
DELETE /api/v1/devices/{id}

POST /api/v1/devices/{id}/enable
POST /api/v1/devices/{id}/disable
POST /api/v1/devices/{id}/regenerate

## Config

GET /api/v1/devices/{id}/config
GET /api/v1/devices/{id}/qr

## Stats

GET /api/v1/stats
GET /api/v1/users/{id}/stats
GET /api/v1/devices/{id}/stats

## Plans

GET /api/v1/plans
POST /api/v1/plans
PATCH /api/v1/plans/{id}
DELETE /api/v1/plans/{id}

Endpoint naming may be improved if you can create a cleaner consistent REST design.

Document deviations.

---

# 22. API pagination

Never return an unlimited number of records.

Use cursor pagination when appropriate.

Example:

GET /api/v1/users?limit=100&cursor=...

Support useful filters:

- username
- status
- expires_before
- expires_after
- traffic_exceeded
- enabled
- created_before
- created_after

Support deterministic sorting.

---

# 23. API errors

All API errors must use a consistent schema.

Example:

```json
{
  "error": {
    "code": "DEVICE_LIMIT_REACHED",
    "message": "Maximum device limit has been reached.",
    "request_id": "req_..."
  }
}
```

Create stable machine-readable error codes.

Examples:

- USER_NOT_FOUND
- USERNAME_EXISTS
- USER_DISABLED
- USER_EXPIRED
- TRAFFIC_EXCEEDED
- DEVICE_LIMIT_REACHED
- INVALID_REQUEST
- UNAUTHORIZED
- FORBIDDEN
- RATE_LIMITED
- NODE_UNAVAILABLE
- INTERNAL_ERROR

Never expose internal stack traces to API clients.

---

# 24. Idempotency

Support idempotency for important mutating API operations.

Use:

Idempotency-Key

At minimum for:

- create user
- bulk create
- renew
- add traffic
- payments-like quota modifications if added later

Retries from Telegram bots or external management panels must not accidentally create duplicate users or add quota twice.

Persist idempotency safely with expiration/cleanup.

---

# 25. Webhooks

Implement lightweight outbound webhooks.

Events should include useful cases such as:

- user.created
- user.updated
- user.enabled
- user.disabled
- user.expired
- user.traffic_exceeded
- user.first_connected
- device.created
- device.deleted
- node.started

Webhook configuration:

- URL
- secret
- enabled
- selected events

Sign webhook requests using a strong HMAC scheme.

Include:

- event ID
- event type
- timestamp
- payload
- signature

Implement retries with sensible backoff.

Do NOT require Redis/RabbitMQ.

Store pending deliveries/retry state in SQLite.

Prevent one broken webhook endpoint from blocking the main WG-Guard process.

---

# 26. OpenAPI

Produce complete OpenAPI documentation.

Expose:

/openapi.json

and preferably:

/docs

The OpenAPI schema must accurately represent actual implementation.

Include:

- authentication
- scopes
- examples
- request schemas
- response schemas
- error schemas
- pagination
- idempotency
- webhook schemas

External developers should be able to integrate WG-Guard without reading source code.

Treat `/api/v1` as a compatibility contract. Within v1, additive changes are preferred. Do not rename fields, change meanings, remove enum values, or make previously optional fields required without a versioning/migration strategy. Keep OpenAPI examples and actual handlers synchronized through tests where practical.

---

# 27. Node integration

The following should be enough for an external management system to add a node:

Node URL:
https://node.example.com

API Token:
wg_xxxxxxxxx

The external system should be able to call:

GET /api/v1/node/health

and receive useful capability/version information.

Include fields such as:

- node ID
- WG-Guard version
- API version
- status
- tunnel backend
- supported capabilities

Example capability information:

users
bulk_users
traffic_quota
expiration
device_limit
speed_limit
amneziawg
webhooks

Design capability discovery so newer WG-Guard versions remain easier to integrate.

---

# 28. QR and configuration generation

Allow:

- display QR in dashboard
- download config
- copy config
- API raw config
- API QR

Treat private keys/configs as sensitive.

Do not log them.

Avoid exposing them to API tokens without configs.read permission.

Set appropriate HTTP cache headers for sensitive endpoints.

---

# 29. Audit log

Record important administrative and API actions.

Examples:

- user created
- user disabled
- user deleted
- traffic added
- traffic reset
- expiration changed
- admin created
- API token created/revoked
- settings changed
- backup restored
- update performed

Audit fields:

- timestamp
- actor type
- actor ID
- action
- target type
- target ID
- source IP
- request ID
- optional safe metadata

Never store secrets in audit logs.

---

# 30. Networking safety

Network configuration code must be handled carefully.

Required:

- IP forwarding configuration
- NAT
- nftables integration
- AWG interface management
- IP allocation
- conflict detection
- restart/recovery logic

Do not flush unrelated firewall rules.

Do not blindly replace the administrator's existing nftables configuration.

Rules should be namespaced/identifiable as WG-Guard-owned.

Installer/uninstaller should only remove rules owned by WG-Guard.

Avoid locking SSH access.

Networking changes should be transactional where reasonably possible.

---

# 31. Internal VPN IP management

Implement safe peer IP allocation.

Requirements:

- configurable VPN subnet
- unique IP per device
- no duplicate allocations
- release IP when permanently deleting peer when safe
- detect DB/config inconsistencies
- support IPv4 initially
- leave architecture ready for IPv6 later

Use database constraints to prevent duplicates.

---

# 32. SQLite

Use SQLite carefully for production.

Requirements:

- migrations
- WAL mode where appropriate
- foreign keys
- transactions
- indexes
- busy timeout
- safe backup strategy
- corruption-aware error handling

Keep DB access structured.

Avoid creating a huge ORM abstraction if not necessary.

Prefer simple explicit repository/data access code.

---

# 33. Backup and restore

Support:

- manual backup
- database backup
- relevant WG-Guard config backup
- tunnel configuration backup where necessary
- restore

Automatically create a backup before:

- migrations with significant risk
- application upgrade

Do not back up transient files unnecessarily.

Store backups with restrictive permissions.

---

# 34. Updates

WG-Guard is hosted on GitHub.

Design an update mechanism capable of:

- checking current version
- checking latest release
- downloading correct architecture
- checksum verification
- release authenticity/signature verification when the release process supports it
- backup before update
- atomic binary replacement
- migration
- rollback if startup fails

Do not silently auto-update by default.

Admin explicitly initiates updates.

Also provide CLI support such as:

wg-guard version
wg-guard update
wg-guard doctor

---

# 35. CLI

Provide a minimal useful CLI.

Potential commands:

wg-guard serve
wg-guard version
wg-guard status
wg-guard doctor
wg-guard backup
wg-guard restore
wg-guard update
wg-guard admin reset-password

Avoid duplicating the entire web/API management system as CLI.

---

# 36. One-click installer

Installation should be extremely easy.

Target UX:

curl -fsSL https://raw.githubusercontent.com/<owner>/wg-guard/main/install.sh | bash

The shell script should remain a small bootstrapper.

The bootstrapper must download artifacts only over HTTPS, select a known release/channel, and verify the downloaded installer/binary against published checksums before execution. Do not execute an unverified downloaded binary.

Prefer downloading a compiled Go installer/TUI.

The installer should:

1. Check root privileges
2. Detect OS/version
3. Detect CPU architecture
4. Validate supported environment
5. Detect primary network interface
6. Detect public networking where possible
7. Install required system packages
8. Install/configure AmneziaWG
9. Configure forwarding safely
10. Configure firewall/NAT safely
11. Install WG-Guard binary
12. Create directories
13. Create system user if appropriate
14. Create systemd service
15. Initialize database
16. Create Owner account
17. Configure web/API listening
18. Start service
19. Verify health
20. Show final login URL

Make the terminal installation experience attractive and professional.

Example visual style:

╭────────────────────────────────────────╮
│ WG-GUARD │
│ Lightweight VPN Node │
╰────────────────────────────────────────╯

✓ System detected
✓ Network detected
✓ AmneziaWG installed
✓ Firewall configured
✓ WG-Guard installed
✓ Service started

Dashboard:
https://SERVER:PORT

Use a lightweight Go TUI library if justified.

Do not sacrifice installation reliability for animations.

---

# 37. Uninstaller

Provide a safe uninstall flow.

It should optionally preserve:

- database
- backups
- configuration

Only remove networking/firewall resources owned by WG-Guard.

Never flush global firewall configuration.

---

# 38. Filesystem layout

Prefer conventional Linux paths.

Example:

/usr/local/bin/wg-guard
/etc/wg-guard/
/var/lib/wg-guard/
/var/lib/wg-guard/wg-guard.db
/var/lib/wg-guard/backups/
/var/log/wg-guard/ if file logging is used

Use restrictive permissions for sensitive data.

---

# 39. Suggested repository structure

Use a clean modular structure similar to:

wg-guard/
├── cmd/
│ ├── wg-guard/
│ └── installer/
├── internal/
│ ├── auth/
│ ├── admin/
│ ├── api/
│ ├── audit/
│ ├── config/
│ ├── database/
│ ├── device/
│ ├── user/
│ ├── plan/
│ ├── accounting/
│ ├── scheduler/
│ ├── webhook/
│ ├── firewall/
│ ├── ratelimit/
│ ├── network/
│ └── tunnel/
│ └── amneziawg/
├── web/
│ ├── templates/
│ ├── components/
│ └── static/
├── migrations/
├── packaging/
├── scripts/
├── docs/
├── tests/
├── install.sh
├── go.mod
├── README.md
└── LICENSE

You may adjust this structure when justified.

Keep packages cohesive.

Avoid giant files and circular package dependencies.

---

# 40. Observability

Keep monitoring lightweight.

Implement structured logging.

Useful fields:

- request_id
- module
- action
- duration
- error

Support configurable log level.

Never log:

- private keys
- raw VPN configs
- passwords
- full API tokens
- session cookies
- webhook secrets

Expose basic node health through API.

Do not embed a heavy metrics stack.

---

# 41. Performance targets

This project prioritizes low resource consumption.

Design for:

- very low idle CPU
- small RAM footprint
- minimal goroutine count
- minimal background polling
- efficient SQLite queries
- no unnecessary frontend runtime
- no constant high-frequency writes

Do not optimize blindly.

Add benchmarks where meaningful.

Provide a simple repeatable way to measure idle resource usage.

---

# 42. Concurrency and correctness

Handle concurrent API requests safely.

Important examples:

Two requests must not:

- allocate the same VPN IP
- exceed device limit due to race
- add traffic twice due to retry
- activate first_connection twice
- corrupt accounting

Use DB transactions and constraints where appropriate.

Run tests with Go race detector.

---

# 43. Security requirements

Treat this as security-sensitive infrastructure.

Follow:

- least privilege
- secure file permissions
- input validation
- output encoding
- CSRF protection
- rate limiting
- secure headers
- no shell command injection
- safe subprocess invocation
- no string-concatenated shell commands using user input
- secret redaction
- dependency pinning
- dependency review

Prefer Go APIs or exec.Command with explicit arguments over shell interpolation.

Never accept arbitrary commands through API.

---

# 44. Secrets

Clearly define storage strategy for:

- device private keys
- admin secrets
- API tokens
- webhook secrets

Use secure random generation via crypto/rand.

Do not use math/rand for secrets.

Store hashes instead of plaintext whenever retrieval is unnecessary.

For secrets that must be retrievable, use the safest reasonable local design and strict filesystem/database permissions.

Device private keys are especially sensitive. Prefer designs that minimize how long private keys need to exist on the server. If the product requirement to re-download configs requires persistent private-key storage, encrypt retrievable secrets at rest using a node-local master key stored separately with restrictive permissions, define rotation/backup behavior, and never log or expose that key through the API/UI. If a one-time-delivery design is chosen instead, clearly document that configs cannot be reconstructed after the private key is discarded.

Document the threat model and limitations, including what an attacker with root access can and cannot be protected against.

---

# 45. Tests

Do not consider a feature complete without tests.

Include:

- unit tests
- repository/database tests
- HTTP/API tests
- authorization tests
- quota tests
- expiration tests
- first-connection tests
- accounting reset tests
- device limit race tests
- idempotency tests
- webhook signature tests
- migration tests
- tunnel backend tests using mock/fake backend where possible

Do not require a real VPN kernel interface for the majority of tests.

Create abstractions so core business logic can be tested without root.

Add integration tests separately for real Linux networking.

---

# 46. CI

Create GitHub Actions for at least:

- gofmt check
- go vet
- tests
- race tests where reasonable
- build amd64
- build arm64
- frontend build if required
- security/static checks where appropriate

Release pipeline should produce checksummed binaries.

Do not place signing secrets directly in repository.

---

# 47. Documentation

README should include:

- what WG-Guard is
- screenshots placeholder/instructions
- features
- supported OS
- installation
- upgrade
- uninstall
- backup
- restore
- API
- security
- troubleshooting
- AmneziaWG client requirements
- development instructions

Also document:

- architecture
- API authentication
- permissions
- webhook verification
- networking behavior
- traffic accounting semantics

---

# 48. Licensing

Before bundling, vendoring, modifying, or redistributing AmneziaWG components:

- inspect upstream licenses
- document relevant license obligations
- ensure WG-Guard distribution complies with them

Do not invent license compatibility assumptions.

Do not remove upstream notices.

If licensing requires architectural changes, report them before proceeding.

---

# 49. Development methodology

Do NOT attempt to implement the entire project in one giant pass.

Work incrementally.

First inspect current repository state.

Then create a written implementation plan.

Suggested phases:

### Phase 0 — Research and foundation

- inspect AmneziaWG integration options
- verify licenses
- establish project structure
- define domain model
- define DB schema
- define tunnel backend interface

### Phase 1 — Core

- config
- database
- migrations
- user model
- device model
- fake tunnel backend
- services
- tests

### Phase 2 — AmneziaWG integration

- install/detection
- interface management
- peer management
- config generation
- handshake/stats
- recovery

### Phase 3 — Limits

- traffic accounting
- expiration
- first connection activation
- device limits
- speed limits

### Phase 4 — REST API

- API tokens
- permissions
- users
- devices
- stats
- bulk actions
- idempotency
- OpenAPI

### Phase 5 — Web UI

- authentication
- dashboard
- users
- bulk creation
- plans
- admins
- API token management
- server settings

### Phase 6 — Webhooks / audit / backup

- webhooks
- audit log
- backup
- restore

### Phase 7 — Installer

- one-click bootstrap
- TUI installer
- systemd
- firewall
- update
- uninstall
- doctor

### Phase 8 — hardening

- security review
- race tests
- network integration tests
- performance testing
- documentation
- GitHub release workflow

For every phase:

1. explain intended implementation
2. write tests
3. implement
4. run tests
5. review code
6. fix issues
7. commit coherent changes

Do not continue after a major architectural uncertainty without explaining it.

A phase is complete only when its scoped acceptance criteria are satisfied, relevant tests pass, documentation is updated, security-sensitive changes are reviewed, and the agent clearly reports any items that still require a real Linux VPS/root integration test.

---

# 50. Engineering principles

Follow these rules throughout the project:

- Simple over clever
- Lightweight over feature-framework-heavy
- Explicit over magical
- Testable business logic
- Minimal external dependencies
- Stable API contracts
- Secure defaults
- No duplicated business logic
- No giant god objects
- No giant handlers
- No unnecessary abstractions
- No premature microservices
- No hidden global mutable state
- Context-aware Go functions
- Graceful shutdown
- Proper error wrapping
- Clear error ownership
- Database transactions for state transitions
- Idempotent system configuration where possible

---

# 51. Important product decisions

These are intentional and should not be changed without discussion:

1. Each VPS is an independent WG-Guard node.
2. No centralized controller is required.
3. No reseller system.
4. Owner + custom-permission Admin only.
5. Full REST API is mandatory.
6. External systems are first-class clients.
7. API token authentication is separate from administrator authentication.
8. SQLite is preferred.
9. Go is preferred.
10. Production frontend must not require Node.js.
11. Docker is optional at most, never required.
12. AmneziaWG is the main VPN backend.
13. Do not reinvent WireGuard cryptography.
14. Device limit means separate peer per device.
15. Traffic quota is RX + TX unless configured otherwise.
16. First-connection activation is required.
17. Bulk management is required.
18. Resource usage must remain very low.
19. Installer UX should be polished.
20. API stability matters from V1.

---

# 52. Initial deliverable

Do NOT start by generating hundreds of files.

First provide:

1. Repository assessment
2. Proposed architecture
3. AmneziaWG integration strategy
4. Dependency list with justification
5. Database/domain model
6. API design summary
7. Security model
8. Installer design
9. Testing strategy
10. Implementation milestones
11. Important risks/questions

Then start Phase 0/Foundation only after the architecture is internally consistent.

Once implementation begins, keep the repository runnable and tested after every major milestone.

Do not use placeholder implementations for critical security/networking functionality and later claim the feature is finished.

When something requires root/kernel/network integration that cannot be tested in the current environment, clearly separate:

- implemented
- unit tested
- integration tested
- requires real Linux VPS verification

---

# 53. Premium UI / UX Design System

WG-Guard is not only an infrastructure tool.

The web dashboard must look and feel like a polished, premium, commercial-grade product designed by a senior product designer and implemented by a senior frontend engineer.

The UI must be:

- Premium
- Modern
- Professional
- Minimal
- Clean
- Fast
- Highly usable
- Visually consistent
- Responsive
- Accessible
- Extremely lightweight

Do NOT interpret "premium" as visually excessive.

Avoid unnecessary:

- huge gradients
- excessive glassmorphism
- distracting animations
- oversized UI
- excessive shadows
- decorative effects
- giant JavaScript libraries
- visual clutter

Premium should come from:

- excellent spacing
- typography
- hierarchy
- alignment
- iconography
- interaction design
- consistency
- polish
- micro-interactions
- responsive behavior
- thoughtful empty/loading/error states
- excellent information density
- fast perceived performance

The visual quality should feel comparable to high-quality modern SaaS dashboards, while remaining substantially lighter.

---

## 53.1 Design philosophy

Design WG-Guard as if it were a paid commercial product.

Every screen should feel intentionally designed.

The user should never feel that WG-Guard is:

- an unfinished admin template
- a generic Bootstrap panel
- a collection of random forms
- a developer-only dashboard
- a copied open-source UI

Create a coherent WG-Guard visual identity.

Prefer:

- restrained visual language
- high information density without clutter
- clear visual hierarchy
- consistent spacing
- subtle borders
- tasteful elevation
- high-quality typography
- clean tables
- polished forms
- excellent mobile behavior

Use whitespace intentionally.

Important information should be immediately visible without forcing the administrator to navigate through unnecessary pages.

---

## 53.2 Design system

Create a small internal design system rather than styling every screen independently.

Define reusable design tokens for:

- spacing
- border radius
- typography
- font sizes
- font weights
- line heights
- colors
- semantic colors
- borders
- shadows
- transitions
- breakpoints
- z-index levels

Use CSS custom properties where appropriate.

Example conceptual structure:

--color-bg
--color-surface
--color-surface-raised
--color-border
--color-text
--color-text-muted
--color-primary
--color-success
--color-warning
--color-danger

--radius-sm
--radius-md
--radius-lg

--space-1
--space-2
--space-3
...

Do not scatter arbitrary color values, margins, shadows, and radii throughout templates.

Maintain visual consistency across the entire product.

---

## 53.3 Components

Build lightweight reusable UI components.

Examples:

- Button
- IconButton
- Input
- Select
- Checkbox
- Radio
- Toggle
- Textarea
- SearchInput
- Badge
- StatusBadge
- Card
- MetricCard
- Modal
- Drawer
- Dropdown
- Tooltip
- Toast
- Tabs
- Pagination
- Breadcrumb
- Table
- DataTable toolbar
- EmptyState
- Skeleton
- ProgressBar
- TrafficProgress
- ConfirmDialog
- UserAvatar / InitialBadge
- Mobile navigation
- Desktop sidebar
- Command / quick-action menu if lightweight enough

Components should have consistent:

- spacing
- states
- focus styles
- hover styles
- disabled styles
- error states
- loading states

Avoid giant frontend component frameworks unless absolutely necessary.

---

## 53.4 SVG icon system

Use high-quality SVG icons throughout the dashboard.

Requirements:

- consistent icon family
- consistent stroke width
- consistent sizing
- visually aligned icons
- SVG-based
- sharp at all resolutions

Prefer a lightweight open-source SVG icon set such as Lucide or an equivalent carefully selected set.

Do NOT ship a large icon runtime library if only a subset of icons is needed.

Prefer:

- compile-time selected SVGs
- embedded SVG sprite
- reusable SVG partials/components

Avoid:

- emoji as primary interface icons
- raster icons
- icon fonts
- loading hundreds of unused icons

Icons should improve recognition, not create visual noise.

---

## 53.5 Motion and animation

Use subtle, smooth, premium micro-interactions.

Animations must remain lightweight and should never increase meaningful CPU usage while the interface is idle.

Prefer GPU-friendly CSS properties such as:

- transform
- opacity

Avoid repeatedly animating:

- width
- height
- top
- left
- expensive filters
- large shadows
- layout-heavy properties

Use short, natural animation durations.

Typical interactions should generally feel within approximately:

120ms - 240ms

Examples:

- button press
- hover
- dropdown opening
- modal opening
- toast appearance
- navigation transitions
- expandable rows
- status changes
- progress updates

Animations should feel:

- smooth
- subtle
- deliberate
- fast

Never:

- animate continuously without purpose
- add decorative background animations
- consume CPU while the dashboard is idle
- make users wait for animation completion

Respect:

prefers-reduced-motion

Users requesting reduced motion must receive a usable low-motion experience.

---

## 53.6 Desktop layout

Desktop UI should be optimized for administrators managing many users.

Use an efficient application shell.

Suggested layout:

Desktop:
┌─────────────────────────────────────────────┐
│ Sidebar │ Header │
│ ├───────────────────────────────────┤
│ │ │
│ │ Main Content │
│ │ │
└─────────────────────────────────────────────┘

Sidebar should be clean and compact.

Suggested navigation:

Dashboard
Users
Plans
Administrators
API Tokens
Webhooks
Node
Audit Logs
Backup
Settings

Use icons + labels.

Allow sidebar collapse if it improves usability without adding unnecessary complexity.

The header may contain:

- page title
- breadcrumb where useful
- quick actions
- system status
- current administrator menu

Do not waste large amounts of vertical space on desktop.

---

## 53.7 Mobile design

Mobile support is mandatory and must be designed intentionally.

Do NOT simply shrink the desktop interface.

The dashboard must work comfortably on:

- phones
- tablets
- small laptops
- desktop monitors
- large monitors

Mobile navigation should use an appropriate compact interaction such as:

- drawer
- bottom navigation where appropriate
- compact menu

Do not force large desktop tables into tiny screens.

For users lists on mobile, intelligently adapt table content.

Example:

Desktop:

Username | Status | Traffic | Expiration | Devices | Last Seen | Actions

Mobile:

┌────────────────────────────┐
│ gs-1024 ● Active │
│ │
│ Traffic 42 / 100 GB │
│ ████████░░░░ │
│ │
│ Expires 18 days │
│ Devices 2 / 3 │
│ │
│ Renew Traffic ••• │
└────────────────────────────┘

Primary actions must remain easy to reach with touch.

Use appropriate:

- touch targets
- padding
- spacing
- bottom sheets/drawers where appropriate

Avoid tiny buttons and dense desktop interactions on phones.

---

## 53.8 Users page

The Users page is one of the most important parts of WG-Guard.

It must be extremely efficient for administrators managing large numbers of accounts.

Desktop should provide a polished data table with:

- search
- filtering
- sorting
- pagination
- selectable rows
- select all
- bulk actions
- configurable useful columns if lightweight enough

Potential columns:

- Username
- Status
- Traffic
- Remaining
- Expiration
- Remaining time
- Devices
- Speed
- Last connection
- Created at
- Actions

Traffic should be visually understandable.

Example:

42.8 GB / 100 GB
████████░░░░ 43%

Expiration:

18 days remaining

Status examples:

Active
Disabled
Expired
Traffic Limit
Waiting
Suspended

Status colors must remain semantic and accessible.

Actions should not make each row visually noisy.

Use:

primary quick actions

- compact overflow menu for less frequent actions.

  ***

## 53.9 User details

The user details page should allow the administrator to manage almost everything without unnecessary navigation.

Suggested sections:

Overview
Traffic
Subscription
Devices
Connection
Activity / Audit

Prominent information:

- username
- status
- traffic usage
- remaining traffic
- expiration
- remaining duration
- device usage
- speed
- last connection

Primary actions:

- Renew
- Add Traffic
- Edit
- Enable / Disable

Secondary actions:

- Reset Traffic
- Change Limits
- Manage Devices
- Config
- Delete

Destructive actions must be visually separated from normal actions.

---

## 53.10 Dashboard

Dashboard should focus on operational information rather than decorative charts.

Use polished summary cards for:

- Total Users
- Active
- Online / Recently Active
- Expired
- Traffic Exceeded
- Expiring Soon
- Total Traffic

Node information should include:

- WG-Guard status
- AmneziaWG status
- uptime
- CPU
- RAM
- disk
- network activity

Charts should only be included when they answer a useful administrative question.

Avoid heavy chart libraries.

If charts are needed, use a very lightweight implementation or server-generated/simple SVG where practical.

Do not continuously animate charts.

---

## 53.11 Forms

Forms must have excellent UX.

Requirements:

- clear labels
- useful descriptions only when needed
- inline validation
- server-side validation
- clear errors
- logical grouping
- sensible defaults
- keyboard usability
- mobile usability

Example Create User form:

Identity

- Username
- Display name
- Notes

Subscription

- Duration
- Start policy
- Traffic quota

Limits

- Devices
- Speed

Connection

- Anti-DPI profile

Advanced options should be collapsed by default.

Do not overwhelm normal administrators with AmneziaWG low-level settings.

---

## 53.12 Bulk creation UX

Bulk creation should be a first-class workflow.

Administrator should easily choose:

Number of users
Username format
Plan / custom limits
Traffic
Duration
Device limit
Speed
Anti-DPI profile
Activation policy

Before committing the operation, show a useful summary:

Create 100 users

Prefix:
gs-

Traffic:
100 GB

Duration:
30 days

Devices:
1

Activation:
First connection

Then provide a clear confirmation.

After creation show:

- success count
- failed count
- export CSV
- export configs
- download ZIP where appropriate

Do not make administrators open every user individually.

---

## 53.13 Empty states

Every empty state must be intentionally designed.

Bad:

"No data"

Good:

No users yet

Create your first VPN account to start managing
traffic, devices and subscriptions.

[ Create User ]

Use useful lightweight SVG illustrations/icons only when appropriate.

Do not include large decorative artwork.

---

## 53.14 Loading behavior

The dashboard should feel instant.

Avoid full-page reloads for routine interactions where HTMX can safely update a specific region.

Use:

- skeleton states where appropriate
- compact spinners for short actions
- button loading states
- optimistic feedback only when correctness permits

Never allow duplicate submissions because an administrator clicked twice.

Disable appropriate action controls while a mutation is being processed.

---

## 53.15 Error UX

Errors should explain:

- what failed
- what the user can do
- whether anything changed

Example:

Could not create device

This user already has the maximum number
of allowed devices.

Device limit: 3

[ Change Limit ] [ Close ]

Do not expose internal stack traces or technical implementation details.

---

## 53.16 Success feedback

Use subtle toast notifications for successful actions.

Examples:

User created
50 GB added
Subscription renewed
Device revoked
API token created

Toasts should disappear automatically but remain accessible.

Do not show modal dialogs for routine success messages.

---

## 53.17 Confirmation UX

Require confirmation for dangerous operations such as:

- delete user
- bulk delete
- revoke device
- reset traffic
- restore backup
- major network change

Confirmation should clearly identify the affected target.

For especially destructive bulk actions, require stronger confirmation.

Avoid generic:

"Are you sure?"

Prefer:

Delete 42 users?

Their active VPN peers will be revoked.
This action cannot be undone.

---

## 53.18 Accessibility

Premium UX includes accessibility.

Requirements:

- semantic HTML
- keyboard navigation
- visible focus indicators
- proper labels
- sufficient contrast
- aria attributes where actually required
- screen-reader friendly status information
- accessible modals
- accessible dropdowns

Do not depend on color alone to communicate status.

Target WCAG-friendly behavior where practical.

---

## 53.19 Light / Dark appearance

Design the visual system so both light and dark themes can be supported cleanly.

If implementing both in V1 does not significantly increase complexity, support:

- Light
- Dark
- System

Theme switching must rely primarily on CSS variables.

Do not duplicate large stylesheets.

Both themes must maintain premium visual quality.

---

## 53.20 Premium details

Pay attention to small details that distinguish professional software.

Examples:

- aligned numbers
- tabular numerals for traffic statistics where useful
- consistent date/time formatting
- clear relative time
- copy-to-clipboard feedback
- subtle hover states
- polished tooltips
- predictable focus behavior
- consistent confirmation dialogs
- clean truncation
- useful breadcrumbs
- excellent responsive breakpoints
- stable layout during loading
- no content jumping
- no accidental double-submit
- useful keyboard shortcuts only where they provide meaningful value

Micro-details matter.

---

## 53.21 UI performance requirements

Beautiful UI must NOT make WG-Guard heavy.

Performance is a hard requirement.

Prefer:

HTML

- CSS
- HTMX
- minimal Alpine.js / vanilla JavaScript

over a large SPA runtime.

Do not add React, Vue, Angular, Next.js, or another large framework merely to make the UI look modern.

Premium appearance must be achieved primarily through:

- good HTML
- excellent CSS
- careful typography
- SVG
- server rendering
- lightweight progressive enhancement

Avoid unnecessary client-side state.

Avoid large dependency trees.

Avoid shipping unused CSS/JavaScript.

Minify production frontend assets.

Use compression where appropriate.

Cache static immutable assets correctly.

Do not cache sensitive dynamic pages improperly.

---

## 53.22 Frontend performance budget

Treat frontend size as a measurable engineering constraint.

Continuously inspect:

- CSS size
- JavaScript size
- number of HTTP requests
- DOM size
- long tasks
- layout shifts

Keep JavaScript extremely small.

Do not introduce a dependency that adds substantial frontend weight for a trivial feature.

Before adding a frontend library ask:

1. Is it truly necessary?
2. Can the browser platform already do this?
3. Can HTMX handle it?
4. Can a small local implementation handle it?
5. What is the runtime and bundle cost?

Prefer zero dependency when the implementation remains maintainable.

---

## 53.23 Perceived performance

WG-Guard should feel faster than typical admin panels.

Optimize for:

- fast first paint
- fast navigation
- immediate interaction feedback
- minimal blocking JavaScript
- minimal layout shift
- small assets
- efficient HTML

Do not make administrators wait for unnecessary transitions.

Performance itself is part of the premium experience.

---

## 53.24 Responsive architecture

Do not create separate desktop and mobile codebases.

Use one maintainable responsive component system.

Components should adapt intentionally across breakpoints.

Test important screens at least at representative sizes such as:

- ~360px phone
- ~390/430px phone
- tablet
- ~1366px laptop
- ~1440px desktop
- large desktop

Avoid relying only on browser resizing.

Consider real touch interaction behavior.

---

## 53.25 Visual QA

Before considering UI work complete, visually inspect every important screen.

Check:

- spacing
- alignment
- typography
- icon consistency
- responsive behavior
- hover
- focus
- active
- disabled
- loading
- empty
- error
- success
- long usernames
- large traffic values
- very long lists
- mobile navigation

Do not consider a page complete just because it technically renders.

UI quality must be reviewed intentionally.

---

## 53.26 UX consistency rule

If the same operation exists in multiple places, interaction behavior must remain consistent.

For example:

Disable User

should not be:

- a toggle on one page
- a modal form on another
- a differently named action elsewhere

Create shared interaction patterns.

The user should learn WG-Guard once.

---

## 53.27 Design quality gate

Do not accept the first functional UI implementation as final.

For each major UI feature:

1. implement functional version
2. review information hierarchy
3. review responsive behavior
4. review interaction states
5. remove unnecessary visual elements
6. improve spacing and typography
7. verify accessibility
8. profile frontend performance
9. polish micro-interactions
10. perform final visual review

Functionality alone is not sufficient.

The result must look production-ready.

---

# 54. Extreme Runtime Efficiency

WG-Guard must remain extremely lightweight even after all features are implemented.

Low RAM and CPU consumption are core product requirements, not optional optimizations.

Assume WG-Guard will frequently run on small VPS instances where VPN traffic itself deserves most of the available resources.

Optimize both:

- idle resource usage
- resource usage under many peers

The control panel must not become the resource bottleneck.

---

## 54.1 Architecture for low resource usage

Prefer one Go process.

Avoid spawning permanent helper processes unless unavoidable.

Do not create microservices.

Do not add:

- Redis
- separate workers
- message queues
- separate frontend server
- application server clusters
- Node.js runtime
- database server

for functionality that can cleanly live inside the WG-Guard process.

SQLite should remain the default database.

Use systemd for lifecycle management.

---

## 54.2 Idle CPU

WG-Guard should consume essentially negligible CPU while idle.

Avoid:

- busy loops
- high-frequency polling
- unnecessary timers
- constant database queries
- constant filesystem scanning
- constant AWG command execution
- frontend polling every few seconds
- unnecessary system statistics polling

Prefer event-driven behavior where possible.

Background schedulers should sleep until meaningful work is required.

---

## 54.3 Memory efficiency

Keep memory allocations controlled.

Avoid:

- loading all users into memory
- loading entire large query results
- retaining large API responses
- unbounded caches
- unbounded logs
- unbounded webhook queues
- unnecessary reflection-heavy frameworks

Use pagination and streaming where appropriate.

Bound all queues and caches.

Be mindful of goroutine leaks.

---

## 54.4 Database efficiency

Design SQLite queries carefully.

Use:

- correct indexes
- transactions
- prepared statements where useful
- bounded queries
- cursor pagination
- batching

Avoid N+1 queries.

Avoid writing unchanged state repeatedly.

Traffic accounting should not perform a database transaction for every network packet or every trivial counter observation.

Aggregate safely.

---

## 54.5 Background work

Centralize periodic/background work where practical.

Do not create one timer/goroutine per user if thousands of users can be managed using a smaller scheduler.

Examples:

Bad:
10,000 users
→ 10,000 expiration timers

Better:
one efficient expiration scheduler
→ queries/processes next relevant expirations

Apply the same thinking to:

- traffic accounting
- webhook retries
- cleanup
- statistics
- token expiration
- idempotency cleanup

---

## 54.6 External process usage

AmneziaWG tooling may require subprocess execution.

Do not repeatedly spawn commands when an API or batched operation can do the work more efficiently.

Where subprocess execution is required:

- use explicit arguments
- enforce timeouts
- capture output safely
- avoid shell interpolation
- minimize invocation frequency

Profile this behavior under many peers.

---

## 54.7 Resource benchmarks

Create repeatable performance measurements.

Measure at least:

- idle process memory
- idle CPU
- startup time
- API latency
- user listing latency
- bulk creation performance
- accounting cycle cost

Test representative peer counts such as:

- 10
- 100
- 1,000
- more where practical

Do not claim performance without measurements.

Document benchmark environment.

---

## 54.8 Performance regression protection

When practical, add benchmarks for performance-sensitive code.

Major changes should not accidentally introduce:

- excessive allocations
- expensive queries
- high-frequency polling
- goroutine leaks
- large frontend bundles

Performance regressions are bugs.

---

# 55. Senior-Level Code Quality

Treat this repository as a long-term professional open-source codebase.

Write code as a senior engineer responsible for maintaining it for years.

The code must be:

- clean
- readable
- idiomatic
- modular
- testable
- secure
- predictable
- documented where necessary
- easy for another engineer to understand

Do not generate low-quality AI-style code.

Avoid:

- giant files
- giant functions
- giant structs
- repeated code
- vague package names
- meaningless helper abstractions
- TODO-heavy implementations
- excessive comments explaining obvious code
- inconsistent error handling
- excessive interface abstraction
- global mutable state
- hidden side effects

---

## 55.1 Package design

Each package should have one clear responsibility.

Good conceptual boundaries include:

auth
admin
api
audit
accounting
user
device
plan
webhook
network
firewall
ratelimit
tunnel
database

Avoid generic packages such as:

utils
helpers
common
misc

unless there is a genuinely coherent purpose.

Prefer domain-specific names.

---

## 55.2 File organization

Keep files focused and logically grouped.

Do not create a 5,000-line:

server.go

or:

handlers.go

Split by responsibility.

Example:

users_handler.go
users_service.go
users_repository.go
users_models.go
users_test.go

Do not mechanically force this exact pattern where unnecessary, but maintain clear organization.

---

## 55.3 Function design

Functions should be reasonably small and cohesive.

A function should usually do one conceptual job.

Prefer explicit inputs and outputs.

Avoid:

- hidden globals
- implicit state
- deeply nested conditions
- 15-parameter functions

Use early returns where they improve clarity.

---

## 55.4 Error handling

Use consistent error handling.

Define domain errors where useful.

Wrap errors with meaningful context.

Preserve error identity for machine-readable API mapping.

Do not:

- swallow errors
- panic for normal runtime failures
- return raw database errors to clients
- expose subprocess output containing secrets

---

## 55.5 Naming

Use precise names.

Names should reflect domain intent.

Prefer:

trafficLimitBytes

over:

limit

Prefer:

expiresAt

over:

time

Prefer:

DisableReasonTrafficExceeded

over unexplained magic strings.

Consistency matters.

---

## 55.6 Comments

Comments should explain:

- why
- invariants
- security constraints
- unusual networking behavior

Do not write comments that merely repeat the next line of code.

Public APIs should be documented appropriately.

---

## 55.7 Dependency discipline

Every dependency must justify its existence.

Before adding a package evaluate:

- binary size
- transitive dependencies
- maintenance status
- security history
- runtime overhead
- whether standard library is sufficient

Prefer mature focused libraries.

Do not use a framework just because it generates code faster.

---

## 55.8 Maintainability

Optimize for future maintainers.

A new experienced Go developer should be able to understand the architecture quickly.

Maintain:

- clear package boundaries
- predictable data flow
- stable service interfaces
- explicit configuration
- deterministic startup
- consistent testing patterns

Keep complexity proportional to actual product requirements.

---

## 55.9 Formatting and static quality

Always maintain:

- gofmt
- go vet
- static analysis where appropriate
- linting with a carefully selected configuration
- tests
- race tests where appropriate

Do not enable hundreds of noisy linter rules with little value.

Use tools to improve code quality, not create ceremony.

---

## 55.10 Refactoring rule

Do not accumulate obvious technical debt while building features.

When a feature reveals a poor local abstraction, improve it before building more functionality on top of it.

However:

Do not perform unrelated large refactors.

Keep changes focused and reviewable.

---

## 55.11 No placeholder quality

Never claim a feature is complete if it contains:

- mock production behavior
- fake networking operations
- hardcoded secrets
- placeholder authorization
- TODO security logic
- incomplete persistence
- dummy quota enforcement

Clearly distinguish:

prototype
implemented
tested
production-ready

---

## 55.12 Senior engineer mindset

Throughout implementation, act simultaneously as:

- senior Go backend engineer
- senior Linux networking engineer
- security engineer
- database engineer
- API designer
- senior frontend engineer
- senior product designer
- UX designer
- performance engineer
- open-source maintainer

Do not optimize one area by destroying another.

Examples:

Do not create a beautiful UI that requires 300 MB RAM.

Do not reduce dependencies by creating insecure custom cryptography.

Do not optimize database writes at the expense of inaccurate accounting.

Do not simplify networking by flushing the host firewall.

Balance:

security
correctness
performance
maintainability
UX
visual quality

---

# 56. Product quality bar

WG-Guard must look and behave like a mature premium product, even though it is open source and lightweight.

The desired combination is:

Premium UI

- Excellent UX
- Very low resource consumption
- Clean Go architecture
- Strong security
- Stable API
- AmneziaWG anti-DPI
- One-click installation

Do not accept trade-offs such as:

"lightweight means ugly"

or:

"premium means heavy"

The objective is specifically to achieve both.

A user opening WG-Guard for the first time should immediately feel that the product is:

- fast
- polished
- trustworthy
- organized
- professional

An engineer opening the repository should immediately feel that the codebase is:

- clean
- intentional
- structured
- testable
- maintainable
- professionally engineered

---

# 57. Final UI / UX verification

Before declaring the web dashboard production-ready, perform a complete UX/UI review.

Review at minimum:

Dashboard
Users
Create User
Bulk Create
User Details
Devices
Plans
Administrators
API Tokens
Webhooks
Node
Settings
Audit Logs
Backup
Login

For every page verify:

Desktop
Mobile
Tablet

and verify states:

Normal
Empty
Loading
Error
Disabled
Success

Check:

- responsive layout
- typography
- spacing
- icons
- animation
- contrast
- keyboard use
- touch usability
- form validation
- destructive actions
- information hierarchy
- frontend performance

Do not declare UI complete until both functional and visual quality are high.

When there is a conflict between visual decoration and performance/usability, choose usability and performance, then find a lighter way to achieve visual polish.

# 58. Critical design requirement

UI/UX quality is a first-class engineering requirement of WG-Guard, not a final cosmetic phase.

Architecture decisions must support a premium interface from the beginning.

However, premium design must NEVER require a heavy runtime.

The challenge of this project is deliberately:

Build one of the most polished and professional VPN management dashboards possible while keeping WG-Guard dramatically lighter than typical modern web applications.

When choosing between two technically correct implementations, prefer the solution that provides:

1. lower RAM usage
2. lower idle CPU
3. fewer runtime dependencies
4. smaller frontend payload
5. simpler maintenance
6. equally excellent or better UX

Measure rather than guess.

Do not sacrifice correctness or security for performance.

Do not sacrifice performance for visual decoration.

Achieve premium quality through excellent engineering and design, not through unnecessary frameworks.

---

# 59. Final objective

The finished WG-Guard should allow an administrator to install a complete lightweight AmneziaWG management node on a fresh VPS with one command, open a polished web dashboard, create and manage VPN users with quotas/expiration/device/speed restrictions, and expose a stable secure REST API so Telegram bots and larger VPN management platforms can use the server as a remotely managed node.

The final result should feel like:

"wg-easy/WGDashboard simplicity + serious commercial user management + API-first design + AmneziaWG anti-DPI capabilities"

while remaining small, fast, secure, maintainable, and easy to deploy.

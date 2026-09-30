# REST API (`/api/v1`)

This pre-installation contract uses **technical templates** (`/templates`, `template_id`,
`templates.read/write`). The former plan-named paths, fields and scopes are absent. A template
is an optional, reusable node access definition; it is not a shop SKU or price record. The
external bot/storefront owns its product catalog, payment and order journal. Once integrations
are deployed, incompatible wire changes require explicit API versioning. Clients can discover
node capability through `GET /api/v1/node/health`.

## Conventions

- **Auth**: `Authorization: Bearer wg_…` API tokens with scopes (users.read/create/update/delete/
  bulk, devices.*, configs.read, traffic.read/update, templates.read/write, stats.read, node.read,
  node.settings, webhooks.read/write, interfaces.*, purchases.create, operations.read and
  subscriptions.read/rotate). Tokens are separate from admin sessions; the
  `wg-guard token create|list|revoke|scopes` CLI mints them (the panel's token screen is the
  day-to-day manager). Panel-issued tokens are bound to the issuing account and cannot retain
  permissions removed from that account. Existing CLI/pre-Phase-14 tokens remain node-wide and
  should be reviewed by the owner when enabling reseller integrations. Reseller-bound tokens may
  use explicitly tenant-gated customer reads, usage reset, quota top-up, successor-plan and customer-link
  actions, template-gated purchase/result operations, and owned webhook endpoints/receipts when
  granted the corresponding scopes. Lists are ownership-filtered. Other mutations,
  unclassified routes and global aggregates remain denied.
- **Errors**: one envelope — `{"error": {"code", "message", "request_id"}}` with stable codes
  (`USER_NOT_FOUND`, `USERNAME_EXISTS`, `DEVICE_LIMIT_REACHED`, `TRAFFIC_EXCEEDED`, `INVALID_REQUEST`,
  `UNAUTHORIZED`, `FORBIDDEN`, `RATE_LIMITED`, `NODE_UNAVAILABLE`, `INTERNAL_ERROR`, …). No stack
  traces. The `X-Request-Id` response header carries the correlation id (client-supplied ids are
  honored when they are ≤ 64 printable-ASCII bytes).
- **Strict request bodies**: JSON object requests reject unknown properties and any second/trailing
  JSON value. This prevents misspelled configuration fields from being accepted as no-ops.
  Malformed AWG scalar/range values use `PARAM_CONSTRAINT`; malformed JSON uses
  `INVALID_REQUEST`.
- **Pagination**: keyset cursor — `limit` ≤ 500, opaque `cursor` (base64url JSON), items ordered
  by the chosen sort; stable ordering even for rows written in the same microsecond (id
  tiebreak). Filters per the archived spec §22 (status, expires_before/after, traffic_exceeded,
  enabled, created range, search with literal `%`/`_` semantics).
- **Tri-state PATCH semantics** for nullable option fields (such as user/template quota, speed,
  device cap and profile/template references): a field **absent** from the
  body means "no change"; an explicit JSON **null** means "clear to unlimited/none" (e.g.
  `{"speed_limit_up_kbps": null}` removes only the upload cap); a value sets it. This is how
  independent up/down speed limits change one at a time without re-sending the other.
  This is not a blanket rule for all fields: user/template PATCH `duration_seconds: null`
  leaves the stored duration unchanged; `note: ""` clears a note and `tags: []` clears tags.
  See [the user-form mapping](#user-form-to-api-mapping) for field-specific behavior.
- **Idempotency**: `Idempotency-Key` header (1–128 printable chars) is persisted for create-user,
  bulk create/action, renew, traffic mutations and successor-plan queue/cancel. Authentication, scope checks and rate limits
  precede every replay. Keys are isolated per verified API token: the same token and request
  replay the stored response with `Idempotency-Replayed: true`; a different request using that
  token's key returns 409 (`IDEMPOTENCY_KEY_REUSED`). Keys remain valid for 24 h, with expired
  rows retired on reuse. The claim and response
  snapshot are not committed atomically with the mutation: an interruption after the mutation but
  before the snapshot can leave an ambiguous in-flight key. These ordinary mutations have no
  lookup-by-key or recovery endpoint; external billing workflows using them must reconcile durable
  resource state. `/purchases` and `/users/{id}/quota/add` use an atomic result journal instead: the key is scoped to
  the owner or reseller identity, survives token rotation within that identity, and can be
  looked up for 90 days. Identical retries return the prior result; changed payloads return 409.
  Keys are hashed at rest and must not contain secrets.
- **Rate limits**: per-token fixed 60 s window (`api.rate_limit_per_minute`, default 600; 0
  disables). Responses carry `X-RateLimit-Limit`/`X-RateLimit-Remaining`; a 429 carries
  `Retry-After`. Setting changes apply live (no restart).
- **Sensitive endpoints** (`/config`, `/qr`, customer subscription link): require their explicit
  scopes, always `Cache-Control: no-store`; private keys and link capabilities are never logged.
- **Config generation is on demand**: client configs are a pure function of current settings, so
  endpoint/DNS/MTU changes propagate to every new download immediately.

## Units and conversions

The API accepts and returns exact numeric values in the units below. It does not infer GB, GiB
or Mbps from a number. Convert an external product's units once, at the integration boundary.

| Quantity | API unit | Panel / integration conversion |
|---|---|---|
| Traffic quota and counters (`traffic_limit_bytes`, `traffic_used_rx/tx/total`, `rx_bytes`, `tx_bytes`, top-up `bytes`, traffic-series `rx`/`tx`) | Integer bytes (B) | Decimal SI: 1 MB = 1,000,000 B; 1 GB = 1,000,000,000 B. Panel forms and traffic displays use these units. |
| Speed limits (`speed_limit_down_kbps`, `speed_limit_up_kbps`) | Kilobits per second; 1 Kbps = 1,000 bits/s | Mbps × 1,000, or panel MB/s × 8,000. Download is server→client; upload is client→server. |
| Telemetry transfer rates (`*_bytes_per_second`) | Bytes per second (B/s) | Divide by 1,000,000 for MB/s; multiply by 8 and divide by 1,000,000 for Mbps. |
| Durations (`duration_seconds`) | Integer seconds | 1 day = 86,400 s; 30 days = 2,592,000 s. The panel's duration-month shortcut means 30 days, not a calendar month. |
| Timestamps (`expires_at`, `activated_at`, etc.) | RFC3339 instants | Responses use UTC (`Z`); localized panel calendars do not change the API instant. |
| Memory, disk and process-size metrics (`*_bytes`) | Integer bytes (B) | The same decimal MB/GB conversions apply; these measure resources, not customer allowance. |
| Utilization (`cpu_percent`, `memory_percent`, `disk_percent`) | Percent on a 0–100 scale | `25` means 25%, not a fraction of 0.25. |
| One-minute load (`load_1`) | Dimensionless host load average | This is task load, not CPU percent; values can exceed 1. |
| Device/peer limits, user/interface/goroutine counts, pagination `limit`/`points`, delivery `attempts` | Integer counts | These count entities, samples or attempts, not bytes or time. `device_limit` caps device configurations, not a bandwidth rate. |
| Uptime / sample cadence / handshake-recency window (`uptime_seconds`, `cadence_seconds`, `online_window_seconds`) | Seconds | Node endpoints expose WG-Guard process uptime; telemetry uptime is host uptime. |
| Interface `mtu` / `listen_port` / subnet | Bytes / UDP port number / CIDR string | `mtu: 1280` means a 1,280-byte tunnel IP-packet MTU; `/24` in an IPv4 subnet is a prefix bit count. |

All numeric request values use JSON numbers in their named unit, without localized digits or
suffixes such as `"100 GB"`. Display rounding does not change the stored quota. An unavailable
telemetry value is `null`, while an observed zero remains `0`. Quota, speed and device limits
use `null` for unlimited where allowed; zero is not an unlimited limit. Direct purchases require
positive finite quota/duration/device limits, and optional speed fields must be positive if sent.

**100 GB** must be sent as `"traffic_limit_bytes": 100000000000`. In contrast, **100 GiB** is
`100 × 1024³ = 107374182400` bytes, which the panel correctly displays as **107.4 GB** (rounded
for display only). Sending the latter while calling the product "100 GB" grants about 7.37%
more bytes. If a create request includes `template_id`, the selected template's saved terms
override manually submitted limits; inspect the response's `traffic_limit_bytes` to confirm
the applied quota.

The lowercase **b** in Mbps means bits; uppercase **B** in MB/s means bytes. For example,
`"speed_limit_down_kbps": 100000` sets **100 Mbps**, equivalent to **12.5 MB/s** in the panel
speed form. `null` means unlimited where the field allows it; direct purchase entitlements
require finite positive limits.

### Traffic direction and aggregation

RX/TX are measured from the **node/server** side: RX is client→server (the customer's upload),
and TX is server→client (the customer's download). The charged user total is
`traffic_used_rx + traffic_used_tx` across that user's devices; **both directions consume quota**.
Device counters and traffic-history buckets are byte amounts, not bytes/s or Mbps. A history
bucket's `rx`/`tx` is the amount for that sample/hour/day, not a cumulative user quota or a speed
limit. Explicit usage resets/corrections affect charged counters; a quota top-up raises allowance.

### Duration and dates

| Intended duration | `duration_seconds` |
|---|---|
| 1 hour | `3600` |
| 1 day | `86400` |
| 7 days | `604800` |
| 30 days (panel's 1-month shortcut) | `2592000` |
| 90 days (panel's 3-month shortcut) | `7776000` |

`start_policy: "immediate"` starts a created subscription immediately; `"first_connection"`
starts its configured duration on the first observed connection. A queued Next Plan's duration
starts when that successor activates, not when it is queued. An API duration has no inferred
calendar-month meaning: a bot selling a calendar month must calculate its intended dates itself.

Expiry-only renewal uses `POST /users/{id}/renew`: `from_now` calculates now + duration;
`from_expiration` calculates max(now, current expiry) + duration; `exact` sets the supplied
RFC3339 `exact` instant. The duration modes use the saved duration if the request omits
`duration_seconds`. This does not add quota or reset usage.

PATCHing a user's `duration_seconds` changes the stored duration term; it does not recompute
that user's existing `expires_at`. Use the renewal endpoint to change the expiry instant.

Send dates with an explicit timezone, for example `"exact": "2026-10-30T12:00:00Z"`.
An offset such as `+03:30` denotes the same absolute instant after conversion; responses use UTC.
Date filters such as `expires_before` and `expires_after` also accept RFC3339 instants. Persian
calendar labels belong to the panel display; do not send a Jalali date or Unix milliseconds
in a `date-time` field. The webhook signature header's `t` is the distinct exception: Unix
**seconds** since 1970-01-01 UTC, while the event body's `timestamp` remains RFC3339.

### Settings and protocol-specific quantities

Settings retain the unit indicated by the key; they do not inherit user-field units:

| Setting / header | Unit |
|---|---|
| `users.default_quota_gb`, `users.quota_presets_gb` | Decimal GB; preset entries are numeric strings, e.g. `"100"`. |
| `users.default_duration_months`, `users.duration_presets_months` | Fixed 30-day periods; preset entries are numeric strings. |
| `accounting.*_seconds`, `network.client_persistent_keepalive` | Seconds. Persistent keepalive accepts the pinned `0`, `N` or `N-M` string format; `0` disables it. |
| `security.session_*_hours`, `accounting.sample_retention_hours` | Hours (3,600 seconds each). |
| `accounting.rollup_hourly_days`, `accounting.rollup_daily_days` | Days (86,400 seconds each). |
| `backup.retention_count`, `webhooks.max_attempts`, `interfaces.max_count` | Archive / delivery-attempt / interface counts. |
| `api.rate_limit_per_minute`, `X-RateLimit-Limit` / `X-RateLimit-Remaining` | Request counts in a fixed 60-second window; a setting of 0 disables rate limiting. |
| API `Retry-After` on 429 | Seconds to wait before retrying. |

Defaults and permitted ranges come from the typed settings registry. Zero/null semantics are
field-specific: a default-quota setting of 0 means no prefilled quota, while REST user/template
`traffic_limit_bytes: 0` is a finite zero-byte allowance, not unlimited. The panel's quota form
requires a positive amount or an empty field, and direct purchases require a positive quota.
AWG packet sizes, count/header fields and timer scalar/range formats use
their own pinned definitions; see [the interface profile contract](#amneziawg-interface-profile-contract)
and [the upstream contract](../integrations/amneziawg.md). They must not be treated as subscription
GB, speed Kbps or calendar dates.

## Endpoint surface

| Group | Endpoints |
|---|---|
| Node | `GET /node`, `GET /node/health`, `GET /node/stats` |
| Users | `POST/GET /users`, `GET/PATCH/DELETE /users/{id}`, `POST /users/{id}/enable\|disable\|renew`, `POST /users/{id}/traffic/add\|set\|reset`, `GET /users/{id}/traffic` (series) |
| Integration | `POST /purchases`, `POST /users/{id}/quota/add`, `GET /operations/result` (all use an `Idempotency-Key` header), `GET /users/{id}/subscription` (private relative customer link), `POST /users/{id}/subscription/rotate` (link and all device keys), `GET/PUT/DELETE /users/{id}/next-plan` (one authorized successor), `GET /users/{id}/next-plan/activations` (bounded recovery history) |
| Bulk | `POST /users/bulk`, `POST /users/bulk-action` (`{action, user_ids, params}`) |
| Devices | `GET/POST /users/{id}/devices`, `GET/PATCH/DELETE /devices/{id}`, `POST /devices/{id}/enable\|disable\|regenerate`, `GET /devices/{id}/config\|qr` |
| Stats | `GET /stats`, `GET /node/telemetry`, `GET /users/{id}/stats`, `GET /devices/{id}/stats` |
| Templates | `GET/POST /templates`, `GET/PATCH/DELETE /templates/{id}` |
| Interfaces | `GET/POST /interfaces`, `GET/PATCH/DELETE /interfaces/{id}` (ports, subnet, MTU, params, rotation) |
| Settings | `GET/PATCH /settings` (typed registry; advanced keys gated by scope) |
| Webhooks | `GET/POST /webhooks`, `GET/PATCH/DELETE /webhooks/{id}`, `POST /webhooks/{id}/redeliver`, `GET /webhooks/{id}/deliveries` and `GET /webhooks/{id}/deliveries/{deliveryID}` |
| Ops | `GET /healthz` (public liveness), `GET /readyz`, `GET /openapi.json`, `GET /docs`; `GET /metrics` (config-gated, served outside `/api/v1`) |

**Backup/restore is deliberately not part of this API** (administrative panel + CLI only —
[ADR-0007](../decisions/ADR-0007-no-backup-rest-api.md)).

## User form to API mapping

Panel labels are presentation, not JSON property names. All paths below are relative to
`/api/v1`. Manual creation is `POST /users` (`users.create`); edits are
`PATCH /users/{id}` (`users.update`). These mutations are owner/node integration operations;
reseller tokens use the authorized template-gated purchase workflow instead.

| Panel field or action | REST equivalent | Exact behavior |
|---|---|---|
| Username / account | `username` on create | Required ASCII letters, digits, `_` or `-`, 3–32 characters; globally unique and immutable. Soft deletion keeps the name reserved. Resource paths use the returned opaque `id`, never the username. |
| Display name | `display_name` | Optional presentation label, separate from immutable identity; an empty string clears it on PATCH. |
| Subscription template / Custom | `template_id` | Omit or use `null` for manual terms. An enabled template copies quota, duration, start policy, device cap, up/down speed caps and interface on creation, overriding conflicting manual terms. PATCH changes only the reference; it does not reapply terms. Prices and external product IDs belong in the caller's catalog. |
| Traffic volume and GB/MB selector | `traffic_limit_bytes` | Exact integer bytes; decimal GB × 1,000,000,000 or MB × 1,000,000. Absent/null on manual create means unlimited; explicit null on PATCH removes the cap. Zero is a finite zero-byte allowance. Quota presets are form shortcuts, not API enums. |
| Device limit / Maximum devices | `device_limit` | Positive integer cap on registered device/peer configurations; absent/null on manual create means unlimited. It neither creates devices nor counts simultaneous connections or physical hardware IDs. See the device rules below. |
| Create ready subscription devices with this account | Purchase `device_count`; no `auto_devices` JSON property | The panel shortcut creates one device when the cap is unlimited, otherwise up to the cap, with a maximum of 10 per panel auto-create. `POST /users` and `/users/bulk` create no devices or customer link. `POST /purchases` accepts `device_count` for 1–100 ready configurations; omitted/null means one. The requested count must fit the account cap. |
| Duration and days/hours/months selector | `duration_seconds` | Positive integer seconds; month shortcut = 30 days. Manual create absent/null means no configured duration. `first_connection` also permits an unlimited duration. PATCH changes a stored duration only; absent/null does not clear it or recalculate expiry. |
| Exact date | No `expires_on`/`expires_at` create or PATCH input | The panel has a create-only calendar convenience. REST sets an existing account's expiry through `POST /users/{id}/renew` with `{"mode":"exact","exact":"2026-10-30T12:00:00Z"}`. This is a separate operation, not atomic exact-date creation; a reseller token cannot invoke this owner/node renewal route. `expires_at` is a response field. |
| Start policy | `start_policy` on create | `immediate` (default) or `first_connection`; not a user PATCH property. `activated_at`, `expires_at` and `status` in the response describe the resulting state. |
| Note | `note` | Internal text; JSON `\n` preserves line breaks. Empty string clears it; absent/null on PATCH leaves it unchanged. Do not put passwords, tokens or private configuration in notes. |
| Interface / connection profile | `interface_id` | Opaque profile ID, not a name such as `awg0`. Absent/null manual terms use automatic profile selection when a device is created. Changing the user reference does not move already-created devices. |
| Download/upload limit (MB/s) | `speed_limit_down_kbps` / `speed_limit_up_kbps` | Panel MB/s × 8,000 = REST decimal Kbps. Positive integers or null, independent in each direction. Caps aggregate across the user's devices on an interface; they are not a per-device guaranteed rate. |
| Comma-separated tags | `tags` | Send a JSON string array, e.g. `["vip","telegram"]`, not a comma-separated string. PATCH `[]` clears tags; absent/null keeps existing tags. |
| Enabled state | `enabled`, or `/enable` and `/disable` actions | Boolean, default true on create. `status` is the derived lifecycle state, not a writable create/PATCH field. |
| Integration annotations | `metadata` | Optional JSON object accepted on single-user creation and PATCH. The current PATCH handler does not persist metadata updates, so do not use it as an editable order journal. It is not a `/purchases` or bulk-create input. Keep the billing journal in the caller. |

### Device cap and provisioning rules

`device_limit: 3` permits at most three registered configurations in total. Disabled devices
still occupy slots. Deleting a device releases its slot; disabling it does not. Lowering the
cap on an existing account does not remove existing devices. New device creation fails with
409 `DEVICE_LIMIT_REACHED` when the current count reaches/exceeds the cap; the check and insert
share a database transaction. Ordinary user/template terms accept positive counts without the
direct-purchase maximum of 100; the panel auto-create maximum of 10 is not the API device cap.
Neither a physical device limit nor simultaneous-use detection is provided by this field.

Owner/node integrations can add one device with `POST /users/{id}/devices` (`devices.write`):
`{"name":"phone","preshared_key":false}`. The trimmed name must occupy 1–64 UTF-8 bytes.
Keys and addresses are generated on the server. Profile selection is explicit device
`interface_id`, then the user's profile, then the first enabled interface in name order.
The account must be enabled and peer-eligible and the selected interface must be enabled.
Read the device's private config/QR with `configs.read` or its customer link with
`subscriptions.read`. These are separate permissions. Device creation is not covered by the
ordinary idempotency replay middleware: after a lost response, inspect the owned device list
before retrying. Names are labels, not uniqueness/idempotency keys.

The panel's auto-create runs after account creation and creates devices in separate transactions;
it may stop partway through. It is not the purchase transaction. For a sale requiring an atomic
account + requested devices + customer link, use `/purchases`, and persist its operation key.
Additional-device writes are not available to reseller-bound REST tokens in the current contract.
Owner and assigned-template reseller purchases both support `device_count`. A larger
`device_limit` is capacity, not an instruction to allocate every slot. Send an explicit positive
`device_count` (at most 100 and not greater than the cap) to request all desired configurations.
A count of zero is invalid; use `/users` for an owner/node account without devices. Device labels
are `device-1`, `device-2`, etc., or `<device_name>-1`, `<device_name>-2`, etc. when a prefix is
supplied for multiple devices; single-device custom names remain unchanged. The final trimmed
label must occupy 1–64 UTF-8 bytes. The result contains `device_ids` in creation order and
`device_id` for the first item. Use each ID to retrieve its own config or QR. Old saved
single-device results may omit `device_ids`; their `device_id` remains available.
An identical count/order replay returns the same IDs, including after token rotation; changing
the count with the same key conflicts. Address-pool exhaustion, invalid generated names or any
device failure roll back all requested devices, the user, link, events and result journal.
Database commit means credentials exist, not that live reconciliation or a handshake succeeded.

### Other management surfaces and documentation coverage

The endpoint table and OpenAPI cover registered REST routes, including bulk creation/actions,
templates, profiles, settings, telemetry/statistics, usage corrections/reset, quota top-up,
renewal, successor queue/activation history, customer access rotation and webhook receipts.
Bulk creation accepts shared display name, note, tags and entitlement terms, not `metadata`,
`enabled`, `auto_devices` or exact expiry. Bulk actions return per-user success/errors and
are not an all-or-nothing financial transaction; `add_traffic` increases charged usage,
not quota. Their `update` parameters are a restricted subset of single-user PATCH.

Panel-only operations must not be guessed as REST paths: administrator/reseller creation and
permission assignment, API-token issuance/revocation, account restore, customer-link
create/revoke/restore, downloading all configs as a ZIP, backup/restore, installer/update control
and browser appearance preferences have no corresponding management REST operation here.
Server defaults and public appearance settings exposed by `/settings` remain distinct from
per-browser/account preferences. `GET /users/{id}/subscription` reads an existing active link;
`/subscription/rotate` replaces access; neither is a dedicated link revoke/restore endpoint.
The owner assigns reseller grants/templates in the panel. Having a token scope does not bypass
the route's owner/reseller boundary. Consult OpenAPI security and the automation boundaries below.

## Connecting a sales bot

Give the bot the panel's HTTPS base URL and a **scoped API token**, sent as
`Authorization: Bearer <token>`. A panel username/password authenticates a human browser session;
it is not the REST automation credential. The owner can issue a node-wide token for an
owner-operated bot. For a reseller bot, create a reseller, assign allowed technical templates and issue a
token bound to that reseller; it cannot read or mutate another reseller's customers. Keep one
token per integration so it can be rotated or revoked independently. The panel's Read only,
Operations and Full access shortcuts select current exact scopes, then permit individual edits;
they do not select family wildcards or silently widen existing tokens when a future scope appears.
Read only omits private configurations and customer-link capabilities. Operations includes
purchase, usage reset and successor-plan actions; Full access includes sensitive management.
The owner token editor shows REST scopes only. Review the resulting grants before issuance.

A bot can create a customer and requested devices atomically through `POST /purchases`, recover an
uncertain result through `GET /operations/result`, read current state with scoped GET endpoints,
and use `POST /users/{id}/quota/add`, `POST /users/{id}/traffic/reset` or the queued Next Plan
endpoints as needed. A quota top-up needs `users.update`; result recovery needs `operations.read`.
The owner-operated bot may send an enabled `template_id` or direct finite `entitlement` terms;
the latter requires positive byte quota, duration in seconds and device count, and does not
create a catalog entry. Reseller-bound tokens must use an owner-assigned enabled template until
an independent reseller entitlement policy is designed. The bot's 100 GB/month SKU and price
therefore never have to become a WG-Guard template. Payment, order ownership and Telegram
delivery belong to the bot, not WG-Guard. Webhooks are optional
change notifications; polling the API works without them. If enabled, the receiver must be an
HTTPS endpoint run by the bot/integration, not the WG-Guard panel address. See
[webhook delivery](../integrations/webhooks.md).

For an owner-operated sale, the bot keeps its own product/price ID and provisions only technical
terms. This single request creates the customer, requested devices and customer link in one database
transaction:

```http
POST /api/v1/purchases
Authorization: Bearer <owner-scoped API token>
Idempotency-Key: order-123-provision
Content-Type: application/json

{"username":"customer123","entitlement":{"traffic_limit_bytes":100000000000,"duration_seconds":2592000,"device_limit":1,"speed_limit_down_kbps":100000,"speed_limit_up_kbps":20000}}
```

This grants 100 GB for 30 days, with download capped at 100 Mbps (12.5 MB/s) and upload at
20 Mbps (2.5 MB/s). These are caps, not a guaranteed measured throughput.

To create **three ready configurations** within a three-device allowance, send:

```json
{"username":"customer123","device_count":3,"entitlement":{"traffic_limit_bytes":100000000000,"duration_seconds":2592000,"device_limit":3}}
```

Use the same required `Idempotency-Key` as above. A reseller sends
`{"template_id":"<owner-assigned-template-id>","device_count":3}`; the assigned template must
allow at least three devices. The response lists all three `device_ids`; the customer link
shows each device's separate config/QR. Setting only `device_limit: 3` still provisions one
initial device by default.

For a reseller integration, use `{"template_id":"<owner-assigned-template-id>"}` instead of
`entitlement`. A direct volume add-on uses the separate optional top-up command below; a simple
sale or replacement does not need it. There is no standalone "add time" purchase command:
expiry-only renewal remains an administrative operation, not a payment workflow.

A paid **1 GB** volume add-on is one request with a fresh order-specific key (exact bytes):

```http
POST /api/v1/users/{id}/quota/add
Authorization: Bearer <scoped API token>
Idempotency-Key: order-123-volume-1
Content-Type: application/json

{"bytes":1000000000}
```

Store the key with the order. If the response is lost, retry that request or read
`GET /api/v1/operations/result` with the same key; do not invent a second key for the same
payment. WG-Guard does not process or reverse payments.

## Automation boundaries

`POST /users` creates a user record, not a device or usable configuration. If its
`template_id` names an enabled template, the template supplies technical terms and overrides
conflicting manual entitlement fields; `POST /users/bulk` applies the same rule to each user.
`PATCH /users/{id}` changes a template reference without retroactively resetting existing
allowances or expiry.
`POST /purchases` accepts exactly one of `template_id` or `entitlement`; it commits the user,
requested devices (one by default), customer link and non-secret result together. Direct `entitlement` is available
only to owner-scoped integrations and requires finite positive quota, duration (at most ten years)
and device count (at most 100). Reseller purchases require an owner-assigned enabled template.
Omitted usernames are generated deterministically from
the principal and key. Reconciliation follows the database commit, so `committed` does not
assert a successful live handshake. A user ID is opaque and `{id}` is not a username.
`POST /users/{id}/renew` changes the expiry policy only; quota and consumed-traffic
operations are separate. `POST /users/{id}/traffic/reset` is the independent one-op Reset Usage
action; a reseller with `traffic.update` may invoke it only for an owned customer. There is no
four-policy combined renewal or batch-by-ID read.
The REST `POST /users/{id}/traffic/add` adjusts **charged usage**, so a sales bot must never
interpret it as extra allowance. `POST /users/{id}/quota/add` is the distinct top-up contract:
`{"bytes": positive_integer}` increases a finite quota while preserving the RX/TX counters.
It requires `users.update` and an `Idempotency-Key`; reseller tokens are limited to owned users.
The quota mutation, webhook outbox rows and a non-secret before/after result commit in one
transaction. Reuse of the same principal/key with different user, amount or operation returns
409; identical retries return the prior result, including after a token rotation within that
principal. `GET /operations/result` can recover it for 90 days. A traffic-only quota block is
lifted when the new limit exceeds current usage; manual blocks remain. A 503 after commit means
runtime reconciliation is pending: read the committed operation and retry with the same key,
which never adds the allowance again. Unlimited accounts need an explicit finite limit via Edit
or PATCH before top-up. The authenticated panel uses its own `/users/{id}/quota/add` form action;
the separate panel `/traffic/add` action corrects charged counters.
WG-Guard records the entitlement but does not collect payment or reverse a settled sale.
`GET/PUT/DELETE /users/{id}/next-plan` uses dedicated read/write scopes and ownership checks.
One explicitly authorized successor is queued per customer; putting another replaces it. This
does not charge the customer. Template terms are copied at queue time, and later catalog edits or
reseller-template access changes do not silently alter a paid successor. The first actual time or
quota boundary activates it once, starting the new duration then and resetting charged usage;
raw peer baselines remain intact. Optional unused-volume carry applies only on time expiry.
Manual disable/suspension pauses activation; current-template or device incompatibility sets a
visible `needs_review` state. The periodic pass inspects at most 50 due successors per cycle.
An incomplete tunnel dump defers this pass so stale metering cannot over-credit carried traffic.
The queue can be canceled before activation; cancellation is not reversal of an activated subscription.
The bounded activation read returns the latest 20 (up to 100) non-secret before/after records
for one year so a bot can recover a missed asynchronous transition.
The customer subscription capability path is readable via a scoped, no-store REST endpoint;
rotation uses a separate sensitive scope and atomically replaces the link and all device keys.
The new state is retained if runtime reconciliation fails, and a 503 tells the caller to inspect
the current link before retrying. Do not treat admin `/config` or `/qr` responses, which contain
device private material, as an equivalent customer-link contract. Webhooks now have per-event
OpenAPI schemas, tenant-scoped fanout and non-secret delivery receipts. Their at-least-once
delivery has no total or per-user ordering guarantee; consumers reconcile with resource GETs.
See [the delivery contract](../integrations/webhooks.md).

Owner integrations retain node-wide authority. Reseller sessions/tokens are restricted to their
owned customers and endpoints under configurable live grants. One queued successor and owned
Reset Usage cover the requested lifecycle; a combined four-policy renewal or immediate subscription
replacement/correction is not part of this contract.

## Live telemetry

`GET /api/v1/node/telemetry` requires `stats.read` and reads only the shared scheduler-owned
in-memory ring. `points` defaults to 60, rejects missing/duplicate/non-positive values, and clamps
larger integers to the fixed 180-point capacity. Points are oldest-to-newest; `latest` includes
staleness evaluation at request time. A fresh process with no point returns `latest: null` and an
empty array.

Every metric key is present. An unavailable reading is JSON `null`, not a fabricated zero. Units
are explicit in field names: bytes, bytes per second, percentages, seconds, and counts. Health is
`healthy`, `degraded`, or `unavailable` with a closed list of non-secret issue codes. The response
never contains interface names, endpoints, database/subprocess errors, or raw configuration. Live
history is intentionally lost on restart; accounting rollups remain the durable traffic history.

## AmneziaWG interface profile contract

The interface API uses explicit request and response DTOs; repository structs never reach the
wire. Property names are lower snake case. H1–H4 accept a JSON integer for a scalar or a string
`"N-M"` for a true inclusive u32 interval. The six padding/timing values use the same shape with
u16 bounds. Responses preserve that compatibility shape: scalars remain numbers and true ranges
remain strings. Enabled H intervals must be non-zero and pairwise non-overlapping.

Supported advanced fields are S3/S4, I1–I5, HeaderProtectionKey, ContentPaddingAddition,
RekeyAfterTime, RekeyTimeout, RejectAfterTime, KeepaliveTimeout, MaxHandshakeAttempts,
RandomTrailers, and DisableCookies, subject to the gates in
[the pinned AWG contract](../integrations/amneziawg.md). `AdvancedSecurity` is unsupported and is
not accepted or advertised. HeaderProtectionKey is write-only: responses expose only
`header_protection_key_set`. Omitting the key while PATCHing an obfuscation block preserves it;
an explicit empty string clears it and causes reconciliation to recreate the link because the
pinned runtime cannot clear it in place.

Values that later occupy a configuration line are validated at the API/service boundary and
again before client rendering. `endpoint_override` accepts only a canonical hostname or IP,
optionally with a port (IPv6 with an explicit port uses brackets); schemes, paths, whitespace,
and control characters are rejected. I1–I5 accept canonical single-line UTF-8 values and reject
leading/trailing whitespace or any control character. This prevents an authorized but malformed
request—or a corrupt database row—from injecting additional client-configuration directives.

On interface creation, `preset` accepts `plain`, the legacy-safe `recommended`, the operational
`performance`, `balanced`, `resilient`, and `suggested` profiles, or `randomized`. It is mutually
exclusive with an explicit `obfuscation` object. Generated values come from the same server policy
used by the panel, are validated as one coherent set, and persist with their actual classification.
The `suggested` profile defaults MTU to 1280 when the request omits MTU; client DNS remains the
global network setting. Explicit parameters are classified as `custom`; an omitted profile with no
parameters is `plain`. API responses report those classifications plus `custom`. Generated HPKs
remain write-only and are represented only by `header_protection_key_set` in REST responses.

The panel's generated-profile preview is not a client-authored classification hint. The server
seals the exact generated policy and values under the installation key and binds that token to
the authenticated session's CSRF value. Create accepts a generated classification only when the
submitted fields match the sealed preview; an edit may preserve one without a new token only when
every AWG field is unchanged. The REST `preset` path remains wholly server-generated and never
accepts client-supplied values alongside the policy.

## Client configuration delivery

`GET /api/v1/devices/{id}/config`, the authenticated panel download, and the public subscription
download delegate to the same `internal/clientconf.Renderer`; no HTTP surface assembles or
rewrites configuration text. The canonical serialization puts all client/interface keys before
`[Peer]`, uses the selected tunnel interface's MTU, preserves scalar/range values, validates
stored DNS/AllowedIPs/PersistentKeepalive before decrypting keys, and ends with exactly one
newline. The REST response remains `text/plain; charset=utf-8`; browser downloads use
`application/octet-stream` to avoid the text MIME type adding a `.txt` extension on mobile.
All are attachments
with the same sanitized filename, `Cache-Control: no-store`, and `X-Content-Type-Options: nosniff`.
The filename uses up to six safe username-label characters (including optional short prefix/
suffix parts), then a stable eight-character device-ID code. The importable profile stem stays
within 15 ASCII characters and does not depend on the editable device name.

The matching QR endpoints pass those same bytes to the single bounded QR encoder. Its PNG has a
four-module white quiet zone, integer module scaling, and medium error correction; independent
test decoding proves content equality. Oversized configurations fail with a typed client error
and no partial image. The test-only decoder is not linked into the production binary.

## Webhooks

Durable and restart-safe (delivery state in SQLite; the event row commits in the SAME
transaction as the state change, so an accepted request can never lose its event); HMAC-signed
(`X-WG-Signature: t=<ts>,v1=<hex>`) with a replay window; exponential backoff (30 s × 2ⁿ, capped
at 6 h), capped concurrency, dead-letter after `webhooks.max_attempts` (default 12), manual
redeliver. The worker runs one delivery pass every 5 s on the central scheduler; event rows are
pruned after 7 days. Endpoint secrets are AES-GCM encrypted at rest and shown exactly once at
creation — they can be rotated but never re-displayed. The owner can manage every endpoint;
ordinary node admins/tokens are limited to node-wide endpoints without owner-only reseller
fanout opt-in, and reseller endpoint CRUD, receipts and fanout are tenant-scoped. Existing
node-wide endpoints migrate with cross-reseller fanout off. Reseller destinations use public
HTTPS with checked IP dialing and no redirects. Event catalog, payload schemas and
reconciliation guidance:
[../integrations/webhooks.md](../integrations/webhooks.md).

## OpenAPI

`/openapi.json` (+ lightweight `/docs` reference) is hand-authored. A route-coverage test checks
that every registered route appears with the correct scope and that the document has no stale
paths; focused contract tests cover selected schemas, the full typed webhook catalog and behavior.
User/device/template DTO and bulk-action parameter field coverage is checked against the
description so accepted or returned fields cannot silently disappear from generated references.
The description uses OpenAPI 3.2.1 and JSON Schema null unions. Its `info.version` remains
`1.0.0` for the V1 API contract; the `openapi` field versions the description format,
not a WG-Guard release or a new endpoint set. Consumers parsing the description need tooling
that understands OpenAPI 3.2; existing HTTP clients do not change.

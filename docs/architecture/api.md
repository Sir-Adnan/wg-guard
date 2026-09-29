# REST API (`/api/v1`)

The API is a **compatibility contract** from V1: additive changes only — no field renames, no
meaning changes, no removed enum values, no newly-required fields without a versioning strategy.
External systems integrate from `GET /api/v1/node/health` alone (capability discovery).

## Conventions

- **Auth**: `Authorization: Bearer wg_…` API tokens with scopes (users.read/create/update/delete/
  bulk, devices.*, configs.read, traffic.read/update, plans.read/write, stats.read, node.read,
  node.settings, webhooks.read/write, interfaces.*, purchases.create, operations.read and
  subscriptions.read/rotate). Tokens are separate from admin sessions; the
  `wg-guard token create|list|revoke|scopes` CLI mints them (the panel's token screen is the
  day-to-day manager). Panel-issued tokens are bound to the issuing account and cannot retain
  permissions removed from that account. Existing CLI/pre-Phase-14 tokens remain node-wide and
  should be reviewed by the owner when enabling reseller integrations. Reseller-bound tokens may
  use only explicitly tenant-gated reads for users, devices/configs, per-user statistics and
  customer links, plus plan-gated purchase/result operations. Lists are ownership-filtered.
  Other mutations, unclassified routes and global aggregates remain denied.
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
- **Tri-state PATCH semantics** (users, plans, interfaces, webhooks): a field **absent** from the
  body means "no change"; an explicit JSON **null** means "clear to unlimited/none" (e.g.
  `{"speed_limit_up_kbps": null}` removes only the upload cap); a value sets it. This is how
  independent up/down speed limits change one at a time without re-sending the other.
- **Idempotency**: `Idempotency-Key` header (1–128 printable chars) is persisted for create-user,
  bulk create/action, renew, and traffic mutations. Authentication, scope checks and rate limits
  precede every replay. Keys are isolated per verified API token: the same token and request
  replay the stored response with `Idempotency-Replayed: true`; a different request using that
  token's key returns 409 (`IDEMPOTENCY_KEY_REUSED`). Keys remain valid for 24 h, with expired
  rows retired on reuse. An unexpired key written before token scoping fails closed with 409;
  its caller must reconcile the previous result before using a fresh key. The claim and response
  snapshot are not committed atomically with the mutation: an interruption after the mutation but
  before the snapshot can leave an ambiguous in-flight key. V1 has no lookup-by-key or recovery
  endpoint; external billing workflows using these legacy routes must still reconcile durable
  resource state. New `/purchases` uses an atomic result journal instead: the key is scoped to
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

## Endpoint surface

| Group | Endpoints |
|---|---|
| Node | `GET /node`, `GET /node/health`, `GET /node/stats` |
| Users | `POST/GET /users`, `GET/PATCH/DELETE /users/{id}`, `POST /users/{id}/enable\|disable\|renew`, `POST /users/{id}/traffic/add\|set\|reset`, `GET /users/{id}/traffic` (series) |
| Integration | `POST /purchases`, `GET /operations/result` (both use an `Idempotency-Key` header), `GET /users/{id}/subscription` (private relative customer link), `POST /users/{id}/subscription/rotate` (link and all device keys) |
| Bulk | `POST /users/bulk`, `POST /users/bulk-action` (`{action, user_ids, params}`) |
| Devices | `GET/POST /users/{id}/devices`, `GET/PATCH/DELETE /devices/{id}`, `POST /devices/{id}/enable\|disable\|regenerate`, `GET /devices/{id}/config\|qr` |
| Stats | `GET /stats`, `GET /node/telemetry`, `GET /users/{id}/stats`, `GET /devices/{id}/stats` |
| Plans | `GET/POST /plans`, `GET/PATCH/DELETE /plans/{id}` |
| Interfaces | `GET/POST /interfaces`, `GET/PATCH/DELETE /interfaces/{id}` (ports, subnet, MTU, params, rotation) |
| Settings | `GET/PATCH /settings` (typed registry; advanced keys gated by scope) |
| Webhooks | `GET/POST /webhooks`, `PATCH/DELETE /webhooks/{id}`, `POST /webhooks/{id}/redeliver` |
| Ops | `GET /healthz` (public liveness), `GET /readyz`, `GET /openapi.json`, `GET /docs`; `GET /metrics` (config-gated, served outside `/api/v1`) |

**Backup/restore is deliberately not part of this API** (administrative panel + CLI only —
[ADR-0007](../decisions/ADR-0007-no-backup-rest-api.md)).

## Automation boundaries (current V1)

Legacy `POST /users` creates a user record, not a device or usable configuration. The new
`POST /purchases` uses an enabled plan and commits the user, initial device, customer link and
non-secret result together. Reseller purchases require owner-assigned plan access; owner
integrations may use any enabled plan. Omitted usernames are generated deterministically from
the principal and key. Reconciliation follows the database commit, so `committed` does not
assert a successful live handshake. A user ID is opaque and `{id}` is not a username.
Legacy `POST /users/{id}/renew`
changes the expiry policy only; quota and consumed-traffic operations are separate. There is no
atomic combined time/volume renewal, conditional reversal of a prior renewal, or batch-by-ID read.
The customer subscription capability path is readable via a scoped, no-store REST endpoint;
rotation uses a separate sensitive scope and atomically replaces the link and all device keys.
The new state is retained if runtime reconciliation fails, and a 503 tells the caller to inspect
the current link before retrying. Do not treat admin `/config` or `/qr` responses, which contain device
private material, as an equivalent customer-link contract. Webhooks have durable event IDs and
documented retry/dead-letter behavior, but no complete per-event OpenAPI payload schemas or total
ordering guarantee across endpoints. These limits are tracked as an additive future integration phase in
[ROADMAP.md](../../ROADMAP.md).

**Remaining Phase 14 direction:** Owner integrations retain node-wide authority;
reseller panel accounts and their tokens are restricted to users owned by that reseller, with
configurable grants that cannot exceed the reseller's current permissions. Ownership applies to
every read, mutation, aggregate, config, public-link management action and webhook, not only list
filters. The purchase/result flow, customer-link delivery and credential rotation are implemented
in the Phase 14 branch. Renewal still needs independent time/volume carry or replace choices,
explicit preconditions and conditional reversal preserving later usage; tenant webhook fanout
also remains. Existing V1 endpoints keep their meanings.

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
creation — they can be rotated but never re-displayed. Event catalog and payload schemas:
[../integrations/webhooks.md](../integrations/webhooks.md).

## OpenAPI

`/openapi.json` (+ lightweight `/docs` reference) is hand-authored. A route-coverage test checks
that every registered route appears with the correct scope and that the document has no stale
paths; focused contract tests cover selected schemas and behavior.
The description uses OpenAPI 3.2.1 and JSON Schema null unions. Its `info.version` remains
`1.0.0` for the unchanged V1 API contract; the `openapi` field versions the description format,
not a WG-Guard release or a new endpoint set. Consumers parsing the description need tooling
that understands OpenAPI 3.2; existing HTTP clients do not change.

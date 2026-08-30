# REST API (`/api/v1`)

The API is a **compatibility contract** from V1: additive changes only — no field renames, no
meaning changes, no removed enum values, no newly-required fields without a versioning strategy.
External systems integrate from `GET /api/v1/node/health` alone (capability discovery).

## Conventions

- **Auth**: `Authorization: Bearer wg_…` API tokens with scopes (users.read/create/update/delete/
  bulk, devices.*, configs.read, traffic.read/update, plans.read/write, stats.read, node.read,
  node.settings, webhooks.read/write, interfaces.*). Tokens are separate from admin sessions; the
  `wg-guard token create|list|revoke|scopes` CLI mints them until the panel ships (Phase 5).
- **Errors**: one envelope — `{"error": {"code", "message", "request_id"}}` with stable codes
  (`USER_NOT_FOUND`, `USERNAME_EXISTS`, `DEVICE_LIMIT_REACHED`, `TRAFFIC_EXCEEDED`, `INVALID_REQUEST`,
  `UNAUTHORIZED`, `FORBIDDEN`, `RATE_LIMITED`, `NODE_UNAVAILABLE`, `INTERNAL_ERROR`, …). No stack
  traces. The `X-Request-Id` response header carries the correlation id (client-supplied ids are
  honored when they are ≤ 64 printable-ASCII bytes).
- **Pagination**: keyset cursor — `limit` ≤ 500, opaque `cursor` (base64url JSON), items ordered
  by the chosen sort; stable ordering even for rows written in the same microsecond (id
  tiebreak). Filters per the archived spec §22 (status, expires_before/after, traffic_exceeded,
  enabled, created range, search with literal `%`/`_` semantics).
- **Tri-state PATCH semantics** (users, plans, interfaces, webhooks): a field **absent** from the
  body means "no change"; an explicit JSON **null** means "clear to unlimited/none" (e.g.
  `{"speed_limit_up_kbps": null}` removes only the upload cap); a value sets it. This is how
  independent up/down speed limits change one at a time without re-sending the other.
- **Idempotency**: `Idempotency-Key` header (1–128 printable chars) persisted for create-user,
  bulk create/action, renew, and traffic mutations — retries never duplicate effects. A replayed
  key returns the stored response with `Idempotency-Replayed: true`; reusing a key with a
  different request is a 409 (`IDEMPOTENCY_KEY_REUSED`). Keys are kept 24 h, then pruned.
- **Rate limits**: per-token fixed 60 s window (`api.rate_limit_per_minute`, default 600; 0
  disables). Responses carry `X-RateLimit-Limit`/`X-RateLimit-Remaining`; a 429 carries
  `Retry-After`. Setting changes apply live (no restart).
- **Sensitive endpoints** (`/config`, `/qr`): require `configs.read`, always
  `Cache-Control: no-store`; private keys are never logged.
- **Config generation is on demand**: client configs are a pure function of current settings, so
  endpoint/DNS/MTU changes propagate to every new download immediately.

## Endpoint surface

| Group | Endpoints |
|---|---|
| Node | `GET /node`, `GET /node/health`, `GET /node/stats` |
| Users | `POST/GET /users`, `GET/PATCH/DELETE /users/{id}`, `POST /users/{id}/enable\|disable\|renew`, `POST /users/{id}/traffic/add\|set\|reset`, `GET /users/{id}/traffic` (series) |
| Bulk | `POST /users/bulk`, `POST /users/bulk-action` (`{action, user_ids, params}`) |
| Devices | `GET/POST /users/{id}/devices`, `GET/PATCH/DELETE /devices/{id}`, `POST /devices/{id}/enable\|disable\|regenerate`, `GET /devices/{id}/config\|qr` |
| Stats | `GET /stats`, `GET /users/{id}/stats`, `GET /devices/{id}/stats` |
| Plans | `GET/POST /plans`, `GET/PATCH/DELETE /plans/{id}` |
| Interfaces | `GET/POST /interfaces`, `GET/PATCH/DELETE /interfaces/{id}` (ports, subnet, MTU, params, rotation) |
| Settings | `GET/PATCH /settings` (typed registry; advanced keys gated by scope) |
| Webhooks | `GET/POST /webhooks`, `PATCH/DELETE /webhooks/{id}`, `POST /webhooks/{id}/redeliver` |
| Ops | `GET /healthz` (public liveness), `GET /readyz`, `GET /openapi.json`, `GET /docs`; `GET /metrics` (config-gated, served outside `/api/v1`) |

**Backup/restore is deliberately not part of this API** (administrative panel + CLI only —
[ADR-0007](../decisions/ADR-0007-no-backup-rest-api.md)).

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

`/openapi.json` (+ lightweight `/docs` reference) is hand-authored and kept accurate by a
route-coverage test: every registered route must appear in the document with correct auth and
pagination declarations.

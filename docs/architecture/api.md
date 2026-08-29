# REST API (`/api/v1`)

The API is a **compatibility contract** from V1: additive changes only — no field renames, no
meaning changes, no removed enum values, no newly-required fields without a versioning strategy.
External systems integrate from `GET /api/v1/node/health` alone (capability discovery).

## Conventions

- **Auth**: `Authorization: Bearer wg_…` API tokens with scopes (users.read/create/update/delete/
  bulk, devices.*, configs.read, traffic.read/update, plans.read/write, stats.read, node.read,
  node.settings, webhooks.read/write, interfaces.*). Tokens are separate from admin sessions.
- **Errors**: one envelope — `{"error": {"code", "message", "request_id"}}` with stable codes
  (`USER_NOT_FOUND`, `USERNAME_EXISTS`, `DEVICE_LIMIT_REACHED`, `TRAFFIC_EXCEEDED`, `INVALID_REQUEST`,
  `UNAUTHORIZED`, `FORBIDDEN`, `RATE_LIMITED`, `NODE_UNAVAILABLE`, `INTERNAL_ERROR`, …). No stack
  traces.
- **Pagination**: cursor-based (`limit` ≤ 500, `cursor`), deterministic sorting; filters per the
  archived spec §22 (status, expires_before/after, traffic_exceeded, enabled, created range).
- **Idempotency**: `Idempotency-Key` header persisted for create-user, bulk create, renew, and
  traffic mutations — retries never duplicate effects.
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
| Ops | `GET /healthz` (public liveness), `GET /readyz` |

**Backup/restore is deliberately not part of this API** (administrative panel + CLI only —
[ADR-0007](../decisions/ADR-0007-no-backup-rest-api.md)).

## Webhooks

Durable and restart-safe (delivery state in SQLite); HMAC-signed (`X-WG-Signature: t=<ts>,v1=<hex>`)
with a replay window; exponential backoff, capped concurrency, dead-letter after N attempts,
manual redeliver. Event catalog and payload schemas:
[../integrations/webhooks.md](../integrations/webhooks.md).

## OpenAPI

`/openapi.json` (+ lightweight `/docs` reference) is hand-authored and kept accurate by a
route-coverage test: every registered route must appear in the document with correct auth and
pagination declarations.

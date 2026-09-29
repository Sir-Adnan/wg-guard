# Webhooks

Webhooks notify an integration that a resource changed. Treat them as signals to reconcile with
the REST API, not as ordered commands or a billing ledger. The event catalog and exact per-event
JSON schemas are published in [`openapi.json`](../../internal/api/openapi.json) under `webhooks`
and `components.schemas.WebhookEnvelope`.

## Ownership and setup

- A node-wide endpoint receives `node.started` and events for users without a reseller. Only the
  owner can explicitly enable **Include reseller customer events** on such an endpoint to receive
  every reseller's subscribed events too. Existing endpoints migrate with that option off, so
  adding resellers cannot silently send their customer data to an old integration. The owner may
  manage every endpoint; non-owner node admins and their tokens manage only node-wide endpoints
  **without** this opt-in and cannot enable it. Legacy host-CLI tokens retain node-wide owner
  authority.
- An endpoint created with a reseller-bound session or API token is bound to that reseller. It
  receives only events for users currently owned by that reseller; it cannot subscribe to
  `node.started`. A reseller can list, change, rotate, delete and inspect only its own endpoints.
  Events are classified from the persisted user in the mutation transaction, never from a
  claimed reseller ID in the payload. Missing user ownership does not fan out to resellers.
- Reseller endpoints require public HTTPS. Literal private/special-use addresses are rejected
  when saved. At delivery, DNS is resolved once and only checked public IPs are dialed; redirects
  are not followed and proxies are not used. This prevents a reseller-controlled receiver from
  reaching loopback, internal networks or node metadata through DNS changes.
- A generated signing secret is returned only on create or rotation. It is encrypted at rest and
  never exposed by endpoint GET/list or delivery receipts. Keep the receiver URL and secret out of
  logs and source control.

The subscriber selects events from this V1 catalog: `user.created`, `user.updated`,
`user.enabled`, `user.disabled`, `user.expired`, `user.traffic_exceeded`,
`user.first_connected`, `device.created`, `device.deleted`, `node.started`.
`user.updated` may indicate a regular edit, deletion/restoration, or a queued successor plan
activation (`plan_id` and `next_plan_trigger`). Read the current resource to distinguish state.
Private keys, configs and customer-link capabilities are never event payload fields.

## Delivery and verification

The state mutation, event row and delivery rows commit in one SQLite transaction. A subscribed,
enabled endpoint gets one pending delivery at event creation. `node.started` is best-effort after
serve starts. The single central scheduler checks deliveries every five seconds, selects at most
64 per pass and sends at most four concurrent POSTs with a ten-second request timeout.

Each POST carries a JSON body with `id` (stable event ID), `type`, `timestamp` (UTC RFC3339),
`node_id` and typed `data`. Headers include `X-WG-Event`, `X-WG-Delivery` (stable for that
endpoint/event), and:

```text
X-WG-Signature: t=<unix-seconds>,v1=<hex HMAC-SHA256(secret, "<t>.<raw-body>")>
```

Verify the signature over the **exact received bytes**, compare in constant time, and reject a
timestamp outside roughly five minutes. Return any 2xx to acknowledge. Transport failure or
non-2xx retries after 30 seconds, doubling to a six-hour cap. After
`webhooks.max_attempts` (default 12), the delivery becomes `dead` until manually redelivered.
Events and deliveries are retained for seven days. Disabled endpoints pause their queued
deliveries until re-enabled or retention prunes them.

Delivery is **at least once**: a crash after the receiver accepts a POST but before WG-Guard
records the receipt can send it again. Deduplicate by event `id`. Concurrent requests and retries
mean no total, per-user or cross-endpoint delivery order is guaranteed. A receiver should fetch
the current owned resource by ID before applying a state change. `suspended` and
`waiting_first_connection` remain user states; receivers must not infer a purchase or payment from
one webhook.

## Reconciliation

`GET /api/v1/webhooks/{id}/deliveries` (`webhooks.read`) returns recent non-secret receipts,
newest first. `limit` is 1–100 (default 50); use `next_before` as `before` for the next page, or
filter by exact `event_id`. `GET /api/v1/webhooks/{id}/deliveries/{deliveryID}` reads one receipt.
Receipts contain event/delivery IDs, event type, `pending|delivered|dead`, attempt count and
timestamps; they contain no payload, URL, secret or raw network error. A reseller cannot inspect
another reseller's receipts, even if it guesses IDs. `POST /api/v1/webhooks/{id}/redeliver`
requeues a retained delivery; the panel provides the same action.

For a lost purchase response, use the principal-scoped operation result by `Idempotency-Key`.
For an asynchronous successor-plan transition, use the user's activation history. A missing
webhook or receipt after retention is not proof that a business operation did not occur.

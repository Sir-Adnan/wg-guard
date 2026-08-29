# Webhooks

Lightweight, durable, restart-safe outbound webhooks. Delivery state lives in SQLite — an
in-memory queue never holds the only copy of an event (a restart must not lose integrations'
events).

## Delivery model

1. The state-changing transaction also inserts the `webhook_events` row (atomic with the
   change — events cannot disappear).
2. A single worker (part of the central scheduler) selects due deliveries by index, delivers
   with capped concurrency, and records attempts — one broken endpoint can never block the
   process.
3. Retry: exponential backoff; after N attempts (configurable, default 12) the delivery becomes
   `dead` (visible in the UI/API, manual redeliver endpoint).
4. Payloads are pruned per retention policy; event rows are compact.

## Signature

```
X-WG-Event: user.created
X-WG-Signature: t=1690000000,v1=<hex hmac-sha256(secret, "<t>.<body>")>
```

Replay window: receivers must reject timestamps older than ~5 minutes. Endpoint secrets are
encrypted at rest and rotatable; a test-ping action verifies configuration.

## Event catalog (V1)

`user.created, user.updated, user.enabled, user.disabled, user.expired, user.traffic_exceeded,
user.first_connected, device.created, device.deleted, node.started`

Payload: event id, type, timestamp, node id, and a typed `data` object per event; documented in
OpenAPI. Subscribing endpoints select which events they receive.

# Database design

SQLite in WAL mode, foreign keys ON, `busy_timeout=5s`, capped page cache (low-RAM requirement).
Driver: `modernc.org/sqlite` (pure Go). Explicit repository code — no ORM. All timestamps UTC.

## Schema (Phase 1 implements; this is the contract)

| Table | Purpose / key columns |
|---|---|
| `tunnel_interfaces` | name (`awgN`, unique), listen_port, ipv4_subnet, mtu, public_key + private_key_encrypted (AES-GCM under the master key), obfuscation params (Jc, Jmin, Jmax, S1, S2, H1–H4, optional I1–I5 blob, preset name), enabled, backend mode, endpoint override |
| `users` | id (UUIDv7), username UNIQUE, display_name, note, tags, status (`active\|disabled\|suspended\|expired\|traffic_exceeded\|waiting_first_connection`), disable_reason (`manual\|expired\|traffic_limit\|admin_action`), traffic_limit_bytes (NULL=unlimited), traffic_used_rx/tx, speed_limit_kbps, device_limit, plan_id FK NULL, interface_id FK, start_policy (`immediate\|first_connection`), duration_seconds, activated_at, expires_at, last_activity_at, enabled, deleted_at (soft delete; username stays reserved), metadata JSON |
| `devices` | id, user_id FK, interface_id FK, name, ipv4_address, public_key UNIQUE, private_key_encrypted, preshared_key_encrypted, enabled, last_handshake_at, last_endpoint, rx_bytes/tx_bytes (accumulated), last_rx/last_tx (raw counter snapshot for delta logic) |
| `plans` | id, name, quota, duration, start_policy, device_limit, speed_limit, interface/profile selector, enabled |
| `admins` | id, username, argon2id hash, role (`owner\|admin`), permissions JSON, enabled |
| `admin_sessions` | id, admin FK, token hash, created/last_seen/expires, source IP |
| `api_tokens` | id, name, prefix (indexed), hash, scopes JSON, expires_at, enabled, cidr allowlist, last_used_at |
| `webhook_endpoints` | id, url, secret_encrypted, enabled, events JSON |
| `webhook_deliveries` | id, endpoint FK, event type, payload, status (`pending\|delivered\|dead`), attempts, next_attempt_at (indexed), last error |
| `webhook_events` | durable event rows inserted in the same transaction as the state change |
| `audit_log` | ts, actor type/id, action, target, source IP, request id, safe metadata |
| `idempotency_keys` | key, request hash, response snapshot, expires_at |
| `settings` | key, value (JSON), updated_at |
| `backup_schedules` | id, mode (daily@time / every-N-hours / weekly), time UTC, retention, enabled |
| `traffic_samples` | device FK, ts, rx_delta, tx_delta (bounded: 24–48 h) |
| `traffic_rollups` | hourly (30 d) and daily (1 y) aggregates |
| `migrations` | version, applied_at |

Allocation: per-interface IPv4 pool with `UNIQUE(interface_id, ipv4_address)`; allocation in a
transaction with conflict retry; IPs released on permanent device delete.

## Invariants

- Accounting: accumulated totals live in SQLite, never in AWG counters. `new < last ⇒ reset ⇒
  count current as delta and re-baseline` (no negative deltas, no reset corruption, no double
  counting). Peer deletion snapshots final usage. One transaction per accounting cycle, writing
  only changed rows.
- Samples: fine-grained `traffic_samples` rows are flushed from an in-memory accumulator every
  `accounting.sample_flush_seconds` (default 300 s) — not every cycle — to bound SQLite churn
  (~288k rows/day per 1000 active devices vs ~5.8M with per-cycle rows). Accumulated totals are
  persisted every cycle, so a crash can only lose chart granularity for one flush interval,
  never usage. Rollup upserts happen in the same transaction as the sample flush, so a retried
  flush can never double count.
- Concurrency: device-limit races and duplicate IP allocation prevented by constraints +
  transactions (race-tested in CI); first-connection activation is idempotent.
- Retention: scheduler prunes `traffic_samples` (24–48 h), rollups per policy,
  `webhook_deliveries`, and `idempotency_keys` (bounded, configurable).

## Migrations

Forward-only, numbered, embedded; each applied in a transaction. Automatic pre-migration
backup on risky upgrades and on every update. Migration tests cover fresh installs and
upgrade-from-backup paths.

# Database design

SQLite in WAL mode, foreign keys ON, `busy_timeout=5s`, capped page cache (low-RAM requirement).
Driver: `modernc.org/sqlite` (pure Go). Explicit repository code — no ORM. All timestamps UTC.

## Schema

| Table | Purpose / key columns |
|---|---|
| `tunnel_interfaces` | name (`awgN`, unique), listen_port, ipv4_subnet (stable primary), ipv4_extra_pools (ordered JSON), mtu, public_key + private_key_encrypted (AES-GCM under the master key), obfuscation params (Jc, Jmin, Jmax, S1–S4, canonical H1–H4 scalar/range text, optional I1–I5/HPK/timer/flag fields, preset name), enabled, backend mode, endpoint override |
| `users` | id (UUIDv7), username UNIQUE, nullable reseller_id owner, display_name, note, tags, status (`active\|disabled\|suspended\|expired\|traffic_exceeded\|waiting_first_connection`), disable_reason (`manual\|expired\|traffic_limit\|admin_action`), traffic_limit_bytes (NULL=unlimited), traffic_used_rx/tx, speed_limit_down_kbps, speed_limit_up_kbps (NULL=unlimited, independent per direction; migration 0002 converted the single speed_limit_kbps), device_limit, template_id FK NULL, interface_id FK, start_policy (`immediate\|first_connection`), duration_seconds, activated_at, expires_at, last_activity_at, enabled, deleted_at (older soft-deleted records; current Delete permanently cascades), metadata JSON |
| `devices` | id, user_id FK, interface_id FK, name, ipv4_address, public_key UNIQUE, private_key_encrypted, preshared_key_encrypted, enabled, last_handshake_at, last_endpoint, rx_bytes/tx_bytes (accumulated), last_rx/last_tx (raw counter snapshot for delta logic) |
| `retired_peer_keys` | interface_id FK + former public_key; durable removal intent until successful runtime reconciliation |
| `templates` | id, name, quota, duration, start_policy, device_limit, speed_limit_down/up, interface/profile selector, enabled; reusable technical defaults, not sale SKUs |
| `resellers` | id, unique slug, display name, permission ceiling, enabled; Phase 14 tenant identity |
| `reseller_template_access` | reseller_id + template_id; owner-assigned technical templates permitted for tenant purchases, without changing existing subscriptions |
| `admins` | id, username, argon2id hash, role (`owner\|admin`), permissions JSON, enabled, optional reseller_id and `appearance_preset` personal override |
| `admin_sessions` | id, admin FK, token hash, created/last_seen/expires, source IP |
| `appearance_defaults` | Singleton installation-wide visual preset and Light/Dark/System mode; missing or invalid values resolve to built-in WG-Guard Neutral/Light |
| `api_tokens` | id, name, prefix (indexed), hash, scopes JSON, expires_at, enabled, cidr allowlist, last_used_at, optional reseller_id and issuer admin ID |
| `webhook_endpoints` | id, url, secret_encrypted, enabled, events JSON, optional reseller_id, owner-only include_reseller_events opt-in (legacy rows default off) |
| `webhook_deliveries` | id, endpoint FK, event type, payload, status (`pending\|delivered\|dead`), attempts, next_attempt_at (indexed), last error |
| `webhook_events` | durable event rows inserted in the same transaction as the state change; reseller_id classified from the persisted user for tenant-scoped fanout |
| `audit_log` | ts, actor type/id, action, target, source IP, request id, safe metadata |
| `idempotency_keys` | hashed token-scoped key, request hash, response snapshot, expires_at; ordinary REST mutations use this bounded response replay |
| `integration_operations` | hashed owner/reseller-scoped key, request hash, non-secret committed result, expiry; purchase result is inserted with user/device/link in one transaction |
| `next_plan_queue` | one queued successor per user; fixed non-secret template terms, source template, carry flag, review state and principal namespace |
| `next_plan_activations` | durable before/after entitlement snapshot and time/quota trigger, inserted with the user/counter transition |
| `settings` | key, value (JSON), updated_at |
| `backup_schedules` | id, mode (daily@time / every-N-hours / weekly), time UTC, retention, enabled |
| `traffic_samples` | device FK, ts, rx_delta, tx_delta (bounded: 24–48 h) |
| `traffic_rollups` | hourly (30 d) and daily (1 y) aggregates |
| `migrations` | version, applied_at |

Allocation: per-interface IPv4 pool with `UNIQUE(interface_id, ipv4_address)`; allocation in a
transaction with conflict retry; IPs released on permanent device or account delete. Ordered pools share the network/gateway/broadcast reservation convention. Migration 0015 adds overflow pools while preserving existing addresses.

Migration 0016 adds independent owner-default and per-admin number presentation preferences.
Latin remains the default and existing locale/preset/mode values are untouched. This changes
no traffic data, API numeric unit or client configuration.

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

Migration `0008_retired_peer_keys.sql` records former public peer identities when a device is
rotated/deleted or a user's subscription access is replaced. The reconciler removes those peers
even under the default report drift policy, then clears each confirmed intent. Failed syncs and
process restarts retain the intent; only public keys are stored, never client private keys.

Migration `0009_visual_appearance.sql` adds the nullable admin preset override and the singleton
installation default. Existing account locale and browser theme cookies remain independent;
neither is rewritten by a visual preset migration or panel-default action.

Migration `0010_reseller_ownership.sql` adds the reseller identity and nullable ownership
references without reassigning existing users, accounts, tokens or webhook rows. NULL remains the
existing node-wide operator namespace. The foreign keys prevent orphan assignments; reseller
access uses explicit route gates while other routes remain closed. Migration tests verify
legacy-row preservation and ownership references. Migration `0011_reseller_template_access.sql`
adds the owner-controlled template allowlist used by reseller purchase operations.
Migration `0012_integration_operations.sql` adds the 90-day result journal. The scheduler prunes
expired rows in bounded batches; plaintext caller keys, link capabilities and private configs
never enter this table.
Migration `0013_next_plan_queue.sql` adds one successor queue row per user and a transactional
activation history (one-year retention with bounded pruning). The fixed terms survive catalog edits; a queued template cannot be deleted until
the queue is canceled or consumed. Activation resets charged user/device counters but keeps raw
peer baselines, so the next accounting cycle charges only new traffic.

Migration `0007_awg_ranges.sql` adds `h1_range` through `h4_range` as canonical, non-null text
columns. Values use strict inclusive `N` or `N-M` syntax. Existing scalar values are copied as
decimal text; unset legacy values become empty text. The original integer `h1` through `h4`
columns remain populated with each canonical interval's low endpoint so a rollback binary can
still read the database without a schema downgrade. Such a rollback is intentionally lossy for
true ranges: it sees the low endpoint while the canonical columns remain intact for a later
forward upgrade.

The same migration copies `network.client_keepalive_seconds` to the range-aware
`network.client_persistent_keepalive` setting only when the new key does not already exist. It
retains the old row for rollback compatibility. Backup/restore regressions now cover both a real
pre-0007 scalar archive (including forward migration) and a post-0007 true-range archive through
stage, apply, reopen, and boot-time pending-restore consumption.

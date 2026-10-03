# Account deletion, address capacity and cleanup

## Permanent account deletion

Panel Delete, reseller Delete, REST `DELETE /api/v1/users/{id}` and bulk `delete` now use
one domain transaction. They remove the account, every registered device, the customer
subscription link, per-device chart history, queued successor and activation history.
All device IP allocations and the username become available again. A new account with
the same username receives a new opaque ID and independent credentials.

Former **public** peer keys are copied into `retired_peer_keys` before the cascading
delete. Runtime reconciliation removes them even under report-only drift policy; failed
syncs retain removal intent for retry. Database deletion is not a claim that an unavailable
kernel has already removed a peer. Audit records, webhook delivery records and
non-secret integration result journals keep their existing retention. A replayed purchase
result is historical evidence, not proof that its subsequently deleted account still exists.
Backups remain independent and are not erased by account deletion.

This replaces ordinary soft-delete behavior. Existing soft-deleted rows are not silently
purged by migration: the cleanup screen can explicitly remove them. Disabling, expiration
and quota exhaustion retain devices/IPs so renewal can reuse existing configs. Restore cannot
undo permanent deletion; recovery requires a coordinated archive.

## Interface address planning

Each profile has one stable primary CIDR and up to 15 ordered overflow CIDRs. `/23`, `/22`,
`/21` and `/20` are valid as well as `/24`. Every pool must be a canonical RFC1918 IPv4
network, no smaller than `/29`. Each pool reserves its network, first host/gateway and
broadcast; device addresses retain `/32`.

| Prefix | Registered configuration capacity |
|---|---:|
| /24 | 253 |
| /23 | 509 |
| /22 | 1021 |
| /21 | 2045 |
| /20 | 4093 |

Capacity counts independently allocated configurations, not users, physical hardware,
simultaneous sessions or guaranteed throughput. Three devices require three addresses even
if all are offline. `device_limit` alone does not reserve addresses.

Choose a contiguous `/22` at creation when about 1000 configurations are expected and
the host/network routes permit it. Overflow pools help with later expansion or noncontiguous
space: `10.8.0.0/24`, `10.8.1.0/24`, `10.8.3.0/24` provide 759 slots. The same interface
keys, UDP port and AWG profile serve all pools. Expansion assigns additional gateways/routes,
applies every pool's NAT/Docker forwarding footprint and preserves existing client addresses
and keys; it does not recreate the tunnel.

Selection is explicit device interface, account/template interface, then the first enabled
interface by name. Within that selected interface, the first free address in the first
available pool is allocated in a serialized write transaction. Freed holes in earlier pools
are reused first. There is no implicit fallback to a different profile, automatic network
growth or automatic migration of existing devices.

Networks cannot overlap each other or pools of another interface, including a disabled one.
Production also checks observed host IPv4 routes, excluding the default route and the selected
owned tunnel's own routes. Blank creation prefers the configured/name default, then searches
the bounded `10.8.N.0/24` ladder for a nonconflicting pool. If none fits, explicit input is
required. Foreign routes introduced after that snapshot are outside this admission guarantee.
The primary stays immutable; occupied overflow pools cannot be removed or resized. Empty
overflow pools can be removed, reordered or replaced explicitly.

API creation accepts either `ipv4_subnet` or a full `ipv4_pools` array. PATCH accepts
`ipv4_pools` as the complete desired list with the unchanged primary first. Capacity is
`GET /api/v1/interfaces/{id}/capacity`, for owner/node tokens with `interfaces.read`.
It is an advisory snapshot, never an address reservation. Full selected pools return
HTTP 409 `DEVICE_POOL_EXHAUSTED`. `/users` alone needs no IP; `/purchases` rolls back
the entire account/device/link/result transaction if any requested device cannot fit.

## Cleanup screen

`/cleanup` needs panel-only `cleanup.manage`. It is excluded from REST token scopes and
reseller grants, and classified as Full access in administrator shortcuts. Node operators
with this permission can choose node-owned accounts, one reseller or all owners explicitly;
the default targets node-owned accounts only.

Account cleanup supports `expired`, `traffic_exceeded`, `disabled`, `suspended` and older
soft-deleted rows. Active and waiting-first-connection accounts are not cleanup targets.
Accounts with a queued Next Plan are excluded unless explicitly included. Choose creation,
expiry, last update, last activity or soft-delete date. Last update is a record timestamp,
**not** the date when a status first changed. Unknown dates do not match a date filter.
The shared picker displays Jalali in Persian and Gregorian in English, including historical dates. It writes Gregorian `YYYY-MM-DD`; boundaries are UTC midnight, start inclusive and end
exclusive. Empty boundaries include all matching dates.

Each preview selects up to 200 accounts or 2000 history rows and shows the first 20, plus the device/IP allocations
to remove. If more match, finish that batch and preview the next. The encrypted preview binds
exact IDs, filter, relevant account/device state and operator for ten minutes. Execute rechecks
them inside the deletion transaction. Renewed, edited, consumed, missing or transferred accounts
invalidate the whole batch; new matching accounts are never added. Tampered, expired and
different-operator previews are denied. CSRF/permissions are checked again at execution.
Batches are atomic and audited; there is no new background deletion loop or scheduler.

History cleanup separately targets detailed traffic samples or hourly/daily rollups using
sample/bucket timestamps. It never resets charged usage or device raw-counter baselines.
Reset Usage remains an explicit single/bulk user operation. Pending webhook deliveries,
audit and idempotency/result journals are not arbitrary delete-table options; automatic
retention continues to own them.

## Database space

The page shows database pages, reusable free pages and WAL bytes. Deleted space is reusable
inside SQLite and need not shrink the file. Optimize runs `PRAGMA optimize` and a PASSIVE
checkpoint, respecting active readers. Manual owner-only compaction runs `VACUUM` outside
a transaction, with a 30-second timeout and the same single-operation lock. It may block
writes and need temporary free space up to about twice the database size; use a quiet period.
See [SQLite VACUUM](https://www.sqlite.org/lang_vacuum.html) and
[PRAGMA optimize](https://www.sqlite.org/pragma.html#optimize). These do not reset quota,
delete live credentials or guarantee forensic erasure of backups/WAL copies.

## Upgrade boundary

Migration `0015_interface_ipv4_pools.sql` preserves primary pools, device addresses and
accounts, adding ordered extras and cleanup/allocation indexes. The installer now advertises
`schema15-ipv4-pools-v1`: pool-unaware `schema7-h-ranges-v1` binaries cannot be selected over
this data via update or rollback. A downgrade needs the coordinated pre-upgrade archive;
otherwise old code could silently ignore overflow routes. This change has no new public
release or real-host certification until separately gated.

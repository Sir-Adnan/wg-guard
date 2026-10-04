# Backup & restore

A production feature, not a DB copy: reliable manual and scheduled backups, retention, Telegram
delivery, portability for disaster recovery and server migration — with a simple default
experience. Administrative surface: **panel (session auth) + CLI** — deliberately not a public
REST API ([ADR-0007](../decisions/ADR-0007-no-backup-rest-api.md)).

## Archive format (`.wgg`)

`tar.gz` containing:

| Member | Content |
|---|---|
| `manifest.json` | schema version, app version, created_at, source host info, per-file SHA-256 |
| `db.sqlite` | consistent snapshot via `VACUUM INTO` |
| `config.toml` | boot configuration |
| `master_key.wrap` | the at-rest master key (required to decrypt device secrets on restore) |

Archive publication validates the exact snapshot against its archived key. Every
encrypted interface/device key, optional PSK, customer link, webhook secret and
secret setting is checked, including records beyond startup's key samples.
Foreign-key references must be intact. A missing key is accepted only for data
containing no encrypted values. A malformed/mismatched pair or data requiring an
unarchived rotation key is refused; retain the original node and repair its
database/key pair before migration. The logical archive format remains schema 1.

Portable verification also checks domain semantics on the immutable snapshot, including
original-schema recovery: contiguous known migration history; canonical matching interface/
device key pairs and PSKs; supported interface names, kernel/userspace mode, port/MTU and the
existing pinned AWG parameter relationships; ordered pools with no cross-profile overlap;
canonical `/32` device addresses belonging to an assignable pool slot; nonnegative accounting
and valid subscription limits/lifecycle values; readable critical timestamps; customer tokens
matching their lookup hashes; and current setting definitions/validators. Creation, preview,
independent verification and pre-replacement apply use the same gate. Correct checksums and
AES authentication alone do not make malformed domain data recoverable.

Inspection reads old scalar/range/overflow layouts without changing archived bytes. Historical
unknown settings, disabled/deleted accounts and legitimate over-limit/expired states survive;
stored device counts need not fall below a subsequently reduced device limit. Fields outside
this documented gate are not claimed to have a complete semantic audit. Stored backend counts
identify required reviewed kernel/userspace support; offline validation does not observe or
certify the target's module, pinned daemon, TLS, routes or client compatibility.

Inspection streams one envelope at a time, caps encoded values at 8 KiB and has
a one-minute deadline. Snapshot size must fit the existing 1 GiB restore limit.
Plaintext is cleared immediately; public errors and counts contain no secret values.
Domain scans additionally bound selected technical values/timestamps before Go allocation;
setting values are capped at 1 MiB and interface pool JSON at 2 KiB. Profile memory is bounded
by the finite `awgN` name language and at most 16 pools per profile; global pool overlap checks
sort intervals rather than doing quadratic comparisons. Other record rows stream and timestamp
scan buffers are reused. Domain checks share the one-minute inspection deadline and report
incomplete on cancellation/timeouts. Invalid-data guidance never includes keys or raw row values.
The encrypted field definitions are shared with startup/rotation in `internal/secrets`;
secret-settings parity is tested. Interrupted or timed-out verification reports
**incomplete**, preserving its cancellation cause, without publishing a restore preview.
It gives neither a valid nor a corrupt result. Malformed/checksum/key failures remain distinct;
retry incomplete verification before approving a restore. Delivery cancellation keeps its
separate delivery guidance and does not invalidate a verified local archive.

**Encryption is optional.** By default the archive is plain `tar.gz` (simple backup
experience). If the administrator sets a **single backup password** — once, from the installer,
CLI, or Settings panel; changeable later; stored encrypted at rest — archives are additionally
encrypted with **age** (age-encryption.org/v1, scrypt passphrase recipient — a standard,
established format; no custom cryptography). Restore asks for a password only for age-encrypted
archives. No other crypto is invented anywhere in the product.

The pinned age writer uses scrypt work factor 18 (about 256 MiB transient KDF memory).
Restore caps accepted work factors at 18 before running the KDF: every archive produced by
this writer remains compatible, but externally encrypted higher-factor archives are explicitly
refused. Streaming bounds archive-member memory; it does not eliminate this crypto working set.

## Sources

### Independent archive verification

Before rebuilding the source server, download a backup off-host and verify it:

```bash
wg-guard backup verify --archive /private/wg-guard-backup.wgg
wg-guard backup verify --archive /private/wg-guard-backup.wgg --password
```

`--password` uses hidden input; `--password-file /private/password` accepts a
regular 0600 file. Password values never belong in arguments. This host command
loads no installed configuration/state, starts no Docker service, opens no active
node data and applies no restore. It privately stages/migrates only the archived
copy, checks references and all encrypted values, and reports stored account/device/
template/access counts and backend inventory. Disabled and historical soft-deleted
rows are included. Temporary files are removed on success or ordinary failure.

Success covers portable data, not the target kernel/TLS/network or client traffic.
Certificates, DKMS and Docker images are not archive members. The
[refactor target](../architecture/deployment-refactor.md) describes planned packaging;
current installation paths have not changed in this preparation update.

- **Manual** — Dashboard can create and download a fresh archive in one action; the Backups page
  also creates local archives. CLI equivalent: `wg-guard backup create [--password] [--output …]`.
- **Scheduled** — stored schedules (`backup_schedules`): daily@HH:MM, every-N-hours,
  weekly-day@time; stored UTC (CLI displays UTC); the central scheduler signals one fixed
  in-process archive worker (no cron dependency); per-schedule retention (default keep 14). Created in the
  panel (`/backups`) or with `wg-guard backup schedule-add -kind daily -time 03:30`; the
  installer can create a daily Telegram schedule during setup.
- **Automatic** — before any pending live-node migration over existing data and every update.

### Migration and concurrency safety

Server startup and data CLI openers inspect migration history before DDL. A fresh empty database
needs no archive. Existing older data is archived under exclusive DB/key ownership before
settings or key initialization; unknown/incomplete history, unreadable data, a wrong key,
reader contention or archive failure blocks migration. The local recovery archive is plaintext,
private (0600 files/0700 directory), has no remote delivery and goes to
`<data_dir>/backups-auto` (keep five after publishing the new verified archive). It preserves
the original schema. It intentionally does not depend on loading a stored backup password from
the database being migrated. Downloadable/off-host backups retain the chosen password policy.

Archive creation/crypto/delivery holds one nonblocking claim across service instances and
host/container processes sharing the data volume. Contention returns safe retry guidance.
Scheduled passes separately hold ownership from the due query through advancement, read at
most eight due rows and have a 15 min worker deadline. A concurrent schedule edit/disable is
not overwritten by the old pass. Cancellation or contention does not consume a due attempt;
ordinary failed attempts retain the existing failed-status/next-slot policy. Streaming checks
cancellation between members and copied chunks; age's synchronous KDF can finish after
cancellation, so the worker is drained before its DB/key ownership is released.

Due rows survive signal coalescing and process restart. Missed slots coalesce into one attempt
per pass, but a crash after archive publication and before row advancement can repeat the
archive or delivery. This is at-least-once execution. Isolation tests cover a stalled worker,
cross-process claims and shutdown; production enforcement lag/peak KDF costs remain a separate
measurement gate. The lease file is never an archive member; do not delete it to bypass a claim.

The local stalled-pass regression also runs actual accounting/expiry and fake-backend peer
removal for two accounts while both worker operations wait. Enforcement completed in 1 ms
against a 15 s cadence budget in the recorded Windows fixture run; this small synthetic result
is not a production latency promise or an actual archive-KDF/remote-delivery measurement.

## Delivery sinks

| Sink | Details |
|---|---|
| `local` | `/var/lib/wg-guard/backups`, mode 0600 (default) |
| `telegram` | bot token + numeric chat ID (optionally provided at install, editable later in Settings/CLI; stored encrypted at rest); delivered via `sendDocument`; archives near the 50 MB Bot-API limit warn loudly |

The `BackupSink` interface leaves room for future sinks (e.g. S3) without redesign.

Telegram accepts positive user IDs and negative group/channel IDs. `backup telegram-test`
sends a small probe; `backup send --archive /path/to/archive.wgg` sends the selected existing
archive without creating another or running retention. Delivery output reports encryption,
destinations and warnings, never tokens, HTTP request URLs or remote response descriptions.
Plaintext off-host archives contain readable node secrets: set a backup password first.

### CLI schedule and secret management

```bash
wg-guard backup schedule-add --name nightly --kind daily --time 03:30 --retention 14
wg-guard backup schedule-add --name interval --hours 6
wg-guard backup schedule-add --name days --days 2
wg-guard backup schedule-list
wg-guard backup schedule-update --id ID --name weekly --kind weekly --weekday 1 --time 03:30
wg-guard backup schedule-disable --id ID
wg-guard backup schedule-enable --id ID
wg-guard backup schedule-delete --id ID
wg-guard settings set backup.password -stdin
wg-guard settings set backup.telegram_token -stdin
wg-guard settings set backup.telegram_chat -1001234567890
```

Hours are 1–168, equivalent whole days 1–7, weekday 0=Sunday through 6=Saturday.
`--days` and `--hours` cannot be combined. Retention is 0–365, with 0 using the node default
(initially 14); `schedule-update` replaces the full editable definition. `--disabled` creates
or updates a disabled schedule. Listings include ID, enabled state, retention and next run in UTC.
Schedule rows and backup-category settings are read from SQLite by the running service; CLI
changes are observed on its next backup pass (one minute). No extra scheduler or restart is
needed for this category. Other cached settings retain their documented restart requirements.

Secret settings require stdin (bounded to 4096 bytes). Backup/restore `--password` uses bounded
hidden terminal input or a newline-terminated stdin password; `--password-file PATH` requires
a regular private file (0600). Password values never belong in argv. An explicitly unset stored
password retains plaintext behavior; unreadable or undecryptable stored password data aborts
archive creation and delivery, never silently downgrades encryption.

### Isolated acceptance helper

`docs/integrations/fixtures/verify-phase8.1-synthetic-backup.py` exercises the production CLI and
the central scheduler against a temporary fake-backend node. It never installs a service, package,
module or container, never touches tunnels/firewall, and never reads the installed node. Run it on
Linux with a current-user-owned executable candidate that is not group/other-writable and supply
its exact hash; the optional result path must not already exist:

```bash
python3 docs/integrations/fixtures/verify-phase8.1-synthetic-backup.py \
  --candidate /root/private/wg-guard_linux_amd64 \
  --expected-sha256 FULL_64_CHARACTER_SHA256 \
  --result /root/private/synthetic-backup-result.json
```

That local mode creates three age-encrypted archives, proves keep-two retention and listing,
performs schedule create/update/list/disable/enable/delete, moves only its owned row into the past,
waits up to 90 seconds for an actual central-scheduler tick, proves keep-one scheduled retention,
then stops only its child and removes only its private workspace. The result calls this an
**accelerated due execution**, not elapsed hours. A pass without credentials explicitly records
Telegram as unverified.

Candidate validation opens without following symlinks and checks permissions, ownership and hash
on that same descriptor. Before any command runs, the helper repeats those checks while copying
and hashing the candidate into its 0700 workspace; every command uses that pinned private copy.
Credential ownership, mode, size and contents are likewise checked from one opened descriptor.
Cleanup records the workspace device/inode and refuses to remove a replacement at the same path.
Service output is drained into a 1 MiB bounded capture and stops only the owned child on overflow;
capture scanning and safe workspace cleanup still run after an earlier cleanup error.

Work locations must have trusted ancestry: every existing component through the requested parent
must be a non-symlink directory owned by root or the current UID and not group/other-writable. The
only shared-directory exception is a root/current-owned sticky directory such as `/tmp`; sticky
ownership prevents a different non-root UID from replacing the helper's entry. This boundary makes
the pinned-candidate and device/inode cleanup checks meaningful against lesser host accounts.
Consistent with [the security threat model](security.md#threat-model-documented-limits), the helper
does not claim protection against root or a malicious process running as the helper's own UID;
either can read its memory or manipulate any resource it owns.

Real Telegram acceptance is an explicit opt-in. Create an administrator-owned 0600 JSON file
outside the repository (do not put either value in shell arguments or evidence):

```json
{"bot_token":"REDACTED","chat_id":"-1001234567890"}
```

Then add both `--real-telegram` and `--telegram-credentials-file /root/private/telegram.json`.
The helper creates/retains the local archives before loading Telegram settings, sends the small
Telegram probe, sends one explicitly selected encrypted archive, and permits one scheduled
encrypted send: at most two archive sends. It scans bounded captures for its random password and
Telegram values before writing sanitized evidence. The credential file is preserved for the
operator; cleanup never removes it. A helper pass is synthetic fixture evidence, not proof of the
managed native/Docker lifecycle, original-data recovery, public networking, or the dedicated VPS.

Run its safe local regressions with:

```bash
python3 scripts/test-phase8.1-synthetic-backup.py
```

Safety errors and warnings retain catalog identities through the shared engine. The terminal
boundary renders them in English; the web panel remains localized in Persian and English.
Missing/short archive passwords, failed encryption, wrong passwords and malformed/damaged age
input use the same keyed messages. Low-level parser
details are not echoed; sentinel/cancellation causes remain available internally. Excessive
scrypt work factors retain their specific pre-KDF refusal rather than a generic password error.
Completed panel backups always redirect after creation (POST/Redirect/GET), preserving safe
localized warnings in the escaped flash message so browser refresh cannot create another
archive. Error causes remain available for cancellation handling but
are excluded from public text and structured warning logs.

## Restore (panel wizard and CLI share one engine)

The panel accepts a downloaded `.wgg` file from another node. The authenticated `backup.manage`
form validates CSRF before reading the file, streams it into the private local sink under a fresh
server-generated name, and publishes it atomically at mode 0600. Import checks the outer gzip/age
container only; the normal restore review performs the full validation below. Compressed input is
bounded at 8 GiB as a security limit. Import and restore remain panel/CLI operations and do not add
a public REST endpoint.

All pair replacement and interrupted recovery acquire exclusive kernel ownership in the
shared data volume. CLI/server DB/key handles hold shared ownership until closed; rotation
holds exclusive ownership even at its confirmation prompt. A stopped service does not imply
other commands have exited. Contention leaves data intact and fails with retry guidance;
see [the lifetime ownership and legacy-binary boundary](lifecycle-recovery.md).

Restore is **stage-then-swap — never a live swap** (open WAL handles make in-place replacement
unsafe):

1. **Decrypt + verify** — age password if the archive is encrypted; manifest checksums; the
   gzip/age container CRCs; schema gate (an archive written by a newer build is refused).
2. **Private preview + migrate** — verified members stream into a unique 0700
   `<data_dir>/restore.preview-*` directory. The database is forward-migrated there and passes
   `PRAGMA integrity_check`. Database members are limited to 1 GiB, config to 1 MiB, manifest
   to 64 KiB and master key to exactly 32 bytes. The total decompressed stream, including tar
   padding, is capped at 1 GiB + 2 MiB + 64 KiB. Unknown, duplicate, path-containing, symlink,
   oversized and truncated entries and incomplete manifests are rejected.
   The offline database/key pair is then checked completely, with reference and
   encrypted-value inventory. Original-schema recovery uses the same data checks
   without forward migration.
3. **Environment review** — the report shows the archive's provenance (source host, app
   version), the staged node id, endpoint, TLS mode/listen from the archived boot config, and
   the interface list, with explicit warnings (missing master key, missing config). The
   operator can edit `node.endpoint`/`node.id` after apply through Settings — client configs
   are generated on demand, so a corrected endpoint is enough for clients to reconnect.
4. **Apply** — one of:
   - **CLI** (`wg-guard restore ARCHIVE [--password] [--yes]`): runs on the deployment host
     in both Docker and native modes. The shared lifecycle lock/journal owns review, verified
     service stop, offline apply, service start and health check. Active data is never opened
     during review. `--yes` is explicit scripted consent; interactive confirmation defaults no.
   - **Panel wizard**: explicit confirmation names the exact preview and publishes it as
     `restore.pending`. Only that approved directory is consumed by `serve` before opening
     the database. Closing the review page/CLI, cancellation, EOF or a restart during review
     cannot apply an unapproved preview. Invalid pending metadata aborts startup.
   - **Paired replacement**: complete staged hashes are mandatory. Original database, WAL/SHM,
     master key and rotation-window key files are copied and synced before a durable
     `restore.transaction` marker is published. A partial replacement is recovered as the
     original pair; recovery failure blocks database opening. Interrupted boot recovery restores
     originals and stops startup so the operator can review before restarting. Successful
     replacements retain those files under `restore.previous`; prior retained sets are moved
     to `restore.previous-<nonce>` so retries never erase the earlier recovery copy. Archived boot config is saved
     as `<config>.restored` for separate review and never replaces active configuration.
     The pair is checked again before replacement, including pending previews
     approved by an older checksum-only implementation.
5. **Reconcile** — the normal boot bring-up recreates tunnels, peers, nftables and shaping
   from the restored database; `wg-guard doctor` confirms.

### Configuration-integrity guarantees

Restore regression tests build and archive an actual schema-0006 database, then stage it through
the current migrator and apply it. They also archive a current database containing true AWG
intervals. Both paths must preserve exact H1–H4 and PersistentKeepalive values, advanced AWG
range fields, the legacy low-bound/keepalive mirrors needed by a rollback binary, settings, and
unrelated foreign-key data. The environment review reads its interface inventory from
`tunnel_interfaces`; a missing optional summary may never disguise a schema/query mismatch.

The panel restart path has separate coverage proving that `restore.pending` is consumed before
the database is opened, the staged values replace later live mutations, and exactly one
`backup.restored` audit event is written. Phase 11 added real-host restore, disk-pressure and
recovery drills; their scope is recorded in [phase11.md](../development/phase11.md).

### Interrupted lifecycle recovery

`wg-guard restore --recover [--password-file PATH] [--yes]` is the explicit offline route for
an M3 `restore-required` update. It verifies the recorded archive SHA-256/encryption identity
and retained binary/immutable image identity, then restores the original database schema and
matching master key **without forward migration** before deploying and starting old code.
Unknown/missing identities keep recovery blocked. This is distinct from an ordinary forward
restore. See [lifecycle-recovery.md](lifecycle-recovery.md).

A failed ordinary managed restore leaves a pending journal and the service stopped; after
correcting the error use `restore ARCHIVE --retry` to review and retry it explicitly. Unfinished
file recovery is resolved first and may require a second invocation after reporting recovery.
The shared-volume `restore.lifecycle-blocked` guard prevents startup/data commands during a
coordinated replacement. All manual active-data openers (backup/settings/doctor/secrets,
token, owner bootstrap and reconcile) check both recovery markers in their actual loaded
configuration's data directory before opening SQLite. Normal serve retains its deliberate
recovery-before-open path.

Managed restore accepts only `/etc/wg-guard/wg-guard.toml`, `/var/lib/wg-guard`,
`/var/lib/wg-guard/wg-guard.db` and `/var/lib/wg-guard/master.key`. A redirected data directory,
database or key path is refused by the shared coordinator before preparation or service stop;
the lifecycle guard cannot diverge from the data opener's directory.
Do not delete the guard to bypass the journal. Abandoned private previews
are never auto-applied; root may remove an exact reviewed `restore.preview-*` directory after
confirming no restore command is running. Review retained `restore.previous` files before
deliberate cleanup; they may contain unencrypted keys and WAL data.

## Server migration & disaster recovery

For the planned Docker refactor, follow the [owner preparation drill](migration-preparation.md)
before any rebuild. A downloaded archive, saved off-host password and independent verification
are prerequisites; current main preparation is not a new public target release.

Migrating = fresh install on the new server + restore + environment review. Because client
configs are generated on demand from current settings, confirming the public endpoint during
review is sufficient for clients to reconnect (hostname-based endpoints need no client-side
change at all). If the master key is unavailable, encrypted device keys cannot be recovered
from public keys retained in the database. An established database with encrypted carriers
never silently regenerates a key: boot refuses until the matching key or a coordinated backup
is restored. Without either, device re-enrollment requires an explicit node recovery/reset.

## Security

Archives 0600; the backup password and Telegram credentials are stored encrypted at rest and
never logged; every backup/restore is audit-logged; restore requires explicit confirmation and
`backup.manage` permission (panel) or root (CLI).

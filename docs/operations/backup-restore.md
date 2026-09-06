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

- **Manual** — UI button or `wg-guard backup create [--password] [--output …]`.
- **Scheduled** — stored schedules (`backup_schedules`): daily@HH:MM, every-N-hours,
  weekly-day@time; stored UTC (CLI displays UTC); run in-process by the central
  scheduler (no cron dependency); per-schedule retention (default keep 14). Created in the
  panel (`/backups`) or with `wg-guard backup schedule-add -kind daily -time 03:30`; the
  installer can create a daily Telegram schedule during setup.
- **Automatic** — before risky migrations and every update.

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

## Restore (panel wizard and CLI share one engine)

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
`backup.restored` audit event is written. These automated guarantees do not replace the broader
real-host disaster-recovery drills in Phase 11.

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
coordinated replacement. Do not delete it to bypass the journal. Abandoned private previews
are never auto-applied; root may remove an exact reviewed `restore.preview-*` directory after
confirming no restore command is running. Review retained `restore.previous` files before
deliberate cleanup; they may contain unencrypted keys and WAL data.

## Server migration & disaster recovery

Migrating = fresh install on the new server + restore + environment review. Because client
configs are generated on demand from current settings, confirming the public endpoint during
review is sufficient for clients to reconnect (hostname-based endpoints need no client-side
change at all). Degraded case, documented honestly: if the master key is unavailable, device
private keys cannot be decrypted — peers survive (public keys in DB) but configs cannot be
re-downloaded; devices must be re-enrolled.

## Security

Archives 0600; the backup password and Telegram credentials are stored encrypted at rest and
never logged; every backup/restore is audit-logged; restore requires explicit confirmation and
`backup.manage` permission (panel) or root (CLI).

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

## Sources

- **Manual** — UI button or `wg-guard backup create [--password] [--output …]`.
- **Scheduled** — stored schedules (`backup_schedules`): daily@HH:MM, every-N-hours,
  weekly-day@time; stored UTC, displayed in server-local time; run in-process by the central
  scheduler (no cron dependency); per-schedule retention (default keep 14).
- **Automatic** — before risky migrations and every update.

## Delivery sinks

| Sink | Details |
|---|---|
| `local` | `/var/lib/wg-guard/backups`, mode 0600 (default) |
| `telegram` | bot token + numeric chat ID (optionally provided at install, editable later in Settings/CLI; stored encrypted at rest); delivered via `sendDocument`; archives near the 50 MB Bot-API limit warn loudly |

The `BackupSink` interface leaves room for future sinks (e.g. S3) without redesign.

## Restore (panel wizard and CLI share one engine)

Restore is **stage-then-swap — never a live swap** (open WAL handles make in-place replacement
unsafe):

1. **Decrypt + verify** — age password if the archive is encrypted; manifest checksums; the
   gzip/age container CRCs; schema gate (an archive written by a newer build is refused).
2. **Stage + migrate** — the verified members land in `<data_dir>/restore.pending/`; the staged
   database is forward-migrated there and passes `PRAGMA integrity_check`.
3. **Environment review** — the report shows the archive's provenance (source host, app
   version), the staged node id, endpoint, TLS mode/listen from the archived boot config, and
   the interface list, with explicit warnings (missing master key, missing config). The
   operator can edit `node.endpoint`/`node.id` after apply through Settings — client configs
   are generated on demand, so a corrected endpoint is enough for clients to reconnect.
4. **Apply** — one of:
   - **CLI** (`wg-guard restore ARCHIVE`): refuses to run while the service answers on its
     listen address; with the service stopped the swap is immediate. Replaced files are kept as
     `*.pre-restore`; the archived boot config is never applied — it is staged as
     `<config>.restored` for review.
   - **Panel wizard**: stages and, on explicit confirmation, applies **at the next service
     restart** — `serve` consumes a pending restore before the database is opened, snapshots
     the replaced state, and audit-logs the event. A staging dir that fails re-verification at
     boot never aborts boot; the operator decides.
5. **Reconcile** — the normal boot bring-up recreates tunnels, peers, nftables and shaping
   from the restored database; `wg-guard doctor` confirms.

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

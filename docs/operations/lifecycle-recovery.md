# Lifecycle recovery

The v0.1.7 [Update Center](update-center.md) adds safe host inventory, operation-stage
feedback and bounded reservations around this engine. Installer `maintenance_protocol: 2`
capability controls the detailed broker/timer independently of data compatibility. Registration
follows the actually deployed artifact during update/rollback/recovery; a legacy artifact never
retains a timer that sends unsupported commands. Elapsed time never permits a web request to
erase a running claim. The fixed runner must obtain its separate kernel ownership first.

Phase 8.1 M3 uses one host lifecycle lock, `/run/lock/wg-guard-lifecycle.lock`.
The Linux kernel releases it when the process exits or dies; do not delete the lock file
to bypass another operator. Install/update/uninstall/restart/core selection, panel-exposure
changes and certificate synchronization share this lock. The application remains one binary
with one scheduler.

DB/key access also uses `/var/lib/wg-guard/.wg-guard-data.lock`, a separate persistent
inode shared by host recovery commands and Docker's bind-mounted data volume. Every data CLI and
the server keep shared kernel ownership until their DB/key users close. `secrets rotate`
takes exclusive ownership before loading keys or carriers, including while awaiting `YES`.
Restore and interrupted pair recovery require exclusive ownership; stopping the service
alone is insufficient if another data command is still running. Startup completes any
pending restore exclusively and converts to lifetime shared ownership while admission stays
closed. Preview/archive staging remains possible while the server is running.
The shared `internal/nodestate` session now owns this opening sequence for startup and all
automatic data CLI callers. Database-only owner/token access never initializes a key;
key-bearing and stopped-node exclusive access retain their existing admission semantics.
Host stop/start and deployment installation are concrete adapters separated from the single
install/update/restore coordinator; they do not create a nested lifecycle lock or new journal.
Startup retains exclusive ownership until DB/key initialization finishes. A CLI opening
a node whose master key does not yet exist also keeps exclusive ownership until key
initialization finishes, preventing concurrent first-key creation. Database-only commands
do not create a key as a side effect. A timed-out server shutdown retains both its DB and lease
until a later successful handler/background-worker drain or process exit. Readiness is false
throughout shutdown.

Before any automatic live migration, read-only inspection distinguishes an empty database
from a known older schema. Existing data requires exclusive ownership and a verified local
pre-migration archive before DDL or key/settings initialization; backup/inspection failure stops
the opener. Shared CLI ownership promotes under closed admission without evicting other readers.
If another server/CLI remains, migration fails immediately and keeps its original schema/key.

Contention fails immediately with fa/en guidance to finish other data commands and stop the
service before rotation/restore. A failed managed restore retains its journal/guard and
keeps the service stopped: finish the contender, then use `restore ARCHIVE --retry` (or
`restore --recover` for recorded original-schema recovery). Locks are kernel-owned and
released on process exit/death; the file is never archived, replaced or removed during
restore. Never unlink the lock file to bypass an owner. Byte 0 serializes admission, byte 1
protects DB/key ownership, byte 2 is the purge marker, byte 3 serializes archive/crypto/inspection
work, byte 4 covers scheduled due queries through conditional advancement, and byte 5
serializes private approval/cancellation, offline apply and interrupted recovery. Purge also
excludes bytes 3 and 5 so private inspection/publish work cannot race deletion; work admission
rechecks the purge tombstone after acquiring its claim. Inspection of a portable archive opens
no active pair and can coexist with exclusive rotation. Archive/schedule claims
are nonblocking and do not exclude ordinary shared accounting access. Cancellation/contention
keeps schedule rows due; process death releases claims, so a crash after publishing an archive
but before advancing its row may repeat it. On Linux these byte ranges use
open-file-description locks, which also serialize distinct processes and survive pathname
aliases into the same volume. No lifecycle-lock inheritance or nested subprocess exception
is required; lifecycle commands may still invoke installed data CLI helpers.

Alternate data directories and custom direct-child DB/key filenames remain supported for
manual operation. Both files must belong to the configured data directory: split directories
or a file symlink escaping that directory fail closed with migration guidance. Directory
aliases are compared by underlying inode, so a legitimate alternate path to the same volume
still participates in the same lock. If using a split legacy layout, stop all data processes,
migrate the database (including its SQLite sidecars) and current/previous master keys together
into one directory, and update all boot configuration/environment overrides before restarting.
Managed restore retains its separate canonical-filename/layout requirement.

## State and retained resources

`/var/lib/wg-guard-host/install-state.json` is the private schema-4 Docker record. The manager
rejects schema 1–3, retired unit fields and legacy `/etc/wg-guard/install-state.json` before
deployment mutation. Use the original release manager for export and the fresh-install/restore
route; changing a JSON schema/path by hand is not migration. Missing state differs from unreadable,
corrupt or unsupported state, all of which block lifecycle operations.

`/var/lib/wg-guard-host/lifecycle.json` is a private schema-2, atomically replaced and synced operation journal.
It records the stage, before/after state, previous/candidate artifact paths, prerequisite
package intents, observed ownership and repository preparation. Package intents mean an
interrupted apt command may have changed those packages; inspect `dpkg-query` before deciding
ownership. The recommended source-backed core adds no PPA; shared PPA configuration from a legacy
package-backed installation is retained on uninstall. No password, token, raw boot configuration
or archive content belongs in state, the journal or diagnostic evidence. Safe fixed lifecycle
outcomes can be inspected with `wg-guard logs --source operations --since 7d`; this does not expose
prompts, command arguments, error text, configuration or archive content.

Retained binaries and Compose snapshots live in random private directories under
`/var/lib/wg-guard-host/lifecycle/`. Their exact paths and binary SHA-256 are recorded. Successful
updates retain current and previous artifacts and prune superseded recorded copies; Docker
images and pre-update archives are retained. A process killed during staging can leave an
unreferenced private directory. Inspect journal/state references before removing such a
directory during maintenance; do not delete the whole lifecycle directory.

### Webhook key-check failure during Docker upgrades

Published binaries through v0.1.5 can reject a valid stored webhook secret during startup or
offline key validation. The webhook writer stores `enc:`-prefixed base64 text, but that check
incorrectly treated it as a binary envelope. The message can therefore say the master key does
not decrypt existing data even when the key is correct. This is not evidence that the key was
lost. Do not rotate/replace the key, delete webhooks, restart a working affected service or skip
the backup merely to get past this error.

The corrected updater first uses the installed Docker backup CLI normally. For that exact
key-check failure only, it can retry using the staged verified Linux helper against the
canonical host DB/key volume. Both artifacts must declare the same data contract and the data
lease protocol; the helper checksum is rechecked immediately before use. Boot data/DB/key paths
must match the managed layout, and inherited host path overrides are replaced with those pinned
paths. The invocation sets the internal direct-execution marker so the host shim cannot forward
the helper's backup command back into the affected old container. The helper still validates
secrets, takes shared data ownership and creates a local
archive whose bytes are hashed and recorded before deployment changes. A real wrong key, helper
tampering, incompatible contract, custom layout or failed archive creation stops the upgrade.
There is no key replacement or backup bypass.

This correction shipped in
[v0.1.6](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.6) after its exact-source gate.
Updating only the old manager/binary to v0.1.5 does not repair it. Automated tests
cover the stored-webhook failure/reload and CLI backup, plus guarded helper retry/refusal;
new real-host execution is not claimed. The terminal recovery URL points to this repository
document; it is not an installed `/docs/...` path. Log source **Installer and update commands**
contains command errors; **Lifecycle outcomes** intentionally contains only safe action/results.

Source acquisition precedes the update journal's deployment stages. A compiler/download
failure at that point does not mean the panel was stopped or data were replaced; inspect
the installed version and current health rather than invoking rollback on an old journal.
Current main records bounded redacted acquisition errors in the installer log and stages
in `/var/cache/wg-guard/staging`. Managers before this correction can have only unrelated
older host-command output in that log. A small or quota-limited RAM-backed `/tmp` is
independent of free root-disk space; see the [acquisition guide](github-install.md).

Pre-update archives use the existing backup service in the owning environment (Docker exec
or explicit offline helper) with a dedicated local output directory:
`/var/lib/wg-guard/backups/lifecycle-<operation-id>/`. The journal records the actual returned
archive name, SHA-256 of local bytes and whether the file has an age header. A remote delivery
claim or a missing local file is insufficient. Archive hashing uses bounded memory; it is
identity evidence, not a replacement for restore verification. These dedicated recovery
archives are outside the ordinary top-level backup retention/listing and need deliberate
retention review after the rollback window. `--purge-data` removes their contents under exclusive
data ownership. It refuses while any admitted data command is active. The data directory retains
only the 0600 lock inode, marked purged so a new command cannot reopen an empty replacement
volume; a fresh installer clears that marker only when no data member remains. After an
interrupted purge, rerun the guided removal before installing again. Default uninstall preserves
data. Older binaries without the lease protocol must be stopped separately.

## Interrupted operations

For interrupted updates, run `wg-guard update --recover` using a current contract-compatible
manager. If the installed host command predates that command, use an acquired compatible
candidate directly. Do not start old code manually
just because a health endpoint responds.

| Journal stage | Meaning and action |
|---|---|
| `prepared` | Staging completed; update recovery marks it aborted without replacing active files |
| `swap-pending`, `started` | A candidate may have executed; recovery stops, checks data compatibility, restores and health-checks the previous artifact when proven compatible |
| `rolled-back` | Previous artifact and install state restored and health checked; the failed update still exits nonzero |
| `complete` | Operation committed; update `--rollback` can select the retained previous artifact |
| `restore-required` | Data compatibility is unproven; service is stopped and coordinated data/key restoration is required |
| `recovery-required` | Recovery did not finish, or installation is incomplete; inspect the error and retained journal/resources |
| `pending-reboot` | Core loaded/disk identities differ; plan a maintenance reboot and repeat the catalog core check |

Recovery after caller cancellation gets an independent three-minute context. Failed stop,
artifact restore, restart, health or state persistence stays visible as an error and pending
journal. New updates/installations refuse to overwrite a pending operation. Interrupted
installation recovery records its partial ownership and stops a possibly started listener;
inspect prerequisites, then use managed uninstall (data preserved) before reinstalling.
Uninstall resumes its own interrupted record, confirms stop before deleting anything and
removes only constrained paths. A failed service-stop command prevents deletion.
The local manager recognizes an uninstall journal before reading the possibly removed boot config
and promotes **Continue uninstall / reset**. Its safe choice preserves data; its separately
confirmed full reset passes the existing data/package purge boundaries. It never dispatches
update recovery for an uninstall record.
Fresh installation queries legacy unit load/activity read-only and refuses anything except
confirmed absence. It never stops/disables an unowned old server. An existing named container
or an unobservable engine also blocks fresh setup. Docker stop must be proven before deletion.

A first-install failure before runtime/data mutation closes as `aborted`, leaves the verified
local manager available, and returns to normal setup on `sudo wg-guard`; observed prerequisite
ownership is inherited by the next attempt. Older safe `recovery-required` records with no prior
install, no completed prerequisites and no possible data change can be closed with the manager's
**Continue required recovery** action or `wg-guard recover-install --yes`. That command refuses
any record that might have started a service or changed node data. Ubuntu package operations wait
for the dpkg lock, and detailed failure output is in `/var/log/wg-guard/installer.log`.

Certificate readiness is separate from process health. A healthy installation with pending
TLS can keep serving the ACME challenge while `wg-guard tls-check` retries certificate proof.

Panel access changes use journal operation `exposure` plus a bounded private snapshot of only the
managed boot/Compose/systemd/Nginx files. The candidate certificate/proxy is prepared before the
minimum required stop; commit follows runtime health and certificate identity proof. Failure
restores the exact prior files and service. An interrupted record blocks unrelated lifecycle
work and is resumed with `wg-guard exposure recover` (also promoted by the manager). Certificate
renewal uses operation `certificate`; only the recorded Certbot lineage can refresh the managed
copies, and failure rolls both files and the service back together.

## Database compatibility and legacy migration

The machine-readable `wg-guard installer-contract` command does not open node data. Revision 2
currently reports `data_contract: schema15-ipv4-pools-v1`, prerequisites, recoverable lifecycle,
persistent-manager and secure-exposure support. `local_owner` is true and required for new candidates: installer-managed setup
prepares the local owner before listener startup. M5 implements bounded coordinated
database/master-key restoration, including original-schema recovery; `coordinated_restore` is
true and required for new candidates. `data_lease=true` is also required by both the
bootstrap and Go candidate admission. Candidate admission
is separate from data compatibility: valid older revision1 records keep their known schema
identity even if they lack the newer owner-setup capability.

Earlier binaries do not participate in data leases. Before upgrading from, rolling back to,
or recovering an earlier artifact, operators must stop every independently running earlier
data command and prevent another from starting during maintenance. The new coordinator
cannot exclude a binary that ignores its locks. Same-schema compatibility of a retained
artifact remains a data-format statement, not evidence of lifetime ownership support.

`wg-guard restart --yes` records a `restart` operation using the same lock and service helpers.
Retry that command after a failed/interrupted restart; `update --recover` remains the update/
rollback recovery route. Restart refuses to overwrite another pending operation.

Matching health or SQL column names does not establish compatibility. Migration0007 retains
scalar mirrors, but a pre-0007 binary can run while losing H-range semantics. M3 therefore
requires matching explicit data contracts before artifact-only rollback.

A valid schema1 Phase7 installation can update forward to a contract-capable candidate after
the pre-update backup is recorded. `--skip-backup` is refused when compatibility is unproven.
If the candidate fails after swap, it is stopped and the journal keeps the previous image,
binary and archive identity. After a healthy upgrade, rollback to the legacy build is refused
before altering the running candidate. Normal same-contract updates can roll back automatically.

For `restore-required`, retain both artifacts and the recorded archive and run
`wg-guard restore --recover --password-file /private/backup-password` on the deployment host.
The file must be private (0600); `--password` also accepts hidden terminal or bounded stdin
input. Omit password input only for a recorded plaintext archive. The coordinator verifies the
archive SHA-256/encryption flag and retained binary/immutable image, obtains explicit consent,
confirms service stop, restores the original database schema and matching key without current
migrations, then deploys and health-checks the previous build. It then synchronizes install
state and marks the journal `rolled-back`. Any identity, restore, deployment or health failure
keeps recovery pending. A shared-volume guard blocks new-code startup during replacement.
Normal `restore ARCHIVE` performs forward migration and cannot substitute for this route.

An ordinary managed restore uses operation `restore`. If it fails, use `restore ARCHIVE --retry`
after reviewing the cause. Other operations continue to refuse a pending journal. Originals,
including WAL and previous-key files, remain recoverable; boot refuses invalid metadata or
unresolved replacements. Automated SQLite/archive, HTTP and service-manager fixture tests do
not certify the dedicated-VPS/M6 lifecycle drills.

## Catalogued core maintenance

`wg-guard core switch recommended --confirm-impact` uses the same lock and journal. The current
recommended bundle is source-backed `awg-2026-09`; recommended and latest-compatible resolve to
it. Exact upstream tags/commits, the versioned DKMS identity and cached source ownership are
verified before readiness. Package-backed `awg-2026-08` remains recognizable for legacy
new-layout transition compatibility. Owned catalogued source can be repaired or moved to the
recommended entry; no arbitrary upstream branch or unreviewed version is accepted. An unknown or
unowned installed combination is refused with a manual migration requirement. Docker core changes
that require a different userspace tool bundle must travel through the panel/runtime update path;
WG-Guard never rewrites only half of that compatibility pair.

For manual migration, retain the panel backup and existing core identity, review the pinned
integration contract, verify the selected source or legacy exact packages are available, and plan
a maintenance window. Loaded module version, loaded `srcversion` and on-disk `srcversion` are distinct facts.
Never unload active tunnels as an installer step. A differing source identity stays pending
until an operator reboot and successful recheck; unknown identity never counts as correct.

After correcting unavailable package/module observations, repeat
`wg-guard core switch recommended --confirm-impact`. This explicitly retries its own
`prepared`, `pending-reboot` or `recovery-required` record under the lifecycle lock, observing
the installed packages and loaded/disk module identity again before completing. It also
handles interrupted or failed state/journal writes; another pending operation is never
overwritten. `update --recover` points core/restart/restore operators to their own recovery
commands rather than treating those records as updates.

Phase 8.1 certified the base transaction paths on the dedicated Docker/native VPS. Phase 8.2
added automated exposure/certificate failure coverage and a real Docker state-migration,
Nginx/webroot, public-IP renewal and restoration drill. Phase 11 native direct ACME issued a
trusted domain certificate, reused its protected cache after restart and rejected an invalid
manual-certificate change while preserving healthy HTTPS.


## Independent domain recovery (current main, unreleased)

A `domains` journal retains boot/Compose/approved policy and certificate-hook
recovery together. Candidate validation does not stop the working listener;
post-activation failures restore its prior snapshot with an independent timeout.
For a pending domain journal run `sudo wg-guard domains recover`; the manager
dispatches this operation explicitly. It must not invoke update recovery for it.
Do not erase the journal or rerun issuance to bypass a pending operation.
The host mailbox records interruption without automatically replaying the action.
Owned pairs are pruned only after terminal recovery, preserving both references
while recovery is pending. Active domain policy requires `domain_protocol: 1`
in the selected runtime; rollback cannot silently remove SNI/HTTP isolation.
External gateway TLS/routing remains operator-owned and is not certified locally.
See [domain/TLS operations](domains-and-tls.md).

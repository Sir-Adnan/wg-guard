# Lifecycle recovery

Phase 8.1 M3 uses one host lifecycle lock, `/run/lock/wg-guard-lifecycle.lock`.
The Linux kernel releases it when the process exits or dies; do not delete the lock file
to bypass another operator. Install/update/uninstall/restart/core selection and TLS-state writes
share this lock. The application remains one binary with one scheduler.

DB/key access also uses `/var/lib/wg-guard/.wg-guard-data.lock`, a separate persistent
inode shared by native commands and Docker's bind-mounted data volume. Every data CLI and
the server keep shared kernel ownership until their DB/key users close. `secrets rotate`
takes exclusive ownership before loading keys or carriers, including while awaiting `YES`.
Restore and interrupted pair recovery require exclusive ownership; stopping the service
alone is insufficient if another data command is still running. Startup completes any
pending restore exclusively and converts to lifetime shared ownership while admission stays
closed. Preview/archive staging remains possible while the server is running.
Startup retains exclusive ownership until DB/key initialization finishes. A CLI opening
a node whose master key does not yet exist also keeps exclusive ownership until key
initialization finishes, preventing concurrent first-key creation. Database-only commands
do not create a key as a side effect. A timed-out server shutdown retains its lease until a later
successful handler drain or process exit.

Contention fails immediately with fa/en guidance to finish other data commands and stop the
service before rotation/restore. A failed managed restore retains its journal/guard and
keeps the service stopped: finish the contender, then use `restore ARCHIVE --retry` (or
`restore --recover` for recorded original-schema recovery). Locks are kernel-owned and
released on process exit/death; the file is never archived, replaced or removed during
restore. Never unlink the lock file to bypass an owner. On Linux the two byte ranges use
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

`/etc/wg-guard/install-state.json` is schema2 and mode 0600. Schema1 remains readable when
its paths match the managed layout. Missing state is distinct from unreadable, corrupt or
unsupported state; the latter cases stop lifecycle commands. Manual/custom layouts need
explicit migration, not editing deletion targets to arbitrary paths.

`/etc/wg-guard/lifecycle.json` is a private, atomically replaced and synced operation journal.
It records the stage, before/after state, previous/candidate artifact paths, prerequisite
package intents, observed ownership and repository preparation. Package intents mean an
interrupted apt command may have changed those packages; inspect `dpkg-query` before deciding
ownership. Shared PPA configuration is retained on uninstall. No password, token, raw boot
configuration or archive content belongs in state, the journal or diagnostic evidence.

Retained binaries and Compose snapshots live in random private directories under
`/etc/wg-guard/lifecycle/`. Their exact paths and binary SHA-256 are recorded. Successful
updates retain current and previous artifacts and prune superseded recorded copies; Docker
images and pre-update archives are retained. A process killed during staging can leave an
unreferenced private directory. Inspect journal/state references before removing such a
directory during maintenance; do not delete the whole lifecycle directory.

Pre-update archives use the existing backup service in the owning environment (Docker exec
or native command) with a dedicated local output directory:
`/var/lib/wg-guard/backups/lifecycle-<operation-id>/`. The journal records the actual returned
archive name, SHA-256 of local bytes and whether the file has an age header. A remote delivery
claim or a missing local file is insufficient. Archive hashing uses bounded memory; it is
identity evidence, not a replacement for restore verification. These dedicated recovery
archives are outside the ordinary top-level backup retention/listing and need deliberate
retention review after the rollback window. `--purge-data` removes them with the data directory.

## Interrupted operations

For interrupted updates, run `wg-guard update --recover` using a Phase8.1 binary. If the installed host command predates
that command, use an acquired compatible candidate directly. Do not start old code manually
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
Native cleanup first validates systemd's load and activity properties. Confirmed
`LoadState=not-found` with `ActiveState=inactive` permits cleanup without stop/disable, including
installation before unit creation or uninstall retried after unit removal. Missing unit files,
failed queries, incomplete properties and inconsistent states do not establish absence.

Certificate readiness is separate from process health. A healthy installation with pending
TLS can keep serving the ACME challenge while `wg-guard tls-check` retries certificate proof.

## Database compatibility and legacy migration

The machine-readable `wg-guard installer-contract` command does not open node data. Revision1
currently reports `data_contract: schema7-h-ranges-v1`, prerequisites and recoverable lifecycle
support. `local_owner` is true and required for new candidates: installer-managed setup
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
catalog has one verified bundle, `awg-2026-08`; recommended and latest-compatible resolve to it.
Reaffirming its exact installed tools/kernel package identities can run offline. No alternative
package transition is invented. An unknown installed combination is refused with a manual
migration requirement; a future catalog transition needs explicit compatibility and package
availability evidence before implementation.

For manual migration, retain the panel backup and existing core identity, review the pinned
integration contract, verify both exact package versions are available, and plan a maintenance
window. Loaded module version, loaded `srcversion` and on-disk `srcversion` are distinct facts.
Never unload active tunnels as an installer step. A differing source identity stays pending
until an operator reboot and successful recheck; unknown identity never counts as correct.

After correcting unavailable package/module observations, repeat
`wg-guard core switch recommended --confirm-impact`. This explicitly retries its own
`prepared`, `pending-reboot` or `recovery-required` record under the lifecycle lock, observing
the installed packages and loaded/disk module identity again before completing. It also
handles interrupted or failed state/journal writes; another pending operation is never
overwritten. `update --recover` points core/restart/restore operators to their own recovery
commands rather than treating those records as updates.

These transaction and failure paths are unit/Linux-fixture tested, not yet certified by the
Phase8.1 dedicated Docker/native VPS lifecycle and legacy-upgrade drill.

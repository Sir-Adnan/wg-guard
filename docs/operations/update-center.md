# Update Center

This is the implementation contract for the maintenance experience shipped in v0.1.7.
Source/artifact verification and real-host limitations remain distinct; see [status](../development/status.md).

## Presentation and permissions

The server-rendered workspace has Overview, Versions, Operations and Recovery sections.
`update.read` is a panel-only permission for observation/history; existing `update.manage`
grants also permit observation. Execution, preparation, scheduling and cancellation require
`update.manage`. Neither permission is reseller-grantable or an API-token REST scope.
Archive downloads additionally require `backup.manage` and maintenance observation access.

Panel, independent manager, AWG tools and recorded core bundle are separate facts. Host
inventory includes deployment mode, platform, kernel, loaded/disk identity, DKMS, Go build
verification and configured backend counts. A recorded bundle does not prove a loaded upstream
commit. Observations carry their timestamp; missing/unreadable facts are unknown. The host
publishes only an allowlisted report in the shared data volume, never privileged install state,
raw commands, endpoints, keys or configuration.

Versions use real GitHub pages of 10/20/30 entries, bounded to 100 upstream pages per navigation.
Drafts/prereleases are excluded without treating a filtered page as the end of the archive.
Next/previous do not invent a total. Search and the newer-version filter apply to the retrieved
page. An exact selected stable tag can be resolved separately; development/free-form source
selection is absent. Opaque tags do not receive guessed upgrade/downgrade labels.

The server keeps at most eight page results for ten minutes, with a thirty-second minimum
explicit refresh interval. Source requests support ETag/304; failures back off for at least a
minute and honor bounded publisher retry/reset guidance. A cached stale result remains visibly
stale and never authorizes execution without fresh acquisition/validation. A cached new-version
indicator makes no background requests; its personal seven-day snooze never installs software.

## Selection, preparation and execution

An operator selects a destination, inspects escaped/plain release notes and chooses panel
maintenance or manager/panel/recommended-core maintenance. Core selection is its own reviewed
catalog. The responsive release browser collapses on phones; the selected destination and actions
remain reachable without scrolling through the entire archive. Density is a local display
preference. Shared tokens/components carry every visual preset, fa/en and light/dark.

Preflight observes platform, pending journal, free data-volume space (a conservative 2 GiB
threshold), deployment tools, candidate installer contract and applicable module/header facts.
Headers may need installation; backup creation and new-service health are verified during actual
execution. These point-in-time checks are not a restore drill or a complete disk-capacity proof
for custom Docker/temp filesystems. Execution repeats them; a checked selection expires after
ten minutes. Backups remain mandatory for forward panel deployment.

A verified prepared binary/receipt lives in one root-owned 0700 directory at
`/var/cache/wg-guard/update-candidate`. Preparation does not replace either the manager or service
binary and does not restart services or change DKMS. Local digest, root ownership and installer
contract are rechecked before reuse, followed by fresh exact upstream tag/commit/checksum-manifest
validation. A scheduled/confirmed checked selection pins both the commit and artifact SHA-256.
A changed publisher identity requires renewed review instead of silently applying new bytes.

The existing lifecycle engine still owns deployment, backup, health, rollback and core changes.
The web process supplies catalog identifiers through a fixed private queue; it receives no
Docker/systemd access, shell execution, executable path or arbitrary download URL capability.
Root-side execution rechecks the requesting account by immutable account ID in a read-only DB
open, without migrations or master-key loading. Disabled/deleted accounts, reseller accounts and
revoked `update.manage` grants cannot execute a new-format request. A shared data lease prevents
concurrent data/key replacement during the authorized host job. Older queued requests retain
their original protocol handling.

## Durable operation feedback

One fixed host runner owns the maintenance lock independently of the per-component lifecycle
lock. Closed component/stage IDs produce safe step records and a monotonic revision; raw output
remains in the existing private installer/service logs. The current operation, actual resulting
component versions and at most 100 completed records are retained. Web history is paged by ten.
Copy/download reports contain fixed operation metadata, not raw errors or secrets.

Only active jobs poll, every three seconds (thirty seconds for a reserved schedule); hidden tabs
pause. A panel restart/transport interruption reconnects to the same job and never resubmits it.
Stage revision changes render even when the overall state stays running. Closing the browser
does not cancel host work. Authentication failures keep the normal session/permission handling.

Elapsed time never grants the web permission to erase an active claim. The fixed runner holds a
separate kernel lock before reconciling an interrupted claim. Completed-but-unpruned records keep
their actual terminal outcome rather than becoming falsely interrupted.

## Maintenance windows

One reservation may be scheduled from one minute to thirty days ahead, with an explicit UTC
Gregorian input. Its destination is fixed. A systemd timer invokes the same bounded runner about
once a minute; the future reservation uses a different file from the immediate systemd path
trigger, avoiding a busy loop. A reservation more than thirty minutes late is canceled, rather
than unexpectedly applied after a long host outage. There is no recurring auto-update, automatic
host reboot or automatic backend fallback.

Cancellation atomically arbitrates with host claiming and is available only before execution.
Running deployment/backup/module changes cannot be canceled from the web. A running browser
status is not proof that the requested component update succeeded; outcomes remain independent.

## Recovery and compatibility

Rollback restores the retained previous artifact only under the engine's explicit data-contract
checks; selecting an older public release is a separate operation. Recovery follows the retained
journal. Core, restart, exposure and coordinated database restoration retain their dedicated
host-side recovery paths in [lifecycle recovery](lifecycle-recovery.md).

Installer contract revision 2 now advertises `maintenance_protocol: 2`. Older records omit it.
Broker registration follows the installed candidate/previous artifact capability during install,
update and recovery: protocol 2 enables detailed reports and the timer; legacy artifacts retain
the original marker/path service and have no unsupported timer. Unknown protocol values are
refused. This capability is distinct from the database-format compatibility contract.

An upgrade driven by an older manager may need the newly installed command
`sudo wg-guard update-broker-install` once. An enhanced marker is never assumed from a panel
version label. First verify the actual panel/host executable was upgraded.

Referenced pre-update archives appear in Recovery and Backups. Downloads verify the recorded
SHA-256 before sending bytes and require the extra backup permission. Recovery archives are
not automatically deleted by ordinary top-level backup retention; retain them through the
rollback window and follow the host guide for deliberate cleanup.

## Verification boundary

Focused package tests cover permissions, account reauthorization/data ownership, safe progress,
scheduled/missed windows, cancellation, history bounds, catalog caching/conditional requests,
prepared-artifact integrity and broker capability transitions. The opt-in
`TestBrowserMaintenance` fixture checks real browser rendering, keyboard confirmation,
responsive collapse, HTMX filtering/reconnection and optional WCAG scanning across the selected
locale/theme/viewport matrix. Fixtures use disposable data and never run actual host updates.

These checks do not certify a new Ubuntu installation, real kernel traffic, physical mobile
device or public release. Current verification results live in the status document.

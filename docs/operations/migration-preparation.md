# Refactor migration preparation

This is the owner's preparation drill for one existing node, not authorization to
rebuild a host. The Docker-only target is still planned. Use the current
[status](../development/status.md) and [refactor gates](../development/refactor-program.md);
do not rebuild until the target release, data verification and acceptance are ready.
The independent `backup verify` and safety corrections are on main, not v0.1.8.
Run verification with a trusted build containing those commands; a new public
preparation release still requires owner approval and its artifact gate.

## Capture a recoverable copy

1. Record safe inventory: build/source identity, account/device counts, charged
   usage, kernel/userspace profile counts, pool CIDRs, endpoint, panel/subscription
   hostnames and TLS mode. Do not copy private keys, tokens, passwords, capability
   URLs or raw configs into logs, issue bodies or evidence documents.
2. Choose a strong unique backup password and store it outside this server in a
   password manager with a separate recovery copy. The setting stored on the node
   is convenient for scheduled backups; it cannot unlock the age archive containing
   that very setting. Losing this password can make an encrypted archive unusable.
3. In Settings, set the backup password through its secret field. Create and
   download a fresh archive from Backups or the Dashboard. Keep the original server
   intact. For the CLI alternative, enter the password through hidden input:

   ```bash
   sudo wg-guard backup create --password --output /root/wg-guard-export --reason migration-export
   ```

   `--output` is a directory, not a filename. This command also attempts configured
   Telegram delivery; its warnings do not substitute for checking the local copy.
   Do not put the password in argv, shell history or an environment variable.
4. Download the resulting `.wgg` to private off-host storage. Record its SHA-256
   on the server and compare the downloaded file's SHA-256. The digest identifies
   bytes; it does not by itself prove publisher identity or decryptability.
5. Verify the downloaded copy with the independent verifier:

   ```bash
   wg-guard backup verify --archive /private/wg-guard-export.wgg --password
   ```

   Enter the saved password, not a newly generated one. Verification needs no
   installed state, running container, AWG or host network changes. A successful
   report covers supported archived data, references and all encrypted fields.
   It also checks the documented [stored-domain contract](backup-restore.md), including
   key-pair identity, IP assignments, pinned AWG parameter relationships, subscription/
   accounting state, critical dates, customer hashes and known settings. Kernel/userspace
   inventory describes target requirements; it does not prove they are installed there.
   Compare the stored inventory with step 1; historical/disabled records are counted.
   A timed-out/canceled check is incomplete and must be retried. A key or integrity
   failure must be resolved while the original server still exists.
6. Keep two reviewed copies where practical and check that the saved password
   unlocks one independently. Retain the old release/backup until the migration
   drill and actual client acceptance finish. Do not rotate the master key merely
   to prepare a backup; rotation is a separate stopped-service operation.

## Review the target before rebuilding

The archive carries the database, master key and boot configuration. It does not
carry TLS certificates/private certificate keys, ACME cache, DKMS/module packages,
Docker images, OS routes/firewall configuration or host lifecycle state. Preserve
manual certificate files privately or plan fresh issuance; preserve DNS/proxy/tunnel
requirements separately. Keep endpoint routing and subscription/admin hostname
roles explicit. The [domain/TLS guide](domains-and-tls.md) describes current limits
and the future integrated workflow.

Review the target release's supported OS/core/backend requirements, installation
paths, restore contract and certificate/network setup. Independent data verification
does not establish that those target requirements are installed. Stock WireGuard,
remote nodes and other VPN engines are outside this refactor's current scope.

Once the preparation/target gates pass and the owner separately authorizes the
rebuild, install the approved target on the fresh host. Use the supported restore
workflow from [backup/restore](backup-restore.md) and
[lifecycle recovery](lifecycle-recovery.md); do not copy live SQLite/WAL or replace
only the database without its matching key. Review settings/endpoints before apply.

Verify the actual restored node: owner login, account/device totals, unchanged
charged usage and customer access, pool assignments, liveness/readiness, public TLS,
config re-download, real handshake and public traffic. Record safe results and
retained artifact identities. Fresh-layout automated restore tests are evidence for
data behavior; this real-host drill remains an additional acceptance gate.

## Export policy and resource review

Downloadable/off-host archives retain optional age encryption; set a password for
the recovery copy. Local required pre-migration recovery archives intentionally use
plaintext under private filesystem permissions, no remote delivery and keep-five
retention. Avoid treating that local policy as a recommendation for public storage.

The writer/reader stream members. Current admission bounds are 8 GiB compressed
input, 1 GiB database, 1 MiB boot config, 64 KiB manifest and 32-byte master key;
decompressed input is additionally bounded. One encoded secret is at most 8 KiB.
age's factor-18 scrypt operation still uses about 256 MiB transient KDF memory.
Archive creation is serialized across cooperating data-volume processes; independent
verification/restore on separate volumes can still run concurrently. This is not a
machine-wide crypto memory limit.

Leave free space for the SQLite snapshot, output archive, staging copy and retained
recovery pair in addition to the running database/WAL. Streaming bounds memory but
does not remove these disk copies. Disk-full creation cleans partial snapshot/output
files on ordinary failure; a killed process can leave private temporary files for
review. No automatic destructive cleanup is part of this drill. Actual peak resource
and host enforcement-lag measurements remain required before refactor certification.

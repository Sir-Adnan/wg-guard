# Operational runbook

Procedure-level documentation for administrators and maintainers. Concepts live elsewhere;
this file is the "type this, expect that" reference.

## Install

```bash
bash -o pipefail -c 'curl --proto "=https" --proto-redir "=https" -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash -s -- --commit main'
sudo wg-guard                              # reopen local management; no download
```

The terminal is English-only. The recommended wizard asks for an optional domain and whether to
open advanced settings; Enter accepts the recommended answer. It uses Docker, the verified AWG
bundle and default networking. A domain selects automatic HTTPS; a blank domain keeps the panel
private on loopback and prints an SSH tunnel. The final fresh-install confirmation defaults to
install, while destructive lifecycle confirmations always default to no.

Advanced setup exposes native systemd, TLS and panel ports, container image, AWG UDP allocation,
first-interface pool, MTU/DNS and optional Telegram backups. The AWG UDP range is not a panel or
HTTPS port. Automatic HTTPS requires external TCP 80 and 443.

The panel domain is also seeded as the client-facing endpoint (`node.endpoint`). All collected
values are applied before first boot and remain editable in Settings. `--yes` skips every prompt
(flags + defaults; it never reads stdin) and never overrides an explicit `--tls` flag. Automation
must supply `--owner-password-file` as a private regular file with mode 0600.

Fresh interactive setup asks for the username (`admin` on Enter). Entering no password generates a
24-character password and prints it once after successful health/lifecycle completion. A manual
password must be at least 10 characters; short or mismatched input is retried without leaving the
installer. Older completed installations keep their existing username (historically `owner`).

What it writes: `/etc/wg-guard/wg-guard.toml` (0600), `/var/lib/wg-guard/`, the compose
project (`/etc/wg-guard/compose.yaml`) or the hardened systemd unit, the host CLI at
`/usr/local/bin/wg-guard` (in Docker mode it is the mode-aware shim: panel commands exec into
the container, `install|update|uninstall|status|doctor|version` run on the host, `serve` is
refused with compose hints), and `/etc/modules-load.d/wg-guard.conf` so the AmneziaWG module
loads at boot. Preflight refuses busy ports and completed installs; a domain that does not
resolve yet is a loud warning (ACME will fail until DNS points at the host).

Docker image layers belong to Docker's engine storage and are not copied into `/opt`. The stable
operator-visible deployment is the Compose file under `/etc/wg-guard`; `/etc/wg-guard` and
`/var/lib/wg-guard` are bind-mounted into the container.

Verify: `sudo wg-guard status` → container/unit healthy; open the printed panel URL and sign in
with the locally supplied administrator credentials. Diagnostics: `sudo wg-guard doctor`. See
[terminal management](terminal-management.md) for navigation, restart, secrets and cancellation.

## Update and recovery

```bash
wg-guard update --release latest               # published stable release; no source fallback
wg-guard update --commit main                  # resolves an immutable development commit
wg-guard update --binary /path/to/new-wg-guard  # explicit local native candidate
wg-guard update --image registry/image:tag --binary /path/to/matching-wg-guard
wg-guard update --local-image --image sha256:IMAGE_ID --binary /path/to/matching-wg-guard
wg-guard update --rollback                     # previous healthy artifact, when data-compatible
wg-guard update --recover                      # interrupted operation from durable journal
```

Updates remain explicit. Remote pulls, acquisition, checksum and installer-contract checks
must succeed before the active deployment changes. Docker runtime and host-command binary
checksums must agree; deployment and rollback use immutable image IDs. A successful update
retains the previous healthy binary, Compose snapshot and backup identity. Startup, health,
state-write and cancellation failures attempt recovery under a separate bounded context.

Automatic artifact recovery requires equal explicit data contracts. Schema1 installations
remain readable, but pre-Phase8.1 binaries have no such proof. Their forward update requires a
local pre-update archive; a later failure leaves the service stopped with `restore-required`.
An explicit rollback after a healthy legacy upgrade refuses before changing the active node.
See [lifecycle-recovery.md](lifecycle-recovery.md) for journal states, encrypted backup handling,
manual recovery and the current coordinated-restore boundary.

## Uninstall

`wg-guard uninstall --dry-run` prints the exact plan first. Real removal
(`wg-guard uninstall --yes`) stops the node and deletes only the state-recorded artifacts
(compose/unit, host CLI, boot-persistence file, boot config, state). Data
(`/var/lib/wg-guard` — database, master key, backups, ACME cache) and installer-installed
packages are **kept** unless `--purge-data` / `--purge-packages` is passed. Uninstall removes
the CLI itself — run it from the installed path and expect the command to disappear.
State-derived paths are restricted to the fixed managed layout. Stop failure or an unconfirmed
stopped service prevents artifact/data deletion. Corrupt or unsupported state refuses the
operation. Interrupted removal can be retried from its journal; shared apt sources are retained.
The interactive manager presents this as **Uninstall / reset WG-Guard**: its recommended choice
keeps data/backups; **Reset node** adds both purge flags but keeps the cached manager for immediate
reinstall; **Remove everything** uses `wg-guard uninstall --purge-all --yes` to additionally remove
the manager cache, installer logs and remaining WG-Guard lifecycle/config directory, then exits.
Destructive choices default to no. An interrupted install or uninstall reopens its matching guided
cleanup view even when the boot config was already removed.

## Backup / restore / migration

See [backup-restore.md](backup-restore.md) for the full procedure. Quick reference:

```bash
wg-guard backup create                          # manual archive to the local sink
wg-guard backup create --password               # prompt for an archive password (age)
wg-guard backup list                            # local archives + schedule status
wg-guard backup schedule-add -kind daily -time 03:30 [-name N] [-retention N]
wg-guard restore /path/to/archive.wgg           # verify, review, coordinated stop/apply/start
wg-guard restore --recover --password-file /private/backup-password # original-schema rollback
echo SECRET | wg-guard settings set backup.telegram_token -stdin   # secret via stdin, not argv
wg-guard settings set backup.telegram_chat 123456789
wg-guard backup telegram-test                   # verify delivery
wg-guard settings list | get KEY | set KEY VAL  # runtime settings (secrets never printed)
wg-guard secrets rotate                         # master-key rotation (service stopped)
```

The panel mirrors all of it: `/backups` (create, schedules, telegram test, restore wizard) and
Settings → Backups for the credentials. The REST API intentionally has no backup endpoints
(ADR-0007).

## Doctor

`wg-guard doctor` (implemented) checks: platform, privileges, data-dir/master-key permissions,
AWG tool version, kernel-module presence, DB integrity (`PRAGMA integrity_check`), interface
state vs DB (missing links, port drift, peer-count mismatch), nftables table presence, the
`ip_forward` sysctl, tc state when speed limits exist, disk free space, endpoint DNS
resolution, TLS certificate expiry (manual mode), NTP synchronization (timedatectl), and the
backups posture (no schedules + no archives is a warning; stale newest archive too). Checks
that cannot run on a platform report `skip` honestly.

`wg-guard doctor --fix` re-runs the boot repairs (recreate interfaces, re-apply configs and
peers, rebuild nft/tc, enable forwarding) through the same orchestration as `serve`, then
re-checks the affected areas. It **refuses to run while the service is up** — it would race
the serialized reconciler for the AWG subprocess. Read-only doctor is safe anytime.

## Incident playbook (first responses)

| Symptom | First response |
|---|---|
| Installed fine, peers get no traffic | `doctor` → firewall coexistence section (ufw/firewalld forward policy) |
| Panel shows peer state differing from reality | drift detected → `doctor --fix`; check `drift_policy` |
| Traffic counters look wrong after restart | expected behavior: delta re-baseline; verify accumulated totals unchanged in DB |
| Users all `expired` suddenly | clock skew — check NTP/timezone; expiry sweeps use UTC |
| DB errors / corruption hints | stop writes, run `doctor`, restore latest backup (runbook steps above) |
| Cert renewal failing | port 80 reachability (HTTP-01), then `doctor` cert section |
| Disk filling | backup retention, log volume, DB size (`traffic_samples` pruning), `doctor` disk check |

## Verification status

Historical Phase 6/7 drills are recorded in [../development/phase7.md](../development/phase7.md).
The Phase 8.1 lifecycle changes have host-seam/fault tests, Linux process-death lock and atomic
filesystem tests, and executable bootstrap fixtures. Their new Docker/native deployment and
legacy-data migration behavior still require the dedicated M6 VPS drill; prior drills do not
certify them. Current certification is tracked in [../development/status.md](../development/status.md).

# Operational runbook

Procedure-level documentation for administrators and maintainers. Concepts live elsewhere;
this file is the "type this, expect that" reference.

## Install

Docker (default): run the installer, answer the prompts (domain/subdomain, panel port, TLS,
AWG ports, MTU, VPN subnet; optional Telegram backup setup and optional backup password —
everything skippable, every value starting at its recommended default). Verify: `wg-guard
status` → service healthy; open the printed panel URL; complete the onboarding wizard (owner
password, endpoint confirmation, first interface + user).

Native: same installer with the native mode selected; systemd unit replaces compose.

## Update

1. `wg-guard update` (or the UI updater) — checks the release, verifies checksums, creates an
   automatic pre-upgrade backup, applies (image pull / atomic binary replace), restarts,
   health-checks.
2. If startup health fails: follow printed rollback instructions (previous image tag or binary
   + `wg-guard restore <pre-upgrade archive>`).

## Uninstall

`wg-guard uninstall [--dry-run] [--keep-data]` — dry-run lists exactly what will be removed
(files, units/containers, the `wgguard` nftables table, recorded sysctls, installer-installed
packages). Data and backups are preserved unless explicitly discarded.

## Backup / restore / migration

See [backup-restore.md](backup-restore.md) for the full procedure. Quick reference:

```bash
wg-guard backup create                          # manual archive to the local sink
wg-guard backup create --password               # prompt for an archive password (age)
wg-guard backup list                            # local archives + schedule status
wg-guard restore /path/to/archive.wgg           # verify + review; applies with the service
                                                #   stopped, stages for boot otherwise
wg-guard settings set backup.telegram_token …   # or: Settings → Backups in the panel
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

Phase 6 procedures (backup/restore/doctor/settings/rotation) are **implemented + unit tested**;
their real-host verification record lives in [../development/status.md](../development/status.md)
and [../development/phase6.md](../development/phase6.md). Installer-era procedures (update/
uninstall) remain Phase 7.

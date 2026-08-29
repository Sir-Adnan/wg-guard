# Operational runbook

Procedure-level documentation for administrators and maintainers. Concepts live elsewhere;
this file is the "type this, expect that" reference.

## Install

Docker (default): run the installer, answer the prompts (domain/subdomain, panel port, TLS,
AWG ports, MTU, VPN subnet; optional Telegram backup setup and backup password — everything
skippable with safe defaults). Verify: `wg-guard status` → service healthy; open the printed
panel URL; complete the onboarding wizard (owner password, endpoint confirmation, first
interface + user).

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
wg-guard restore /path/to/archive.wgg           # guided; reviews environment on a new host
wg-guard settings set backup.telegram_token …   # or: Settings → Backup in the panel
wg-guard settings set backup.telegram_chat 123456789
wg-guard backup telegram-test                   # verify delivery
```

## Doctor

`wg-guard doctor` checks: OS/arch support, root/permissions, data-dir permissions, pinned AWG
tool version vs supported range, kernel module presence (or userspace fallback), interface
state vs DB (drift), peer sets, nftables table presence + rule drift, sysctl values, tc state,
disk space, endpoint DNS resolution, TLS cert expiry, DB integrity (`PRAGMA integrity_check`),
clock skew (NTP), update channel reachability. `wg-guard doctor --fix` applies safe repairs
(recreate interfaces, re-apply configs, rebuild nft/tc, restore sysctls) and reports anything
requiring human judgment.

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

Procedures above are **designed**; they become "implemented / integration tested / production
verified" per [../development/status.md](../development/status.md) as phases land, and
kernel/firewall behavior on real hardware is confirmed in the Phase 8 VPS matrix.

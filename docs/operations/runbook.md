# Operational runbook

Procedure-level documentation for administrators and maintainers. Concepts live elsewhere;
this file is the "type this, expect that" reference.

## Install

```bash
wg-guard install                      # interactive wizard (Docker default)
wg-guard install --mode docker --domain vpn.example.com --yes
wg-guard install --mode native --tls proxy --panel-port 8080 --yes
```

The wizard asks for: mode (Docker default / native systemd), domain (blank = IP-only),
TLS mode (ACME automatic with a domain; manual certs; proxy; dev), panel port, ACME challenge
port (default 80), and the container image (Docker mode). Two optional sections follow, both
defaulting to skip — **pressing Enter everywhere is a complete install**:

- *VPN network defaults*: AWG listen-port allocation range, the VPN pool offered to the
  first interface (awg0), client MTU, client DNS resolvers. Per-interface values remain
  hot-editable in the panel.
- *Telegram backups*: bot token (hidden input on terminals, transported via stdin — never
  argv or logs), chat ID and a daily UTC backup time (creates an enabled `installer-daily`
  schedule).

The panel domain is also seeded as the client-facing endpoint (`node.endpoint`), so the
first exported config works without a Settings visit. All collected values are applied
before the service first boots (the settings registry caches in memory) and remain editable
in the panel: Settings → Backups/Networking. `--yes` skips every prompt (flags + defaults;
it never reads stdin) and never overrides an explicit `--tls` flag.

What it writes: `/etc/wg-guard/wg-guard.toml` (0600), `/var/lib/wg-guard/`, the compose
project (`/etc/wg-guard/compose.yaml`) or the hardened systemd unit, the host CLI at
`/usr/local/bin/wg-guard` (in Docker mode it is the mode-aware shim: panel commands exec into
the container, `install|update|uninstall|status|doctor|version` run on the host, `serve` is
refused with compose hints), and `/etc/modules-load.d/wg-guard.conf` so the AmneziaWG module
loads at boot. Preflight refuses busy ports and completed installs; a domain that does not
resolve yet is a loud warning (ACME will fail until DNS points at the host).

Verify: `wg-guard status` → container/unit healthy; open the printed panel URL; complete the
onboarding wizard. Diagnostics: `wg-guard doctor`.

## Update

```bash
wg-guard update --image wgguard/wg-guard:vX      # docker mode
wg-guard update --binary /path/to/new-wg-guard   # native mode (staged file; nothing is downloaded)
```

Explicit only — nothing auto-updates. The flow: pre-upgrade backup (in the owning
environment) → swap (compose image switch + pull + recreate, or previous binary kept at
`<bin>.pre-update` and the staged one applied) → restart → health check → **automatic
rollback** to the previous artifact when the health check fails.

If an update is interrupted (killed mid-flight, host reboot), the state file still records
the last health-checked artifact:

```bash
wg-guard update --rollback          # re-deploy the recorded image / <bin>.pre-update
```

## Uninstall

`wg-guard uninstall --dry-run` prints the exact plan first. Real removal
(`wg-guard uninstall --yes`) stops the node and deletes only the state-recorded artifacts
(compose/unit, host CLI, boot-persistence file, boot config, state). Data
(`/var/lib/wg-guard` — database, master key, backups, ACME cache) and installer-installed
packages are **kept** unless `--purge-data` / `--purge-packages` is passed. Uninstall removes
the CLI itself — run it from the installed path and expect the command to disappear.

## Backup / restore / migration

See [backup-restore.md](backup-restore.md) for the full procedure. Quick reference:

```bash
wg-guard backup create                          # manual archive to the local sink
wg-guard backup create --password               # prompt for an archive password (age)
wg-guard backup list                            # local archives + schedule status
wg-guard backup schedule-add -kind daily -time 03:30 [-name N] [-retention N]
wg-guard restore /path/to/archive.wgg           # verify + review; applies with the service
                                                #   stopped, stages for boot otherwise
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

Phase 6 procedures (backup/restore/doctor/settings/rotation) and the Phase 7 deployment
procedures (install docker/native, update + rollback, uninstall, status, ACME issuance,
reboot persistence) are **implemented + unit tested + verified on the real VPS**; the drill
record lives in [../development/phase7.md](../development/phase7.md) and
[../development/status.md](../development/status.md).

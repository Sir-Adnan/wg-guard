# Terminal management

Run `sudo wg-guard manage --lang fa` or `sudo wg-guard manage --lang en` on the Linux
host. `wg-guard` without arguments opens management only when stdin is a terminal;
otherwise it prints help and exits without starting setup. Opening the menu reads the
installation record, health endpoint, TLS/core readiness and lifecycle journal; it performs
no deployment mutation. `WGG_LANG=fa|en` selects the default language, with `LANG=fa…`
also recognized. The language action switches the current menu session.

The [one-command GitHub entry](github-install.md) opens this manager on installed nodes.
On fresh nodes it starts setup using the exact acquired build; rerunning it does not imply an
update. Forwarded setup flags or `--yes` explicitly retain install-only behavior.

The stable numbered groups are:

| Group | Actions |
|---|---|
| 1 — Setup / lifecycle | Install, update, rollback, interrupted-update recovery, uninstall with data preserved |
| 2 — Operations / diagnostics | Status, read-only doctor, TLS verification, installed/recommended/latest-compatible core, controlled core switch, service restart |
| 3 — Backups / recovery | Create/list archives, coordinated restore, full schedule CRUD, Telegram setup/test/selected-archive send, backup password and original-schema recovery |
| 4 — Language | Persian / English |

`0` or `back` returns; `q` cancels. Invalid numbered/numeric answers are retried. Persian
and Arabic numerals are accepted for numeric fields; paths, passwords and tokens are not
normalized. Disruptive operations show impact and require explicit confirmation. Blank
confirmation means **no**; EOF, a partial line at EOF and interrupted input never grant
consent. Core switching only admits the documented verified catalog: currently the existing
bundle can be reaffirmed, not an invented package transition.

Install/update source selection shows the installed version, the actual latest stable tag
and publication date, a bounded page of at most 30 stable releases, or explicit development
`main`/full SHA. Development is resolved to an immutable commit before review. An empty
release catalog does not select development automatically. Source metadata and the final
install review show build identity and impact, never credentials.

Setup groups the public VPN endpoint, panel/TLS TCP settings, per-interface AWG UDP allocation,
and optional Telegram/daily backup settings. The default UDP range is 30000–50000, allocated
one port per interface; it is not the panel TCP port. HTTP-01 always requires external TCP80
to reach its challenge listener. A loopback panel URL is distinct from the public VPN endpoint.

## Owner before public access

Installer-managed setup creates or reuses the local owner after settings are seeded and
before Docker starts or systemd enables the service, inside the lifecycle lock. It calls
the host-local `owner-bootstrap` command against the shared data volume and existing admin
service. Existing owners are detected without resetting credentials. Both web bootstrap and
direct owner creation use a single conditional SQLite insert, preventing concurrent creation
of multiple owners.

Interactive fresh setup asks for a hidden password and confirmation. The shared password
policy is at least 10 bytes. For automation, supply a regular private password file (0600)
containing one password, at most 4096 bytes including its optional newline:

```bash
sudo wg-guard install --commit FULL_40_CHARACTER_LOWERCASE_SHA \
  --mode native --domain vpn.example.com --yes \
  --owner-username owner --owner-password-file /root/wg-guard-owner-password
```

Prepare that file with a trusted password manager/editor, not a shell command containing the
password. The installer reads it with a size bound, transports credentials to the owner
command through stdin, and never copies its contents into lifecycle records or summaries.
Remove the supplied file when it is no longer needed. A preserved existing owner skips password
file input altogether. Failure to verify/create an owner prevents listener startup; the
partial-install journal remains available for preservation-aware cleanup and retry.

After setup, sign in using the supplied credentials and create the first interface in the
panel. Installer contract revision1 now requires `local_owner=true` for newly selected
candidates. Older artifacts' known data contracts remain recognizable for same-schema
rollback even when they lack this setup capability.

Manual, uninstalled `wg-guard serve` retains its existing web onboarding posture. This is not
certified for unattended fresh public exposure; restrict access until an owner exists. Broader
manual-deployment hardening remains Phase11 review.

## Terminal constraints and interruption

The interface streams a single column, stacks labels/values and wraps long dynamic data.
48/80/120-column scripted layouts are tested. ANSI color is disabled with `NO_COLOR`,
`TERM=dumb` or redirected output; no full-screen terminal, animation or polling loop is used
for presentation. Dynamic display values are stripped of terminal escape/control sequences
and bidi overrides. Technical values keep Latin digits. Persian shaping and bidi rendering
depend on the SSH client/font; text tests do not certify every client.

Input is bounded and performs no read-ahead. Hidden secrets use the actual input terminal FD,
not an assumed stdin descriptor. Linux PTY tests verify pasted-answer sequencing, secret
echo suppression, Ctrl-C/SIGINT cancellation and restoration of terminal state. Linux input
uses a blocking poll with a bounded cancellation check, without reader goroutines. Password
editing supports backspace and Ctrl-U; Ctrl-C/Ctrl-D cancel. Native package tools and detailed
legacy diagnostics can retain their own output language and formatting.

Acquisition and lifecycle actions receive cancellation contexts. Management runs lifecycle
actions in-process so cancellation cannot kill a supervising child before its independent
recovery context finishes. Wait for the recovery result after Ctrl-C; do not repeatedly
interrupt recovery. SIGKILL/power loss still requires journal inspection.

`sudo wg-guard restart --yes` uses the shared lifecycle lock, stop/start helpers, health
validation and a `restart` journal. A failed/interrupted restart can be retried with the same
command. It refuses to replace another pending lifecycle operation. It neither upgrades the
binary nor pulls a Docker image.

## Backup and recovery workflow

The menu invokes shared backup/settings commands in their owning host/container context.
Restore always stays on the host so the shared lifecycle coordinator can stop/start either
deployment mode. The actual archive is validated and reviewed before final apply consent.
Telegram secrets travel through bounded hidden input/stdin. Test files and selected archives
are delivered only after explicit review; unencrypted off-host archives warn about readable
node secrets. Schedule forms cover daily/weekly, every 1–168 hours or equivalent 1–7 days,
retention, enabled state, edit/list/delete and next-run reporting in UTC.

The running service observes backup settings and schedule edits on its next pass without
restart. Other settings retain their cached/restart behavior. Pending lifecycle journals block
generic restore; the explicit recovery action handles `restore-required` with the recorded
original archive and retained artifact, without forward migration. See
[backup-restore.md](backup-restore.md) and [lifecycle-recovery.md](lifecycle-recovery.md).
Automated tests do not establish real Docker/native/public-TLS deployment verification;
M6 owns the dedicated-VPS drills.

# Terminal management

Run the host manager with:

```bash
sudo wg-guard
```

`sudo wg-guard manage` is the explicit equivalent. The terminal experience is English-only;
legacy language flags and locale environment variables no longer change terminal output. The web
panel remains bilingual Persian/English with RTL support.

Opening the manager reads installation, health, TLS/core readiness and lifecycle state without
changing the deployment. The [GitHub installer](github-install.md) opens this same local manager
when it detects a valid installed node, but the local command is faster and performs no download.

## Navigation

The root menu is state-aware. A fresh host promotes installation; an interrupted operation
promotes its matching recovery action; a healthy installed node shows these groups:

| Group | Actions |
|---|---|
| Install & updates | Build selection, install, update, rollback and interrupted-operation recovery |
| Panel access & HTTPS | Access overview, reversible configuration, certificate renewal check and private fallback |
| Backups & recovery | Create/list/send archives, coordinated restore, schedules and Telegram settings/tests |
| System & diagnostics | Status, read-only doctor, TLS verification, compatible core review/switch and service restart |
| Uninstall | Data-preserving removal by default; purge remains an explicit destructive choice |

Enter the displayed number, use `0` to go back or `q` to cancel. Invalid input is retried. Menus
and prompts use a compact single-column layout that remains readable in narrow SSH terminals.
Confirmations use `[Y/n]` or `[y/N]`; `y`, `yes`, `n`, `no` and their case variants are accepted.

Defaults follow two rules:

- Setup and other reversible choices: **Enter accepts the recommended value**.
- Destructive or disruptive operations: **Enter does not grant consent**; type the explicit
  confirmation shown by the prompt.

EOF, partial input and interruption never grant consent.

## Recommended setup

The first verified GitHub acquisition opens the manager and persists it before setup. Choose
**Install WG-Guard**; canceling or failing later leaves `sudo wg-guard` ready for a local retry.
Fresh interactive setup then starts with only two decisions:

1. Optional domain for automatic HTTPS. Leave it blank to keep the panel on loopback and access it
   through the displayed SSH tunnel. This can be changed after installation.
2. Whether to customize advanced settings. Press Enter for the recommended setup.

The recommended path uses Docker, detects the public VPN address, selects the source-backed pinned
`awg-2026-09` bundle, allocates per-interface UDP ports from 30000–50000 and uses the documented
network defaults. A domain enables ACME HTTPS; external TCP ports 80 and 443 must reach the VPS.
Without a domain the panel TCP listener remains private. The VPN UDP port is separate from the
panel/HTTPS TCP ports.

Advanced setup exposes native systemd, TLS mode and ports, network/MTU/DNS settings, container
image and Telegram backup setup. It does not permit arbitrary or unverified AmneziaWG versions.

Before any public listener starts, setup securely creates or reuses the administrator account.
Password input is hidden, existing credentials are never reset, and a failed account check prevents
listener startup. After setup, sign in and create the first interface (`awg0`) in the web panel.

## Panel access & HTTPS

The recommended post-install wizard presents product choices rather than raw TLS modes:

| Choice | Use when | Requirement |
|---|---|---|
| Private SSH tunnel | safest default or no hostname | no public panel port |
| Automatic domain HTTPS | 80/443 are free | WG-Guard built-in ACME |
| Existing standard Nginx | Nginx already owns 80/443 | unused hostname; shared webroot or DNS-01 |
| Cloudflare DNS-01 | validation ports cannot be opened | scoped token supplied as hidden input/private file |
| Public-IP HTTPS | no hostname is available | public TCP 80 free; short-lived Certbot certificate |
| Existing proxy/manual/Origin CA | operator owns the edge/PKI | trust and renewal remain explicit operator duties |

Unknown listeners and conflicting virtual hosts are diagnosed but never stopped or overwritten.
There is no public-HTTP option. Direct IP HTTPS is intentionally not offered behind a busy public
proxy; use a hostname through that proxy or DNS-01 instead.

Useful direct commands mirror the menu:

```bash
sudo wg-guard exposure status
sudo wg-guard exposure configure
sudo wg-guard exposure renew
sudo wg-guard exposure private --yes
```

`configure` shows an English guided flow; `--help` lists automation flags. Managed changes use
the lifecycle lock, private snapshots, certificate identity/health proof and rollback. If an
interruption leaves an exposure journal, use the promoted menu action or
`sudo wg-guard exposure recover`.

## Build selection and updates

When stable releases exist, Enter selects the latest published stable release. The bounded release
list can also select an exact tag. Development builds require an explicit `main` or full 40-character
commit selection; `main` is resolved to an immutable commit before review. An empty release catalog
never falls back to development automatically.

Update, rollback, recovery and restart use the shared lifecycle lock, health checks and recovery
journal. Do not repeatedly interrupt recovery; after a power loss or forced termination, inspect
`sudo wg-guard status` and follow [lifecycle recovery](lifecycle-recovery.md).

## Automation and secrets

Non-interactive fresh setup requires a private regular password file with mode `0600`:

```bash
sudo wg-guard install --commit FULL_40_CHARACTER_LOWERCASE_SHA \
  --mode native --domain vpn.example.com --yes \
  --owner-username owner --owner-password-file /root/wg-guard-owner-password
```

Create the file with a trusted password manager/editor. Never place passwords, bot tokens or backup
keys in command arguments. The installer reads bounded secret input and does not store it in logs,
summaries or lifecycle records. Remove the supplied password file when it is no longer needed.

## Terminal behavior

The UI uses no full-screen framework, animation or presentation polling. It is tested at narrow and
wide terminal widths. Information/headings are cyan, success is green, warnings are yellow and
failures are red. Color is automatically disabled for redirected output, `TERM=dumb` or
`NO_COLOR`; dynamic values are stripped of terminal control and bidi characters. Hidden input uses
the real terminal descriptor and restores terminal state after Ctrl-C/Ctrl-D.

Lengthy quiet operations print a short elapsed-time heartbeat while detailed command output is
written to root-only `/var/log/wg-guard/installer.log`. This keeps ordinary and narrow SSH sessions
responsive without flooding the screen.

The running service picks up backup schedules/settings on its next scheduler pass. Coordinated
restore stays on the host so it can safely stop and restart either deployment mode. See
[backup and restore](backup-restore.md) for encryption, Telegram delivery and schedule details.

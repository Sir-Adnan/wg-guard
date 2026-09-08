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

The main menu has three stable groups:

| Group | Actions |
|---|---|
| Install & updates | Install, update, rollback, interrupted-operation recovery and uninstall with data preserved |
| System & diagnostics | Status, read-only doctor, TLS verification, compatible core review/switch and service restart |
| Backups & recovery | Create/list/send archives, coordinated restore, schedules and Telegram settings/tests |

Enter the displayed number, use `0` to go back or `q` to cancel. Invalid input is retried. Menus
and prompts use a compact single-column layout that remains readable in narrow SSH terminals.

Defaults follow two rules:

- Setup and other reversible choices: **Enter accepts the recommended value**.
- Destructive or disruptive operations: **Enter does not grant consent**; type the explicit
  confirmation shown by the prompt.

EOF, partial input and interruption never grant consent.

## Recommended setup

Fresh interactive setup intentionally starts with only two decisions:

1. Optional domain for automatic HTTPS. Leave it blank to keep the panel on loopback and access it
   through the displayed SSH tunnel.
2. Whether to customize advanced settings. Press Enter for the recommended setup.

The recommended path uses Docker, detects the public VPN address, selects the compatible pinned
AmneziaWG bundle, allocates per-interface UDP ports from 30000–50000 and uses the documented
network defaults. A domain enables ACME HTTPS; external TCP ports 80 and 443 must reach the VPS.
Without a domain the panel TCP listener remains private. The VPN UDP port is separate from the
panel/HTTPS TCP ports.

Advanced setup exposes native systemd, TLS mode and ports, network/MTU/DNS settings, container
image and Telegram backup setup. It does not permit arbitrary or unverified AmneziaWG versions.

Before any public listener starts, setup securely creates or reuses the administrator account.
Password input is hidden, existing credentials are never reset, and a failed account check prevents
listener startup. After setup, sign in and create the first interface (`awg0`) in the web panel.

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
wide terminal widths. Color is automatically disabled for redirected output, `TERM=dumb` or
`NO_COLOR`; dynamic values are stripped of terminal control and bidi characters. Hidden input uses
the real terminal descriptor and restores terminal state after Ctrl-C/Ctrl-D.

The running service picks up backup schedules/settings on its next scheduler pass. Coordinated
restore stays on the host so it can safely stop and restart either deployment mode. See
[backup and restore](backup-restore.md) for encryption, Telegram delivery and schedule details.

# WG-Guard

A lightweight, self-hosted **AmneziaWG** VPN node management panel for Ubuntu VPS servers.

One Go binary. SQLite. A premium bilingual (Persian/English) web panel with full RTL support. A
stable REST API so Telegram bots, billing systems, and VPN platforms can manage the node
remotely. Extremely low RAM and idle CPU — the VPN traffic gets the server's resources.

Think: *wg-easy simplicity + serious commercial user management + API-first design + AmneziaWG
anti-DPI capabilities.*

## Status

**In active development — Phases 0–8.2 are complete; Phase 9 is next and has not started.**
Phase 8 verified AmneziaWG config/QR correctness with real clients. Phase 8.1 delivered the
GitHub installer, recoverable lifecycle management, backups and an English-only host terminal,
with Docker and native verification on Ubuntu 24.04 amd64. Phase 8.2 added a persistent local
manager, existing-Nginx coexistence, DNS-01, trusted short-lived public-IP HTTPS, certificate
renewal diagnostics and safe post-install access changes. Broader compatibility and public
release work remain later. A closing installer hardening pass moved the recommended AWG core off
the vanished PPA package pin to exact reviewed GitHub source, hardened interrupted retries, and
passed a fresh Ubuntu 24.04 Docker install/purge drill. See
[ROADMAP.md](ROADMAP.md) and the
[development status](docs/development/status.md).

## Features

- **AmneziaWG tunnel profiles** (`awg0`, `awg1`, …) — each with its own obfuscation parameters,
  listen port, subnet pool, and MTU; managed entirely from the panel
- **User management** — subscriptions with duration, expiration (including first-connection
  activation), traffic quotas (RX+TX), **independent upload/download speed limits**, device
  limits, bulk creation
- **Devices** — one peer per device, config/QR download, revocation, regeneration
- **Stable REST API** (`/api/v1`) — token auth with scopes, idempotency keys, cursor pagination,
  per-token rate limits, durable signed webhooks, OpenAPI document (`/openapi.json`, `/docs`)
- **Premium bilingual panel** — Persian (default) and English, full RTL, light/dark themes,
  excellent on mobile and desktop, server-rendered with HTMX — no heavy frontend runtime
- **Backups** — manual, scheduled, Telegram delivery, optional password protection; restore with
  server-migration support
- **Safe Linux integration** — namespaced nftables table (never touches foreign firewall rules),
  kernel-module AmneziaWG, drift reconciliation, and `doctor` diagnostics; pinned userspace
  runtime compatibility is tested, while automatic fallback lifecycle remains a Phase 11 gate
- **Clean deployment** — Docker by default (verified runtime image + Compose), native systemd
  supported; private SSH access, built-in domain ACME, standard-Nginx/webroot, Cloudflare DNS-01,
  trusted public-IP HTTPS and operator-owned proxy/certificate paths

## Install

WG-Guard supports **Ubuntu 24.04 or newer on amd64/x86_64**. Ubuntu 24.04 LTS is the currently
verified target. Until the first stable release is published, run:

```bash
bash -o pipefail -c 'curl --proto "=https" --proto-redir "=https" --tlsv1.2 -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash -s -- --commit main'
```

The first run verifies and stores the local manager, then opens its menu; it does not begin setup
without your choice. Select **Install WG-Guard**. The terminal is English-only and **Enter** accepts
the safest recommended answer: Docker, the compatible AmneziaWG bundle, automatic HTTPS for a
usable domain, or private SSH access when no domain is supplied. Advanced settings contain custom
ports, native systemd, network defaults and Telegram backup setup. Confirmations display `[Y/n]`
or `[y/N]`; `y`, `yes`, `n`, `no` and case variants are accepted.

The first development-source build can take several minutes. A published release installs much
faster because it uses a verified prebuilt binary. When stable releases exist, the default command
becomes the same command without `--commit main`. Long quiet operations emit a short progress
heartbeat; detailed installer output is kept in root-only `/var/log/wg-guard/installer.log`.

## Open WG-Guard again

After installation, use the local manager:

```bash
sudo wg-guard
```

This starts immediately and does **not** contact GitHub. The explicit form
`sudo wg-guard manage` is equivalent. The one-line GitHub command is different: it checks the
selected release or `main` revision, reuses the manager when current, and downloads/builds only
when that revision changed. A verified manager update is cached separately and never silently
restarts or replaces the active panel service. If GitHub is temporarily unavailable, an ordinary
check opens the last verified manager with a warning; `--refresh` remains strict and fails.

Useful read-only checks:

```bash
sudo wg-guard status
sudo wg-guard doctor
```

Updates, rollback, recovery, backups and uninstall are available from the local manager. Open its
**Update center** with:

```bash
sudo wg-guard update
```

Press Enter for **Update everything**, or choose panel + manager, manager only, AmneziaWG core,
or current versions. Automation uses explicit commands such as
`sudo wg-guard update all --commit main --yes`. Panel updates create a backup and require a healthy
restart; core updates accept only the exact WG-Guard compatibility catalog and never force-unload
active tunnels. WG-Guard does not perform a blanket Ubuntu package or OS-kernel upgrade.

The manager's **Panel access & HTTPS** section can later move a private installation to domain
HTTPS, a standard existing Nginx, Cloudflare DNS-01, trusted public-IP HTTPS, manual/Origin CA, or
an operator-owned proxy. Unknown port owners are never stopped, public plaintext is never offered,
and an interrupted change has a dedicated recovery action.

## Inspect before running

For inspection before execution:

```bash
curl --proto '=https' -fsSLo wg-guard-install.sh https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh
less wg-guard-install.sh
bash wg-guard-install.sh --commit main
```

The bootstrap verifies releases and checksums, or resolves `main` to an immutable commit before
building it. It installs only missing prerequisites and refuses unsupported platforms, unknown
AmneziaWG packages and unsafe artifacts. See the [GitHub installation guide](docs/operations/github-install.md)
for exact releases/commits and automation, or [terminal management](docs/operations/terminal-management.md)
for lifecycle and backup commands.

## Documentation

Start with the [documentation index](docs/README.md):

- [Product requirements](docs/product/requirements.md)
- [Architecture overview](docs/architecture/overview.md)
- [AmneziaWG integration](docs/integrations/amneziawg.md)
- [Deployment](docs/operations/deployment.md) · [Backup & restore](docs/operations/backup-restore.md)
- [REST API](docs/architecture/api.md)

## Client requirements

Generated configurations target AmneziaWG clients (AmneziaVPN desktop/mobile apps and the
amneziawg-android/apple/windows forks). Plain WireGuard clients connect only to profiles created
with the "Plain WG" (all-zero obfuscation) preset. The compatibility matrix is documented in
[docs/integrations/amneziawg.md](docs/integrations/amneziawg.md).

## Development

```bash
make build   # build ./cmd/wg-guard
make test    # go test ./...
make lint    # gofmt + go vet + golangci-lint
```

See [docs/development/workflow.md](docs/development/workflow.md) and
[AGENTS.md](AGENTS.md) for conventions.

## License

[MIT](LICENSE). AmneziaWG components are executed as separate processes, never vendored;
third-party components are listed in [THIRD_PARTY.md](THIRD_PARTY.md).

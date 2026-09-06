# WG-Guard

A lightweight, self-hosted **AmneziaWG** VPN node management panel for Linux VPS servers.

One Go binary. SQLite. A premium bilingual (Persian/English) web panel with full RTL support. A
stable REST API so Telegram bots, billing systems, and VPN platforms can manage the node
remotely. Extremely low RAM and idle CPU — the VPN traffic gets the server's resources.

Think: *wg-easy simplicity + serious commercial user management + API-first design + AmneziaWG
anti-DPI capabilities.*

## Status

**In active development — Phases 0–8 complete; Phase 8.1 is active.** Phase 8 completed the
configuration-integrity gate: lossless supported AmneziaWG parameters, canonical recommended and
randomized profiles, byte-identical config/QR delivery, and independent QR decoding. Both profile
classes were imported into isolated real kernel clients on Ubuntu 24.04 and passed handshake plus
bidirectional traffic; the recommended profile also passed through the pinned userspace daemon.
That gate exposed and fixed a peer-sync path that cleared the live interface private key. Phase 9
owns operational observability and bounded logs, after the inserted
[Phase 8.1 installer/lifecycle work](docs/development/phase8.1.md). The full compatibility matrix
remains Phase 11 and final release engineering remains Phase 12. Phase 8.1 implements GitHub
acquisition, prerequisite/core checks, recoverable lifecycle operations and bilingual terminal
management. Backup/recovery completion and integrated VPS certification are still in progress.
See [ROADMAP.md](ROADMAP.md) and
[docs/development/status.md](docs/development/status.md).

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
- **Clean deployment** — Docker by default (official image + compose), native systemd supported;
  built-in TLS/ACME, no reverse proxy required

## Installation

The GitHub bootstrap acquires a verified release or builds an explicitly selected commit; it
does not require a preinstalled Go compiler or a published Docker image. Docker is the default
deployment mode; native systemd uses the same installation engine and data layout.

**Pre-release:** the new installer is on `codex/installer-lifecycle`, not yet on `main`, and no
compatible public release is currently available. Select a reviewed, pushed full commit SHA
from that branch. Do not substitute `main` or assume that downloading a feature-branch bootstrap
also selects that branch's binary. See the [single-command recipe and inspection-first option](docs/operations/github-install.md).

After installation, use the same bilingual management interface:

```bash
sudo wg-guard manage --lang fa
sudo wg-guard manage --lang en
```

Setup reviews the domain or public VPN IP, panel TCP/TLS settings, per-interface AWG UDP
allocation, compatible core and optional backup settings before deployment. Domain ACME needs
external TCP80; IP-only defaults to loopback access with an SSH tunnel. TLS readiness is checked
separately from service health.

The owner is created locally **before the public listener starts**, using hidden password input;
automation requires a private password file unless an owner already exists. Existing owners are
never reset. The installer writes `/etc/wg-guard/wg-guard.toml`, uses `/var/lib/wg-guard`, and
builds a Docker runtime image from the selected binary when needed. No registry publication is
implied. The first VPN interface is created in the panel after signing in.

Read [terminal navigation and automation](docs/operations/terminal-management.md),
[deployment](docs/operations/deployment.md), and [lifecycle recovery limits](docs/operations/lifecycle-recovery.md)
before unattended changes. Full new Docker/native installation and recovery verification remains
the active Phase 8.1 gate; earlier Phase 7 evidence does not certify the redesigned installer.

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

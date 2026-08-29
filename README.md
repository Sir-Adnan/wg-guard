# WG-Guard

A lightweight, self-hosted **AmneziaWG** VPN node management panel for Linux VPS servers.

One Go binary. SQLite. A premium bilingual (Persian/English) web panel with full RTL support. A
stable REST API so Telegram bots, billing systems, and VPN platforms can manage the node
remotely. Extremely low RAM and idle CPU — the VPN traffic gets the server's resources.

Think: *wg-easy simplicity + serious commercial user management + API-first design + AmneziaWG
anti-DPI capabilities.*

## Status

**In active development — Phase 0 (documentation & foundation) complete.** The architecture is
approved and pinned; implementation proceeds phase by phase. See
[ROADMAP.md](ROADMAP.md) and [docs/development/status.md](docs/development/status.md).

## Features

- **AmneziaWG tunnel profiles** (`awg0`, `awg1`, …) — each with its own obfuscation parameters,
  listen port, subnet pool, and MTU; managed entirely from the panel
- **User management** — subscriptions with duration, expiration (including first-connection
  activation), traffic quotas (RX+TX), per-device speed limits, device limits, bulk creation
- **Devices** — one peer per device, config/QR download, revocation, regeneration
- **Stable REST API** (`/api/v1`) — token auth with scopes, idempotency keys, cursor pagination,
  durable signed webhooks, OpenAPI document
- **Premium bilingual panel** — Persian (default) and English, full RTL, light/dark themes,
  excellent on mobile and desktop, server-rendered with HTMX — no heavy frontend runtime
- **Backups** — manual, scheduled, Telegram delivery, optional password protection; restore with
  server-migration support
- **Safe Linux integration** — namespaced nftables table (never touches foreign firewall rules),
  kernel-module AmneziaWG with userspace fallback, drift reconciliation, `doctor` diagnostics
- **Clean deployment** — Docker by default (official image + compose), native systemd supported;
  built-in TLS/ACME, no reverse proxy required

## Installation (designed UX — shipping in Phase 7)

```bash
curl -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash
```

The interactive installer asks for the panel domain/port, AmneziaWG ports, MTU, VPN subnet, and
optionally Telegram backup delivery — everything has a safe default; press Enter to accept.
Docker is the default installation method; a native systemd mode is also supported.

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

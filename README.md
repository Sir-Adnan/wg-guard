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
remains Phase 11 and final release engineering remains Phase 12. GitHub acquisition and artifact
acquisition components are implemented in Phase 8.1; the complete guided installation and
lifecycle workflow is still in progress. See [ROADMAP.md](ROADMAP.md) and
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

Build the binary, then run the interactive installer as root (Docker is the default target; a
native systemd mode is fully supported):

```bash
go build -o wg-guard ./cmd/wg-guard
sudo ./wg-guard install            # wizard: mode, domain, TLS, ports — every value has a safe default
sudo ./wg-guard install --mode docker --domain vpn.example.com --yes   # non-interactive
```

The wizard also offers two optional sections (skipped with Enter): VPN network defaults —
AWG port allocation range, first-interface VPN pool, client MTU and DNS — and Telegram
backup delivery (bot token, chat ID, daily schedule). Everything stays editable later in
the panel, and the panel domain is seeded as the client endpoint so the first exported
config works immediately.

The installer writes `/etc/wg-guard/wg-guard.toml` and the data directory, brings the service
up (compose project or hardened systemd unit), health-checks it, and prints the panel URL —
open it and finish the first-run owner wizard. Certificate issuance (ACME) happens
automatically on the first visit; port 80 must stay reachable. For Docker mode the container
image is built from [Dockerfile](Dockerfile) (`docker build -t wgguard/wg-guard:<tag> .`) and
selected with `--image`; the registry publication of versioned images is part of the Phase 12
release pipeline. Day-2 operations: `wg-guard status · update · uninstall · doctor · backup`,
all documented in [docs/operations/runbook.md](docs/operations/runbook.md).

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

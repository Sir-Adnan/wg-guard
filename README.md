<div align="center">
  <img src="web/static/img/favicon.svg" width="88" height="88" alt="WG-Guard logo">
  <h1>WG-Guard</h1>
  <p><strong>A polished, self-hosted AmneziaWG node panel for modern VPN operations.</strong></p>
  <p>One Go binary · SQLite · SSR + HTMX · English and Persian · Full RTL · REST API</p>
  <p>
    <a href="README.md"><strong>English</strong></a>
    ·
    <a href="README.fa.md">فارسی</a>
  </p>
  <p>
    <img alt="Go" src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white">
    <img alt="Ubuntu" src="https://img.shields.io/badge/Ubuntu-24.04-E95420?logo=ubuntu&logoColor=white">
    <img alt="Architecture" src="https://img.shields.io/badge/Architecture-amd64-4F46E5">
    <img alt="License" src="https://img.shields.io/badge/License-MIT-16A34A">
  </p>
</div>

> [!IMPORTANT]
> WG-Guard is in active development. Phases 0–10 are complete; Phase 11 production
> certification is underway and no public release exists. Ubuntu 24.04 LTS on amd64 is
> the real-host verified deployment target.

## Why WG-Guard

WG-Guard combines the quick setup of a small VPN panel with the controls needed to operate a
serious node:

| | Capability |
|---|---|
| 🛡️ | **AmneziaWG profiles** with per-interface ports, pools, MTU and server-generated anti-DPI presets |
| 👥 | **Commercial user controls** for quota, expiry, first-connection activation, devices, plans and independent speed limits |
| 📱 | **Per-device delivery** with canonical .conf downloads, QR codes, bulk ZIP export and secure access replacement |
| 📊 | **Operational dashboard** for host health, CPU, RAM, disk, live VPN rates, peers and historical traffic |
| 🔌 | **Stable REST API** with scoped tokens, idempotency, cursor pagination, rate limits, OpenAPI and signed webhooks |
| 💾 | **Recovery workflow** with encrypted backups, schedules, Telegram delivery, import review and staged restore |
| 🌐 | **Premium bilingual UI** with English/Persian, RTL/LTR, light/dark/system themes and responsive desktop/mobile layouts |
| ⚙️ | **Safe host lifecycle** for Docker or native systemd, HTTPS choices, diagnostics, rollback and clean removal |

The panel stays lightweight: server-rendered HTML, HTMX and focused vanilla JavaScript. There is
no SPA framework or production Node.js runtime.

## Install

The installer accepts **Ubuntu 24.04 or newer on amd64/x86_64**; production verification currently
covers Ubuntu 24.04. Later releases need their own host certification. Until a stable release is published,
install the reviewed main source:

~~~bash
bash -o pipefail -c 'curl --proto "=https" --proto-redir "=https" --tlsv1.2 -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash -s -- --commit main'
~~~

The bootstrap verifies the selected revision, installs the local manager and opens an
English-only guided menu. Choose **Install WG-Guard**; Docker is the recommended default.
Provide a domain for automatic HTTPS, or leave it empty for private SSH-tunnel access.

To inspect the installer first:

~~~bash
curl --proto '=https' -fsSLo wg-guard-install.sh https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh
less wg-guard-install.sh
bash wg-guard-install.sh --commit main
~~~

## Operate the node

~~~bash
sudo wg-guard                         # local management menu
sudo wg-guard status                  # service and deployment state
sudo wg-guard doctor                  # configuration and host diagnostics
sudo wg-guard logs                    # recent safe operational logs
sudo wg-guard logs --follow --component awg
sudo wg-guard update                  # verified panel/core lifecycle
~~~

The manager covers updates, rollback, panel access, backup, restore and uninstall. It never
accepts arbitrary AmneziaWG bundles, never logs client keys or credentials, and does not remove
unrelated host services.

## Architecture at a glance

~~~text
Browser ── SSR + HTMX ── Go web panel ── domain services ── SQLite
                           │                    │
                           ├── REST /api/v1     ├── AmneziaWG reconciliation
                           ├── OpenAPI          ├── nftables + shaping
                           └── signed webhooks  └── backup + lifecycle manager
~~~

- One process and bounded background work
- One canonical renderer for direct, API, panel and subscription configs/QR
- Namespaced firewall ownership that leaves foreign rules untouched
- Secret-safe storage, responses and logs
- Docker-first deployment with native systemd support

## Documentation

- [Installation guide](docs/operations/github-install.md)
- [Deployment and HTTPS](docs/operations/deployment.md)
- [Backup and restore](docs/operations/backup-restore.md)
- [REST API](docs/architecture/api.md)
- [AmneziaWG compatibility](docs/integrations/amneziawg.md)
- [Architecture](docs/architecture/overview.md)
- [Project status](docs/development/status.md)
- [Documentation index](docs/README.md)

Generated profiles require compatible AmneziaWG clients. Standard/plain profiles can be used
with ordinary WireGuard clients. See the compatibility document before choosing a profile.

## Development

~~~bash
make build
make test
make lint
~~~

Read [AGENTS.md](AGENTS.md) and the [development workflow](docs/development/workflow.md) before
contributing.

## License

WG-Guard is available under the [MIT License](LICENSE). Third-party notices are listed in
[THIRD_PARTY.md](THIRD_PARTY.md); AmneziaWG components run as separate processes and are not
vendored into WG-Guard.

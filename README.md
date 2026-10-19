<div align="center">
  <a href="https://github.com/Sir-Adnan/wg-guard"><img src="docs/assets/readme-hero.svg" width="100%" alt="WG-Guard — one node, clear control"></a>
  <h1>WG-Guard</h1>
  <p><strong>A considered control panel for your AmneziaWG node.</strong></p>
  <p>One Go binary · SQLite · Server-rendered UI · Docker runtime · REST API</p>
  <p><strong>English</strong> · <a href="README.fa.md">فارسی</a></p>
  <p>
    <a href="https://github.com/Sir-Adnan/wg-guard/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/Sir-Adnan/wg-guard"></a>
    <img alt="Ubuntu 24.04" src="https://img.shields.io/badge/Ubuntu-24.04-E95420?logo=ubuntu&logoColor=white">
    <img alt="amd64" src="https://img.shields.io/badge/amd64-verified-0891b2">
    <a href="LICENSE"><img alt="MIT" src="https://img.shields.io/badge/license-MIT-16a34a"></a>
  </p>
</div>

WG-Guard runs a VPN node from one responsive, bilingual panel: interfaces, users, devices,
delivery, routing, backups and updates. It stays small: Go, SQLite, server-rendered HTML and
HTMX. The VPN data plane stays on the host; the panel only organizes it.

> [!NOTE]
> **New in v0.2.0:** egress routing through WARP or imported WireGuard/AmneziaWG exits with
> balancers and rules, a site test for every exit, an opt-in IPv6 site bridge, a new design
> system with 29 themes, and four subscription page designs. See the [changelog](CHANGELOG.md).

## Features

| | |
|---|---|
| 🛡️ **AmneziaWG control** | Interfaces with ordered IPv4 pools, generated anti-DPI profiles, peers and guarded host reconciliation |
| 🧭 **Egress routing** | Outbounds through WARP, imported WireGuard/AmneziaWG or a source IP; balancers; ordered rules with service presets and geodata; a site test per exit |
| 👥 **Subscribers** | Templates, quotas and top-ups, expiry, first-use activation, devices, speed limits, usage reset and a queued next plan |
| 📱 **Delivery** | Per-device configs and QR codes, bulk downloads, one-tap AmneziaVPN import and a subscription page with four designs and app guides |
| 📈 **Overview** | Node and tunnel health, CPU, memory, disk, live rates, traffic history and diagnostics |
| 🔐 **Safe operations** | Owner and reseller permissions, scoped API tokens, signed webhooks, encrypted backups, updates and rollback |
| 🌐 **Polished panel** | English and Persian, RTL and LTR, light and dark, 29 themes, desktop and mobile |

## Quick start

On a fresh **Ubuntu 24.04 amd64** server with root or sudo:

```bash
bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash'
```

The guided setup installs the latest published release. It verifies the release assets, then
sets up the Docker runtime, the owner account and panel access. If setup is interrupted, run
`sudo wg-guard` to continue.

| Panel access | Choose it when |
|---|---|
| **Private SSH tunnel** | No domain yet; the panel stays on loopback |
| **Managed HTTPS** | You have a domain or a public IP certificate; WG-Guard runs TLS itself |
| **Existing reverse proxy** | You already run Nginx or another HTTPS proxy |

<details>
<summary><strong>Other ways to install</strong></summary>

Exact published release (replace `vX.Y.Z`):

```bash
bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash -s -- --release vX.Y.Z'
```

Development build from `main` (not a production claim):

```bash
bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash -s -- --commit main'
```

Read the script before running it:

```bash
curl -fsSLo wg-guard-install.sh https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh
less wg-guard-install.sh && bash wg-guard-install.sh
```

Unattended setup, exact commits and integrity details are in the
[installation guide](docs/operations/github-install.md).

</details>

<details>
<summary><strong>Upgrading a node installed before v0.1.10</strong></summary>

v0.1.10 moved to a Docker-only runtime and a new host layout, and older native deployments are
not converted in place. Export and verify an off-host backup with the original manager, then
install fresh and restore it. See [migration preparation](docs/operations/migration-preparation.md).

</details>

## First steps

1. Check the dashboard, or run `sudo wg-guard doctor`.
2. Create an interface with a plain or AmneziaWG profile.
3. Add a user, then share a device's QR code, config or subscription link.
4. Optional: under **Routing**, add a WARP outbound and send chosen services through it.

## Operate

```bash
sudo wg-guard             # management menu
sudo wg-guard status      # service and deployment
sudo wg-guard doctor      # host and tunnel diagnostics
sudo wg-guard logs        # recent operational logs
sudo wg-guard update      # update or roll back panel and core
```

The menu also covers encrypted backups, restore, schedules, access changes and uninstall. Take a
backup before moving to a new server; see [backup and restore](docs/operations/backup-restore.md).

## Support scope

Verified on Ubuntu 24.04 LTS amd64 with the Docker runtime and both the kernel and userspace
AmneziaWG backends. Generated AmneziaWG profiles need AmneziaWG-capable clients; plain profiles
work with standard WireGuard clients. Newer Ubuntu releases, non-amd64 hosts and active
firewalld are not supported yet. Egress routing has unit and namespace tests, but its client
traffic on a real host is not yet verified. The [status matrix](docs/development/status.md) lists
every verified cell.

## Integrate

`/api/v1` is a documented REST API with scoped tokens, cursor pagination, idempotent purchases,
reseller isolation and signed webhooks. Start with the [API contract](docs/architecture/api.md)
and the [webhook contract](docs/integrations/webhooks.md).

## Documentation

[Installation](docs/operations/github-install.md) ·
[Deployment and HTTPS](docs/operations/deployment.md) ·
[Egress routing](docs/architecture/egress-routing.md) ·
[Client compatibility](docs/integrations/amneziawg.md) ·
[Security](docs/operations/security.md) ·
[All documentation](docs/README.md)

Each [release](https://github.com/Sir-Adnan/wg-guard/releases/latest) ships the Linux/amd64
binary, the runtime image, checksums, an SBOM and attestations. To check a download, run
`sha256sum --check --ignore-missing checksums.txt`.

WG-Guard is [MIT-licensed](LICENSE). Third-party components are listed in
[THIRD_PARTY.md](THIRD_PARTY.md). Contributors start with [AGENTS.md](AGENTS.md).

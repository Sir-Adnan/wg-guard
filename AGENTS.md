# Agent rules for WG-Guard

WG-Guard is a Go/SQLite/HTMX panel for a self-hosted AmneziaWG node. The living `docs/` contracts
define intended and supported behavior; code shows the implementation; checks establish what is
verified. When docs and code disagree, resolve it with evidence, not a new guess.

## The project at a glance

| Path | Holds |
|---|---|
| `cmd/wg-guard` | The single binary: panel server, terminal manager and installer entry |
| `internal/` | Domain packages (`user`, `device`, `iface`, `egress`, `firewall`, `tunnel`, `backup`, `api`, `web`, …); `internal/subprocess` is the only place that runs host commands |
| `web/` | Embedded templates, `styles/` (CSS layers), `themes/` (29 JSON presets) and static assets |
| `migrations/` | Forward-only, numbered SQL migrations, embedded and applied in a transaction |
| `scripts/`, `install.sh` | Bootstrap, asset, artifact and docs checks |
| `docs/` | Living contracts; `docs/archive/` holds completed records only |

Production is one Linux/amd64 binary in a Docker runtime on Ubuntu 24.04. The VPN data plane is
the host kernel (or the managed userspace backend), nftables and policy routing; the panel
renders state and applies it.

## Find the relevant contract

- Keep context already established in the task. For a new area, use the
  [documentation map](docs/README.md); do not read every document.
- Check [status](docs/development/status.md) before making a support or verification claim, and
  [release readiness](docs/development/release-readiness.md) for release gates or decisions.
- For boundaries, use the [architecture overview](docs/architecture/overview.md),
  [project structure](docs/architecture/project-structure.md) or the relevant product or
  operations page. Architectural choices are recorded as [ADRs](docs/decisions/); a change that
  overturns one needs a new or amended ADR.
- Before changing AmneziaWG behavior, read the [pinned upstream contract](docs/integrations/amneziawg.md)
  and its cited version and evidence. Gate unresolved behavior; never invent flags or parameters.
- Files in [`docs/archive/`](docs/archive/README.md) are historical evidence, not work orders.
  Do not replay their gates or treat their paths as current.

## Preserve product and safety boundaries

- **Bounded resources.** One WG-Guard server process, one scheduler goroutine, bounded queues,
  caches and connection budgets, cursor pagination and no busy loops. See
  [architecture §Resources](docs/architecture/overview.md#resources).
- **Dependencies.** Justify a meaningful new dependency and its maintenance cost. Production has
  no Node.js runtime: the panel stays server-rendered HTML, HTMX and minimal vanilla JavaScript,
  with prebuilt embedded assets. Do not trade away correctness, accessibility or product quality
  merely for smaller assets.
- **Security.** Follow the [security model](docs/operations/security.md) for auth, secrets,
  subprocesses, backups and host or network changes. Never log or commit credentials, tokens,
  private keys or raw configs; pass secrets through stdin or 0600 files, not argv or shell
  interpolation. Run host commands only through `internal/subprocess`.
- **Strict CSP.** No inline styles, inline scripts or inline event handlers in templates.
- **Host ownership.** WG-Guard changes only what it owns: its namespaced nftables tables, its
  policy rules and routing tables, its interfaces. Never flush a foreign table or change a global
  policy ([networking](docs/architecture/networking.md)). Egress routing stays in the kernel
  ([ADR-0016](docs/decisions/ADR-0016-kernel-egress-routing.md)); the IPv6 site bridge is its one
  bounded userspace exception ([ADR-0018](docs/decisions/ADR-0018-ipv6-site-bridge.md)).
- **Data.** Never edit an applied migration; add a new one. Changes to the schema or to
  backup/restore must keep existing archives restorable.
- **Bilingual web contract.** Keep fa/en catalog parity, RTL/LTR layout and isolated LTR technical
  values. The installer and host terminal stay English-only, including under environment or
  legacy language flags. See the [UI/UX contract](docs/product/ui-ux.md).
- **Authorization.** Keep the change within its authorized scope. Real-host mutation, destructive
  data operations and new public releases need scope-specific authorization; do not request it
  again when already granted. An ordinary local check does not require permission.

## Change and verify

- Select checks for the behavior, dependencies and risk actually affected: focused checks during
  development, the applicable gate in the [workflow](docs/development/workflow.md) for delivery,
  and the [test map](docs/development/testing.md) for specialized coverage. A full suite is
  neither the default for each edit nor forbidden when its evidence is needed.

  | Check | Command |
  |---|---|
  | Format (inspect only) | `gofmt -l .` |
  | Static analysis | `go vet ./...` |
  | Affected package | `go test ./internal/<pkg>` |
  | Full suite / with race detector | `go test ./...` / `go test -race ./...` |
  | Docs links, anchors and named paths | `python3 scripts/check-docs.py` |
  | Embedded assets / bootstrap fixtures | `bash scripts/check-assets.sh` / `bash scripts/test-bootstrap.sh` |

- Reuse a passing result only while its source, inputs, dependencies, environment and relevant
  risk still apply. Do not rerun it merely because a response, document or work stage ends.
- Update the matching living docs **in the same change** when behavior, a public contract,
  compatibility or a documented claim changes; update the API/OpenAPI only for an affected public
  contract. An internal edit that leaves those claims true needs no ceremonial doc sweep.
- Keep commits coherent and imperative. Do not create micro-commits to checkpoint work. User-facing
  changes get a `CHANGELOG.md` entry under `[Unreleased]`.

## Report honestly

- Distinguish fresh checks, reused evidence, unrun checks and blocked checks.
- Distinguish designed, implemented, unit-tested, integration-tested and real-host verified
  behavior; never infer one level from another. WSL, containers and emulated viewports do not
  verify a real host or device.
- Do not claim an untested commit is verified. CI and release acceptance are separate from
  focused local checks.

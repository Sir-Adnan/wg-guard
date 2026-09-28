# Agent rules for WG-Guard

WG-Guard is a Go/SQLite/HTMX panel for a self-hosted AmneziaWG node. The living `docs/`
contracts define intended and supported behavior. Code shows implementation; relevant checks
establish verification. Resolve a documented/code mismatch with evidence, not a new guess.

## Find the relevant contract

- Keep context already established in the task. For a new area, use the
  [documentation map](docs/README.md) to locate the relevant contract; do not read every document
  or replay completed phase evidence by default.
- Check [status](docs/development/status.md) before making support or verification claims. Use
  [release readiness](docs/development/release-readiness.md) for certification, blockers or release
  decisions, not as required reading for every edit.
- For implementation boundaries, use the relevant [architecture](docs/architecture/overview.md),
  [project structure](docs/architecture/project-structure.md), product or operations document.
  Before changing AmneziaWG behavior, inspect the [pinned upstream contract](docs/integrations/amneziawg.md)
  and its cited version/evidence; gate unresolved behavior rather than inventing flags or params.

## Preserve product and safety boundaries

- Keep the Go node resource model bounded: one WG-Guard server process, one scheduler goroutine,
  bounded queues/caches, cursor pagination and no busy loops. See
  [architecture §Resources](docs/architecture/overview.md#resources).
- Justify meaningful new dependencies and their maintenance cost. Production has no Node.js
  runtime; the panel remains server-rendered HTML, HTMX and minimal vanilla JavaScript, with
  prebuilt embedded assets. Do not trade away correctness, accessibility or product quality merely
  for smaller assets.
- Follow the [security model](docs/operations/security.md) for auth, secrets, subprocesses,
  backups and host/network changes. Never log or commit credentials, tokens, private keys or raw
  configs; pass secrets through stdin or 0600 files, not argv or shell interpolation.
- Preserve the bilingual web contract: fa/en catalog parity, RTL/LTR layout and isolated LTR
  technical values. The installer/host terminal stays English-only, including under environment
  or legacy language flags. See the
  [UI/UX contract](docs/product/ui-ux.md) for details.
- Keep the requested change within its authorized scope. Real-host mutation, destructive data
  operations and new public releases need scope-specific authorization; do not request it again
  when already granted. An ordinary local check does not require permission.

## Change, verify and report

- Select checks for the behavior, dependencies and risk actually affected. During development use
  focused checks; for delivery use the applicable gate in
  [workflow](docs/development/workflow.md). Consult the [test map](docs/development/testing.md)
  when specialized coverage is needed. A full suite is neither the default for each edit nor
  forbidden when its evidence is needed.
- Reuse a passing result only while its source, inputs, dependencies, environment and relevant
  risk remain applicable. Do not rerun it merely because a response, document or work stage ends.
  Distinguish fresh checks, reused evidence, unrun checks and blocked checks in the report.
- Update the matching living docs **in the same change** when behavior, a public contract,
  compatibility or a documented claim changes; update API/OpenAPI only for an affected public
  contract. An internal edit that leaves those claims true needs no ceremonial doc sweep.
- Keep commits coherent and imperative. Do not create micro-commits to checkpoint work; do not
  claim an untested commit is verified. CI and release acceptance remain separate from focused
  local checks. Distinguish designed, implemented, unit-tested, integration-tested and real-host
  verified behavior. Historical phase records and fixtures are evidence, not standing work orders.

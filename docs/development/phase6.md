# Phase 6 — Backup / ops (roadmap + verification record)

Scope (from [status.md](status.md) "Phases 6–8" and the approved phase plan): backup archives
(plain + optional age password), schedules, Telegram delivery, restore wizard, the full panel
settings UI, administrators / API tokens / webhooks / audit screens, `doctor`, and the
master-key rotation trigger. Plus the approved visual rebase: a shadcn-style neutral default
theme (white light mode, near-black dark mode) replacing the warm-sand palette, and a
content-alignment/centering pass for large displays.

Everything in this file follows the status rules in [status.md](status.md): a stage is marked
done only when its tests are green and the record below says exactly what was verified where.

## Stages

| # | Stage | Contents | Status |
|---|---|---|---|
| 1 | Theme rebase | shadcn-style zinc token system (white `#ffffff` light surfaces, `#09090b` dark base, ink primary, semantic status tones only), monochrome data-viz, favicon, centering/max-width pass (forms vs tables vs dashboards) | done |
| 2 | Backup engine | `internal/backup`: `.wgg` archive (manifest + db via `VACUUM INTO` + config + master key), per-file SHA-256, optional age encryption (ADR-0008), retention pruning, restore engine (verify → preflight → stage → migrate → environment report), `BackupSink` (local + telegram), schedule store (migration 0006) + due computation | done |
| 3 | Serve wiring | `backups` scheduler job (minute due-scan), automatic pre-migration backup, pending-restore staging consumed at boot (crash-safe swap + safety snapshot) | planned |
| 4 | CLI | `wg-guard backup create/list/telegram-test`, `wg-guard restore` (service-stopped guard), `wg-guard doctor [--fix]`, `wg-guard settings get/set/list` | planned |
| 5 | Rotation trigger | `iface.Service` carrier added (rotation gap found during planning), `secrets.Rotate` wired: `wg-guard secrets rotate` + panel security action, service-stopped guard | planned |
| 6 | Backups screen | `/backups`: create-now (optional password), archive list (size/created/encrypted/schedule), delete, download, restore wizard (pick → password → review report → confirm → staged-for-restart), schedule CRUD, telegram config + send-test | planned |
| 7 | Ops screens | `/admins` (roles + permission matrix, owner protection), `/tokens` (create show-once, revoke), `/webhooks` (CRUD, secret show-once/rotate, redeliver, deliveries), `/audit` (cursor page, filter, metadata detail) — all behind `requirePermission` | planned |
| 8 | Full settings | `/settings` regrouped: General, Users, Subscription, Downloads, Networking, Accounting, API, Security, Backup (secrets write-only with set/clear semantics) | planned |
| 9 | Docs | this file, status.md, backup-restore.md, runbook.md, CHANGELOG, OpenAPI (no backup endpoints per ADR-0007; contract additions only where real) | planned |
| 10 | Verification | see the record below | planned |
| 11 | Push | GitHub main | planned |

## Design decisions taken in this phase

- **Restore is stage-then-swap, never a live swap.** The panel stages the verified archive
  (`<DataDir>/restore.pending/`) and the swap happens at the next service start, before the DB
  is opened; `serve` keeps a safety snapshot of the replaced state. The CLI applies directly
  but refuses to run while the service answers on its listen address. This avoids all
  live-DB-swap hazards (open WAL handles, cache incoherence) honestly.
- **Environment review is report + endpoint override.** The engine reports hostname,
  interfaces, endpoint, TLS mode, ports, subnets and module state; the operator can override
  `node.endpoint` (and node id) before apply — written into the staged DB. Other settings are
  edited post-restore through the normal settings flow.
- **Pre-migration backups are plain archives** (settings aren't loaded yet at migrate time;
  they stay on-box, 0600, retention 5).
- **Schedules are DB rows scanned once per minute** by the scheduler (`next_run_at` index) —
  restart-safe, catch-up-safe (a missed window runs once), no cron dependency.
- **`doctor --fix` refuses to run while the service is running** (it would race the serialized
  reconciler for the AWG subprocess). Read-only doctor is safe anytime.
- **New dependency: `filippo.io/age`** — pinned by ADR-0008 (scrypt passphrase recipient,
  standard format). No other new modules.

## Verification record

_(filled in as stages land)_

Will be filled as stages land — Windows/Go 1.27 unit suite, WSL2 `-race`, real VPS
(Ubuntu 24.04, kernel module) integration + operational drills, browser QA (fa/en × light/dark
× desktop/mobile), and asset budgets.

| Item | Verified |
|---|---|
| Theme rebase visible + consistent (fa/en × light/dark, 390 px–1920 px) | browser QA |
| Backup archive round-trip (create → verify manifest/checksums → restore staged DB → migrate) | unit tests + real VPS drill |
| Age-encrypted archive round-trip (wrong password rejected, correct restores) | unit tests + real VPS drill |
| Scheduled backup fires on the due minute; retention prunes | unit tests (fake clock) + real VPS drill |
| Telegram sink | unit tests against a local HTTP stub (request shape, size warning); real Bot-API delivery **not verified** (no bot token available) |
| Restore CLI refuses while the service runs; pending-restore consumed at boot with safety snapshot | unit tests + real VPS drill |
| `doctor` check list + `--fix` repairs on a real host | real VPS |
| Rotation trigger: round-trip after rotate (configs decrypt, device keys intact, crash-window recovery) | unit tests + real VPS drill |
| Ops screens permission gating (non-owner admin cannot reach restricted screens) | unit tests |
| Asset budgets (JS/CSS) | scripts/check-assets.sh |

## Round notes

(Frozen round-1/2 Phase 5 refinement history lives in [phase5-refinement.md](phase5-refinement.md).)

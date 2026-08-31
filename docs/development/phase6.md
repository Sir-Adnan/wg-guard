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
| 3 | Serve wiring | `backups` scheduler job (minute due-scan), automatic pre-migration backup, pending-restore staging consumed at boot (crash-safe swap + safety snapshot) | done |
| 4 | CLI | `wg-guard backup create/list/telegram-test`, `wg-guard restore` (service-stopped guard), `wg-guard doctor [--fix]`, `wg-guard settings get/set/list` | done |
| 5 | Rotation trigger | `iface.Service` carrier added (rotation gap found during planning), `secrets.Rotate` wired: `wg-guard secrets rotate` + panel security action, service-stopped guard | done |
| 6 | Backups screen | `/backups`: create-now (optional password), archive list (size/created/encrypted/schedule), delete, download, restore wizard (pick → password → review report → confirm → staged-for-restart), schedule CRUD, telegram config + send-test | done |
| 7 | Ops screens | `/admins` (roles + permission matrix, owner protection), `/tokens` (create show-once, revoke), `/webhooks` (CRUD, secret show-once/rotate, redeliver, deliveries), `/audit` (cursor page, filter, metadata detail) — all behind `requirePermission` | done |
| 8 | Full settings | `/settings` regrouped: General, Users, Subscription, Downloads, Networking, Accounting, API, Security, Backup (secrets write-only with set/clear semantics) | done |
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

Windows/Go 1.27 unit suite green (0 failures, all packages); WSL2 `-race` green (36 packages,
exit 0); asset budgets after the phase: JS 26.2/30 KiB gz, CSS 10.9/25 KiB gz, fonts
99/150 KiB gz (`scripts/check-assets.sh`).

**Real-VPS drills (Ubuntu 24.04, kernel module `amneziawg` loaded, pinned tools
v3.1.20260812, disposable drill node with a real `awg0` interface and peer):**

| Drill | Result |
|---|---|
| Integration suite (`go test -tags integration ./...`) | 36 packages ok, 0 FAIL |
| Interface lifecycle on the kernel path | **two real bugs found and fixed here**: the creation spec missed the decrypted private key (empty `PrivateKey=` rejected by the pinned tooling; commit `ddf4333`), and the kernel's plain-profile dump baseline H1..H4=1,2,3,4 broke verify-after-apply (commit `523589c`). After the fixes: boot bring-up creates and verifies `awg0` (link UP, address applied), the API-created device's peer is on the interface, and the device config downloads |
| Archive create (stored password) | age-encrypted archive (`age-en` magic verified), listed, CLI + panel paths |
| Scheduled backup | due schedule inserted; the once-per-minute in-process scan fired it (last_status=ok, archive created); retention kept the newest 2 |
| Restore — service running | verified + staged (`restore.pending`), explicit "applies at next restart" |
| Restore — boot consumption | next start logged "staged restore applied at boot", staging consumed, `*.pre-restore` safety copies present, `backup.restored` audit row, config-review warning, data intact after reconcile |
| Restore — service stopped | immediate apply, safety copies, then service restart green |
| Master-key rotation | refuses while the service answers; rotated when stopped; device config downloads under the NEW key (decryption round-trip); kernel peer unchanged; no secrets in logs |
| `doctor` | read-only pass/warn/skip honest (data-dir perms + endpoint warnings on the drill node, skips for platform-specific checks); `--fix` refuses while running, applies repairs when stopped, recheck has 0 FAILs |
| Telegram sink | real Bot-API HTTP path exercised with a bogus token — clean "HTTP 401: Unauthorized" failure, no token in the error; actual delivery **not verified** (no live bot) |
| Log hygiene | 0 occurrences of the drill password/token in server logs |

**Browser QA** (in-app browser, smoke node `-backend fake`): `/backups`, `/admins`, `/tokens`,
`/webhooks`, `/audit`, `/settings` exercised fa/en × light/dark at 1280 px and 390 px; token and
webhook creation driven through the UI (show-once secret views render exactly once, listings
never leak plaintext); schedule modal kind-switching + edit prefill verified live; two further
UI defects found and fixed (hidden CSS tooltips inflated every table's scroll width; mobile
card adaptation hid row actions — commit `45d6866`). No horizontal overflow at either width.

## Round notes

(Frozen round-1/2 Phase 5 refinement history lives in [phase5-refinement.md](phase5-refinement.md).)

# WG-Guard — Architecture Proposal (final direction)

Status: **Approved.** Incorporates the approved review (v2), the final product direction
(API scope, Docker-default deployment, installer scope, `awgN` profile naming), and the final
product decisions (revision 3): **MIT license; bilingual panel (Persian default + English) with
full RTL; light/dark themes with light default; backup encryption optional via a single
configurable password**. This document is absorbed into the Phase 0 docs tree and archived.
Upstream research reference: `docs/INITIAL_DELIVERABLE.md`.

Guiding principle for this revision: professional, clean, **lightweight, and as simple as
reasonably possible** — no over-engineering, no unnecessary security or architectural complexity.

---

## 1. Confirmed decisions (carried from the v2 review)

| # | Decision | Status |
|---|---|---|
| D1 | Obfuscation profiles = tunnel interfaces (`awg0, awg1, …`) | Confirmed — one profile per interface is the only correct mapping upstream (obfuscation params live in `[Interface]`, shared by all peers). Cap 8 interfaces, lazy creation. Naming follows ecosystem convention `awgN`; ownership tracked in the DB registry + nftables comments, not the name |
| D2 | Subprocess `awg` CLI, never import amneziawg-go | Confirmed |
| D3 | Kernel module primary, userspace fallback | Confirmed |
| D4 | Namespaced nftables table `wgguard`; never touch foreign rules | Confirmed + firewall coexistence (ufw/firewalld detection) and sysctl record/restore for clean uninstall |
| D5 | `modernc.org/sqlite` (pure Go, CGO_ENABLED=0) | Confirmed |
| D6 | Go 1.22+ `net/http` ServeMux, no router framework | Confirmed |
| D7 | AES-256-GCM at-rest secrets, argon2id admins, SHA-256-hashed tokens | Confirmed + master-key rotation procedure; all standard primitives, no invented crypto |
| D8 | Protocol default = legacy 1.0 params (randomized per interface); I1–I5 opt-in; 2.0/3.x deferred behind capability gates | Confirmed |
| D9 | "Online" = handshake within configurable window (default 3 min); raw last-handshake authoritative | Confirmed |
| D10 | Server-rendered `html/template` + HTMX + vanilla ES modules (no Alpine), hand-written CSS design system, Lucide SVG sprite | Confirmed |

## 2. Architecture changes in this revision

1. **Backup/restore is out of the public REST API.** The stable `/api/v1` contract covers
   management (node, users, devices, configs, stats, plans, interfaces, settings, webhooks).
   Backup and restore are administrative features exposed through the **web panel (session
   endpoints) and CLI only**. Rationale: restore is destructive and off-box-oriented; token-scoped
   REST exposure adds attack surface and API surface for no integration value. Documented as an
   intentional scope decision.
2. **Docker is the default installation method** — polished compose deployment, one official
   multi-arch image, generated compose file, host `wg-guard` shim. Native systemd remains a fully
   supported secondary path (spec §51.11 compliance: Docker never *required*); both modes share
   identical data paths (`/etc/wg-guard`, `/var/lib/wg-guard`).
3. **Backup encryption is optional and simple.** An archive is a plain `tar.gz` (manifest,
   `VACUUM INTO` DB snapshot, boot config, wrapped master key) by default. If the administrator
   sets a single backup password (set once from installer/CLI/panel, changeable later, stored
   encrypted at rest), archives are additionally encrypted with **age**
   (age-encryption.org/v1 scrypt passphrase recipient — a standard, established format). Restore
   asks for a password only for age-encrypted archives. No custom cryptography anywhere.
4. **Installer scope finalized** (interactive TUI, safe defaults on empty input):
   panel domain/subdomain, panel port (plain or TLS), TLS port incl. non-standard
   (e.g. `sub.example.com:34562`), AWG listen port for the first interface (+ optional range),
   MTU (default 1420), VPN subnet base, client DNS, and — all optional/skippable — Telegram bot
   token, numeric chat ID, backup schedule. Everything is editable after installation from
   Settings and the CLI.
5. **Profiles (`awg0…`) fully manageable from the panel**: name, listen port, obfuscation
   preset/params, **VPN subnet/address pool (default `10.8.N.0/24`, editable with validation:
   RFC1918, no overlap with host routes or other profiles, minimum size warnings)**, MTU,
   endpoint override, enable/disable. "Changing params" is a guided **rotation** (new interface →
   migrate users → retire old), never a silent in-place edit that would strand existing clients.

## 3. Missing requirements covered (from the v2 review, unchanged in substance)

Boot-time reconciliation & drift detection (`drift_policy: report|adopt|remove`, default
`report`; `doctor --fix`); firewall coexistence (ufw/firewalld); master-key rotation/loss story;
retention pruning for operational tables (webhook deliveries, idempotency keys, traffic samples,
audit); NTP/clock-skew policy (UTC stored, doctor warns); arm64 + MTU-overhead verification
against the pinned upstream in Phase 0 (no guessing); uninstall `--dry-run` + repair;
non-interactive installer mode (`--yes` + flags/env); first-login onboarding wizard (owner
password, endpoint confirmation, first profile + first user); client configs generated on demand
from current settings (endpoint/domain changes propagate instantly); traffic samples with
hourly/daily rollups for server-rendered SVG charts; observability baseline (`/healthz`, `/readyz`,
optional hand-written `/metrics` off by default, slog JSON with request IDs).

## 4. Documentation plan

Original SPEC frozen under `docs/archive/`; living docs:

```
README.md  AGENTS.md  CHANGELOG.md  ROADMAP.md  LICENSE (Apache-2.0)  THIRD_PARTY.md
docs/
  README.md                    index + reading map
  product/requirements.md      scope, user model, lifecycle, plans
  product/ui-ux.md             design system, components, motion, a11y, budgets
  architecture/overview.md     process model, decisions, resource design
  architecture/project-structure.md   package layout, naming conventions, boundaries
  architecture/database.md     schema, migrations, allocation, retention
  architecture/api.md          REST contract, errors, idempotency, pagination, versioning
  architecture/networking.md   interfaces, nftables, sysctls, firewall coexistence, shaper
  integrations/amneziawg.md    pinned upstream facts + verification log (Phase 0 output)
  integrations/webhooks.md     event catalog, HMAC scheme, delivery semantics
  operations/deployment.md     Docker (default) + native, TLS modes, ports, matrix
  operations/backup-restore.md archive format (age), scheduling, Telegram, migration
  operations/runbook.md        install/update/rollback/uninstall/DR, doctor
  operations/security.md       threat model, secrets, hardening
  development/workflow.md      build, test, lint, CI, release
  development/testing.md       strategy, fake backend, integration tags, benchmarks
  development/status.md        PROGRESS matrix: designed/implemented/unit/integration/needs-VPS
  decisions/ADR-0001…          one per major decision, incl. deviations from the SPEC
  archive/                     frozen SPEC + prior proposals
```

Rules: no duplication between docs, each ≤ ~400 lines, AGENTS.md stays a concise pointer file.

## 5. Final architecture (summary)

### 5.1 Core

One Go binary, one process, embedded UI, SQLite (WAL), `TunnelBackend` abstraction
(`amneziawg` exec-based + `fake`), namespaced nftables table, centralized due-heap scheduler,
delta-based accounting, cursor pagination + idempotency on the API, token scopes + owner/admin
RBAC, audit log. Packages: `cmd/wg-guard`, `internal/{config,database,domain,settings,user,device,
plan,interface,admin,auth,token,tunnel/{amneziawg,fake},firewall,network,shaper,accounting,
scheduler,webhook,audit,backup,api,web,reconcile,metrics}`, `web/`, `migrations/`, `packaging/`,
`scripts/`.

### 5.2 Settings (two layers)

- **Boot config** (TOML + env, restart-required): bind host, panel HTTP/TLS ports, TLS mode +
  domain + cert paths, data dir, log level/format, exposure mode.
- **Runtime settings** (DB, typed registry, live-applied where safe): public endpoint
  (host/domain, host:port) with per-profile override; client DNS; default MTU; keepalive; online
  threshold; accounting interval; backup (schedules, retention, Telegram token/chat); webhook
  retry caps; rate limits; session TTLs; UI defaults (theme, page size); node name. Each key has
  type/validator/category/`advanced` flag/help text; changes validated + audit-logged; generic
  Settings UI with an Advanced section (collapsed, warning); `GET/PATCH /api/v1/settings`.
- **All ports remain editable after installation** (panel/TLS ports via boot config + Settings
  with restart note; AWG ports per profile, hot-applied with a brief re-listen documented).

### 5.3 TLS / exposure modes

Built-in ACME via `autocert`: HTTP-01 challenge on port 80, TLS served on the configured panel
port — any port works for serving (e.g. 443, 8443, 34562); port 80 must be reachable for
issuance/renewal (installer checks, warns, offers manual-cert fallback). Modes: ACME domain,
manual cert/key, loopback HTTP behind an external reverse proxy, dev mode (loopback-only, loud
warnings). No nginx/Caddy anywhere.

### 5.4 API

- Base `/api/v1`, `Bearer wg_…` tokens, scopes, spec-§23 error envelope with `request_id`,
  cursor pagination (≤ 500), filters/sorting, `Idempotency-Key` on create/bulk/renew/traffic.
- Surface: `/node`, `/node/health`, `/node/stats`, `/users` (+enable/disable/renew/traffic/
  bulk/bulk-action), `/users/{id}/devices`, `/devices` (+enable/disable/regenerate/config/qr),
  `/stats` (+per user/device), `/plans`, `/interfaces` (profiles), `/settings`,
  `/webhooks` (+redeliver), `/users/{id}/traffic` (series). **No backup/restore endpoints**
  (administrative UI/CLI only — decision §2.1).
- Durable webhooks: event row inserted in the same transaction as the state change; delivery
  worker picks due rows by index (attempts, exponential backoff, capped concurrency, dead-letter,
  manual redeliver); survives restarts; `X-WG-Signature: t=<ts>,v1=<hex>` HMAC with replay window.
- OpenAPI at `/openapi.json` (+ lightweight `/docs`), route-coverage test keeps it accurate.

### 5.5 Reconciliation

Boot: for every enabled profile — ensure interface exists, verify port/params vs DB (DB wins,
applied + audited), sync peer set (missing re-added; unknown per `drift_policy`, default
`report`). Steady state: the 30 s accounting dump is diffed against expected peer keys; drift
flagged. `wg-guard doctor [--fix]` covers environment, permissions, pinned upstream version,
kernel module, interfaces, peers, nft, sysctls, shaper, disk, endpoint DNS, cert expiry, DB
integrity.

## 6. Deployment strategy

**Docker = default installation method.**

- **What runs where (Docker mode):** host = kernel module (DKMS via distro packages) + Docker +
  sysctl fragment; container = `wg-guard` + `awg` tooling + nftables, `network_mode: host`,
  `CAP_NET_ADMIN`, restart unless-stopped, volumes `/etc/wg-guard` + `/var/lib/wg-guard`. Data
  plane is forwarded by the host kernel module → zero container overhead on VPN traffic. Image:
  Ubuntu 24.04 base + pinned `amneziawg-tools` from `ppa:amnezia/ppa` + nftables + ca-certs,
  multi-arch (amd64/arm64; arm64 PPA availability verified in Phase 0, else built from source).
- **Native mode** stays fully supported as a secondary path: same binary, hardened systemd unit,
  identical data paths. Installer selects Docker by default, `--native` to override.
- **Rejected alternatives (recorded):** panel container + privileged host agent (second process,
  IPC surface, zero data-plane gain); privileged container loading DKMS itself (≈ host root with
  worse isolation; module must match host kernel anyway).
- **Host `wg-guard` shim** (Docker mode) execs into the container so every CLI command is
  identical in both modes: `status, doctor, backup, restore, user …, admin reset-password,
  update, uninstall`.
- **Updates:** `wg-guard update` — pre-backup → pull new image / atomic binary replace →
  restart → health check → rollback instructions (previous image/binary + restored backup).
  Never auto-updates.
- **Uninstaller:** `--dry-run` listing every artifact (files, units, nft table, sysctls,
  packages-we-installed), preserve options for data/backups, removes only WG-Guard-owned
  resources.

## 7. Backup, restore & migration

- **Archive (`.wgg`)**: `tar.gz{manifest.json, db.sqlite (VACUUM INTO), config.toml,
  master_key.wrap}` — **plain by default** for a simple backup experience. If a backup password
  is configured (one password, set once via installer/CLI/panel, changeable later, stored
  encrypted at rest), the archive is additionally encrypted with **age** (scrypt passphrase
  recipient — standard format, standard primitives). Per-file SHA-256 in the manifest; source
  host info recorded. Restore asks for a password only for age-encrypted archives.
- **Sources:** manual (UI + CLI), scheduled (DB schedules: daily@HH:MM / every-N-hours /
  weekly-day@time; stored UTC, displayed local; run in-process, no cron), automatic pre-upgrade /
  pre-migration. **Retention:** keep-N per schedule (default 14).
- **Delivery sinks:** local dir (default `/var/lib/wg-guard/backups`, 0600) and **Telegram**
  (bot token + numeric chat ID, stored encrypted at rest; `sendDocument`, 50 MB guard with
  warning). `BackupSink` interface leaves room for future sinks.
- **Installer (optional, skippable):** Telegram bot token, numeric chat ID, backup schedule, and
  an optional backup password — all editable later from CLI and Settings.
- **Restore (UI wizard + CLI, same engine):** decrypt+verify (identity or passphrase) → preflight
  (schema/version compat, disk space, refuse over existing data without confirmation) → stage DB
  → migrate forward → **environment review** (hostname, interfaces, endpoint/domain, TLS, ports,
  subnet collisions, kernel module — each with a suggested safe value) → apply → reconcile →
  rebuild nft/shaper → doctor → summary.
- **Server migration** = fresh install + restore on the new node; because client configs are
  generated on demand from current settings, confirming the endpoint during review is sufficient
  (hostname-based endpoints need no client-side change at all). Master-key-loss degraded case
  documented honestly (public keys survive; configs not re-downloadable).
- **Security:** archives 0600, secrets never logged, restore audit-logged, Telegram credentials
  encrypted at rest, all backup operations require admin session (UI) or root CLI — no public
  API surface.

## 8. Resource-efficiency strategy (measured, not guessed)

| Metric | Budget |
|---|---|
| Steady RSS @ 100 devices / 3 interfaces | ≤ 50 MB |
| Steady RSS @ 1000 devices | ≤ 80 MB |
| Idle CPU (1 vCPU, incl. accounting cycle) | ≤ 0.5 % average |
| Startup to healthy | < 1 s (excl. interface bring-up) |
| Accounting cycle cost (dump + delta + 1 txn) | ≤ 15 ms typical |
| Binary size (CGO_ENABLED=0) | ≤ 30 MB |
| Frontend payload per page | JS ≤ 30 KB gz, CSS ≤ 25 KB gz, fonts ≤ 60 KB |

Levers: single scheduler goroutine (due-heap, no busy loops, no per-user timers); one
`awg show dump` per interface per 30 s; one SQLite txn per cycle; capped SQLite page cache;
bounded queues/caches; cursor pagination; dashboard auto-refresh 30 s and paused on hidden tabs
(Page Visibility); rollup pruning; data plane on the host kernel module. Measurement:
`scripts/bench-idle.sh` (RSS/CPU over 10 min at 0/100/1000 fake peers), Go benchmarks
(accounting, bulk create 100/1000, user list), recorded per release in the status doc.

## 9. UI/UX strategy

- **Identity:** restrained, product-grade. One accent (deep teal/indigo), semantic status colors
  with icon+text (never color alone), 8-px grid, fluid type scale, tabular numerals for metrics,
  self-hosted Inter (latin subset, 2 weights) with system fallback.
- **System:** CSS custom-property tokens; light (default) / dark / system themes from one
  stylesheet;
  hand-written components per spec §53.3 (buttons, inputs, tables, cards, badges, `<dialog>`
  modals, drawer, dropdown, toast, tabs, pagination, skeletons, empty states, progress, traffic
  bars, confirm patterns); Lucide SVG sprite (~10–15 KB), consistent 1.5 px stroke.
- **Interaction:** server-rendered pages + HTMX partial swaps; vanilla ES modules (~8–10 KB gz)
  for modals/dropdowns/toasts/copy/confirms/theme/form dirty-guard/mobile nav/refresh control.
  No Alpine, no SPA framework, no chart/QR/icon runtime libraries.
- **Bilingual & RTL:** Persian (**default**) and English. Server-side string tables
  (`internal/i18n`, embedded catalogs, key-parity enforced by tests); per-admin language
  preference; `<html lang dir>` set server-side. **Full RTL via CSS logical properties**
  (margin-inline-*, inset-inline-*, text-align: start) so one stylesheet serves both directions —
  `[dir="rtl"]` overrides only where unavoidable. Typography: **Vazirmatn** (OFL; covers Latin +
  Arabic scripts, so one self-hosted family for both locales) as unicode-range-split woff2
  subsets. Data (IPs, keys, counters) always rendered LTR with Latin digits and tabular numerals;
  UI labels translated. fa locale renders dates in the **Jalali (Solar Hijri) calendar** via a
  small, table-tested conversion package; en locale uses Gregorian. Installer/CLI remain English.
- **Motion:** 120–240 ms, transform/opacity only, `prefers-reduced-motion` honored, zero idle-CPU
  animation.
- **Mobile and desktop are equally first-class:** desktop gets a dense, polished admin layout
  (compact sidebar, data tables, keyboard-friendly flows); mobile gets intentionally designed
  card rows, bottom sheets, 44 px targets, one table→card breakpoint — not a shrunken desktop.
- **Accessibility:** semantic HTML, visible focus, full keyboard operability, aria only where
  needed, WCAG-AA contrast in both themes.
- **Quality gates:** every screen × state (normal/empty/loading/error/success) × breakpoint
  (360/390/768/1366/1440) reviewed per spec §53.27/§57; payload budgets enforced in CI.

## 10. Phased roadmap (final)

Each phase: design → tests → implement → verify (tests/benchmarks) → review → document → commit.
Status labels (designed / implemented / unit tested / integration tested / requires-real-VPS)
maintained in `docs/development/status.md`. Repo buildable and green at every boundary.

- **Phase 0 — Documentation & scaffold**
  Docs restructure (§4), AGENTS/README/ROADMAP/LICENSE/CHANGELOG/THIRD_PARTY, ADRs for the
  decisions in this document. Repo scaffold (go.mod, CI, lint config, Makefile). Install Go;
  WSL2 Ubuntu 24.04; install pinned AWG packages; produce `docs/integrations/amneziawg.md`
  (exact tool version, accepted key list, `awg set` runtime behavior, UAPI path, dump fixtures,
  arm64 + MTU-overhead checks, PPA arm64 availability).
  *Acceptance: docs tree complete and navigable; `go build ./...` + `go test ./...` green in CI; upstream doc complete with real fixtures.*
- **Phase 1 — Core foundation**
  Config, DB/migrations, settings registry, crypto/secret store + master-key rotation,
  admins/sessions/tokens/scopes (RBAC registry), domain services (user/device/plan/interface)
  + repositories, `TunnelBackend` + fake backend, reconcile engine (vs fake).
  *Acceptance: unit/repo/race tests green; authz matrix tested; device-limit race tested; settings validation tested.*
- **Phase 2 — AWG backend & networking**
  Exec wrapper against the pinned CLI, interface lifecycle, `syncconf` peer ops, dump parsing,
  capability detection, nftables manager, sysctls, firewall coexistence, reconcile-on-boot.
  Integration tests in WSL2.
  *Acceptance: integration suite green in WSL2; drift scenarios tested; needs-VPS items recorded.*
- **Phase 3 — Limits & accounting**
  Delta accounting + reset handling, quota/expiry enforcement, first-connection activation,
  tc shaper (deterministic render + rebuild), traffic samples + rollups.
  *Acceptance: accounting invariant tests (reset/negative/restart), scheduler catch-up tests, benchmarks recorded.*
- **Phase 4 — REST API**
  Full management surface (§5.4 — deliberately no backup endpoints), idempotency, pagination,
  error envelope, rate limits, durable webhooks, OpenAPI + route-coverage test.
  *Acceptance: API conformance suite green; webhook restart/delivery tests; OpenAPI accurate.*
- **Phase 5 — Web UI (design system + primary workflows)**
  Design system + app shell (light/dark/system), login/sessions/CSRF, onboarding wizard,
  dashboard, users (table/details/bulk create/bulk actions), devices + config/QR, plans,
  interfaces/profiles management.
  *Acceptance: screens × states × breakpoints reviewed; fa/en string-parity test green; RTL visual review; budgets enforced in CI; a11y checks pass.*
- **Phase 6 — Backup, settings UI, operations**
  Backup engine (plain archives + optional age password encryption, schedules, retention, Telegram sink), restore wizard +
  environment review, migration runbook, full Settings UI (all categories + Advanced),
  admins/tokens/webhooks/audit screens, doctor (+ `--fix`), metrics/health.
  *Acceptance: backup→restore round-trip incl. cross-version migration + env-change scenarios; Telegram delivery integration-tested; all Settings keys rendered from the registry.*
- **Phase 7 — Deployment & installer**
  Official multi-arch image, compose generation, interactive Go TUI installer (Docker default +
  native secondary; domain/panel port/TLS port/AWG ports/MTU/subnet/Telegram prompts with
  defaults; `--yes` non-interactive mode), host shim, update/rollback, uninstall (`--dry-run`).
  *Acceptance: clean-VM install → configured node → update → uninstall verified in WSL2 (Docker mode; native path smoke-tested); `sub.example.com:34562`-style TLS deployment verified; needs-VPS items recorded.*
- **Phase 8 — Hardening & release**
  Security review vs threat model, race/soak tests, performance benchmarks vs §8 budgets,
  VPS matrix (Ubuntu 22.04/24.04, Debian 12; amd64/arm64), release pipeline with checksums,
  final docs + screenshots.
  *Acceptance: budgets met and recorded; release checklist complete; VPS-matrix results documented.*

## 11. Decisions made under delegated authority

- **License: MIT** (+ THIRD_PARTY.md for embedded assets and dependencies: HTMX, Lucide,
  Vazirmatn OFL, `filippo.io/age`, `modernc.org/sqlite`; exact SPDX confirmed at pin time;
  GPLv2 AWG components executed, never vendored).
- **Panel language: Persian (default) + English**, full RTL; light theme default.
- **Defaults:** panel HTTP 8080 (TLS 443 when a domain is configured, any custom TLS port
  supported); AWG ports randomized 30000–50000 (low-port prompt available for censored networks);
  MTU 1420 (Phase 0 verifies AWG overhead before finalizing guidance); client DNS 1.1.1.1;
  VPN pool `10.8.0.0/16` carved into per-profile `10.8.N.0/24`.
- **Spec deviations:** Docker-default deployment (native kept — §51.11 honored); backups
  excluded from the public API (administrative UI/CLI only); Alpine.js dropped; endpoint names
  kept spec-§21-style for contract stability with documented additions.

**STOP — awaiting approval before starting Phase 0.**

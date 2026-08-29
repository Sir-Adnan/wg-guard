# Status

Feature-level verification matrix, maintained with every phase (AGENTS.md rule: never claim
more than this table says). Statuses: `designed` → `implemented` → `unit tested` →
`integration tested` → `production verified`; items that fundamentally need real hardware stay
marked `requires real VPS`.

## Phase 0 — Documentation & scaffold (complete, 2026-08-29)

| Item | Status |
|---|---|
| Documentation tree restructured; original spec archived | ✅ implemented |
| Repo scaffold (module, CLI skeleton, Makefile, lint config, CI) | ✅ implemented + unit tested (`go build/test` green) |
| Go toolchain (local + CI) | ✅ verified (Go 1.27 local, stable in CI) |
| Amneziawg-tools pin + parser/dump/UAPI facts | ✅ verified in WSL2 — [../integrations/amneziawg.md](../integrations/amneziawg.md) |
| Userspace daemon (v3.1.20260828) build + runtime | ✅ verified in WSL2 (TUN, setconf, dump) |
| DKMS module build | ✅ build verified; ⚠️ module load + netlink dump **requires real VPS** |
| PPA on Ubuntu 26.04 | ✅ verified with noble-suite pin (workaround documented) |

## Phases 1–8 — not started

Everything below is `designed` (architecture approved) until implemented:

- Phase 1 Core: config, DB/migrations, settings registry, crypto/secrets, authn/RBAC, domain
  services, TunnelBackend + fake, reconcile engine
- Phase 2 AWG backend & networking: exec wrapper, interface lifecycle, syncconf peers, dump
  parsing, nftables, sysctls, firewall coexistence, reconcile-on-boot
- Phase 3 Limits & accounting: delta accounting, quota/expiry, first-connection, tc shaper,
  samples/rollups
- Phase 4 REST API: full surface, idempotency, webhooks, OpenAPI
- Phase 5 Web UI: design system, shell, i18n/RTL, dashboard, users, plans, interfaces
- Phase 6 Backup/ops: archives (plain + optional password), schedules, Telegram, restore wizard,
  settings UI, doctor
- Phase 7 Deployment: image, installer (Docker default), shim, update/uninstall
- Phase 8 Hardening: security review, benchmarks vs budgets, VPS matrix

## Requires real VPS (carried forward)

- Kernel-module load + `awg show dump` format against the kernel backend (fields may
  default-fill differently than userspace)
- nftables + NAT behavior and firewall coexistence (ufw/firewalld) on a production host
- PPA on Ubuntu 22.04 / Debian 12; arm64 end-to-end
- ACME issuance on a public host; installer end-to-end on clean VPS images

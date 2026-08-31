# Roadmap

Phased implementation plan. Each phase ends with: tests green, docs synchronized, a coherent
commit, and an honest verification report (implemented / unit tested / integration tested /
requires real VPS verification). Full acceptance criteria per phase:
[docs/archive/ARCHITECTURE_V2_PROPOSAL.md §10](docs/archive/ARCHITECTURE_V2_PROPOSAL.md).

| Phase | Scope | Status |
|---|---|---|
| **0 — Documentation & scaffold** | Docs tree, ADRs, repo scaffold, CI; Go toolchain; WSL2 Ubuntu + pinned AmneziaWG packages → verified upstream facts | ✅ Complete |
| **1 — Core foundation** | Config, DB/migrations, settings registry, crypto/secret store, admins/sessions/tokens/scopes, user/device/plan/interface services, `TunnelBackend` + fake backend, reconcile engine | ✅ Complete |
| **2 — AWG backend & networking** | Exec wrapper (pinned CLI), interface lifecycle, `syncconf` peer ops, dump parsing, nftables manager, sysctls, firewall coexistence, reconcile-on-boot | ✅ Complete |
| **3 — Limits & accounting** | Delta accounting, quota/expiry enforcement, first-connection activation, tc shaper, traffic samples/rollups | ✅ Complete |
| **4 — REST API** | Full `/api/v1` management surface, idempotency, pagination, error envelope, rate limits, durable webhooks, OpenAPI + coverage test; node runtime (`serve`) + token CLI; independent up/down speed limits (egress HTB + IFB ingress) | ✅ Complete |
| **5 — Web UI (design system + primary workflows)** | Design system/shell/themes, login, onboarding, dashboard, users/devices/bulk, plans, interfaces; fa/en + RTL | ✅ Complete |
| **6 — Backup, settings UI, operations** | Backup engine (plain + optional password), schedules, Telegram sink, restore wizard + environment review, full Settings UI, admins/tokens/webhooks/audit screens, doctor | ⬜ Not started |
| **7 — Deployment & installer** | Official multi-arch image, compose, interactive TUI installer (Docker default, native secondary), host shim, update/rollback, uninstall | ⬜ Not started |
| **8 — Hardening & release** | Security review, race/soak, benchmarks vs budgets, VPS matrix (Ubuntu 22.04/24.04, Debian 12; amd64/arm64), release pipeline with checksums | ⬜ Not started |

## Verification policy

Nothing is "done" unless the corresponding entry in
[docs/development/status.md](docs/development/status.md) can honestly say how it was verified.
Kernel-module, firewall, and NAT behavior ultimately requires a real KVM VPS — WSL2 covers userspace
tunnel paths and most control-plane logic only.

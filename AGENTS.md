# AGENTS.md — operating guide for coding agents

WG-Guard: lightweight self-hosted AmneziaWG VPN node panel (Go + SQLite + HTMX). This file is a
concise operating guide; **the documents under `docs/` are authoritative** — read them instead of
guessing, and update them in the same change when behavior changes.

## Start here

1. [docs/README.md](docs/README.md) — documentation map.
2. [docs/development/status.md](docs/development/status.md) — what is designed / implemented /
   tested. Never claim more than this matrix says.
3. [docs/development/workflow.md](docs/development/workflow.md) — build/test/lint/CI rules.
4. [docs/architecture/overview.md](docs/architecture/overview.md) and
   [docs/architecture/project-structure.md](docs/architecture/project-structure.md) before
   writing any code.
5. [docs/integrations/amneziawg.md](docs/integrations/amneziawg.md) before touching anything
   AmneziaWG-related — upstream behavior is pinned there, not assumed from memory.

## Hard rules

- **Never guess upstream behavior.** If AmneziaWG behavior is uncertain, inspect the pinned
  version or mark the item unresolved. Do not invent CLI flags, config keys, or protocol params.
- **No heavy dependencies.** Justify every new Go module and frontend asset; prefer stdlib.
  No Node.js in production; frontend assets are prebuilt and committed/embedded.
- **Resource budgets are requirements.** One process, one scheduler goroutine, bounded queues,
  cursor pagination, no busy loops. See docs/architecture/overview.md §Resources.
- **Security-sensitive code** (auth, secrets, subprocess, firewall, configs): follow
  docs/operations/security.md. Never log keys, tokens, passwords, raw configs, or webhook
  secrets. Secrets via argv are forbidden where stdin/file (0600) works.
- **Bilingual UI**: all user-visible strings go through `internal/i18n` catalogs (fa + en, key
  parity tested). CSS uses logical properties (RTL-safe). Data (IPs/keys/numbers) renders LTR.
- **Two phases never mix.** Follow the phase in ROADMAP.md; a phase ends with tests green,
  docs updated, a coherent commit, and an honest verification report.
- Distinguish clearly: designed / implemented / unit tested / integration tested /
  requires real VPS verification.

## Commands

```bash
make build       # go build ./cmd/wg-guard
make test        # go test ./...
make test-race   # go test -race ./...
make fmt         # gofmt -l -w (must leave tree clean)
make vet         # go vet ./...
make lint        # fmt + vet + golangci-lint (when configured)
make bench       # go test -bench ./...
```

Linux-specific integration tests use the build tag `integration` and run inside WSL2 Ubuntu
(`wsl -d Ubuntu`) or CI. Tests must not require root or a real VPN interface except under that
tag.

## Commits

Small, coherent, imperative messages (`docs: …`, `feat(user): …`, `fix(api): …`, `build: …`).
The repository must build and pass `make test` at every commit.

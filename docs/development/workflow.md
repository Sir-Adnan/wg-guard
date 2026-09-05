# Development workflow

## Prerequisites

- Go ≥ 1.25 (the version declared in `go.mod`; module
  `github.com/Sir-Adnan/wg-guard`), `CGO_ENABLED=0`.
- Linux-specific verification runs inside WSL2 Ubuntu (`wsl -d Ubuntu`) or CI; a real KVM VPS is
  required for final kernel/firewall verification (see [status.md](status.md)).

## Commands

```bash
make build        # go build ./cmd/wg-guard
make test         # go test ./...
make test-race    # go test -race ./...
make fmt          # gofmt (must leave tree clean)
make vet          # go vet ./...
make lint         # fmt + vet (+ golangci-lint when configured)
make bench        # go test -bench=.
make tidy         # go mod tidy
```

## Conventions

- Commits: small, coherent, imperative (`feat(user): …`, `fix(api): …`, `docs: …`, `build: …`).
  The repo must build and pass `make test` at every commit.
- Every new dependency is justified in the PR/change description (binary size, transitive deps,
  maintenance); frontend assets are prebuilt and committed — Node.js is a *build-time* tool
  only, never a runtime requirement.
- Behavior changes update the matching `docs/` file in the same change (AGENTS.md rule).
- Phase workflow: design → tests → implement → verify → review → document → commit; one phase
  at a time per [ROADMAP.md](../../ROADMAP.md).

## CI (GitHub Actions)

- `gofmt` check, `go vet`, unit tests + race tests on Linux — against both the minimum
  supported toolchain (`1.25.x`) and `stable`.
- Build matrix: `CGO_ENABLED=0` for linux/amd64 and linux/arm64.
- `govulncheck` (tool version pinned in the workflow) scans all packages for reachable
  vulnerabilities in dependencies.
- Release pipeline (Phase 12): checksummed binaries + provenance notes; no signing secrets in
  the repository.
- Phase 8.1 local candidate/acquisition contract: `bash scripts/build-artifacts.sh --version
  VERSION --output NEW_DIRECTORY` builds immutable local HEAD for Linux amd64/arm64 with
  checksums; it never publishes. Linux CI runs `bash scripts/test-bootstrap.sh`. See
  [GitHub acquisition](../operations/github-install.md) for commands and verification limits.

## Frontend assets

Hand-written CSS + vanilla ES modules; HTMX and the Lucide sprite are vendored prebuilt files
under `web/static/` with licenses noted in [../../THIRD_PARTY.md](../../THIRD_PARTY.md).
Vazirmatn subsets are pre-generated (unicode-range split) and committed. Nothing is compiled at
install time on the server.

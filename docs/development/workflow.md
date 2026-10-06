# Development workflow

Use [AGENTS.md](../../AGENTS.md) for the standing rules, this page for selecting and interpreting
checks, and the [testing map](testing.md) for WG-Guard-specific test layers. The current support
boundary is in [status.md](status.md); completed phase records are evidence, not instructions to
replay their gates.

## Choose evidence for the change

1. Identify the behavior and contract affected, including callers, persisted data, rendered
   assets, host integration and failure paths. Write down the risk that the check must cover;
   file count and directory name alone do not determine scope.
2. During editing, run the smallest relevant tests or local inspection that can falsify the
   change. Expand when a failure, shared dependency, concurrency, migration, permission boundary,
   platform assumption or newly discovered regression crosses that scope. Fix and rerun affected
   checks, not an unrelated historical matrix.
3. At coherent delivery, confirm the applicable checks on the resulting revision. A docs-only
   change can use text/link/diff inspection; a code or asset change needs tests for its affected
   behavior and a build when compilation, embedding or distribution is in question. Use the full
   repository suite when the change is cross-cutting, the test boundary is unclear, or acceptance
   requires it. Do not rerun an unchanged, still-applicable result solely for a commit, a final
   response or a documentation edit.
4. Acceptance and publication have separate gates. CI exercises its configured source checks on
   PRs and `main`; release runs the exact selected source again and verifies built artifacts.
   Targeted local tests do not certify an Ubuntu host, browser matrix or public release. The
   required final gate is set by the affected product claim and the relevant acceptance record,
   not by routine implementation habits.

The [test layers and risk map](testing.md#current-test-selection) identify targeted starting
points for web/i18n, API/SQLite, AmneziaWG/network, lifecycle/security and distribution changes.
For auth, secrets, restore, firewall and update paths, include failure/rollback tests before any
real-host drill; refer to [security](../operations/security.md) and
[lifecycle recovery](../operations/lifecycle-recovery.md). Do not exercise a live host or data set
merely to add confidence to an unrelated edit. Routine local read-only checks and isolated tests
need no separate approval; real-host mutation, destructive maintenance and new publication must
stay within the owner's explicit authorization for that scope.

## Reuse and report results honestly

A result applies only to the tested source, inputs and fixtures, relevant dependencies, toolchain,
flags, environment and risk. A prose change need not invalidate an unchanged Go test; a template,
embedded asset, dependency, generated contract or security assumption might. A CI result for a
different SHA, a WSL userspace test for a real kernel, or an emulated viewport for a physical
device proves a different claim. Record which evidence was **run now**, **reused from a named
revision/environment**, **not run**, or **blocked**. Do not call reused evidence a fresh pass.

Count actual work, not command names. `make test-race` reruns the package tests under race
instrumentation: it adds concurrency evidence, but repeating `make test` on the same revision and
environment may add little functional evidence. The two CI Go versions test compatibility, so
they are not interchangeable. `make lint` already invokes `fmt` and `vet`; invoking those again
without an intervening change duplicates work. A prior main CI pass does not replace the exact
release workflow's artifact and publication gate, and the release workflow does not retroactively
certify untested host or browser states.

## Commands and their effects

Go ≥ 1.25 is declared in `go.mod`; release builds use `CGO_ENABLED=0` on Linux/amd64. `make`
targets use a POSIX shell; on Windows use equivalent Go commands or WSL as appropriate.

| Command | Actual effect | When useful |
|---|---|---|
| `go test ./internal/web` (example) | Package tests (plus its dependencies at compile time) | Focused implementation feedback; choose the affected package and add caller/contract tests |
| `make test` | `go test ./...` | Full repository functional gate when warranted |
| `make test-race` | `go test -race ./...` | Full suite under race instrumentation for concurrency/acceptance |
| `make build` | Writes `bin/wg-guard` with version metadata | Local build/distribution check; not a test suite |
| `make vet` | `go vet ./...` | Static Go analysis on affected delivery/gates |
| `make fmt` / `make lint` | `gofmt -l -w .` / formatting **and** vet; both can rewrite Go files | Formatting only when intended; use `gofmt -l .` to inspect without edits |
| `make bench` | `go test -bench=. ./...` (also runs ordinary tests by default) | Resource/performance work, not a routine regression check |
| `make tidy` | Rewrites module metadata | Dependency changes only; review the diff |

Linux integration tests use the `integration` build tag in WSL2 Ubuntu or CI when their
prerequisites match; some require privilege or a real interface. They do not establish real
Ubuntu VPS behavior by themselves. Browser drivers and affected-cell filters are documented in
[testing.md](testing.md#phase-10-ui-verification). The installer and recovery fixtures are separate
checks, not aliases for the Go package suite.

## CI, commits and documentation

[CI](../../.github/workflows/ci.yml) currently runs for every PR and push to `main`, including
docs-only pushes: format check, vet, asset measurement, bootstrap and synthetic-backup fixtures,
full race tests on Go 1.25.x and stable, a Linux/amd64 build, and a reachable-vulnerability scan.
There is no docs-only path filter. Do not duplicate these entire gates locally simply because CI
will run; inspect its result for the exact revision before claiming CI passed.

The [manual release workflow](../../.github/workflows/release.yml) takes an exact verified `main`
SHA and version, reruns source gates, builds/checksums the binary and bundle, checks image/binary
identity, attests the binary and SBOM, verifies downloaded draft assets, then publishes. A new
public version or official registry image needs its own owner approval. Existing v0.1.0 evidence
is in [phase12.md](phase12.md); v0.1.1 through v0.1.5 were separately authorized and published.
The owner explicitly authorized v0.1.5 on 2026-10-01; its
[exact-source release gate](https://github.com/Sir-Adnan/wg-guard/actions/runs/36782237324) passed.
That approval covered v0.1.5 only.
The owner separately authorized the v0.1.6 backup/startup correction on 2026-10-01; its
[exact-source release gate](https://github.com/Sir-Adnan/wg-guard/actions/runs/36786717685) passed
and the release is public. This does not authorize a later version or registry image.
The owner explicitly authorized v0.1.7 on 2026-10-03. Its maintenance implementation and UI
refinement passed exact main CI; the release-preparation revision must pass its own main CI
and the manual release workflow before publication. Both gates passed at
`f166360a799268a18a0449e485fbb619b9c37d27` and v0.1.7 is public. This approval covers v0.1.7 only;
later versions and official registry images still need separate owner authorization.
The owner explicitly requested v0.1.8 on 2026-10-04. Exact released source
`cf718a926f81202b7ed8060cac27bb5322dbba82` passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37157707988) and the
[manual release workflow](https://github.com/Sir-Adnan/wg-guard/actions/runs/37158268158).
All public assets were independently downloaded and verified, and v0.1.8 is Latest stable.
This authorization covers v0.1.8 only; it does not authorize a later version or registry publication.
On 2026-10-04 the owner explicitly authorized the next Phase 15 preparation release, v0.1.9,
after its source/artifact gates, then full Phase 16 on main without another release. This
authorizes that preparation publication only; no live-host rebuild or registry publication,
and no post-Phase-16 release, is included. The owner takes/verifies the off-host copy after
updating to the preparation release and before any rebuild.
For local immutable candidate
artifacts and acquisition limits, use the [GitHub installation guide](../operations/github-install.md).

On 2026-10-05 the owner conditionally requested v0.1.10 after remaining work and
acceptance gates are complete. This overrides the earlier no-further-release scope
for v0.1.10 only. Their subsequent fresh-install/restore/client report and the
remaining physical boundaries are recorded in [Phase 20](phase20.md).
Exact released source `4f78adcc4add323035e43f5404f75392938207ee` passed
[main CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37460729245) and the
[manual release workflow](https://github.com/Sir-Adnan/wg-guard/actions/runs/37461855976). All seven public
assets were independently downloaded: tag→commit, six checksums, release/runtime metadata,
SBOM root and runtime image config identity matched; binary SHA-256
`53675169480beb3ff41b7b8d43f5a76be1c9553643834e263a06e59dcf845e9b`, image
`sha256:d3645ff85c36d62efd0d99fddf4c908f4b54894798664e517b399a586476bff6`.
v0.1.10 is Latest stable (published 2026-10-06). No registry image was published.
This authorization covers v0.1.10 only; later versions and registry images need their own scope.

Keep commits coherent and imperative (`feat(user): …`, `fix(api): …`, `docs: …`, `build: …`). Do not
knowingly deliver broken code, but an extra full local build/test cycle is not required for every
checkpoint commit when applicable evidence and CI cover the final revision. When behavior,
compatibility, a documented claim or the public API changes, update the corresponding living docs
in the same change; edit OpenAPI only if its contract changes. Internal refactors and prose edits
do not require touching unrelated status, phase or release documents. Frontend assets are
prebuilt/embedded; dependency notices remain in [THIRD_PARTY.md](../../THIRD_PARTY.md).


Phase 17 source adds an unpublished runtime CI job: exact immutable-HEAD manager assets,
one embedded recipe, image/binary/engine provenance and fake-node persistence/hardening smoke.
Candidates are GitHub Actions artifacts retained for seven days, not a public release/registry.
The release workflow includes offline image metadata/archive in its checksums and attestations;
publication remains subject to the owner's current version-specific request and gates. Physical host/client acceptance is
not inferred from this container fixture.

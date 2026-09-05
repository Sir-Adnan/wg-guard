# GitHub installer and lifecycle implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Install and maintain WG-Guard from GitHub through one professional, recoverable terminal workflow.

**Architecture:** Thin bootstrap for first acquisition; a shared Go distribution package stages
immutable builds; the existing `internal/install` engine owns host mutation. The terminal layer
orchestrates existing commands/services and never duplicates database or tunnel business logic.

**Tech Stack:** Go >=1.25, stdlib, existing x/term and i18n; Bash/curl for bootstrap, SQLite,
systemd or Docker Compose, existing AWG pinned contract.

**Spec:** `docs/development/phase8.1.md`

## Global Constraints

- One Go binary and one application scheduler; no new resident daemon, cron or Node.js runtime.
- Shared lifecycle code stays in `internal/install`; archive/schedule semantics stay in `internal/backup`.
- English and Persian user-visible terminal copy uses `internal/i18n` catalogs with key parity.
- No secrets in argv, logs, state records or committed evidence; use bounded stdin/0600 files.
- Download requests and subprocesses have cancellation, time and size bounds; never disable module/checksum verification.
- Stable failure never falls back to development; development resolves `main` or a full 40-character SHA to an immutable commit.
- Candidate binary asset names are `wg-guard_linux_amd64` and `wg-guard_linux_arm64`; the release checksum asset is `checksums.txt`.
- Recommended AWG uses the versions in `docs/integrations/amneziawg.md`; no unknown arbitrary version is promoted as compatible.
- Phase 8.1 only; Phase 9 metrics/log retention and Phase 10 web redesign remain separate.
- Final public tags/releases/registry publication require explicit owner approval; commits and feature-branch push are authorized.
- Use apply_patch for local edits, keep each commit buildable with `go test ./...` passing, record actual verification levels.

## File boundaries and execution order

Distribution owns GitHub HTTP/catalog/download/build identity. Install owns prerequisites, host
deployment, AWG bundle application and transaction state. Terminal owns presentation/input only.
CLI joins these layers. Backup continues to own archive/schedule semantics. Each task may add
focused files in its named package rather than grow existing large files.

### Task 1: GitHub acquisition and candidate artifact contract (M1)

**Files:** create `internal/distribution/{catalog,download,build}.go` and focused tests;
`install.sh`; `scripts/build-artifacts.sh`; `scripts/test-bootstrap.sh`;
`docs/operations/github-install.md`. Modify version/build docs only as necessary.

**Interfaces:** consumes ordinary context + net/http client and explicit staging directory;
produces `Selection{Channel, Ref}`, `Build{Channel, Ref, Commit, Version, SHA256, BinaryPath}`,
`Client.Releases(ctx)`, `Client.Resolve(ctx, Selection)`, `Client.Acquire(ctx, Selection, dir)`.
Use constructors/options for test HTTP endpoints and build runner. Production defaults target
only `Sir-Adnan/wg-guard`; source identity is validated before becoming argv/path/ldflags.

- [x] Write table-driven tests for published stable filtering, empty releases, exact tag lookup,
  immutable commit resolution, malformed SHA/ref, bounded/error HTTP, duplicate/missing checksum,
  corrupt/truncated/oversized download and cancellation. Use httptest with independently hashed
  binary fixtures; failed acquisition must leave no runnable final artifact.

```go
// Example contract: a release request against [] must fail, never resolve main.
_, err := client.Resolve(ctx, Selection{Channel: "release"})
if err == nil { t.Fatal("empty release catalog was accepted") }
```

- [x] Run `go test ./internal/distribution` and retain meaningful failing output before implementation.
- [x] Implement a bounded release catalog, HTTPS artifact downloading into private staging,
  exact SHA-256 verification and atomic promotion. Reject unsafe/ambiguous asset names/URLs.
  Resolve source before fetching; safely extract only regular source files/directories with
  traversal/symlink/total-size bounds, build `./cmd/wg-guard` with CGO off, trimpath and version/SHA
  metadata. Never run arbitrary shell interpolation. Preserve current environment only where
  justified; no repository hooks. Source builds may use an existing compatible Go compiler.
- [x] Implement first-entry Bash bootstrap: Linux amd64/arm64 check, root/sudo handling, curl/CA/
  tar/checksum prerequisites, bounded HTTPS transfer, cleanup trap, release/latest/list/exact and
  commit/main/exact selection, safe temporary toolchain installation when needed from official
  Go download metadata with verified checksum. Do not install global Go or upgrade all packages.
  Reconnect interactive input to `/dev/tty` when appropriate; `--help`/noninteractive usage must
  work without tty and never consume piped script bytes as answers. Pass remaining arguments to
  the acquired binary's `install` command (M4 changes interactive default to `manage`).
- [x] Add local candidate builder for both asset names plus checksums, with version/commit
  identity. It does not publish a tag/release. Execute bootstrap fixture tests against fake
  curl/system utilities to prove mode dispatch, integrity refusal, input handling and cleanup;
  no source-grep tests. Integrate shell tests with Linux CI where appropriate.
- [x] Document trust boundaries, source-build cost, no-release behavior and exact commands;
  run focused Go/shell tests, build + full Go suite, self-review and commit.

### Task 2: Prerequisites, compatible core selection and network/TLS plan (M2)

**Files:** `internal/install/{plan,run,render,health}.go`; create focused `platform.go`, `core.go`
and tests; `Dockerfile` / runtime Docker build helper; integration/deployment docs.

**Interfaces:** consumes distribution.Build from Task 1; produces host capability report and
catalogued AWG bundle selection usable by installer and manager. Preserve `Install` and `Host`
where possible; additive options distinguish explicit port choices and prerequisite policy.

- [ ] Add failing tests for missing native tools, ignored SkipModule, Docker/Compose prerequisites,
  unsupported OS before writes, explicit TLS 8080 preservation, negative Telegram chat, unsafe
  domain, conflicting panel/challenge TCP ports and loopback URL/tunnel instructions.
- [ ] Implement OS/arch/init preflight with explicit package adapters. Ubuntu24 first-class;
  other hosts receive checked/manual-prerequisite paths until verified. Install native
  iproute2/nftables/AWG tools and host module; Docker dependencies on host and pinned tools inside
  image. Preserve pre-existing dependencies and fail full install if managed tunnels cannot work
  unless operator explicitly chose external/manual core. Never claim automatic userspace fallback.
- [ ] Add catalog commands for installed/recommended/latest-compatible/exact bundle, with pinned
  tools/kernel metadata. Verify package availability before installing exact versions, avoid
  blind upgrades/downgrades, report loaded module/reboot state; never unload active tunnels.
  Build a local runtime image from the acquired panel binary plus pinned compatible tools and
  record its immutable Docker image identity. Existing `--image` remains explicit advanced input.
- [ ] Correct TLS/IP terminology/defaults and preflight; verify certificate issuance separately
  from liveness with bounded retries and actionable errors. Keep partial TLS readiness recoverable.
  Seed the VPN public endpoint from the domain or validated detected/operator-provided server IP,
  independently from the loopback panel address. Do not produce endpoint-less initial configs.
- [ ] Run host-seam/pure tests + full suite/build, update docs/status and commit.

### Task 3: Recoverable lifecycle transactions (M3)

**Files:** `internal/install/{run,update,uninstall,host,plan}.go`; new lock/journal/state helpers
and tests; `cmd/wg-guard/install.go`; lifecycle runbook.

**Interfaces:** consumes staged distribution.Build and M2 core/runtime image. `UpdateOptions`
gains explicit staged/local-versus-remote semantics and build identity. State remains backward
readable from schema1; journal records operation stage and previous/current artifacts.

- [ ] Write failing fault-injection tests for corrupt install-state refusal; lock contention;
  failed pull without active mutation; compose-up/native-restart failure rollback; healthy update
  then explicit rollback; state-write failure; cancellation and interrupted journal recovery.
  Also cover tampered/out-of-layout state paths and uninstall stop failure: never purge data or
  remove artifacts unless the owning service is confirmed stopped; constrain all deletion targets.
- [ ] Implement exclusive Linux lock (released on process death), atomic state/journal writes,
  preflight/staging before active mutation, pre-update backup identity, previous known-good
  artifact retention, Docker shim synchronization, recovery on all post-swap errors. If schema
  compatibility cannot be proven, require coordinated backup restore rather than start old code
  against upgraded data. Failed recovery must remain visible in state/exit status.
- [ ] Integrate source selection into install/update flags using Task 1 shared package. Resolve
  current selection explicitly; no silently stale local fallback after failed remote fetch.
  Provide a machine-readable installer/build compatibility contract and reject selected builds
  that cannot satisfy it before deployment. Bootstrap must not silently execute an older
  pre-Phase8.1 install command lacking the new prerequisite/owner/recovery guarantees.
- [ ] Run fault tests/full suite/build, synchronize operator recovery instructions and commit.

### Task 4: Terminal design system and complete management workflow (M4)

**Files:** create `internal/terminal` UI/input files and tests; refactor
`internal/install/prompt.go`; create `cmd/wg-guard/manage.go`; modify CLI dispatch/route,
`internal/i18n` en/fa catalogs and bootstrap interactive entry.
Also modify `internal/admin/admin.go` and its tests for atomic owner bootstrap, and add a
focused host CLI owner-bootstrap command used before the installer starts the service.

**Interfaces:** terminal UI takes io.Reader/io.Writer, locale and width/color options; actions
are explicit callbacks/CLI calls, no SQL/tunnel logic. Manager invokes existing CLI/services in
the correct host/container context; pass secrets via stdin, not nested argv.

- [ ] Write scripted flow tests for main menu/setup/source choices, invalid/retry/back/EOF/cancel,
  48/80/120-column wrapping and no ANSI under NO_COLOR/nonTTY/dumb, hidden secret input and locale parity.
- [ ] Implement restrained brand/color, section hierarchy, status/review cards, deterministic
  numbered navigation, progress/result/error treatment and actionable recovery hints. Avoid
  animations/busy loops/full-screen dependencies. TTY secret reading uses the actual input FD.
- [ ] Expose install/update/rollback/status/doctor/core versions/backup/restore/schedules/Telegram/
  service restart and uninstall with preservation-aware confirmation. Permit command-line flags
  for automation; interactive no-args entry only on TTY. No unexpected startup mutations.
- [ ] Source picker presents latest release, bounded release list, main development and pinned
  SHA with actual metadata and installed version. Setup groups domain/IP, TLS/panel TCP port,
  AWG UDP defaults and optional backup settings; final review displays impact without secrets.
- [ ] Provision the first owner locally through the existing admin service before opening the
  public listener. Hidden password+confirmation for interactive fresh setup; protected password
  file for noninteractive public setup; existing-owner detection skips creation without reset.
  Add concurrent single-owner regression (atomic conditional insert/transaction, no count-then-
  insert race), secret-transport and start-order tests. Keep uninstalled manual serve posture
  explicit for Phase11 review, and never let an installer-managed fresh public node be claimed
  by the first anonymous web visitor.
- [ ] Run UI/CLI tests/full suite/build, document management navigation/terminal constraints and commit.

### Task 5: Backup, Telegram, scheduling and restore safety (M5)

**Files:** `cmd/wg-guard/{backup,restore,settings,manage}.go`; `internal/backup` streaming/schedule
helpers/tests as needed; i18n catalogs; `docs/operations/backup-restore.md`.

**Interfaces:** preserve .wgg format and existing service methods; extend CLI schedule CRUD and
manual Telegram send using existing delivery engine. Existing scheduler remains sole owner.

- [ ] Reproduce missing-flag panic and unbounded stdin; add service/CLI tests for negative chat
  IDs, every-N-hours/equivalent days, enable/disable/delete/list, explicit archive send, secrets
  absent from argv/output, safe EOF and no scheduler duplication.
  Include real net/http URL-error redaction and token echoes in Telegram response descriptions.
- [ ] Implement coherent backup CLI/manager forms and flags, all bounded and validated. Ensure
  schedule/settings changes are observed by running service (cache invalidation or documented
  managed restart), show timezone/retention/next run. Warn appropriately for unencrypted off-host
  backups and show delivery result without token/backup content.
- [ ] Replace archive member-sized allocations with streaming stage writes plus member/total
  decompressed-size limits and bounded small metadata/key reads. Test oversized/truncated/duplicate/
  traversal/symlink/invalid-manifest archives and no active-data mutation before validated swap.
- [ ] Verify coordinated service stop/restore/start in both deployment modes and clearly separate
  configuration review from automatic application; old data remains recoverable on failure.
  Require complete valid staged hash metadata; atomically recover database/key as a coherent
  pair on partial replacement failure. Preview/confirmation must not expose an unapproved
  restore to boot-time auto-apply if the CLI exits or the service restarts during review.
- [ ] Run real SQLite/archive/Telegram HTTP-fixture tests + full suite/build, docs and commit.

### Task 6: Real-host integration, documentation and delivery (M6)

**Files:** repeatable owned-resource harness under `docs/integrations/fixtures/`; sanitized evidence;
README, CLI/deployment/runbook/security docs, architecture map, testing/status, phase8.1 and
release-readiness; AGENTS references only if needed; CI candidate artifacts/bootstrap checks.

- [ ] Audit dedicated VPS read-only first; record kernel/OS/arch/tools/Docker/service/ports and
  preserve pre-existing node data. Use existing SSH trust; never commit credential material.
- [ ] Build exact candidate and exercise bootstrap/source identity, new install, ACME certificate,
  AWG readiness and config traffic smoke, Docker/native update/rollback/failed update recovery,
  backup/restore and timer execution. Record each actually executed matrix cell separately.
- [ ] Exercise real terminal at 48/80/120 columns and NO_COLOR/dumb in both locales. Record what
  Persian shaping cannot verify on the available client; don't infer visual bidi from text tests.
- [ ] Run build/vet/unit/race/appropriate integration/shell checks; scan repository diff and
  evidence for secrets. Finish task review and whole-branch review; fix material findings.
- [ ] Update all living docs with implemented/tested/VPS/blocked distinctions, commit and push
  coherent feature branch work, inspect GitHub CI for that revision. Preserve Phase9 design
  branch. Do not publish any final release/tag/registry artifact.
- [ ] Produce concise Persian handoff with features, exact test/host evidence, commits/CI and
  genuine limits (e.g. no public release or Telegram credentials); mark phase complete only if
  its implemented gates are actually satisfied, otherwise name the remaining gate precisely.

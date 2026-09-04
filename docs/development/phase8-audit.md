# Phase 8 project audit

Initial whole-project audit for Phase 8. This is a bounded architecture/edge review, not a
claim that every implementation file was reread. The audit follows documented component
boundaries and keeps later discoveries in the release-readiness tracker.

Audit date: 2026-09-01. Baseline revision:
789e54fac0e1f8b56393f083c2b1f5d8e2b275b7.

## Method and scope

Reviewed:

- composition, scheduler/resource model, and repository/package boundaries;
- interface/profile repository, reconciliation, real/fake tunnel backends, configuration
  rendering, dump parsing, and pinned upstream contracts;
- REST route/DTO/OpenAPI boundaries and panel/public configuration delivery;
- authentication, authorization, CSRF, request caps, cookies, security headers, secret
  handling, subprocess execution, and log/error paths;
- SQLite migrations, backup/restore staging, settings, and upgrade behavior;
- CLI, installer, Docker/native deployment guidance, CI, dependencies, tests, and living docs.

Checks included unit/race/vet/format/integration baselines, asset budgets, module verification,
reachable-vulnerability scanning, tracked secret-like filenames, risky I/O/subprocess/logging
patterns, shell syntax, local Markdown links, and upstream source inspection at the exact pins.
Matches from source scans were manually assessed; a text match alone was not recorded.

## Baseline

| Check | Environment | Result |
|---|---|---|
| Git | Windows worktree | clean branch at the revision above |
| Toolchain | Windows | Go 1.27.0 windows/amd64; module declares Go 1.25 |
| Unit tests | Windows | pass, all packages |
| Vet | Windows | pass, all packages |
| Format | Windows | clean |
| Race | WSL2 Ubuntu | pass, all packages; Go 1.26.0 linux/amd64 |
| Race | Windows | CGO is disabled; not counted as a failure or pass |
| Asset budgets | WSL2 Ubuntu | JS 26,876/30,720 B gzip; CSS 10,914/25,600 B; fonts 101,799/153,600 B |
| Module integrity | Windows | go mod verify pass |
| Reachable vulnerabilities | Windows | govulncheck v1.7.0: no reachable vulnerabilities; one required-module advisory not called |
| Linux integration tag | WSL2 Ubuntu | unprivileged tests reach nftables then fail EPERM; sudo requires interactive authentication. A privileged run remains required |
| Pinned AWG tools | WSL2 Ubuntu | amneziawg-tools v3.1.20260812; amneziawg-go present |
| Shell/deployment tools | WSL2 Ubuntu | shell syntax passes; shellcheck unavailable; Docker integration unavailable in this distro |
| Tracked secret-like artifacts | Git index | no matching env/key/PEM/database/archive artifacts |
| Remote CI | GitHub Actions | run 33469154880 succeeded for the baseline revision |

## Phase 8 findings

### P8-001 — QR raster is initialized black

Severity: critical. Owner: Phase 8. Tracker: AUD-001 / RB-001.

internal/clientconf/clientconf.go creates an image.Gray whose zeroed pixels are black, then only
sets black QR modules to black. It never paints a white background or white modules, so the
result can be uniformly black. TestQRFillsCanvas only requires dark pixels in a finder-area
sample, which an all-black image satisfies. No independent decode/content test exists at the
encoder or an HTTP surface.

Required correction: white initialization, independent decode equality, quiet-zone/module
assertions, HTTP-surface byte comparison, and real browser/client verification.

### P8-002 — H ranges are deliberately truncated by the dump parser

Severity: critical. Owner: Phase 8. Tracker: AUD-002 / RB-003.

internal/tunnel/amneziawg/dump.go strips everything after the first hyphen. The existing
TestParseDumpHeaderRangeTolerated passes only when 1234567-7654321 becomes 1234567. The upper
endpoint is lost before reconciliation. Interface/tunnel models, database columns, forms,
client rendering, and OpenAPI are scalar too.

Required correction: one typed representation through migration, storage, apply/dump, drift,
API/forms, config, backup/restore, and QR.

State 2026-09-01: in progress. Typed values and migration 0007 now preserve both endpoints
through storage, apply/dump, reconciliation, API/OpenAPI, settings, and forms with unit tests.
Backup/restore, QR, and real-host equality gates remain.

### P8-003 — Interface API runtime shape disagrees with OpenAPI

Severity: high. Owner: Phase 8. Tracker: AUD-003 / RB-002.

ifaceDTO serializes iface.Obfuscation directly. That domain struct has no JSON tags, so encoding
uses exported Go field names while OpenAPI documents lower-snake-case names. OpenAPI also omits
2.0/3.x fields that the runtime model accepts and models H as integer-only.

Required correction: explicit request/response DTO mapping plus schema/behavior parity tests.

State 2026-09-01: verified. Explicit lower-snake-case DTOs cover every supported field,
scalar/range compatibility is schema-tested, HPK is write-only with a presence indicator, and
unsupported `AdvancedSecurity` is absent.

### P8-004 — Configuration generation has multiple authorities

Severity: high. Owner: Phase 8. Tracker: AUD-004 / RB-002.

The browser generates a full profile with crypto.getRandomValues, iface.Create separately fills
only zero H values with crypto/rand, and presets.go supplies fixed values. REST preset names are
stored but do not apply a preset. This prevents one reproducible policy and lets surfaces
produce different profile shapes.

Required correction: server-side policy generation with an injectable entropy boundary,
relationship/property tests, and thin UI/API consumers.

State 2026-09-01: verified. `iface.ProfileGenerator` is the single policy authority, generation
uses injectable `crypto/rand` entropy and returns no partial result on failure, REST creation
applies rather than merely stores preset names, and the authenticated/CSRF panel endpoint only
returns server-generated form values. Static-asset regression coverage forbids browser-owned AWG
randomness; 10,000 generated randomized profiles pass the general and policy validators.

### P8-005 — Balanced/strong presets retain fixed protocol headers

Severity: high. Owner: Phase 8. Tracker: RB-002 / RB-004.

The pinned upstream README recommends H values in 5..2147483647. balanced uses 1,2,3,4 (the
protocol defaults observed on a fresh kernel interface) and strong uses another fixed shared
set. iface.randomizeHeaders leaves non-zero preset values unchanged, contradicting the living
status claim that presets no longer hard-code weak values.

Required correction: generated per-profile headers; recommended policy separated from the
broader randomized/gated policy.

State 2026-09-01: verified. Recommended profiles use safe fixed J/S product defaults with one
fresh scalar header from each of four disjoint upstream-recommended bands. Randomized profiles
use true non-overlapping H ranges and generate HPK/S1–S4/timers coherently. Unsafe flags and
client-specific I values remain off in both generated policies.

### P8-006 — H validation does not understand interval overlap

Severity: high. Owner: Phase 8. Tracker: RB-003.

ValidateObfuscation checks scalar equality only. Pinned kernel code checks closed-range overlap,
so 100-200 and 150-250 must be rejected even though their text and endpoints differ.

Required correction: closed-interval types and pairwise non-overlap validation.

### P8-007 — Interface forms silently coerce malformed numbers

Severity: high. Owner: Phase 8.

internal/web/ifaces.go discards strconv.Atoi errors. Some zero results are rejected later, but
others (for example malformed Jmin becoming zero while Jmax remains valid) can produce a valid
yet unintended profile.

Required correction: error-returning parsing for every AWG numeric/range field and handler tests
proving invalid text never mutates state.

State 2026-09-01: verified. Ordered strict parsing plus fa/en handler regressions prove exact
range persistence and no mutation after malformed numeric or overlapping interval input.

### P8-008 — API JSON decoding permits silent configuration typos

Severity: medium. Owner: Phase 8.

decodeJSON accepts unknown fields and does not require EOF after the first JSON value. An
authenticated interface writer can misspell an optional AWG field without an error, or append a
second value that is ignored. For a configuration API, silent omission is a correctness failure.

Required correction: reject unknown fields and trailing values, with compatibility tests for
documented request bodies.

State 2026-09-01: verified. The shared decoder now rejects unknown fields and any second JSON
value; range syntax failures retain the stable `PARAM_CONSTRAINT` code.

### P8-009 — AdvancedSecurity is not generally supportable at the pins

Severity: high compatibility risk. Owner: Phase 8. Tracker: RB-004.

The pinned tools parser accepts AdvancedSecurity in a peer section, but the ordinary eight-field
peer dump never emits it. The pinned userspace tools transport explicitly returns `EINVAL` when
the setting is requested. Kernel source defines the attribute and accepts its netlink shape, but
`set_peer` never consumes or stores it; the apparent successful kernel `setconf` is therefore an
unobservable no-op, not feature verification.

Correction: classify it parser-only/unobservable and unsupported. Do not model or advertise it
unless a future pinned upstream stores and exposes the value and real client traffic proves the
behavior. Recorded as AUD-016 in the release tracker and corrected in the Phase 8 contract.

### P8-010 — Canonical client serialization was structurally unsafe

Severity: high. Owner: Phase 8. Tracker: AUD-017 / RB-002.

The shared renderer wrote AWG interface-only keys after the `[Peer]` marker, used the global MTU
instead of the device's selected interface MTU, and silently omitted a malformed persisted
PersistentKeepalive. Its REST lifecycle test could also print the complete private-key-bearing
configuration on failure. These paths had no literal full-field golden or cross-surface byte
identity check.

State 2026-09-05: verified. One literal golden now fixes section placement, ordering, all
supported gated fields and ranges, per-interface MTU, spacing, and final newline. Stored DNS,
AllowedIPs, and keepalive are validated before keys are decrypted. Direct rendering plus REST,
admin, and subscription downloads are byte-identical; mismatch output is limited to lengths,
digests, and first-difference offsets.

## Findings assigned to later phases

### P11-001 — Restore can allocate multi-gigabyte archive members

Severity: high. Owner: Phase 11.

internal/backup/restore.go reads each allowlisted member into memory with a 4 GiB per-member
limit. Integrity and path safety are tested, but a locally supplied or panel-uploaded hostile
archive can exceed the product memory budget before validation.

Required correction: stream bounded members to 0600 files, enforce realistic per-member and
aggregate limits, and add resource/hostile-archive tests.

### P11-002 — CLI stdin setting input is unbounded

Severity: medium. Owner: Phase 11.

wg-guard settings set KEY -stdin uses unrestricted io.ReadAll. It is local administration
rather than a remote request, but an accidental large pipe can violate the memory contract.
Apply a small explicit limit and test the error.

### P11-003 — HSTS policy is not defined at the application boundary

Severity: medium. Owner: Phase 11.

The panel sets CSP, nosniff, frame denial, and a referrer policy, but no
Strict-Transport-Security header is emitted for direct TLS modes. Proxy mode needs an explicit
ownership statement because the proxy may own HSTS.

### P12-001 — Third-party inventory has stale lifecycle labels

Severity: low. Owner: Phase 12, with touched dependency rows corrected earlier.

THIRD_PARTY.md lists filippo.io/age as active and again as planned although backup encryption is
implemented. The final licensing audit must remove stale planned entries; Phase 8 will also add
the QR decoder's test-only notice.

## Checks with no material finding

- Panel/API request bodies are capped before idempotency reads them.
- Session cookies are HttpOnly, mode-aware Secure, and SameSite=Lax; authenticated mutations
  pass through CSRF enforcement.
- API routes have centralized auth/scope definitions and bidirectional route/OpenAPI coverage.
- Runtime subprocesses use explicit argv and bounded execution; AWG configs use 0600 temp files
  and key-bearing output is parsed rather than logged.
- Webhook work is batch/concurrency/time bounded and response bodies are not stored.
- Dynamic SQL fragments select from closed internal switches; no user-provided SQL fragments
  were found.
- Backup archive writes are 0600, member paths are not honored, checksums/container integrity
  are verified, and restore swaps retain safety copies.
- Scheduler queues/channels, SQLite pool/cache, pagination, and delivery batches are bounded.

## Continuing audit rule

This initial audit does not freeze discovery. New evidence is added here and summarized in
release-readiness.md; critical/high findings are fixed in the earliest dependency-safe phase.

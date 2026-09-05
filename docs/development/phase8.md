# Phase 8 — Audit & configuration integrity

Status: **complete**. Started 2026-08-31 and completed 2026-09-05 after RB-001 through RB-004
closed with local, integration, browser, and dedicated real-VPS evidence. Phase 9 is active.
Cross-phase blockers and findings are tracked in
[release-readiness.md](release-readiness.md).

## Objective

Establish an evidence-backed project baseline and remove the release-blocking QR/client-config
defects. The phase ends only when every supported AmneziaWG value is preserved through the full
WG-Guard pipeline and real default/randomized clients exchange traffic.

## Boundaries

Included:

- whole-project audit and triage register;
- pinned source/runtime/client capability verification;
- lossless AWG parameter models, validation, persistence, API/OpenAPI, forms, apply/dump,
  reconciliation, backup/restore, client rendering, subscription rendering, and QR;
- canonical recommended/default and randomized profile generation;
- automated QR decoding plus real browser/client/config verification;
- documentation and compatibility evidence created by this work.

Deferred:

- live monitoring and operational logs (Phase 9);
- wholesale design-system/page migration and copy audit (Phase 10), except UI fields directly
  required for correct Phase 8 behavior;
- broad OS/architecture/load/recovery certification (Phase 11);
- public artifacts and release pipeline (Phase 12).

## Stage 8.0 — Baseline audit and reproduction

- [x] Record clean Git, toolchain, test, race, vet, integration, and asset-budget baselines.
- [x] Audit backend, web, CLI, installer, networking, database, security-sensitive paths, API,
      OpenAPI, backup/restore, deployment, settings, tests, and living documentation.
- [x] Add material findings to `release-readiness.md` with severity and owning phase.
- [x] Reproduce QR failures at encoder, HTTP, browser, and public/admin surface boundaries.
- [x] Capture configurations from DB intent, runtime dump, download, QR decode, and subscription
      without persisting private material in fixtures or logs.

Exit: every material discovery is assigned; QR/config failures have isolated root causes and
failing regression tests.

## Stage 8.1 — Pinned AmneziaWG contract

- [x] Re-read the exact pinned `amneziawg-tools`, kernel, and userspace sources; update the
      accepted-key/value-format/constraint evidence.
- [x] Classify every relevant field: parser, kernel, userspace, server/client placement, client
      compatibility, defaults, clearing semantics, and gating.
- [x] Verify H1–H4 `N` and `N-M`, all u16 range fields, ranged `PersistentKeepalive`, I1–I5,
      `HeaderProtectionKey`, flags, and peer `AdvancedSecurity` semantics.
- [x] Decide and document the supported compatibility contract; never expose parser-only fields
      as generally supported.

Exit: `docs/integrations/amneziawg.md` contains reproducible evidence for every modeled field and
lists unsupported/unverified behavior explicitly.

## Stage 8.2 — Lossless model and migration

- [x] Introduce validated value types for scalar-or-range u32/u16 values.
- [x] Migrate existing scalar H1–H4 values losslessly to the new representation while retaining
      rollback-readable low-bound mirrors.
- [x] Verify backup and restore compatibility with both pre-0007 scalar databases and post-0007
      true-range databases.
- [x] Carry the types through interface/device models, repositories, tunnel contracts, fake and
      real backends, drift comparison, reconciliation, API DTOs, and OpenAPI.
- [x] Update web form parsing/rendering only where needed to expose the correct values.
- [x] Add migration, round-trip, JSON, validation, dump, render, and drift regression tests.

Exit: no supported range can be truncated, coerced, or compared lossy anywhere in the pipeline.

## Stage 8.3 — Canonical profiles and client configuration

- [x] Define verified recommended defaults separately from randomized generation policy.
- [x] Generate profiles server-side with `crypto/rand`; validate dependent fields as a coherent
      set and keep unsafe/client-specific options explicitly gated.
- [x] Seal panel profile previews to the authenticated session and require exact value/provenance
      equality before persisting a generated classification.
- [x] Render one canonical client-config byte sequence for API, admin panel, and subscription
      surfaces.
- [x] Verify private/server/preshared keys, address, DNS, MTU, endpoint, AllowedIPs, keepalive,
      every enabled obfuscation field, filename, and final newline/serialization behavior.
- [x] Synchronize REST API, OpenAPI, UI fields, and documentation for any visible model changes.

Exit: default and randomized configurations are deterministic in shape, valid by construction,
and identical across every delivery surface.

## Stage 8.4 — QR correctness

- [x] Add a decoder-based failing test using a representative full client configuration.
- [x] Correct raster colors, quiet zone, scaling, error correction, response headers, and
      oversized-input handling at the root cause rather than adding surface-specific workarounds.
- [x] Decode QR responses from REST, admin device/user flows, and public subscription flows; compare
      exact bytes to their `.conf` downloads.
- [x] Verify cache/security/filename headers, deterministic output, empty/multilingual/full/near-
      capacity payloads, oversized-config errors, and auth/cross-user isolation automatically.
- [x] Verify presentation and decodability on mobile/desktop, fa/en, and light/dark against the real
      deployment.

Exit: automated decode equality passes on all product surfaces; the actual HTTP PNGs render
correctly in the browser and their decoded bytes import into real clients without third-party QR
services. A physical optical camera was unavailable and is recorded as unperformed rather than
inferred; the implementation plan explicitly permits that honest residual.

## Stage 8.5 — Runtime and real-client verification

- [x] Create one recommended/default and one randomized interface/profile on the dedicated Ubuntu
      24.04 VPS using the pinned kernel backend.
- [x] Create test users/devices, download configs, decode QR, inspect subscriptions, and compare
      against DB and runtime state.
- [x] Import both configurations into a compatible client or isolated real AWG client endpoint;
      establish handshakes and exchange bidirectional traffic.
- [x] Repeat the parameter round trip through the userspace fallback where supported.
- [x] Record sanitized commands/results and compatibility limitations without credentials,
      private keys, raw configs, or subscription tokens.

The reproducible gate is `docs/integrations/fixtures/verify-phase8-vps.sh`. It collision-checks
and owns its isolated namespaces/resources, verifies pinned packages and revisions, compares
normalized API/database/runtime/config/decoded-QR state, tests all delivery surfaces and
bidirectional UDP/TCP traffic, scans diagnostics for the run's actual secrets, and can hold the
panel for browser QA. The exact commit-stamped run passed on the dedicated VPS; sanitized evidence
is recorded in
[`../integrations/fixtures/verify-phase8-vps-2026-09-05.txt`](../integrations/fixtures/verify-phase8-vps-2026-09-05.txt).

Exit: both profile classes establish real traffic; discrepancies are fixed and regression-tested.

## Stage 8.6 — Phase gate

- [x] Run formatting, vet, unit, race, integration, API/OpenAPI, asset, and relevant benchmark
      suites.
- [x] Review migrations, security-sensitive diffs, logs/errors, and repository hygiene.
- [x] Update status, roadmap, release tracker, AWG/API/database/testing docs, and CHANGELOG.
- [x] Commit and push coherent changes with tests green at every commit.
- [x] Publish an honest Phase 8 report: implemented, unit tested, integration tested, real-VPS
      verified, unsupported/unverified, and deferred.

Phase 8 is complete only when RB-001 through RB-004 are closed with evidence.

## Verification log

| Date | Stage | Environment | Result | Evidence |
|---|---|---|---|---|
| 2026-08-31 | Planning | Windows workspace | Phase structure approved; no implementation verification yet | `ROADMAP.md`, release-readiness tracker |
| 2026-09-01 | Detailed plan | Windows workspace | Twelve dependency-ordered tasks committed; baseline CI green | implementation plan, CI run 33469154880 |
| 2026-09-01 | 8.0 baseline | Windows + WSL2 Ubuntu | Unit/vet/format/race/assets/module/vulnerability checks green; privileged integration unavailable locally (EPERM, no non-interactive sudo) | `phase8-audit.md` |
| 2026-09-01 | 8.1 source contract | Exact pinned tools/kernel/userspace revisions + prior VPS evidence | Full field matrix frozen; H intervals and u16 ranges classified; `AdvancedSecurity` corrected to parser-only/unsupported | `../integrations/amneziawg.md`, `../integrations/fixtures/phase8-upstream-contract.txt` |
| 2026-09-01 | 8.2 core + contracts | Windows/Go unit and vet suites | Migration 0007 plus exact storage/apply/dump/drift, strict fa/en forms, range-aware keepalive, explicit lower-snake-case API DTOs, write-only HPK, strict JSON, and OpenAPI parity implemented; backup/restore and real-host equality still pending | package tests under `internal/{awgparam,database,iface,tunnel,reconcile,settings,api,web}` |
| 2026-09-01 | 8.3 profile policies | Windows/Go unit, HTTP, and asset suites | One injectable server generator now owns plain/recommended/randomized policies; 10,000 randomized profiles satisfy relationship/property checks; API preset application and conflict semantics are schema-tested; the authenticated CSRF panel endpoint only populates fields; browser generation was removed; stored HPK values are no longer rendered into edit HTML | `internal/iface/profile_test.go`, `internal/api/*_test.go`, `internal/web/ifaces_profile_test.go` |
| 2026-09-05 | 8.3 canonical delivery | Windows/Go unit + HTTP suites | A literal full-field golden fixes the one canonical byte contract; AWG fields now precede `[Peer]`, the selected interface MTU is honored, corrupt stored keepalive fails before key decryption, and direct/REST/admin/subscription downloads are byte-identical with exact secret-safe headers and filenames | `internal/clientconf/clientconf_test.go`, `internal/api/handlers_test.go`, `internal/web/config_surfaces_test.go` |
| 2026-09-05 | 8.4 automated QR | Windows/Go unit + HTTP suites; CGO-free production build | The all-black raster failed the independent decoder before the white-background fix. Empty, UTF-8, full-field, and 2.3 KB payloads now decode exactly; every HTTP QR equals its matching config and fails closed when oversized. Test decoder absent from the 26,234,880-byte production binary. | `internal/testutil/qrdecode`, `internal/clientconf/qr_test.go`, `internal/web/config_surfaces_test.go` |
| 2026-09-05 | 8.2 restore + 8.5 userspace ranges | Windows/Go unit; WSL2 Ubuntu 26.04 root integration | Real pre-0007 archives forward-migrate scalar H/legacy keepalive and post-0007 archives preserve true H/keepalive/padding ranges, rollback mirrors, settings, and foreign keys through stage/apply and boot consumption. The restore review's broken interface query was reproduced and fixed. Pinned tools v3.1.20260812 and userspace v3.1.20260828 apply/dump/reapply every supported range-bearing field exactly; full `integration` suite passed. `sudo -n` remains unavailable locally, so the successful run used `wsl -u root`. Kernel/client traffic evidence remains. | `internal/backup/awg_ranges_test.go`, `internal/serve/serve_test.go`, `internal/tunnel/amneziawg/backend_integration_test.go` |
| 2026-09-05 | 8.6 integration isolation | WSL2 Ubuntu 26.04 root, pinned userspace runtime | A fresh full-suite run reproduced the pinned space-separated `awg show interfaces` output with two concurrent fixture interfaces and exposed newline-only parsing. Whitespace parsing plus a real-shape unit regression fixed false combined-name state; focused and complete privileged integration suites pass. | `internal/tunnel/amneziawg/backend.go`, `backend_test.go`, `backend_integration_test.go` |
| 2026-09-05 | 8.3/8.6 security review | Windows unit/API suites; exact pinned source; shell syntax | Config-line values reject control injection and corrupt rows fail closed; reconcile applies/compares I1–I5 and validates stored profiles before mutation; generated panel classifications require a session-bound sealed preview and exact policy shape; S2 generation has no retry loop. The real-host harness owns cleanup targets explicitly and compares exact normalized API/DB/runtime/config/QR state. | `internal/{settings,iface,reconcile,clientconf,web}`, `internal/api/openapi.json`, `verify-phase8-vps.sh` |
| 2026-09-05 | 8.5 peer-sync integrity | Pinned kernel module on dedicated Ubuntu 24.04 VPS; Windows unit + WSL2 privileged integration | Real traffic exposed that a peers-only `syncconf` clears the interface private key. The backend now preserves the complete live interface section, fails closed on invalid state, and verifies it is byte-identical after peer replacement. Unit/integration regressions and the repeated real-host traffic run pass. | `internal/tunnel/amneziawg/backend.go`, `backend_test.go`, `accounting_integration_test.go` |
| 2026-09-05 | 8.4 browser gate | Held isolated real deployment; desktop and 390 x 844 mobile | Recommended QR rendered without clipping/overflow in fa/en, RTL/LTR, and light/dark; the larger randomized QR passed Persian mobile/light. Browser warnings/errors were zero. A physical optical camera was unavailable; actual HTTP PNGs were independently decoded and those exact bytes were imported into real clients. | `../integrations/fixtures/verify-phase8-vps-2026-09-05.txt` |
| 2026-09-05 | 8.5 exact real-host gate | Ubuntu 24.04 amd64, kernel 6.8.0-138, pinned kernel/tools/userspace | Commit `91b317d` passed normalized API/DB/runtime/config/three-surface QR equality, key-relationship checks, recommended and randomized kernel-client handshakes plus bidirectional ICMP/UDP/TCP, recommended userspace-daemon traffic, actual-secret diagnostics scanning, and owned-resource cleanup. | `../integrations/fixtures/verify-phase8-vps-2026-09-05.txt` |
| 2026-09-05 | 8.6 final gate | Windows Go 1.27 + WSL2 Ubuntu Go 1.26 + GitHub CI | Full unit and vet suites pass on Windows; full race and privileged integration-tag suites pass in WSL2; formatting, OpenAPI tests, embedded Python/shell syntax, asset budgets, module verification, amd64/arm64 Linux builds, and govulncheck are green. Current benchmarks: recommended profile 497 ns/op, randomized 3.60 µs/op, canonical config 59.4 µs/op, full QR 3.15 ms/op. | final Phase 8 commit; draft PR #1 CI |

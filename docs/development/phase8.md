# Phase 8 — Audit & configuration integrity

Status: **active**. Started 2026-08-31 after approval of the Phase 8–12 release-readiness
program. Cross-phase blockers and findings are tracked in
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
- [ ] Reproduce QR failures at encoder, HTTP, browser, and public/admin surface boundaries.
- [ ] Capture configurations from DB intent, runtime dump, download, QR decode, and subscription
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
- [ ] Verify presentation and scanning on mobile/desktop, fa/en, and light/dark against the real
      deployment.

Exit: automated decode equality passes and real camera/client scanning works on all product
surfaces without third-party QR services.

## Stage 8.5 — Runtime and real-client verification

- [ ] Create one recommended/default and one randomized interface/profile on the dedicated Ubuntu
      24.04 VPS using the pinned kernel backend.
- [ ] Create test users/devices, download configs, decode QR, inspect subscriptions, and compare
      against DB and runtime state.
- [ ] Import both configurations into a compatible client or isolated real AWG client endpoint;
      establish handshakes and exchange bidirectional traffic.
- [x] Repeat the parameter round trip through the userspace fallback where supported.
- [ ] Record sanitized commands/results and compatibility limitations without credentials,
      private keys, raw configs, or subscription tokens.

Exit: both profile classes establish real traffic; discrepancies are fixed and regression-tested.

## Stage 8.6 — Phase gate

- [ ] Run formatting, vet, unit, race, integration, API/OpenAPI, asset, and relevant benchmark
      suites.
- [ ] Review migrations, security-sensitive diffs, logs/errors, and repository hygiene.
- [ ] Update status, roadmap, release tracker, AWG/API/database/testing docs, and CHANGELOG.
- [ ] Commit and push coherent changes with tests green at every commit.
- [ ] Publish an honest Phase 8 report: implemented, unit tested, integration tested, real-VPS
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
| 2026-09-05 | 8.4 automated QR | Windows/Go unit + HTTP suites; CGO-free production build | The all-black raster failed the independent decoder before the white-background fix. Empty, UTF-8, full-field, and 2.3 KB payloads now decode exactly; every HTTP QR equals its matching config and fails closed when oversized. Test decoder absent from the 26,234,880-byte production binary. Real browser/camera verification remains. | `internal/testutil/qrdecode`, `internal/clientconf/qr_test.go`, `internal/web/config_surfaces_test.go` |
| 2026-09-05 | 8.2 restore + 8.5 userspace ranges | Windows/Go unit; WSL2 Ubuntu 26.04 root integration | Real pre-0007 archives forward-migrate scalar H/legacy keepalive and post-0007 archives preserve true H/keepalive/padding ranges, rollback mirrors, settings, and foreign keys through stage/apply and boot consumption. The restore review's broken interface query was reproduced and fixed. Pinned tools v3.1.20260812 and userspace v3.1.20260828 apply/dump/reapply every supported range-bearing field exactly; full `integration` suite passed. `sudo -n` remains unavailable locally, so the successful run used `wsl -u root`. Kernel/client traffic evidence remains. | `internal/backup/awg_ranges_test.go`, `internal/serve/serve_test.go`, `internal/tunnel/amneziawg/backend_integration_test.go` |

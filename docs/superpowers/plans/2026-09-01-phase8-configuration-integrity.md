# Phase 8 Configuration Integrity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Close Phase 8 release blockers RB-001 through RB-004 with a lossless, source-verified AmneziaWG configuration pipeline, independently decoded QR delivery, and real default/randomized tunnel traffic evidence.

**Architecture:** Introduce one small internal/awgparam value package for the scalar-or-range wire types used by the pinned tools. The database remains the source of truth; legacy scalar columns are retained for rollback compatibility while new canonical text columns preserve H ranges. Domain, tunnel, API, forms, settings, renderers, dumps, reconciliation, downloads, subscriptions, and QR all consume the same typed values. Profile generation moves to the server and uses crypto/rand; delivery surfaces continue to call the single internal/clientconf.Renderer. Parser-only or unobservable upstream features remain explicitly gated rather than being advertised as supported.

**Tech Stack:** Go 1.25+, net/http, SQLite (modernc.org/sqlite), HTMX plus vanilla JavaScript, pinned AmneziaWG tools/kernel/userspace revisions, rsc.io/qr for encoding, and github.com/makiuchi-d/gozxing only in test utilities for independent QR decoding.

**Spec:** [Phase 8](../../development/phase8.md), [approved roadmap design](../specs/2026-08-31-release-readiness-roadmap-design.md), and [release-readiness tracker](../../development/release-readiness.md).

## Global Constraints

- Follow AGENTS.md and the authoritative documents under docs/; never infer upstream behavior from memory.
- Use strict test-driven development for behavior changes: add one behavior-focused failing test, observe the intended failure, make the smallest implementation pass, then refactor.
- Preserve secrets: no credentials, private keys, raw client configs, subscription tokens, API tokens, or webhook secrets in logs, fixtures, command arguments, commits, or verification evidence.
- Keep the production runtime dependency-light. gozxing is test-only, MIT/Apache licensed, and is not linked into the wg-guard binary; record its version and purpose in THIRD_PARTY.md.
- Preserve the documented API's lower-snake-case shape. Scalar H/keepalive values remain JSON numbers for compatibility; genuine ranges are JSON strings ("N-M"). Requests accept both forms.
- Preserve upgrade and restore safety. Migration 0007 is forward-only, runs transactionally, copies every legacy scalar, and leaves rollback-readable scalar columns populated with each range's low bound.
- Treat a range as a closed interval. Enabled H1-H4 values must be non-zero and pairwise non-overlapping, not merely textually unequal.
- Do not expose AdvancedSecurity as supported unless the pinned backend can apply and observe or behaviorally verify it. Source evidence currently classifies ordinary dump observability as absent and the pinned userspace implementation as absent.
- A coherent commit must build and pass relevant tests. Do not mark real-VPS, client, browser, or camera verification complete from source inspection or unit tests.
- Phase 8 may modify the interface form only where configuration correctness requires it. The wholesale visual redesign remains Phase 10.
- Public release/tag/image publication remains prohibited without the owner's explicit approval.

---

## Task 1: Establish the Phase 8 Baseline and Audit Register

**Files:**

- Create: docs/development/phase8-audit.md
- Modify: docs/development/release-readiness.md
- Modify: docs/development/phase8.md

**Step 1: Capture a clean, reproducible baseline**

From the isolated Phase 8 worktree, record revision, branch, Go version, module graph status, and working-tree status. Run:

~~~powershell
git status --short --branch
git rev-parse HEAD
go version
go test ./...
go test -race ./...
go vet ./...
gofmt -l .
wsl -d Ubuntu -- bash scripts/check-assets.sh
~~~

Run the Linux integration suite where the pinned userspace runtime and privileges are available:

~~~bash
sudo -n env PATH="$PATH" go test -tags integration ./...
~~~

Expected: unit/race/vet/format/asset checks pass. If the integration environment is unavailable, record the exact environmental constraint as unverified; never convert a skipped run into a pass.

**Step 2: Audit bounded project surfaces**

Use architecture boundaries rather than reading every file. Inspect the service composition and public/security-sensitive edges for backend, web, CLI, installer, tunnel/networking, repositories/migrations, settings, backup/restore, API/OpenAPI, deployment scripts, CI, tests, and living documentation. Run targeted checks:

~~~powershell
go list -deps ./cmd/wg-guard | Sort-Object -Unique
go mod verify
git ls-files | rg -n "(^|/)(\.env|id_rsa|.*\.key|.*\.pem|wg-guard\.db|.*\.wgg)$"
rg -n -S "TODO|FIXME|panic\(|log\..*(key|token|password|secret|config)" cmd internal scripts web
rg -n -S "exec\.Command|RunWithInput|os\.WriteFile|MkdirTemp|Chmod" internal scripts
~~~

Manually validate matches before recording findings; source-text matches alone are not defects.

**Step 3: Reproduce known defects without fixing them**

Record concrete reproductions for:

- the all-black/unreadable QR raster passing the existing dark-pixel test;
- H 14600319-413859944 becoming only 14600319 after dump parsing;
- lower-snake-case OpenAPI versus current Go-field JSON output;
- browser random generation diverging from server creation/preset behavior;
- balanced retaining protocol headers 1,2,3,4 despite docs claiming per-profile random headers;
- overlapping but non-identical H ranges escaping current validation.

**Step 4: Triage findings**

For every material finding, record evidence, impact, severity, owner phase, and state in phase8-audit.md. Mirror only the release-relevant summary in release-readiness.md. Assign later-phase issues without implementing them now.

**Step 5: Verify and commit**

~~~powershell
git diff --check
git status --short
~~~

Commit: docs(audit): record phase 8 baseline and findings

---

## Task 2: Freeze the Pinned AmneziaWG Capability Contract

**Files:**

- Modify: docs/integrations/amneziawg.md
- Create: docs/integrations/fixtures/phase8-upstream-contract.txt
- Modify: docs/development/phase8.md
- Modify: docs/development/release-readiness.md

**Step 1: Reproduce source facts at exact pins**

Inspect, at the versions already pinned in amneziawg.md:

- tools v3.1.20260812 / ee0f0a9: src/type.[ch], config.c, show.c, showconf.c, ipc-linux.h, and the UAPI header;
- kernel v3.1.20260828 / 3c38e16: README constraints, UAPI, and netlink policy/set/get behavior;
- userspace v3.1.20260828 / b5928ef: device/noise-types.go and device/uapi.go.

Record sanitized commands, exact revisions, placement, formats, clearing semantics, backend presence, dump observability, and concise evidence excerpts in phase8-upstream-contract.txt. Do not copy long source passages.

**Step 2: Publish a field-by-field matrix**

Cover Jc/Jmin/Jmax, S1-S4, H1-H4, I1-I5, HPK, all six timer/padding fields, RandomTrailers, DisableCookies, PersistentKeepalive, and AdvancedSecurity. Columns must include parser location, server/client/peer placement, range width, kernel support, userspace support, dump observability, client parity, clearing semantics, WG-Guard support state, and required gate.

Record these established constraints precisely:

- H is a packed closed u32 range; timer and PersistentKeepalive values are packed closed u16 ranges.
- H ranges must be non-zero and pairwise non-overlapping at runtime.
- ordinary tools dump preserves H, timer, and PersistentKeepalive range text.
- ordinary peer dump does not emit AdvancedSecurity; the pinned userspace backend has no corresponding UAPI setting.
- kernel acceptance alone is not client compatibility.

**Step 3: Close only the contract milestone**

Mark Stage 8.1 complete only when every modeled field has a matrix row and every unsupported field is explicit. Keep RB-004 open until Task 11 real-client evidence.

**Step 4: Verify and commit**

~~~powershell
git diff --check
~~~

Commit: docs(awg): pin the phase 8 parameter contract

---

## Task 3: Add Lossless AWG Range Value Types

**Files:**

- Create: internal/awgparam/range.go
- Create: internal/awgparam/range_test.go

**Step 1: Write failing parser/format tests**

Use hand-derived cases for u32 and u16 scalar/range input. Cover 0, max bounds, 5-9, equal endpoints normalized to scalar text, inverted ranges, empty input, signs, extra separators, whitespace policy, and overflow. Assert exact Low, High, String, IsZero, and Overlaps behavior.

~~~powershell
go test ./internal/awgparam -run 'Test(Parse|Format|Overlap)' -count=1
~~~

Expected: fail because the package/types do not exist.

**Step 2: Implement the minimal comparable types**

Implement immutable-by-convention comparable structs:

~~~go
type U32Range struct { low, high uint32 }
type U16Range struct { low, high uint16 }
~~~

Provide constructors, strict parsers, Low, High, String, IsZero, and Overlaps. Match pinned packing semantics but reject values above the declared u16 bound even though the upstream C parser currently truncates them.

**Step 3: Write failing JSON and SQL-boundary tests**

Assert:

- scalar JSON encodes as a number and decodes from a number or numeric string;
- ranged JSON encodes/decodes as "N-M";
- floats, negatives, null for required fields, and malformed strings fail;
- database scan accepts legacy int64, canonical string/[]byte, empty/nil, and preserves ranges;
- database value is canonical text.

~~~powershell
go test ./internal/awgparam -run 'Test(JSON|Scan|Value)' -count=1
~~~

Expected: fail until marshaling and database/sql interfaces exist.

**Step 4: Implement JSON and SQL boundaries**

Add MarshalJSON, UnmarshalJSON, Scan, and driver.Valuer without reflection or dependencies. Error messages name invalid syntax but contain no secret-bearing data.

**Step 5: Verify and commit**

~~~powershell
gofmt -w internal/awgparam
go test ./internal/awgparam -count=1
go vet ./internal/awgparam
~~~

Commit: feat(awg): add lossless scalar-range value types

---

## Task 4: Migrate and Round-Trip the Canonical Interface Model

**Files:**

- Create: migrations/0007_awg_ranges.sql
- Modify: internal/database/migrate_test.go
- Modify: internal/iface/iface.go
- Modify: internal/iface/iface_test.go
- Modify: internal/reconcile/reconcile.go
- Modify: internal/reconcile/reconcile_test.go
- Modify: docs/architecture/database.md

**Step 1: Write the failing migration test**

Build a pre-0007 database by applying migrations through 0006, insert plain, scalar-obfuscated, and fully gated interface rows plus a customized legacy keepalive setting, then run DB.Migrate. Assert:

- h1_range through h4_range contain exact decimal scalar text;
- null legacy H values become empty canonical values;
- the new persistent-keepalive string setting preserves old numeric text;
- unrelated columns and foreign-key relationships survive;
- applying migrations twice is a no-op.

~~~powershell
go test ./internal/database -run TestMigration0007AWGRanges -count=1
~~~

Expected: fail because migration 0007_awg_ranges.sql is absent.

**Step 2: Add the forward-only migration**

Add canonical TEXT NOT NULL DEFAULT '' H range columns, populate them from legacy H integers, and copy network.client_keepalive_seconds to network.client_persistent_keepalive only when the new key is absent. Keep legacy H columns and the old setting row for rollback compatibility.

**Step 3: Write failing repository/model tests**

Change fixtures to use true scalar and ranged values. Assert create/get/list/update round trips exact endpoints, legacy low-bound columns stay populated, disabled profiles reject set ranges, and overlapping H intervals are rejected even when their strings differ.

~~~powershell
go test ./internal/iface -run 'Test(Interface|Obfuscation|Range)' -count=1
~~~

Expected: compile/test failure against scalar H fields and equality-only validation.

**Step 4: Carry typed values through repositories**

Replace H fields and six timer/padding strings in iface.Obfuscation with awgparam types. Update insert/select/update scans and helpers to read/write canonical columns while mirroring H low bounds into legacy columns. Replace regex range validation with type-level validation. Enforce non-zero, pairwise non-overlapping H ranges.

**Step 5: Carry the model through reconciliation storage**

Replace duplicated reconciliation row fields with the same types and assert DB intent -> reconciliation spec equality for ranges. Keep existing capability gating until Task 5 classifies each field.

**Step 6: Verify and commit**

~~~powershell
gofmt -w internal/iface internal/reconcile internal/database
go test ./internal/database ./internal/iface ./internal/reconcile -count=1
go test ./internal/backup ./internal/serve -count=1
go vet ./internal/database ./internal/iface ./internal/reconcile
~~~

Commit: feat(iface): preserve AWG ranges in storage

---

## Task 5: Make Tunnel Apply, Dump, and Drift Lossless

**Files:**

- Modify: internal/tunnel/tunnel.go
- Modify: internal/tunnel/tunnel_test.go
- Modify: internal/tunnel/amneziawg/render.go
- Modify: internal/tunnel/amneziawg/render_test.go
- Modify: internal/tunnel/amneziawg/dump.go
- Modify: internal/tunnel/amneziawg/dump_test.go
- Modify: internal/tunnel/amneziawg/backend_test.go
- Modify: internal/tunnel/amneziawg/backend_integration_test.go
- Modify: internal/tunnel/fake/fake.go
- Modify: internal/tunnel/fake/fake_test.go
- Modify: internal/reconcile/reconcile.go
- Modify: internal/reconcile/reconcile_test.go
- Modify: docs/integrations/fixtures/dump-awg0-userspace.txt only if a newly captured sanitized fixture is needed

**Step 1: Write failing dump and render tests**

Use literal 29-field and 8-field lines containing H ranges, all six u16 ranges, and PersistentKeepalive 25-35. Assert exact parsed endpoints and byte-for-byte setconf/syncconf output. Change the existing low-bound regression into a preservation regression.

~~~powershell
go test ./internal/tunnel/amneziawg -run 'Test(ParseDump|Render).*Range' -count=1
~~~

Expected: fail because H and peer keepalive upper bounds are discarded.

**Step 2: Update the tunnel contract**

Use awgparam.U32Range for H and awgparam.U16Range for timer/padding values and peer keepalive in tunnel.Obfuscation, PeerConfig, and PeerState. Keep structs comparable where drift relies on equality.

**Step 3: Parse and render canonical strings**

Remove parseHeaderField truncation. Parse exact range values and emit String() for setconf, syncconf, and peer PersistentKeepalive. Preserve kernel plain-interface normalization (1,2,3,4 -> zero profile only when Jc is zero).

**Step 4: Make drift correction exact**

Assert any changed H endpoint is legacy drift and triggers apply. Preserve unknown-peer keepalive ranges under report/adopt. For every 2.0/3.x field proven observable in Task 2, compare and correct it; retain report-only behavior only for a specifically documented unobservable/capability-gated field.

**Step 5: Verify and commit**

~~~powershell
gofmt -w internal/tunnel internal/reconcile
go test ./internal/tunnel/... ./internal/reconcile -count=1
go vet ./internal/tunnel/... ./internal/reconcile
~~~

Commit: fix(tunnel): round-trip AWG ranges without truncation

---

## Task 6: Synchronize Settings, REST API, OpenAPI, and Interface Forms

**Files:**

- Modify: internal/settings/defaults.go
- Modify: internal/settings/settings_test.go
- Modify: internal/web/settings.go
- Modify: internal/web/settings_test.go
- Modify: web/templates/page_settings.html
- Modify: internal/api/dto.go
- Modify: internal/api/handlers_misc.go
- Modify: internal/api/handlers_test.go
- Modify: internal/api/openapi.json
- Modify: internal/api/api_test.go
- Modify: internal/web/ifaces.go
- Modify: internal/web/parse.go
- Modify: internal/web/parse_test.go
- Modify: internal/web/plans_test.go
- Modify: web/templates/page_iface_form.html
- Modify: internal/i18n/catalog_en.go
- Modify: internal/i18n/catalog_fa.go
- Modify: docs/architecture/api.md

**Step 1: Write failing API contract tests**

POST/PATCH an interface using both scalar JSON numbers and range strings. Assert response keys are lower snake case; every supported AWG field is present in the DTO contract; scalar ranges remain numbers; ranged values remain strings; malformed/overlapping ranges return PARAM_CONSTRAINT; and no internal/encrypted field appears.

Add a schema test that resolves the OpenAPI Obfuscation component and checks every DTO property, its integer-or-patterned-string union, limits, descriptions, and examples.

~~~powershell
go test ./internal/api -run 'Test.*(Obfuscation|OpenAPI).*Range' -count=1
~~~

Expected: fail because runtime output uses Go field names and OpenAPI omits current fields.

**Step 2: Add explicit API request/response DTOs**

Do not serialize iface.Obfuscation directly. Define explicit obfuscation DTO/request mapping with lower-snake-case JSON tags. Map every supported field deliberately and use range compatibility JSON behavior.

**Step 3: Upgrade the persistent-keepalive setting**

Replace the panel/runtime definition with network.client_persistent_keepalive, a validated string accepting 0, N, or N-M within u16 bounds. Keep migration compatibility from Task 4. Update the settings UI input to LTR text with bilingual range guidance and update callers/tests.

**Step 4: Write failing form tests**

Submit scalar and ranged H/timer values through the real form handler; assert exact persistence and rejection of malformed or overlapping values. Verify technical values render LTR in English and Persian.

~~~powershell
go test ./internal/web -run 'Test.*(Interface|Obfuscation|Keepalive).*Range' -count=1
~~~

Expected: fail because H inputs/parsing are numeric scalars.

**Step 5: Update forms without redesigning the page**

Change H inputs to text with N / N-M guidance and strict server parsing. Keep every visible string in both catalogs with parity. Invalid numeric form values must not silently become zero.

**Step 6: Verify and commit**

~~~powershell
gofmt -w internal/settings internal/api internal/web internal/i18n
go test ./internal/settings ./internal/api ./internal/web -count=1
go vet ./internal/settings ./internal/api ./internal/web
~~~

Commit: feat(api): expose the complete lossless AWG profile

---

## Task 7: Centralize Recommended and Randomized Profile Generation

**Files:**

- Replace: internal/iface/presets.go
- Create: internal/iface/profile.go
- Create: internal/iface/profile_test.go
- Modify: internal/iface/iface.go
- Modify: internal/iface/iface_test.go
- Modify: internal/api/handlers_misc.go
- Modify: internal/api/handlers_test.go
- Modify: internal/web/ifaces.go
- Modify: internal/web/web.go
- Modify: internal/web/web_test.go
- Modify: web/templates/page_iface_form.html
- Modify: web/static/js/app.js
- Modify: internal/i18n/catalog_en.go
- Modify: internal/i18n/catalog_fa.go

**Step 1: Write failing deterministic and property tests**

Inject an io.Reader into the internal generator. With deterministic bytes, assert exact shape for:

- plain: no AWG fields;
- recommended: upstream-recommended J/S defaults, per-profile cryptographic scalar headers in 5..2147483647, no client-risk gated fields;
- randomized: relationship-aware J/S values, non-overlapping H ranges, S3/S4+HPK coupling, valid timer ranges, and unsafe/client-specific flags off unless explicitly selected.

Run at least 10,000 generated profiles and assert every one passes ValidateObfuscation, all bounds/relationships hold, and independent profiles are not identical. Derive expectations from declared policy constants rather than generator helpers.

~~~powershell
go test ./internal/iface -run 'Test(ProfileGeneration|GeneratedProfiles)' -count=1
~~~

Expected: fail because generation is split between hard-coded presets and browser JavaScript.

**Step 2: Implement unbiased server-side generation**

Use crypto/rand.Reader and crypto/rand.Int or rejection sampling; do not use modulo reduction. Generate the complete value set before validation, retry only bounded relationship collisions, and return entropy errors instead of partial profiles.

**Step 3: Make creation policy explicit**

Support preset plain|recommended|randomized on REST creation. Reject requests supplying both a generated preset and explicit custom obfuscation. Keep custom profiles available. Persist the actual policy name separately from generated values.

**Step 4: Replace browser randomization**

Add an authenticated, CSRF-protected panel endpoint that returns a server-generated recommended or randomized field set. Small vanilla JS may populate inputs, but it must not generate cryptographic/protocol values locally. Test unauthorized, CSRF failure, recommended, randomized, and entropy-error paths.

**Step 5: Verify and commit**

~~~powershell
gofmt -w internal/iface internal/api internal/web internal/i18n
go test ./internal/iface ./internal/api ./internal/web -count=1
wsl -d Ubuntu -- bash scripts/check-assets.sh
~~~

Commit: feat(iface): centralize secure AWG profile generation

---

## Task 8: Prove One Canonical Client Configuration on Every Surface

**Files:**

- Modify: internal/clientconf/clientconf.go
- Create: internal/clientconf/clientconf_test.go if existing tests do not cover full rendering
- Modify: internal/api/handlers_test.go
- Modify: internal/web/users_test.go
- Modify: internal/web/sub_test.go
- Modify: internal/web/devices.go
- Modify: internal/web/sub.go
- Modify: docs/product/requirements.md

**Step 1: Write a literal full-config golden test**

Create a representative device/interface using H ranges, all supported gated fields, a ranged PersistentKeepalive setting, IPv4/IPv6 AllowedIPs, DNS, MTU, endpoint override, PSK, and optional I values. Hand-author the exact expected configuration including section order, field placement, spacing, and final newline.

~~~powershell
go test ./internal/clientconf -run TestRenderFullAWGConfig -count=1
~~~

Expected: fail on scalar H and integer-only keepalive output.

**Step 2: Render canonical typed values**

Keep Renderer.Render as the sole source of client-config bytes. Render supported interface-side values in Interface, peer-side values in Peer, and preserve pinned client/server parity. Parse persistent keepalive once and return an explicit error if stored data is invalid.

**Step 3: Prove surface byte identity**

For the same device, request admin web download, REST download, and public subscription download. Assert status, headers, filename, no-store, exact bytes, and final newline equal the direct renderer result. Never print fixture key/config content on failure; report a digest and first differing byte offset.

**Step 4: Verify and commit**

~~~powershell
gofmt -w internal/clientconf internal/api internal/web
go test ./internal/clientconf ./internal/api ./internal/web -count=1
~~~

Commit: fix(config): unify complete client configuration delivery

---

## Task 9: Fix QR Rasterization and Decode Every Delivery Surface

**Files:**

- Modify: go.mod
- Modify: go.sum
- Modify: THIRD_PARTY.md
- Create: internal/testutil/qrdecode/qrdecode.go
- Create: internal/testutil/qrdecode/qrdecode_test.go
- Modify: internal/clientconf/qr_test.go
- Modify: internal/clientconf/clientconf.go
- Modify: internal/api/handlers_test.go
- Modify: internal/web/users_test.go
- Modify: internal/web/sub_test.go

**Step 1: Add the independent failing decode test**

Pin github.com/makiuchi-d/gozxing and build a test-only decoder helper. Encode a representative near-capacity full config with clientconf.QR, independently decode the PNG, and assert exact byte equality.

~~~powershell
go test ./internal/clientconf -run TestQRDecodesExactConfiguration -count=1
~~~

Expected: fail because image.NewGray initializes every pixel black and the current encoder never paints the white background.

**Step 2: Correct the raster at the root**

Initialize the complete canvas white, then paint black modules at integer scaling with a four-module quiet zone. Keep medium error correction unless decode/camera evidence requires a documented change. Preserve the 2,600-byte application bound and typed invalid-request error for oversized input.

**Step 3: Test raster behavior independently**

Assert:

- quiet-zone corners are white and finder/module pixels are black;
- dimensions are square and module-aligned;
- empty, multilingual UTF-8, representative full, and near-limit payloads decode exactly;
- over-limit input fails without panic;
- repeated encoding of identical text is byte-identical.

**Step 4: Decode HTTP responses**

For REST, authenticated admin/device QR, and public subscription QR endpoints, decode responses and compare them to matching conf downloads. Assert image/png, safe Content-Disposition, cache/security headers, not-found/cross-user isolation, and oversized-config errors.

**Step 5: Verify dependency and binary impact**

~~~powershell
go mod tidy
go mod verify
go test ./internal/testutil/qrdecode ./internal/clientconf ./internal/api ./internal/web -count=1
$env:CGO_ENABLED='0'
go build -trimpath -o bin/wg-guard-phase8.exe ./cmd/wg-guard
go version -m bin/wg-guard-phase8.exe
~~~

Expected: gozxing is absent from the production binary module list. Remove the temporary build artifact after recording its size.

**Step 6: Commit**

Commit: fix(qr): render and independently decode client configs

---

## Task 10: Verify Migration, Backup/Restore, and Linux Runtime Integration

**Files:**

- Modify: internal/backup/backup_test.go
- Modify: internal/serve/serve_test.go
- Modify: internal/tunnel/amneziawg/backend_integration_test.go
- Modify: docs/development/testing.md
- Modify: docs/operations/backup-restore.md
- Modify: docs/integrations/amneziawg.md
- Modify: docs/development/phase8.md

**Step 1: Write a pre-0007 archive restore regression**

Create a real archive from a database at schema 0006 containing scalar H values and a customized legacy keepalive. Stage/restore through internal/backup, reopen, migrate, and assert canonical ranges/settings plus unrelated data. Also back up a post-0007 database with true ranges and assert exact restore equality.

~~~powershell
go test ./internal/backup -run 'Test.*AWGRange.*Restore' -count=1
~~~

Expected: fail until the fixture and migration path work end to end.

**Step 2: Add Linux integration cases**

Against the pinned userspace backend, apply/dump/reapply an interface with supported ranges and a peer keepalive range. Assert exact range equality and no false drift. Explicitly skip kernel-only fields with a reason tied to the capability matrix.

**Step 3: Run local and WSL gates**

~~~powershell
go test ./...
go vet ./...
gofmt -l .
~~~

~~~bash
sudo -n env PATH="$PATH" go test -tags integration ./...
~~~

Expected: available suites pass. Record exact tool/userspace versions and any environment limitation honestly in the Phase 8 log.

**Step 4: Commit**

Commit: test(awg): cover migration restore and runtime ranges

---

## Task 11: Perform Real VPS, Browser, QR, and Client Traffic Verification

**Files:**

- Create: docs/integrations/fixtures/verify-phase8-vps.sh
- Create: docs/integrations/fixtures/verify-phase8-vps.txt
- Modify: docs/integrations/amneziawg.md
- Modify: docs/development/phase8.md
- Modify: docs/development/release-readiness.md
- Modify: docs/development/status.md

**Step 1: Write a safe, sanitized verification script**

The script must:

- require root and verify Ubuntu/kernel/tool/module revisions before mutation;
- refuse to touch interface names that already exist;
- use dedicated Phase 8 interface and network-namespace names with an exact cleanup trap;
- put no credentials or private material in command arguments or output;
- hash/redact config, QR, key, token, and subscription values;
- verify database intent, awg dump, downloaded config, decoded QR, and subscription config using digests plus structural assertions;
- leave the product node and unrelated firewall/interface state untouched.

Review resolved cleanup targets before running. Do not embed connection credentials in the script/repository.

**Step 2: Deploy the exact branch revision**

Use the documented Docker/native workflow on the dedicated Ubuntu 24.04 VPS, record commit and artifact digest, run migrations, and verify health. Do not call remote CI green until the pushed commit's checks are observed green.

**Step 3: Exercise recommended and randomized profiles through WG-Guard**

Create both policies through the actual API/panel, create isolated users/devices, and collect config/QR/subscription responses without persisting secret contents. Compare canonical digests and inspect exact runtime ranges in awg show dump.

**Step 4: Establish real isolated-client traffic**

Import each downloaded configuration into an isolated Linux network namespace using the exact pinned compatible AWG client/runtime. Establish a handshake and prove bidirectional ICMP plus a TCP or UDP payload in both directions. Verify recommended scalar-header and randomized ranged/full profiles separately. Repeat the userspace fallback subset classified supported in Task 2.

**Step 5: Verify browser presentation**

Inspect admin and public subscription QR surfaces on the real server in fa/en, RTL/LTR, light/dark, desktop, and mobile viewport. Confirm PNGs are not clipped/inverted, download and QR bytes agree, and cache/security headers remain correct. Use an independent decoder and, when an accessible camera/client exists, import by scanning; otherwise record camera scanning as unverified rather than inferring it.

**Step 6: Close blockers only from evidence**

- RB-001: independent HTTP QR decode equality and real browser presentation; camera status remains explicit.
- RB-002: both real profile classes exchange traffic.
- RB-003: DB/runtime/config/QR/backup range equality.
- RB-004: every field has a supported/gated/unsupported client/backend classification.

If a real client cannot be provisioned or a pinned capability fails semantically, leave the blocker open and report the genuine decision blocker.

**Step 7: Commit sanitized evidence**

Commit: test(vps): verify phase 8 config and QR integrity

---

## Task 12: Run the Phase Gate, Review, Synchronize Documentation, and Push

**Files:**

- Modify: ROADMAP.md
- Modify: README.md where current status is summarized
- Modify: CHANGELOG.md
- Modify: docs/development/phase8.md
- Modify: docs/development/phase8-audit.md
- Modify: docs/development/release-readiness.md
- Modify: docs/development/status.md
- Modify: docs/development/testing.md
- Modify: docs/architecture/database.md
- Modify: docs/architecture/api.md
- Modify: docs/integrations/amneziawg.md
- Modify: docs/product/requirements.md
- Modify: THIRD_PARTY.md
- Review and modify: AGENTS.md only if authoritative references/workflow changed

**Step 1: Run the complete fresh verification suite**

~~~powershell
gofmt -w .
if (gofmt -l .) { throw 'gofmt left changes' }
go test ./...
go test -race ./...
go vet ./...
go test -bench='(Config|QR|Interface)' -benchmem ./internal/clientconf ./internal/iface
go mod verify
git diff --check
wsl -d Ubuntu -- bash scripts/check-assets.sh
~~~

Run Linux integration and supported builds:

~~~bash
sudo -n env PATH="$PATH" go test -tags integration ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/wg-guard-amd64 ./cmd/wg-guard
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/wg-guard-arm64 ./cmd/wg-guard
~~~

Run OpenAPI route/schema tests and the local Markdown-link check. Inspect git status --ignored for accidental evidence, databases, configs, credentials, binaries, or archives.

**Step 2: Perform focused reviews**

Review:

- migration forward/restore/rollback implications;
- secret-bearing config/QR/key code and failure output;
- range conversions for truncation, overflow, overlap, and zero semantics;
- API DTO/OpenAPI bidirectional parity;
- generated-profile entropy, bias, bounded retries, and validation;
- QR dependency licensing and absence from the production binary;
- real-VPS cleanup and evidence sanitization;
- every Phase 8 finding/blocker state.

Use superpowers:requesting-code-review and resolve substantive findings with TDD before completion.

**Step 3: Synchronize living documentation**

Mark only evidence-supported checklist rows complete. Distinguish implemented, unit tested, integration tested, browser verified, real-VPS verified, real-client verified, unsupported, and unverified. Move Phase 8 to complete and Phase 9 to active only if RB-001 through RB-004 are closed and every Phase 8 exit criterion is met.

**Step 4: Commit and push**

Commit: docs: complete phase 8 configuration integrity

Push every coherent Phase 8 commit, observe remote CI for the pushed head, and record the result. Do not publish a tag, GitHub release, or public container image.

**Step 5: Deliver the Phase 8 completion report**

Report exact pushed revision/integration state; implemented behavior and compatibility; unit/race/vet/integration/benchmark/asset/build/browser/VPS/QR/client evidence; blocker/finding closure; unsupported/unverified behavior and residual risks; and deferred Phase 9 work.

If required evidence is missing, report Phase 8 incomplete and continue unless a genuine authority/decision blocker requires the owner.

# Testing strategy

Nothing is "complete" without tests matching its risk. The majority of tests run without root
and without a real VPN interface.

## Layers

| Layer | Scope | Environment |
|---|---|---|
| Unit | parsers, validators, crypto, scheduling logic, i18n parity (fa/en key sets), Jalali conversion | plain `go test` |
| Repository | SQLite migrations, constraints, cursor pagination, delta accounting invariants | temp-file DB, real migrations |
| Service/API | authz matrix, quotas, expiration, first-connection, device-limit races, idempotency replay, webhook signatures, error envelope | `httptest` + fake tunnel backend |
| Tunnel adapter | conf renderer + dump parser against golden fixtures captured from the pinned upstream ([../integrations/fixtures/](../integrations/fixtures/)), exec wrapper against a scripted fake `awg` | plain `go test` |
| Deployment | the whole install/update/uninstall/rollback flow against an in-memory `Host` seam (fs map + recorded commands), incl. health-checked rollback with real probe endpoints on loopback | plain `go test` (`internal/install`) |
| Integration (`integration` build tag) | real interface lifecycle, syncconf, reconcile, nftables, sysctls — userspace backend in WSL2/CI | WSL2 Ubuntu / CI runner |
| Real VPS matrix | kernel module, netlink dump format, NAT/NAT-less paths, firewall coexistence, install/update/uninstall | Ubuntu 22.04/24.04, Debian 12, amd64/arm64 (Phase 11; the 24.04/amd64 slice is Phase-7 drill-verified) |

## Invariants with dedicated tests

- Accounting: counter reset, negative-delta prevention, restart persistence, no double counting
  (delta pipeline is property-tested).
- Concurrency: duplicate IP allocation, device-limit races (run under `-race`).
- Idempotency: replayed `Idempotency-Key` returns the original response, no side effects.
- Webhooks: signature scheme, replay window, restart-safe delivery (kill mid-flight), backoff.
- Drift: unknown peer / missing interface / changed port → reconcile per policy.
- Configuration text boundaries: endpoint and I1–I5 newline/control injection is rejected before
  persistence; reconciliation refuses an invalid stored profile before backend mutation, and I1–I5
  differences participate in exact drift correction.
- Client configuration: one literal full-field golden covers exact section placement, ordering,
  range preservation, spacing, and final newline; REST/admin/subscription bytes and headers are
  compared against the direct renderer. Failures report only length/digest/offset metadata and
  never configuration contents or key material.
- QR delivery: PNGs are decoded with an independent test-only implementation and compared with
  the canonical config for direct, REST, admin, and public-subscription paths. Geometry/color,
  empty, UTF-8, full, near-capacity, oversized, and deterministic-output cases are covered.
- Upgrade/restore: real archives from schema 0006 and the current schema are staged, migrated,
  applied, reopened, and compared for exact H/keepalive ranges, rollback mirrors, settings,
  interface review data, and unrelated foreign-key relationships. A separate boot test consumes
  the pending snapshot before database open and verifies the restore audit event.
- Pinned userspace runtime: the `integration` suite applies, dumps, and reapplies all supported
  H/u16 interval fields plus peer PersistentKeepalive and requires exact equality, preventing a
  lossy conversion from appearing as perpetual drift. This is a privileged WSL2/CI test; it does
  not substitute for the real kernel/client traffic gate.
- Phase 8 real-host gate: `docs/integrations/fixtures/verify-phase8-vps.sh` creates only
  collision-checked, harness-owned namespaces/interfaces inside a mode-0700 work directory. It
  compares normalized API, database, runtime, client-config, and independently decoded QR state;
  exercises recommended/randomized kernel traffic plus recommended userspace traffic; checks all
  three delivery surfaces and secret-free diagnostics; and can hold the isolated panel for browser
  QA. Its cleanup ownership and comparison logic must itself be reviewed before each evidence run.

## Benchmarks (measured, not guessed)

Accounting cycle cost, bulk create 100/1000, user list at 1000 peers, idle RSS/CPU
(`scripts/bench-idle.sh`, 10-minute soak at 0/100/1000 fake peers). Budgets and results are
recorded in [status.md](status.md); regressions are treated as bugs.

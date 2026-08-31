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
| Real VPS matrix | kernel module, netlink dump format, NAT/NAT-less paths, firewall coexistence, install/update/uninstall | Ubuntu 22.04/24.04, Debian 12, amd64/arm64 (Phase 8; the 24.04/amd64 slice is Phase-7 drill-verified) |

## Invariants with dedicated tests

- Accounting: counter reset, negative-delta prevention, restart persistence, no double counting
  (delta pipeline is property-tested).
- Concurrency: duplicate IP allocation, device-limit races (run under `-race`).
- Idempotency: replayed `Idempotency-Key` returns the original response, no side effects.
- Webhooks: signature scheme, replay window, restart-safe delivery (kill mid-flight), backoff.
- Drift: unknown peer / missing interface / changed port → reconcile per policy.

## Benchmarks (measured, not guessed)

Accounting cycle cost, bulk create 100/1000, user list at 1000 peers, idle RSS/CPU
(`scripts/bench-idle.sh`, 10-minute soak at 0/100/1000 fake peers). Budgets and results are
recorded in [status.md](status.md); regressions are treated as bugs.

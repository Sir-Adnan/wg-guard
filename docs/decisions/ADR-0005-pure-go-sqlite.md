# ADR-0005 — Pure-Go SQLite (`modernc.org/sqlite`)

Status: accepted · Date: 2026-08-29

## Context

SQLite is the right database for a single-node panel. The two Go options are mattn/go-sqlite3
(CGO) and modernc.org/sqlite (pure Go, transpiled).

## Decision

Use `modernc.org/sqlite` with `CGO_ENABLED=0`. WAL mode, `busy_timeout=5s`, foreign keys ON,
capped page cache, short transactions.

## Consequences

- Static cross-compilation for amd64/arm64 from any host, no C toolchain for WG-Guard itself,
  smaller container build story — decisive for install simplicity (a core product goal).
- Trade-off: lower peak throughput than CGO. Irrelevant here: the control plane writes one
  small transaction per accounting cycle plus admin actions; production load is certified in Phase 11.

## Alternatives rejected

- mattn/go-sqlite3: CGO build pain and cross-compile friction for marginal throughput we cannot
  use.
- PostgreSQL/MySQL: violates the single-binary/low-footprint product constraint.

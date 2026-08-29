# ADR-0012 — stdlib `net/http` ServeMux routing

Status: accepted · Date: 2026-08-29

## Context

The spec allows "net/http or Chi". The API surface is small, static, and contract-stable.

## Decision

Use Go 1.22+ `net/http` ServeMux method+wildcard patterns. No router framework.

## Consequences

- Zero routing dependencies; patterns are declarative and greppable.
- If route count ever makes a table unwieldy, that is a code-organization problem solved with a
  tiny internal route table, not a dependency.

## Alternatives rejected

- Chi: good library, but adds a dependency for capabilities ServeMux now covers natively.

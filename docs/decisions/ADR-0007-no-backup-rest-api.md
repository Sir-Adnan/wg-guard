# ADR-0007 — Backup/restore excluded from the REST API

Status: accepted · Date: 2026-08-29

## Context

The REST API is the integration surface for external management systems (bots, billing
platforms). Backup and restore are administrative operations — restore is destructive and
archives contain every secret the node holds.

## Decision

Backup/restore is exposed through the **web panel (session-authenticated admin endpoints) and
the CLI** only. The token-authenticated REST API has no backup endpoints by design.

## Consequences

- Smaller, stabler API contract with no destructive/off-box operations behind long-lived
  tokens.
- External orchestrators that need backups use SSH + CLI (documented in the runbook), which is
  the appropriate security boundary for this operation class.

## Alternatives rejected

- Token-scoped `/api/v1/backups` + restore: adds attack surface and contract surface for no
  integration value; a leaked `backup.manage` token would exfiltrate the entire node.

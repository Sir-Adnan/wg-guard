# ADR-0014 — Secure exposure orchestration without public plaintext

Status: accepted · Date: 2026-09-09

## Context

ADR-0011 made direct in-process ACME the lightweight default, but a multi-service VPS may already
have Nginx on ports 80/443. Operators also need DNS-01, trusted HTTPS on a public IP, and a safe
way to change panel access after installation. Treating every busy port as an error makes the
safe product harder to deploy; stealing the port or offering public HTTP would make it unsafe.

## Decision

Keep the application runtime's existing `acme`, `manual`, and loopback `proxy` modes, and add an
installer-owned exposure layer:

- direct domain ACME remains the zero-extra-dependency default on a free host;
- a standard conflict-free host Nginx can be managed transactionally as the public TLS owner;
- Certbot from its explicit snap path provides shared-webroot, Cloudflare DNS-01, and short-lived
  public-IP certificates only when that strategy is selected;
- manual and Cloudflare Origin CA files remain explicit specialist paths;
- private SSH and operator-owned reverse proxy remain safe fallbacks;
- public plaintext is never a production exposure mode.

The installer owns only deterministic WG-Guard files, validates before reload, rolls back failed
changes, stores DNS credentials in a `0600` file, and records no secret material. Shared tools and
CA lineages are retained on uninstall. Nonstandard or conflicting reverse-proxy layouts are
diagnosed but never rewritten.

## Consequences

- Existing simple domain installs remain one process and retain automatic `autocert` renewal.
- Selected advanced certificate paths add an optional Certbot/snap dependency, justified by
  current IP-certificate/profile support and official DNS plugins without adding Go modules.
- IP certificates require 160-hour renewal automation and therefore carry stricter health and
  VPS gates than ordinary domain certificates.
- Nginx becomes an explicitly recorded external TLS owner while WG-Guard remains loopback-only.
- DNS-01 removes inbound validation-port dependence and supports private origins, but introduces a
  powerful DNS credential that must be narrowly scoped and protected.

## Rejected alternatives

- Public HTTP with a warning or high random port: credentials and sessions remain interceptable.
- Automatically stop or replace unknown port owners: violates coexistence and recovery safety.
- Download and install `acme.sh` at runtime: adds a mutable second installer, cron/updater, and
  credential store outside the existing lifecycle contract.
- Embed `go-acme/lego`: its provider dependency graph is disproportionate to the lightweight
  binary; invoking the optional official host tool keeps that surface isolated.
- Request wildcard certificates by default: the panel needs one exact hostname and least
  privilege is the better product default.

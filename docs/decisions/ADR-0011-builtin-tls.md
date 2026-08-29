# ADR-0011 — Built-in TLS/ACME without a reverse proxy

Status: accepted · Date: 2026-08-29

## Context

The panel and public API must use HTTPS in production without requiring a second long-running
web server. Administrators want domain-based installs on any port (e.g.
`https://sub.example.com:34562`).

## Decision

Serve TLS from the WG-Guard process: `golang.org/x/crypto/acme/autocert` for ACME
(HTTP-01 challenge on port 80; TLS served on the configured panel port — any port; port 80 must
remain reachable for issuance/renewal), manual cert/key mode, loopback HTTP behind an external
reverse proxy (explicit choice), and a loud-warnings dev mode. The installer refuses silent
public plaintext.

## Consequences

- One process, no nginx/Caddy dependency, renewal is automatic.
- Documented requirement: port 80 reachability for ACME; manual-cert fallback when it cannot be
  met.

## Alternatives rejected

- Bundled Caddy/nginx sidecar: a second resident service (RAM, updates, config drift) for
  something the Go process does safely.
- DNS-01 only: requires provider API credentials for every DNS host — poor default UX.

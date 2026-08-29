# Security model

WG-Guard is security-sensitive infrastructure (VPN control plane with secrets and host network
access). Principles: least privilege, secure defaults, standard primitives only, honest limits.

## Threat model (documented limits)

- **Protected against**: remote/web attackers, unauthenticated API abuse, brute force on login,
  CSRF, malicious API clients exceeding their scopes, subprocess injection via user input.
- **Not protected against**: an attacker with **root on the VPS** — they can read process
  memory, the master key file, the DB, and the AWG private keys. This is true of every
  self-hosted panel (wg-easy, WGDashboard, …) and is documented honestly rather than claimed
  otherwise. WG-Guard's secrets-at-rest design raises the bar for *lesser* compromises
  (DB-file leak, backup leak, web-layer RCE in a sibling service) without pretending root
  equivalence.

## Secrets inventory & storage

| Secret | Storage |
|---|---|
| Admin passwords | argon2id (OWASP parameter baseline), never plaintext, never logged |
| Admin sessions | random tokens, stored hashed; HttpOnly, Secure, SameSite=Lax cookies; absolute + idle expiry; rotation on login |
| API tokens | `wg_` + 32 chars crypto/rand; stored as SHA-256 with indexed prefix; scopes, expiry, optional CIDR allowlist; revocable |
| Device private keys / preshared keys | AES-256-GCM encrypted with the node-local master key (32 B, file 0600 outside the DB); required for config re-download; rotation procedure below + loss consequence documented |
| Webhook secrets, Telegram credentials, backup password | encrypted at rest with the master key |
| Audit log | never contains secrets (redaction list enforced in code) |

### Master-key rotation (implemented in `internal/secrets`)

Rotation is crash-safe via a dual-key window: (1) the old key file is renamed to
`master.key.prev` and a new key takes its place — from this instant both key versions can
decrypt; (2) every carrier (interface keys, device keys, encrypted settings) re-encrypts its
rows old→new; (3) on full success `.prev` is deleted. A crash at any point leaves the key ring
(current + previous) able to decrypt every stored envelope; the next boot resumes with both keys
loaded. If the master key **and** every backup are lost, encrypted secrets (device private keys,
webhook/Telegram credentials) are unrecoverable by design — devices can be re-enrolled, but this
is documented honestly as data loss.

No `math/rand` for secrets; `crypto/rand` everywhere. Secrets are passed to subprocesses via
stdin or 0600 temp files, never argv, never shell interpolation. All exec traffic goes through
`internal/subprocess` — the single audited choke point (explicit argv, per-command timeout,
structured exit errors): `awg` config files are written to 0600 temp files that live only for
the duration of one CLI call; command stdout (which can contain key material, e.g. `awg show
dump`) is parsed, never logged, and never embedded in errors.

## Panel hardening

- CSRF token on all mutating form/HTMX requests; security headers (CSP, X-Content-Type-Options,
  frame denial, referrer policy); `no-store` on sensitive endpoints.
- Login rate limiting (per-IP and per-account lockout), audit-logged login activity.
- Authorization is centralized: a permission registry checked server-side per handler; the UI
  never hides what the server doesn't enforce; the Owner role cannot remove itself.
- Strict request size limits, timeouts, panic recovery returning the standard error envelope.
- Transport modes per [deployment.md](deployment.md): ACME, manual certs, loopback-behind-proxy,
  dev-only loopback HTTP — never silent public plaintext.

## Linux/network security

- Namespaced nftables table only; scoped rules; no global policy changes; firewall-manager
  coexistence handled explicitly (see [../architecture/networking.md](../architecture/networking.md)).
- Subprocess surface minimized: pinned `awg` binary, argv-only, timeouts, output treated as
  untrusted input, parsed strictly (see [../integrations/amneziawg.md](../integrations/amneziawg.md)).
  Applied configs are verified after apply (post-apply dump must match the applied key/port/
  obfuscation set), so a silently-ignored write becomes a hard error rather than invisible
  drift.
- Systemd hardening in native mode; non-privileged container defaults with only `NET_ADMIN`
  added, in Docker mode.

## Dependency discipline

Every dependency justified (binary size, transitive deps, maintenance, security history);
pinned versions; `govulncheck` in CI; no vendoring of GPL components (executed, not linked).

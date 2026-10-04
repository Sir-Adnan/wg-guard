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

Live data/key access is orchestrated by `internal/nodestate`, including startup pending restore,
required pre-migration recovery, database-only versus key-bearing access and lifetime ownership.
`internal/layout` defines the fixed host/node paths and closed managed-replacement targets.
Host execution/state/artifacts remain outside the node's writable data mount. Device key/PSK
provisioning is one shared sealed-material operation in `internal/device`; request parsing and
principal checks remain in their web/REST adapters. The refactor adds no host privileges.

| Secret | Storage |
|---|---|
| Admin passwords | Argon2id (OWASP parameter baseline), never persisted/logged as plaintext; an installer-generated password is displayed once on the interactive terminal after successful lifecycle/health completion |
| Admin sessions | random tokens, stored hashed; HttpOnly, Secure, SameSite=Lax cookies; absolute + idle expiry; rotation on login |
| API tokens | `wg_` + 32 chars crypto/rand; stored as SHA-256 with indexed prefix; scopes, expiry, optional CIDR allowlist; revocable |
| Interface/device private keys, preshared keys and customer-link capabilities | AES-256-GCM encrypted with the node-local master key (32 B, file 0600 outside the DB); required for config/link re-display; rotation procedure below + loss consequence documented |
| Webhook secrets, Telegram credentials, backup password | encrypted at rest with the master key |
| Audit log | never contains secrets (redaction list enforced in code) |

### Master-key rotation (implemented in `internal/secrets`)

Stop the server and finish other data commands before `wg-guard secrets rotate`. The CLI
holds exclusive kernel ownership of the shared-volume DB/key pair from before opening its
carriers through confirmation, rotation and database close. Ordinary data commands and
the server hold shared ownership for their lifetime, and restore/recovery require exclusive
ownership. Contention fails closed immediately; never delete the persistent data lock to
bypass it. Earlier binaries do not implement this protocol and must be stopped separately;
see [lifecycle recovery](lifecycle-recovery.md).

Rotation is crash-safe via a dual-key window: (1) the old key file is renamed to
`master.key.prev` and a new key takes its place — from this instant both key versions can
decrypt; (2) the shared node storage carrier re-encrypts interface/device keys, optional PSKs,
customer-link capabilities, webhook secrets and secret settings; (3) after full current-key
verification and a completed SQLite WAL checkpoint, `.prev` is deleted. Key publication uses
private temporary files, file fsync, atomic rename and directory fsync on Linux.
A crash leaves current/previous keys available. Startup loads both without rewriting rows;
the next explicit `secrets rotate` completes this existing window, skips already-current values
and never overwrites either retained key. A malformed predecessor is refused, not ignored or
replaced. The service must remain stopped until completion; the command has a 15 min deadline
and preserves its dual-key recovery window on failure/cancellation.
If the master key **and** every backup are lost, encrypted secrets (device private keys,
webhook/Telegram credentials) are unrecoverable by design — devices can be re-enrolled, but this
is documented honestly as data loss. If encrypted node data exists, service startup and offline
data commands refuse a missing or wrong master key before creating replacement key material.
Restore the matching key or a coordinated archive.

Carrier validation follows each writer's envelope format: interface/device/subscription values
are binary envelopes; webhook secrets and encrypted settings are `enc:`-prefixed base64 text.
The startup/offline check must decode webhook text before authenticating it. A valid webhook
must not be mistaken for a wrong master key. Missing keys, malformed text and failed
authentication still fail closed without regenerating key material or exposing stored values.

`internal/secrets/storage.go` is the closed source inventory for startup sampling, full portable
backup inspection and rotation. A parity check covers the secret settings catalog. Startup
samples at most one nonempty optional value per field; it is not a full data-integrity audit.
Inspection streams values; rotation reads/writes at most 128 records per page and performs a
full current-key check before discarding the predecessor. Encoded values are capped at 8 KiB,
storage IDs at 128 bytes and key-file reads at 33 bytes before exact 32-byte validation.
Plaintext is cleared after each cryptographic operation. No keys, tokens, envelopes or configs
are emitted as diagnostics. Local regressions cover all fields, multi-page rotation, interrupted
mixed-key retries, foreign/invalid keys and a portable post-rotation archive; these do not
replace a real-host power-loss or client acceptance drill.

Archive admission now reuses the typed key/pool/parameter/setting contracts to inspect the
immutable stored domain, not just secret decryption. Canonical key pairs, PSKs, usable device
addresses, non-overlapping profiles and valid critical lifecycle/expiry/revocation dates prevent
a readable archive from silently losing connectivity or access-control state after restore.
Historical/disabled/over-limit data remains admissible. This gate runs again before replacing an
approved payload, including previews from older builds. Errors expose fixed fa/en guidance,
not private keys, customer capabilities, raw settings or detailed SQL causes. See the exact
scope and resource/host boundaries in [backup/restore](backup-restore.md).

No `math/rand` for secrets; `crypto/rand` everywhere. Secrets are passed to subprocesses via
stdin or 0600 temp files, never argv, never shell interpolation. All exec traffic goes through
`internal/subprocess` — the single audited choke point (explicit argv, per-command timeout,
structured exit errors): `awg` config files are written to 0600 temp files that live only for
the duration of one CLI call; command stdout (which can contain key material, e.g. `awg show
dump`) is parsed, never logged, and never embedded in errors.

## Operational logging boundary

Every production text/JSON `slog.Handler` is wrapped by `internal/logsafe` before output. The
wrapper recursively sanitizes messages, errors, groups, maps, URLs and pre-bound attributes;
recognized credentials, authorization/cookie values, WG-Guard tokens, subscription capabilities,
private/PSK/HPK directives, and webhook/Telegram/backup secrets become `[REDACTED]`. Useful fixed
metadata such as request/token IDs, counts and safe paths remains available. Collection traversal
and recursion are bounded.

Composition assigns one closed component value (`serve`, `http`, `scheduler`, `accounting`,
`webhook`, `backup`, `awg`, or `network`) for later CLI filtering. This boundary is defense in
depth: callers must still avoid logging raw configs, subprocess output, request bodies, headers,
or secret-bearing URLs. Text and JSON secret-corpus tests plus race tests enforce the handler
contract; real failure-log disclosure checks remain part of the Phase 9 VPS gate.

Installer lifecycle observability contains only fixed `operation`, `outcome`, `mode` and timestamp
metadata; error text, prompts, stdin, argv and subprocess output are structurally absent. Files are
0600 in a 0700 directory, read back only after strict canonical validation, and bounded to seven
UTC daily files/8 MiB. A fixed installer-owned tmpfiles rule removes files by mtime after seven
days using Ubuntu's existing cleanup timer; unowned policy conflicts are refused. This journal is
removed when the operator explicitly purges node data.

## Panel hardening

- CSRF token on all mutating form/HTMX requests; security headers (CSP, X-Content-Type-Options,
  frame denial, referrer policy); `no-store` on sensitive endpoints.
- Login rate limiting (per-IP and per-account lockout), audit-logged login activity.
- Authorization is centralized: a permission registry checked server-side per handler; the UI
  never hides what the server doesn't enforce; the Owner role cannot remove itself.
- Web-triggered updates require `update.manage` and cross to the host only as a fixed 0600 envelope
  containing a stable release or reviewed core identity. The root-owned broker revalidates that
  identity and maps it to closed argv; no shell text, executable path, development ref, credential,
  raw config or lifecycle error is accepted from or returned to the panel. Docker socket/systemd
  access is never mounted into the web container.
- Strict request size limits, timeouts, panic recovery returning the standard error envelope.
- Transport modes per [deployment.md](deployment.md): direct ACME/managed certificate,
  loopback-behind-proxy, and private/dev loopback HTTP — never silent public plaintext.
- Session cookies are `Secure` on direct/proxied HTTPS. Only the explicitly private, loopback-only
  dev listener omits `Secure` so authentication works through its documented local SSH tunnel.
- HSTS is emitted only after actual TLS or a trusted `X-Forwarded-Proto: https` assertion from a
  loopback/private proxy peer. Public clients cannot enable it by spoofing the header; managed
  Nginx hides upstream copies and emits one canonical edge header.
- Listener/proxy discovery never stops an unknown owner or overwrites a foreign virtual host.
  Exposure changes use a private snapshot, lifecycle journal, health/certificate proof and
  rollback. Cloudflare tokens are bounded hidden input or 0600 files, never argv/state/output;
  managed certificate keys are 0600 and deploy hooks accept only the recorded lineage.

For REST mutations, authentication, current token scopes and rate limits are checked before
idempotency lookup or response replay. Stored keys are isolated per token; a pre-upgrade key with
unknown owner is rejected until it expires rather than replayed across principals. This closes
the released V1 replay-before-auth path, but does not make a multi-request billing workflow
atomic or provide reseller row isolation; those are Phase 14 work.
Reseller ownership columns and live grant ceilings are staged behind explicit route gates:
reseller panel sessions reach only dedicated owned-customer and device reads,
scoped token management, personal preferences and logout; operator routes remain denied.
Reseller token listing and revocation are ownership-filtered; the owner may issue a token for a
specific reseller without granting it node-wide access. REST allows only explicit,
ownership-checked read routes and denies unclassified routes, mutations and global aggregates.
Only the node owner can create or disable reseller records and bind panel accounts. Reducing a
reseller's grants or disabling it takes effect on existing sessions and tokens at validation;
the reseller's customers are retained. Node-wide staff management pages exclude reseller-bound
accounts and tokens, and their write actions reject those targets. Existing unbound tokens retain
their node-wide meaning. Only the owner can change reseller plan assignments; these form a
fail-closed product allowlist for tenant purchases. Purchase results persist only IDs and state
for 90 days; the caller key is hashed, while customer-link capabilities and private device
material remain outside the operation journal. Customer-link reads and rotations are separately
scoped, ownership-gated and no-store; rotation preserves the new DB state if runtime
reconciliation fails, rather than reviving old credentials.

## Linux/network security

- NAT/general rules stay in the namespaced nftables table; Docker coexistence uses only one tagged
  jump to an owned, interface/subnet-scoped child chain at its documented `DOCKER-USER` extension.
  No global policy or foreign chain is flushed; unsupported blocking paths fail closed (see
  [../architecture/networking.md](../architecture/networking.md)).
- Subprocess surface minimized: pinned `awg` binary, argv-only, timeouts, output treated as
  untrusted input, parsed strictly (see [../integrations/amneziawg.md](../integrations/amneziawg.md)).
  Applied configs are verified after apply (post-apply dump must match the applied key/port/
  obfuscation set), so a silently-ignored write becomes a hard error rather than invisible
  drift. Ordinary subprocess stdout is capped at 4 MiB and stderr at 1 MiB; configured build
  commands retain their tighter 1 MiB cap. Truncation is an explicit error, never silently parsed.
- Explicit userspace profiles run one foreground `amneziawg-go` child per interface. The node
  checks embedded pinned-source provenance and its UAPI socket, never captures daemon output, refuses an active
  unowned daemon, and terminates owned children on shutdown; Linux parent-death signaling covers
  abrupt node exit. `/dev/net/tun` is mapped only as a device in Docker.
- Systemd hardening in native mode; non-privileged container defaults with only `NET_ADMIN`
  added, in Docker mode.

## Planned domain/certificate management boundary

The [Phase 18 specification](domains-and-tls.md) adds an owner-authorized, bounded
certificate operation model in the future. It grants no new filesystem or host
rights today. Keep the existing update bridge identity-only; certificate imports
need their own reviewed staging/ownership contract and closed requests, never
arbitrary root paths, raw commands, Docker socket access or public private-key output.
SNI certificate selection and HTTP hostname-role authorization are distinct checks;
unknown/retired names must be denied even with cached certificates. A dedicated
subscription hostname must not serve administrative/API routes or log customer
capability URLs. See the [refactor acceptance program](../development/refactor-program.md).

## Dependency discipline

Every dependency justified (binary size, transitive deps, maintenance, security history);
pinned versions; `govulncheck` in CI; no vendoring of GPL components (executed, not linked).

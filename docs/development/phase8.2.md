# Phase 8.2 — Secure access & persistent manager

Status: **complete (2026-09-09)**. Phase 9 is next, designed but not started.

## Objective and placement

Make first entry, repeated management, and public panel exposure safe and predictable on the
supported Ubuntu 24.04+ amd64 host. This phase sits between completed Phase 8.1 and Phase 9
because certificate ownership, reverse-proxy coexistence, and post-install access changes are
installer lifecycle concerns that must be stable before observability and the web redesign.

The phase extends the existing Go installer and terminal manager. It does not add a second
deployment engine, a resident shell scheduler, a public-HTTP production mode, or a new REST API.

## Validated certificate choices

- **Built-in ACME** remains the simplest direct-domain path when the panel and HTTP-01 ports are
  free. The current Go `autocert` path continues to own issuance and renewal.
- **Shared HTTP-01 webroot** is the default for a standard host Nginx that already owns ports
  80/443. Nginx keeps those listeners; Certbot writes challenges below a dedicated WG-Guard
  webroot and the panel stays on loopback.
- **Cloudflare DNS-01** is a first-class advanced path for a domain. It needs no inbound
  validation port and can work behind a firewall or NAT. WG-Guard uses the official Certbot
  Cloudflare plugin with a zone-scoped `Zone:DNS:Edit` token in a root-only `0600` file. The
  exact panel hostname is requested; a wildcard is not requested because WG-Guard does not need
  one and broader authorization would violate least privilege.
- **Public IP HTTPS** uses Certbot 5.4 or newer and Let's Encrypt's mandatory `shortlived`
  profile. The certificate is valid for 160 hours, so successful automatic renewal, deploy-hook
  reload, and expiry diagnostics are release blockers for this path. WG-Guard uses standalone
  HTTP-01 and therefore requires public TCP 80 to be free; a busy multi-service host should use
  domain HTTPS through managed Nginx/webroot or DNS-01 instead.
- **Cloudflare Origin CA** is accepted as an explicitly labelled manual-origin certificate for
  an orange-cloud hostname in Full (strict) mode. It is not browser-trusted when Cloudflare is
  bypassed, cannot contain an IP SAN, and is never advertised as direct public HTTPS.
- **Manual/external certificate** remains available for other DNS providers and existing PKI.
  Manual DNS challenges without an API hook are not described as automatically renewable.

Sources reviewed 2026-09-09: [Let's Encrypt IP availability](https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability.html),
[Certbot IP workflow](https://letsencrypt.org/2026/03/11/shorter-certs-certbot),
[challenge constraints](https://letsencrypt.org/docs/challenge-types/),
[Certbot Cloudflare credentials](https://certbot-dns-cloudflare.readthedocs.io/en/stable/), and
[Cloudflare Origin CA limits](https://developers.cloudflare.com/ssl/origin-configuration/origin-ca/).
`acme.sh` was evaluated but is not automatically downloaded or executed: its mutable installer,
own upgrade/cron lifecycle, and plaintext account configuration would create a second trust and
ownership system. Certbot is an optional host tool, invoked as explicit argv and never linked or
vendored into the Go binary.

## Architecture

### Persistent acquisition and manager

The GitHub bootstrap performs compatibility and integrity checks, acquires one immutable build,
then atomically installs that verified binary as `/usr/local/bin/wg-guard` and stores a private,
bounded build receipt under `/var/cache/wg-guard`. It opens the manager instead of starting an
installation. The cached build becomes the default fresh-install candidate. A canceled or failed
install keeps the manager and receipt, so `sudo wg-guard` resumes locally without another GitHub
download. Explicit release/commit selection still refreshes the manager intentionally.

The main menu is state-aware:

- fresh host: Install, choose/refresh build, system readiness, help/exit;
- healthy installed node: overview, updates/recovery, panel access & HTTPS, backups, diagnostics
  & AmneziaWG core, uninstall;
- interrupted lifecycle: the matching resume/recovery action is promoted before normal actions.

All terminal copy remains English-only, width-aware, streaming, and usable without color.

### Exposure model

Installer intent is separate from runtime TLS mode:

| Exposure | Runtime listener | Certificate owner | Public URL |
|---|---|---|---|
| private | `127.0.0.1` HTTP | none; SSH tunnel | local only |
| direct | public HTTPS | WG-Guard ACME or managed/manual files | domain or IP |
| nginx | `127.0.0.1` HTTP | managed Nginx + Certbot/manual files | domain HTTPS |
| external proxy | `127.0.0.1` HTTP | operator | domain HTTPS, unverified until checked |

Certificate sources are `auto`, `builtin`, `webroot`, `cloudflare-dns`, `ip`, `manual`,
`cloudflare-origin`, and `external`. Legacy `--tls` flags remain compatible but cannot conflict
with the new explicit exposure/certificate flags.

Automatic selection is conservative:

1. no domain defaults to private SSH; trusted IP HTTPS is an explicit short choice;
2. a domain with free direct ports uses built-in ACME;
3. a domain with a standard, active, conflict-free Nginx uses loopback + shared webroot;
4. an unknown listener, conflicting virtual host, unsupported proxy layout, or unavailable
   validation route fails with a concise diagnosis and safe alternatives;
5. no process is killed and no foreign configuration is overwritten to make a port available.

### Nginx ownership

Managed Nginx integration is limited to Ubuntu's standard host service with the verified
`/etc/nginx/conf.d/*.conf` include and no existing exact `server_name` conflict. WG-Guard owns
only `/etc/nginx/conf.d/wg-guard.conf` and `/var/www/wg-guard-acme`. It renders the ACME location,
HTTP-to-HTTPS redirect, TLS 1.2/1.3 endpoint, security headers, forwarded headers, and loopback
proxy. Every write is atomic and followed by `nginx -t`; reload failure restores the previous
state. Successful tests run quietly (`nginx -q -t`), and upstream security headers are hidden
before Nginx emits one canonical edge copy. Existing Caddy, Apache, Traefik, containerized, or
custom Nginx layouts are detected but not edited; the manager displays a minimal generated
upstream target and operator guidance.

### Certificates, renewal, and secrets

Certbot-managed certificates use a deterministic `wg-guard-<12 hex>` lineage. Certbot material
is copied into `/etc/wg-guard/tls/{fullchain.pem,privkey.pem}` with private permissions; neither
the key nor Cloudflare token appears in argv, logs, the install state, or UI output. A managed
deploy hook invokes a host-only WG-Guard certificate-sync command. That command verifies the
expected lineage, atomically refreshes both files under the lifecycle lock, restarts the correct
Docker/native service, and health-checks it. Unrelated Certbot lineages are ignored.

The install state records exposure type, certificate source, public URL, deterministic managed
paths, and shared dependency ownership—not secret contents. WG-Guard removes only its proxy,
hook, webroot, and copied certificate files. Certbot, snapd, and Let's Encrypt lineages are shared
host resources and remain unless an operator removes them separately.

### Post-install reconfiguration

`Panel access & HTTPS` uses one lifecycle-locked reconfiguration service for private, direct,
managed-Nginx, and external-proxy transitions. It snapshots the prior boot/proxy state, stops the
node only when required, provisions and verifies the candidate, rewrites Docker/native runtime
artifacts, restarts, then proves health and the intended certificate identity. Any failure
restores the previous configuration and service. Public plaintext is rejected in both the plan
and runtime config.

## Milestones

- [x] **8.2.0** — Validate certificate/proxy options; freeze architecture, roadmap, risks, and
  verification plan.
- [x] **8.2.1** — Persist the verified bootstrap manager/build receipt; replace implicit setup
  with the state-aware premium main menu and retry-without-download flow.
- [x] **8.2.2** — Add exposure/certificate models, listener discovery, bounded port selection,
  CLI flags, concise context-aware wizard, and fail-closed public-HTTP rules.
- [x] **8.2.3** — Implement optional pinned-path Certbot preparation, Cloudflare DNS-01, shared
  webroot, short-lived IP certificates, secure credential handling, and certificate validation.
- [x] **8.2.4** — Implement transactional standard-Nginx configuration, forwarded-header/security
  policy, coexistence refusal, and safe cleanup.
- [x] **8.2.5** — Add renewal sync, certificate/exposure status and doctor checks, and rollback-safe
  post-install reconfiguration from the manager.
- [x] **8.2.6** — Complete automated/race/CI gates, normal and narrow-terminal QA, and targeted
  Ubuntu 24.04 amd64 VPS drills; synchronize permanent documentation and repository state.

Detailed task order: [Phase 8.2 implementation plan](../superpowers/plans/2026-09-09-phase8.2-secure-access-manager.md).

## Verification results

- Test-first coverage passes for derivation, occupied listeners, Nginx conflict/refusal and
  rollback, rendered runtime files, protected secret transport, schema migration, lineage sync,
  uninstall cleanup, manager dispatch and Docker/native reconfiguration seams.
- Bootstrap fixtures prove atomic private manager/receipt persistence, zero-network repeated
  entry, deliberate refresh, failure cleanup and exact source/release identity. PTY tests cover
  40/48/80 columns, plain/no-color output, cancellation and English-only terminal text.
- The final formatting, complete Go test, vet, Linux amd64 build, bootstrap and Linux race gates
  passed on the closing revision; API/OpenAPI did not change.
- The dedicated Ubuntu 24.04.4 amd64 Docker drill passed legacy-state migration, private access,
  real Nginx/webroot domain issuance, occupied-port no-mutation refusal, renewal/deploy-hook,
  staging and production short-lived public-IP issuance, 40-column manager QA and complete
  cleanup/restoration. [Sanitized evidence](../integrations/fixtures/verify-phase8.2-vps-2026-09-09.txt).
- No scoped Cloudflare token was available. DNS-01 token handling and command/plugin behavior are
  automated-test verified, but real Cloudflare mutation/issuance is explicitly unverified.
  Native secure-exposure real-host recertification remains a Phase 11 matrix cell; unchanged
  Phase 8/8.1 native, Telegram, QR/config/client and one-command drills were not repeated.

Real CA rate limits are respected: fixture/staging checks precede at most one production issuance
per required identity. Secrets and private certificate material are never committed as evidence.

## Completion

The one-line entry now opens a durable cached local manager after its first verified acquisition;
failed or canceled setup can resume locally. Every advertised exposure is explicit, public
plaintext is unrepresentable, certificate/proxy changes are journaled and reversible, short-lived
IP issuance/renewal and standard-Nginx coexistence passed the real Docker gate, and the repository,
documentation and verification records are synchronized. Phase 9 remains untouched.

## Explicitly deferred

- General provider-neutral DNS plugin automation; Cloudflare is the only managed DNS provider.
- Automatic edits to nonstandard Nginx, Caddy, Apache, Traefik, or container proxy layouts.
- Cloudflare account/proxy-mode changes and automatic Origin CA issuance; operators supply Origin
  CA files and enable Full (strict) themselves.
- Wildcard certificates unless a later product requirement names a concrete WG-Guard consumer.
- Broader load/soak, supported-later-Ubuntu, firewall matrix, and release-candidate repetition in
  Phase 11; public artifact publication remains Phase 12 and owner-approved.

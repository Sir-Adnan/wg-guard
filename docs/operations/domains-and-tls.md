# Panel, subscription domains and certificates

Current behavior is described separately from the Phase 18 target. Entering a
subscription URL does not currently create an HTTPS endpoint or enroll a hostname.

## Current supported behavior

`subscription.base_url` changes generated `/sub/{token}` URLs only. Empty uses the
panel origin. The operator must provide DNS/routing and valid TLS where that
hostname's HTTPS connection terminates. VPN `node.endpoint` and interface endpoint
overrides are separate; changing panel access does not change them automatically.

WG-Guard's direct built-in ACME currently admits the single boot-configured
`tls.domain`. Manual mode loads one certificate/key pair. There are no independent
subscription cert/key fields in the web panel or subscription-domain installer flow.
This is an incomplete management feature, not a Go/TLS limitation or a requirement
to use Nginx for every separate hostname.

On the same server, a browser-trusted manual SAN certificate covering both panel
and subscription hostnames can use the existing one TLS listener. Point both names
to that server, then use **Panel access & HTTPS → Configure or change secure access
→ Manual browser-trusted certificate files**. Import the certificate/key through
the wizard; managed copies are `/etc/wg-guard/tls/fullchain.pem` and
`/etc/wg-guard/tls/privkey.pem`. Set the subscription base URL to the second origin.
These are the active shared pair, not an independent subscription certificate slot.
The imported material must cover the panel name as well; replacing it with a cert
for only the subscription name breaks panel TLS. External issuance/renewal must
update and activate the managed pair through the supported lifecycle.

This workaround is a code-supported composition, not a newly real-host-certified
two-domain workflow. Current direct serving does not isolate private routes by
subscription hostname. A separate HTTPS gateway/proxy can instead own the second
hostname, terminate its TLS and forward the necessary public subscription routes.
When TLS terminates remotely, certificate files belong on that remote gateway.
A local cert/path field cannot configure its listener, DNS or forwarding.

For comparison, 3x-ui has separate subscription-server address/port/domain and
certificate/key settings. Those paths load existing files into that server; they
do not eliminate certificate issuance, port ownership or Docker mount requirements.
See the [official subscription documentation](https://github.com/MHSanaei/3x-ui/blob/main/docs/content/docs/en/config/subscription.mdx)
and [certificate documentation](https://github.com/MHSanaei/3x-ui/blob/main/docs/content/docs/en/config/ssl-certificates.mdx).

## Phase 18 target — planned, not implemented

Provide **Settings → Domains and HTTPS** with independent panel and subscription
cards. Keep the VPN endpoint clearly separate. Same-origin operation stays simple;
one optional distinct subscription hostname may be managed initially.

| Choice | Product interaction | Ownership |
|---|---|---|
| Automatic HTTPS | Enter hostname, review discovery, request/verify certificate | WG-Guard-managed supported ACME strategy |
| Manual certificate | Import cert/key, or enter controlled host file paths | Operator supplies material; WG-Guard validates/copies/activates |
| Existing external proxy | Enter verified external origin and review guidance | Operator owns edge TLS/routing |

In direct same-host operation, use a shared owned HTTPS listener and SNI to select
the approved certificate. Two hostnames can share port 443; two independent servers
cannot bind the same address/port. Support a shared SAN pair or distinct managed
pairs without requiring operators to write Nginx configuration. Nginx remains an
existing-proxy/coexistence option, not a mandatory additional production service.
Go's [TLS certificate callback](https://pkg.go.dev/crypto/tls#Config) and the existing
[autocert allowlist](https://pkg.go.dev/golang.org/x/crypto/acme/autocert#HostWhitelist)
provide primitives; lifecycle/host authorization still require explicit implementation.

## Enrollment and operation policy

- Saving a URL is distinct from approving domain enrollment. Normalize and validate
  names/origins, discover owned ports and supported DNS/challenge strategy, review
  the intended change, then persist a bounded domain identity and operation.
- Certificates are selected only for approved names. Unknown SNI/Host and retired
  names are denied even if material remains in the ACME cache. Handle same-origin
  roles and SNI/Host mismatch explicitly; never expand the allowlist from request input.
- Only authenticated authorized node owners initiate host certificate operations
  initially, with CSRF and audit. Read-only operators receive safe metadata. A
  subscription capability or bot token is not host filesystem authority.
- Use a dedicated schema-checked host request contract. Requests identify approved
  domains/staged imports, not arbitrary commands. Keep the current update bridge's
  restricted contract; the panel gets no Docker socket or general host executor.
- Manual path mode resolves and validates allowed sources on the host, including
  known certificate lineages. It is not arbitrary root-file browsing. Docker paths
  must be translated or copied into approved mounts; a path inside a container is
  not automatically a path on the host.
- Uploaded/private material goes into bounded private staging, is validated by the
  trusted executor and becomes root-owned managed copies. Request bodies, argv,
  logs, public receipts and source-control must not expose private keys/credentials.
- Check matching key/pair, chain and intended trust class, SAN/name, validity and
  permissions. DNS-01 credentials stay in approved private storage. Automatic
  issuance/renewal is serialized and bounded; CA failures never trigger busy retry.
- Keep the last working pair/listener until a candidate is ready. Activate a
  coherent pair, account for Docker bind-mount inode replacement, and perform the
  minimum required restart/recreate/reload plus certificate/readiness proof.
  Failure retains/restores the prior pair with recovery guidance.
- Manual imports show expiry and require an explicit source renewal/update flow;
  do not label them auto-renewed merely because a file path was entered.
- Present requested, issued, active, verified, expiring and failed states separately.
  DNS eligibility is not proof that a public browser reaches the correct server.
- Preserve subscription tokens, device keys and VPN endpoints. Changing the panel
  origin must not unnecessarily replace customer URLs or credentials.

## Public hostname routing

A distinct managed subscription hostname serves only the required subscription
page/config/QR, embedded assets and GET-based public preferences. Deny admin, API,
login, traversal and unsupported methods. Preserve normal same-host behavior when
panel/subscription origins are equal. Certificate selection alone is not route isolation.
Do not put real capability URLs in access/error logs, use private/no-store responses,
and avoid caching client configurations in a proxy. Browser/API origins, CSRF,
redirects, cookie scope and trusted forwarded headers require matching acceptance.

## Acceptance

Unit/integration checks cover names, role boundaries, certificate validation,
unknown/retired names, malformed paths/imports, ownership, serialization and rollback.
Browser checks cover fa/en, RTL/LTR, keyboard/touch, responsive/manual/automatic/error
flows and safe receipts. Real-host acceptance proves two-hostname HTTPS, renewal,
manual replacement, reboot, conflicting ports, private-route denial and unchanged
customer access. A remote gateway remains an explicitly operator-owned scenario.

Implementation gates and dependencies: [refactor program](../development/refactor-program.md#phase-18--domains-and-tls-inside-the-panel).

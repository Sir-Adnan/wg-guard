# Phase 18 — Independent domains and HTTPS

Source implementation on 2026-10-05 following Phase 17. The owner asked to continue
without another release. Latest stable remains preparation v0.1.9. No live-host,
CA issuance, registry publication or server rebuild was performed.

## Implemented boundary

- One panel origin, optional distinct public subscription origin and independent
  VPN endpoints. Panel changes preserve the prior customer origin as the public
  role when needed; tokens, keys, account/usage data and endpoints stay intact.
- Shared direct HTTPS listener with approved SNI/certificate selection. Every
  ClientHello, including ticket resumption, and every HTTP request rechecks role
  approval. Retired/unknown names, SNI/Host mismatch, traversal and private/API/
  login routes on the public role are refused, including existing connections.
- Separate bounded `domaintls` validation/admission, `domainqueue` intent/status/
  private staging and `install` host CA/lifecycle services. No new dependency,
  production Node runtime, node scheduler worker or general root executor.
- Owner-only CSRF/audit submission and host DB reauthorization under a shared
  data lease. One complete atomically published request/running receipt, strict
  operation/status schema and four bounded private imports. Safe status contains
  no keys, raw configs or credentials; it can link the owner to a changed panel.
- Versioned root-private certificate pairs, controlled import/recorded-lineage
  paths, matching key/SAN/system-chain/validity checks and expiry/fingerprint
  metadata. Manual upload is 256 KiB per file and does not enable renewal.
- New automatic slots use named Certbot HTTP webroot/standalone or existing
  private Cloudflare DNS credentials. Existing autocert cache and recorded
  Certbot lineage are retained. Normal due checks do not force renewal; changed
  material is validated even when public fingerprints match. Removed owned
  lineages stop renewing, unused failed candidate lineages are cleaned where
  safe, and unreferenced owned pairs are pruned after terminal recovery.
- One lifecycle snapshot/journal for boot, Compose, policy and hooks. Validation
  keeps the listener available; readiness/certificate proof precedes commit.
  Failure/cancellation restores the working configuration with a separate timeout.
  Pending domain recovery has explicit CLI/manager dispatch and never retries
  a destructive operation merely because time elapsed.
- `domain_protocol: 1` binds compatible manager/image metadata and labels. Old
  artifacts cannot remove active isolation. Owned broker installation/rollback/
  uninstall follows the deployed contract and refuses foreign units/hooks.
- The external-proxy role remains explicitly operator-owned/unverified. Owned
  single-domain Nginx is not silently expanded. Separate direct origins require
  DNS hostnames initially; public-IP setup remains in the original access flow.
- Responsive fa/en owner/reader page reuses existing semantic select/help/confirm,
  card, field, status and typography primitives. Native multipart and labelled
  sections remain usable without JavaScript. Settings link to the authoritative
  Domains page instead of presenting a conflicting managed base-URL input.
- Account REST paths/scopes are unchanged. OpenAPI/reference clarify the fallback
  setting and relative capability paths; no certificate/host-file bot API is added.

## Verification and limits

Fresh focused source checks passed for domain admission/queue, host lifecycle,
server integration, web permission/CSRF/upload/links, catalogs and CLI. Coverage
includes concurrent mailbox publication, owner revocation/data replacement lease,
key/chain/name/port rejection before stop, start/policy/state/proof/cancellation
failures, bootstrap preservation, removal/pruning, controlled archive symlinks,
foreign unit refusal/uninstall, named HTTP/DNS preparation and renewal validation.

Local real HTTPS sockets with synthetic trusted material prove both hostnames,
private-route denial, traversal/SNI mismatch, persistent connection retirement,
actual ticket resumption before retirement and refusal to resume after it.
Synthetic host/CA seams do not certify public CA issuance or a physical gateway.

Chromium and WebKit each passed 16 fa/en Light/Dark 320/390/768/1440px responsive
cells plus 2 no-JavaScript cells, method/source selection, keyboard movement and
contained controls. These emulate viewport/input behavior, not physical devices.
No fresh axe scan was run; template/catalog/icon/permission checks and the listed
browser interactions are the actual evidence.

Fresh delivery: the full ordinary Go suite, vet and Windows/amd64 build passed
with Go 1.27.0. Focused Linux race tests passed with Go 1.26.0 for domaintls,
domainqueue, install, web and serve; the subsequently added completion/recovery
and owned-uninstall cases passed separate focused race checks. The final install
package, vet/build and both browser matrices passed after those additions.
Bootstrap fixtures and asset measurements passed (JS 43,350 B gzip; CSS 37,877 B
gzip); measurements are observations, not product ceilings. Unchanged packages
in the full ordinary suite reuse applicable Go test cache. No earlier passing
result is relabelled as an exact later source/CI/image result. Exact main CI is
pending at this source delivery and will be recorded separately. Earlier Phase 17
image/resource evidence is not relabelled as new domain runtime acceptance.
Physical two-hostname CA issuance/renewal/import/replacement, reboot, remote
forwarding and unchanged client access remain Phase 20 gates. Publication and
the owner's verified off-host backup/rebuild checkpoint remain separate.

Contracts: [domains and TLS](../operations/domains-and-tls.md),
[security](../operations/security.md), [recovery](../operations/lifecycle-recovery.md),
[execution program](refactor-program.md).

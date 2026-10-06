# Panel, subscription domains and certificates

v0.1.10 and later implement independent domains; v0.1.9 retains its original
single-domain flow. [Phase 18](../development/phase18.md) records source/browser
checks; [Phase 20](../development/phase20.md) records real HTTP-01 issuance,
scoped live renewal/replacement, SNI/role isolation, reboot and client forwarding.
Unobserved DNS-01/external-proxy and long-interval renewal cells remain distinct.

## Independent addresses

- **Panel origin** accepts administration, login and the authenticated API.
- **Subscription origin** is an optional distinct public hostname. Its routes are
  limited to GET/HEAD subscription pages, device config/QR and embedded assets.
- **VPN endpoint** remains in `node.endpoint` and interface overrides. Changing
  HTTPS addresses never changes tunnel endpoints, customer tokens or device keys.

Direct hostnames share the existing HTTPS listener/port, normally 443, with SNI
certificate selection. Initially manage one panel origin and at most one distinct
subscription origin. Direct additional origins require DNS hostnames; existing
IP HTTPS access is retained. ASCII/punycode names are normalized; credentials,
paths, query strings and fragments are refused in origins.

If the panel hostname changes while customer links used its origin, the old
approved hostname becomes the public subscription role. An existing separate
subscription origin remains unchanged. The old private status page is then denied;
open the new panel address and sign in again. A queued operation displays that
new link in advance. Selecting **Use the panel origin** explicitly removes the
separate subscription role and changes generated links back to the panel origin.
It does not rotate tokens. A dedicated public origin cannot be reassigned to the
panel without first reviewing/removing its role.

## Settings → Domains and HTTPS

Only an enabled node owner can submit operations, with CSRF and audit. A reader
with `server.view` receives safe certificate/address/status metadata. Bot tokens
and customer capabilities cannot issue certificates or read host files.

| Choice | Required input | Renewal |
|---|---|---|
| Automatic HTTPS | HTTPS origin, HTTP-01 or managed Cloudflare DNS-01, optional email | Host Certbot, named lineage only, normal due checks |
| Manual certificate | Bounded PEM chain/key upload or controlled host paths | Operator replaces/imports it before expiry |
| Existing external proxy | HTTPS origin in the explicitly external-proxy topology | Gateway owns certificates, routing and renewal |

Current source preserves an existing built-in autocert certificate/cache when
introducing the second hostname. New automatic slots use the supported host
Certbot path. DNS credentials remain host-private; they are never browser form
values or argv. DNS-01 needs no HTTP listener for that slot. HTTP-01 uses the owned
challenge webroot while the panel stays available. A first manual-only listener
may use standalone issuance when port 80 is free, then owns the challenge sidecar.
An explicitly customized challenge port needs correct external port-80 forwarding;
the CA challenge must actually succeed. Unknown listeners are never stopped.

Manual uploads are limited to 256 KiB per file (576 KiB whole form). Candidate
pairs must match, have the intended SAN, chain to system trust roots and remain
valid for more than one hour. Certificate storage is versioned and root-private;
keys are 0600. Import does not enable renewal merely because a source path exists.
A shared SAN pair can be imported for both roles; distinct pairs also work.

### Existing ESSL or externally obtained PEM files

Prefer uploading the chain and key from the owner page over HTTPS. For host path
mode, copy them into the fixed import directory for the selected role:

```sh
sudo install -d -m 0700 /etc/wg-guard/certificate-import/subscription
sudo install -m 0644 /your/certificate/fullchain.pem /etc/wg-guard/certificate-import/subscription/fullchain.pem
sudo install -m 0600 /your/certificate/privkey.pem /etc/wg-guard/certificate-import/subscription/privkey.pem
```

Then select **Read controlled host paths**, or use:

```sh
sudo wg-guard domains configure --role subscription --origin https://sub.example.com --method manual --cert-file /etc/wg-guard/certificate-import/subscription/fullchain.pem --key-file /etc/wg-guard/certificate-import/subscription/privkey.pem
```

The panel role uses `/etc/wg-guard/certificate-import/panel/`. Arbitrary `/root`,
application, database, master-key and container paths are refused. Known recorded
Certbot live paths may be read only through their corresponding owned archive
symlinks. Other symlinks/escapes are refused. Sources are copied; updating an
external source file does not automatically update the active managed pair.

### Terminal operations

```sh
sudo wg-guard domains status
sudo wg-guard domains configure --role subscription --origin https://sub.example.com --method automatic --challenge http
sudo wg-guard domains renew subscription
sudo wg-guard domains remove --role subscription
sudo wg-guard domains recover
```

`status` emits bounded non-secret JSON. `renew` omits force renewal and checks only
an active recorded automatic lineage. `recover` restores the recorded working
boot/Compose/policy after an interrupted operation; finish other pending lifecycle
operations first. `domains bridge-install` repairs exact owned units only for a
compatible installed runtime. The original access wizard establishes the initial
HTTPS topology; after domain isolation is enabled it refuses to overwrite it.

## Host ownership and failure behavior

`domain_protocol: 1` is bound to the manager/runtime metadata and image labels.
Selecting an artifact without it is refused while domain policy is active.
`internal/domaintls` owns origin/role/route/certificate validation; `domainqueue`
owns the closed mailbox; `install` owns root lifecycle and CA work. The update
bridge remains identity-only. There is no Docker socket or general root executor.

- `/etc/wg-guard/domains/active.json`: approved roles/revision, not enrollment intent.
- `/etc/wg-guard/domains/certificates/<id>/`: immutable managed chain/key pairs.
- `/var/lib/wg-guard-host/domain-challenges/`: short-lived approved challenge lease;
  only this child directory is mounted read-only, not host state/journals.
- `/var/lib/wg-guard/domain-operations/`: one pending request, one running receipt,
  latest status/inventory and at most four private imports. Interrupted orphan
  imports expire after one hour; normal completion removes them.
- `wg-guard-domains.path/service/timer`: narrow root worker, bounded timeout and
  12-hour renewal checks with jitter. Host workers share the runner/lifecycle locks.

Requests are atomically published and claimed. The worker rechecks the current
owner in the existing database while holding shared data ownership; restore cannot
replace its authority source. Preparation validates material before stopping the
listener. Activation uses one journal/snapshot for boot, Compose, domain policy
and hooks, then recreates the listener, checks readiness and proves each local
certificate. Failure restores the prior runtime with an independent recovery
context. No automatic retry of interrupted destructive work occurs.

Certificate cache is bounded to the two approved slots. Approval/expiry is checked
on every ClientHello, including TLS session resumption, and role policy on every
HTTP request, including existing connections. Unknown/retired SNI, mismatched Host,
private routes on the public hostname, traversal and unsupported methods fail
closed. Challenge enrollment does not authorize HTTPS or administrative routes.
Unreferenced owned pairs are pruned after terminal recovery/activation; retired
owned CA lineages stop renewing. No unrelated certificate is revoked or removed.

## External gateways, settings and backups

An external proxy must preserve the approved hostname and explicitly route the
required public paths. Certificates belong where TLS terminates. A local field
cannot configure another server's DNS, firewall, listener or certificate files.
Externally owned TLS stays **external-unverified**. The existing installer-owned
single-domain Nginx topology is not silently expanded or adopted; configure an
explicit external gateway before using this mode.

Without managed policy, `subscription.base_url` remains a URL-generation setting,
not issuance/routing. With policy, its approved subscription origin controls web
links and the conflicting field is replaced by a Domains-page link. REST capability
responses remain relative `/sub/{token}` paths; the panel origin can still serve
those paths, or a bot may combine them with the separately configured public origin.
No certificate/host-file REST API is introduced.

Portable archives retain the established DB/master-key/account contract. Host
certificates, CA credentials, policy and queue imports are host authority, not
portable customer backup contents. Preserve/provision target HTTPS independently
when rebuilding; restore customer records using the verified migration procedure.
See [backup/restore](backup-restore.md) and [migration preparation](migration-preparation.md).

[Go TLS callbacks](https://pkg.go.dev/crypto/tls#Config) and
[Certbot challenge/renewal documentation](https://eff-certbot.readthedocs.io/en/stable/using.html)
describe the underlying mechanisms. Their availability does not certify the
owner's DNS, gateway or physical host; that acceptance remains separate.

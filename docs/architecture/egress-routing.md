# Egress routing (outbounds, balancers, rules)

Egress routing decides where traffic from tunnel clients leaves the node. It covers chosen sites
(for example only Gemini, or only `geosite:google-deepmind` plus `ipdata.co`) and the default
route. It uses the kernel only: policy routing, nftables and extra AmneziaWG/WireGuard links. It
does not use a proxy engine (no Xray, sing-box or tun2socks). See
[ADR-0016](../decisions/ADR-0016-kernel-egress-routing.md) for the decision. The one exception
is the opt-in [IPv6 site bridge](#ipv6-site-bridge)
([ADR-0018](../decisions/ADR-0018-ipv6-site-bridge.md)), which re-originates an outbound's web
connections over its IPv6 exit.

Status: implemented and covered by unit, namespace and web tests. It has **not** been verified
on a real host; see [status](../development/status.md#egress-routing).

## Objects

| Object | Purpose | Panel page |
|---|---|---|
| Outbound | An exit. `warp` (Cloudflare WARP account registered by the panel), `wireguard` (imported WireGuard/AmneziaWG client config, e.g. a wgcf or provider profile) or `source_ip` (leave directly from another local IPv4 via SNAT). A `warp` or `wireguard` outbound can be chained through another one (see [Chained outbounds](#chained-outbounds)). | `/routing/outbounds` |
| Balancer | Optional group of outbounds, with a strategy, per-member weight 1–10 and a fallback for when every member is down. | `/routing/balancers` |
| Rule | An ordered list of presets and/or matchers that sends matching traffic to direct, block, an outbound or a balancer. The first match wins. | `/routing/rules` |
| Default route | Where unmatched traffic goes: direct, an outbound or a balancer (never block). | `/routing/rules` |
| Geodata | Optional `geosite.dat` / `geoip.dat` (v2fly format), downloaded over HTTPS or uploaded. | `/routing/rules` |

Data lives in migration `0017_egress_routing.sql`:

- `egress_outbounds`: each row has a unique link slot from 1 to 63. Private keys, the preshared
  key and the WARP token/license are sealed in `secret_encrypted` with the node secret ring.
- `egress_balancers`, `egress_rules`, and the `egress_settings` singleton.

An outbound or balancer cannot be deleted while a rule, a balancer, an outbound chained through it
or the default route uses it. The card lists those users and offers no Delete; a refused request
is reported as an error that names them.

## Imported configs

`internal/egress/wgconf.go` reads a client config the way the pinned `amneziawg-tools` parser
does ([amneziawg.md](../integrations/amneziawg.md)):

- `#` starts a comment anywhere on a line, keys are case-insensitive and whitespace is
  insignificant except inside I1–I5. A UTF-8 BOM and CRLF line ends are accepted.
- Every key the pinned runtime knows is imported: `Jc`, `Jmin`, `Jmax`, `S1`–`S4`, `H1`–`H4`
  (scalars or ranges), `I1`–`I5`, `HeaderProtectionKey`, `ContentPaddingAddition`,
  `RekeyAfterTime`, `RekeyTimeout`, `RejectAfterTime`, `KeepaliveTimeout`,
  `MaxHandshakeAttempts`, `RandomTrailers`, `DisableCookies` and a `PersistentKeepalive` range.
- Understood and not used: `DNS`, `ListenPort`, `Table`, `FwMark`, scripts, `SaveConfig`,
  `AllowedIPs`, the tools-only `AdvancedSecurity` flag and the AmneziaWG 1.5 sender-side keys
  `J1`–`J3` and `Itime`, which the pinned 2.x runtime no longer has.
- Validation is what the runtimes themselves enforce, not WG-Guard's policy for the profiles it
  generates: an imported config was written for someone else's server. H1–H4 must not overlap,
  `Jmin ≤ Jmax` while junk is sent, `Jc ≤ 128`, u16 bounds for `S1`–`S4` and the ranges, and
  `S1`–`S4 ≥ 12` with a header-protection key. `Jc = 0` with padding or headers is valid.
  I1–I5 must be single-line `<tag value>` sequences of the tags either runtime builds.
- `Address` keeps the first IPv4 address (required: client traffic is IPv4) and the first IPv6
  address, which only the [IPv6 site bridge](#ipv6-site-bridge) uses.
- A junk-only config without H lines gets the stock headers 1–4. A set that changes nothing on
  the wire is plain WireGuard.
- Errors name the key (`Jc`, `Endpoint`, `[Peer]` …), never a value. The pasted text is never
  shown back, so the form asks for it again.

## Matchers

Rules accept curated **presets** (multi-select) and free-form **matchers**, one per line. Lines
starting with `#` are comments. The syntax follows the Xray/v2fly conventions operators already
know:

| Form | Meaning |
|---|---|
| `example.com`, `domain:example.com` | the domain and all its subdomains |
| `full:api.example.com` | that exact name only |
| `keyword:openai` | any name that contains the text |
| `regexp:^chat\d+\.example\.com$` | RE2 regular expression (at most 32 per rule) |
| `geosite:google-deepmind`, `geosite:google@cn`, `geosite:apple@!cn` | a geosite category, with optional attribute include/exclude |
| `1.2.3.4`, `1.2.3.0/24`, `ip:` / `cidr:` | IPv4 address or prefix |
| `geoip:ir` | a geoip country/list |

Validation rules:

- IPv6 addresses and `0.0.0.0/0` are rejected. IPv6 egress is not supported, and the default
  route covers "everything".
- Matchers are normalized: lower case, IDNA, trailing dot removed.
- Geosite and geoip categories are resolved when the plan is compiled, not when the rule is
  saved. A missing file or unknown category becomes a plan warning shown in the panel
  (`geodata_unavailable`, `geosite_missing`, `geoip_missing`). That matcher stays empty and the
  rule's other matchers keep working.

Presets (`internal/egress/presets.go`) are small, maintained domain lists per service: Gemini,
ChatGPT/OpenAI, Claude, Copilot, GitHub Copilot, Perplexity, Grok, Meta AI, Mistral, Poe,
Character.AI, Cursor, Google Play, Android developer, Google Cloud, Spotify, Netflix, YouTube,
Disney+, Docker, Hugging Face, JetBrains, Gradle, and IP lookup services such as ipdata.co and
ipinfo.io. They do not depend on geodata, so you can pick a single service without also routing
its parent company's other domains.

The rules page also has a **site check**. It takes a URL or host name and shows which rule and
target would handle it.

## Data path

```
client ─awgN─▶ prerouting (mangle)
                 ├ ct mark set? → restore mark (connection stays on its exit)
                 ├ new flow → classify: reserved/local → direct
                 │             rule sets (static CIDR + resolver-fed) → t_<target>
                 │             default → t_<default>
                 └ t_* sets meta mark + ct mark
mark 0x7fe… (direct)  → main table, existing masquerade
mark slot<<20         → ip rule fwmark … table 51800+slot → default dev wgoN
mark 0x7ff… (block)   → block_forward drops (TCP gets a reset)
```

- **Marks.** Only the field `0x7ff00000` is used, so marks from other tools are preserved.
  Slot `N` gets mark `N<<20`. Direct is `0x7fe<<20` and block is `0x7ff<<20`. Every write is
  `meta mark set meta mark and 0x800fffff or <mark>` (the same for `ct mark`), so bits outside
  the field (Docker, kube-proxy, Tailscale) are kept on both the packet and the connection.
- **Connections stay on one exit.** The chosen mark is saved in the conntrack mark. A pinned
  flow gets its mark back through `ct mark and 0x7ff00000 vmap { <mark> : goto k_<exit> }`, with
  one constant restore chain for direct, block and every enabled outbound (used by a rule or
  not). nft cannot OR two runtime values on supported kernels, hence the map. Flows that existed
  before a table rebuild go to direct. A flow never changes exits mid-connection.
- **Policy routing.** There is one `ip rule fwmark <mark>/0x7ff00000 table <51800+slot>
  priority 5180` per tunneled outbound, and the table holds `default dev wgoN` plus
  `unreachable default metric 4294967295`. The kernel deletes the link route when the link goes
  down or away; the unreachable route keeps marked traffic from falling through to the main
  table and leaving from the node's own address. Uninstall flushes all 63 slot tables.
  `source_ip` outbounds need no table; `source_nat` (postrouting priority 99) changes the
  source address to the chosen local IPv4.
- **Tunnel links.** Links are named `wgo1…wgo63` and created through the same `tunnel.Backend`
  as the client interfaces: kernel module first, then userspace fallback
  ([ADR-0003](../decisions/ADR-0003-kernel-first-userspace-fallback.md)).
  - AmneziaWG obfuscation parameters are passed through unchanged.
  - Each link gets `rp_filter=2` and MSS clamping (`mss_clamp`, link MTU − 40).
  - The default MTU is 1280.
  - The kernel module and the userspace daemon accept different I1–I5 tags. When the kernel
    module rejects a configuration, the link is retried on the userspace runtime.
  - Links are adopted after a panel restart when the keys, obfuscation and socket mark match. Peer settings
    (endpoint, preshared key, keepalive) are then converged in place with `syncconf`, the MTU
    with `ip link set mtu` and the address with the backend, so changes saved but not applied
    before the restart still land without a session reset. If any of those steps fails, the
    link is recreated.
  - Links are removed when their row is deleted or disabled.
- **nftables.** Everything lives in `table ip wgguard_egress`, which sits next to the existing
  `inet wgguard` table and never touches foreign tables
  ([ADR-0004](../decisions/ADR-0004-namespaced-nftables.md)). It is changed in place in one
  `nft -f` transaction:
  1. Ensure the objects exist.
  2. Flush the chains and static sets.
  3. Delete obsolete chains and sets.
  4. Re-add the rules.

  Resolver-fed timeout sets keep their elements across edits. When no egress feature is active,
  the table is deleted.
- **Connection drain.** When an exit stops carrying traffic (down, disabled, deleted, or its
  link failed), the apply that moves its traffic elsewhere also deletes the conntrack entries
  carrying its mark (`NETLINK_NETFILTER` delete filtered by `CTA_MARK`/`CTA_MARK_MASK`, Linux
  4.6+). Without this, connections pinned to a dead exit hang until each application gives up,
  which takes minutes for long-lived connections such as messengers and HTTP/2. Drained, a
  connection's next packet is classified again, the remote side resets it, and the application
  reconnects through the new route at once. The first apply after a start drains nothing.

## Balancer strategies

| Strategy | Unit | nftables | Notes |
|---|---|---|---|
| `sticky` (default) | device | `jhash ip saddr mod N seed 0x57474744` vmap over weighted buckets | Each device keeps one exit IP. This is the safest choice for Google, AI and banking sites that tie a session to an IP. The fixed seed keeps the mapping across rule reloads; without it the kernel drew a new seed on every apply. |
| `round_robin` | connection | `numgen inc mod N` | Even rotation. |
| `random` | connection | `numgen random mod N` | Weighted random. |
| `least_conn` | connection | `numgen random` over 20 adaptive buckets | Every health pass counts conntrack flows per outbound mark (`NETLINK_NETFILTER` dump filtered by `CTA_MARK`/`CTA_MARK_MASK`) and gives new flows to the members carrying fewer. If counting is unavailable, it falls back to the weights. |
| `failover` | — | single jump | Uses the first healthy member by priority (the form's Priority field; 1 first). |

Strategies that change exit per connection can change the visible IP mid-session; the form warns
about this. When no member is healthy, the balancer uses its fallback (`direct` or `block`). A
single outbound that is unhealthy fails open to direct.

## Health

- Every 30 seconds the scheduler signals the `egress-health` slow worker. It probes each enabled
  outbound by fetching `https://1.1.1.1/cdn-cgi/trace` from a socket with `SO_MARK` set to that
  outbound's mark.
- Each probe has a 6-second timeout, and at most 4 probes run at once. A probe reports latency,
  exit IP, country, the Cloudflare data centre (`colo`) and, for WARP, the trace's `warp` field
  (`on`, `plus` or `off`). A probe cut off by the pass deadline is not counted.
- A `source_ip` probe that leaves from another address than the configured one fails
  (`wrong_exit`). Disabled outbounds are not probed.
- An outbound whose link carries IPv6 (the IPv6 site bridge is on) also fetches
  `https://[2606:4700:4700::1111]/cdn-cgi/trace` and reports its IPv6 exit. This is
  informational: an IPv6 failure never fails the probe, because the bridge falls back to IPv4.
- An outbound is marked down after 2 failures in a row and up after 1 success. A state change
  rebuilds the plan.
- The panel **Test** button (`POST /routing/outbounds/{id}/test`, `interfaces.read`) probes
  once and records the result, but never changes the kernel itself. If the result flips the
  health state, the next health pass rebuilds the plan. Health of deleted outbounds is
  dropped on the next apply. **Test all** (`POST /routing/outbounds/test-all`,
  `interfaces.read`) does the same for every enabled outbound, 4 at a time.
- The same pass recreates the table if something removed it, and refreshes the least-connection
  shares.
- An apply that did not converge (a failed link, policy or table step, for example an endpoint
  name that did not resolve at boot) is retried by the health pass: first after 30 seconds, then
  with doubling backoff up to 10 minutes. A converged apply resets the backoff.
- **Endpoint re-resolution.** WireGuard resolves an endpoint name once, when the peer is set.
  For a down link whose endpoint is a name (a dynamic-DNS server, a provider that moved
  addresses), the health pass looks the name up again at most every 5 minutes. When the name no
  longer includes the live endpoint address, the next apply sets the peer again in place, which
  resolves the name anew without replacing the link.
- Rules and the default route show **Going direct** while their single-outbound target is
  disabled, down or failed.

## Speed test

The **Speed test** row action (`POST /routing/outbounds/{id}/speed`, `interfaces.write`) measures
one exit against Cloudflare's speed test endpoints (`speed.cloudflare.com/__down` and `/__up`)
through the outbound's mark, IPv6 first when its link carries IPv6:

- **Round trip.** The best of four small requests on a warm connection.
- **Download and upload.** Up to 25 MiB down and 10 MiB up, each phase bounded to 8 seconds; the
  rate counts what moved in that time. The whole test is bounded to 25 seconds.
- **One at a time.** The node runs one speed test at a time; a second request is answered at
  once with "another speed test is running". **Speed test all** (JavaScript) queues the visible
  outbounds one after another and can be stopped.
- **Results.** The last result per outbound is kept in memory (dropped with the outbound, lost on
  restart) and shown in the list as download/upload Mbit/s, with the round trip in the details.

## Site test

The **Tests** tab (`/routing/tests`, `interfaces.read`) opens one service, every service, or any
URL through the node's own route and every enabled outbound. It uses the same socket mark as
client traffic; an outbound with the IPv6 site bridge is tested IPv6 first, like the bridge. The
test never changes the kernel.

- **Grid.** One row per exit, one column per service. With HTMX the grid renders at once and
  each cell loads itself (`POST /routing/tests/cell`), so results appear as exits answer; each
  cell can be run again. A plain post runs the whole grid (bounded to 2 minutes) before
  answering.
- **Bounds.** At most 6 site tests run at once on the node (`routing.SiteTestSlots`), whatever
  the number of open panels; the rest wait their turn. One test is bounded to 15 seconds,
  redirects (up to five), the second page and the region lookup included; at most 2 MiB is read
  per page. An IPv6-first dial gives IPv6 4 seconds before falling back to IPv4. A failed attempt
  (not a DNS failure) is retried once while at least 5 seconds remain.
- **Requests.** A current desktop Chrome user agent with its `Sec-Ch-Ua` and `Sec-Fetch-*`
  headers. The client is still a script: a site's bot protection can answer it differently than a
  browser.
- **Services.** Detectors follow the open-source
  [RegionRestrictionCheck](https://github.com/lmc999/RegionRestrictionCheck) script
  (source-reviewed at `b6d4a6f`, 2026-10-19) where it still matches the live pages
  (`internal/egress/routing/sitejudge.go`; region lists in `siteregions.go`):
  - Gemini: the region code the app page reports, judged against Google's list of supported
    countries (reviewed 2026-10-19; unsupported: AFG, BLR, CHN, CUB, IRN, PRK, RUS, SYR). The
    page no longer carries the availability flag the script reads; a page without a region is
    Unknown.
  - ChatGPT: `unsupported_country` from the compliance endpoint.
  - Claude: `claude.ai` answers every non-browser client with a Cloudflare challenge, so the test
    reads the country Cloudflare places the exit in from `claude.ai/cdn-cgi/trace` and checks it
    against Anthropic's Supported Regions Policy (reviewed 2026-10-19, the version effective
    2026-11-12, plus the territories of listed countries).
  - Google Search: the `/sorry/` CAPTCHA and the abuse texts.
  - Netflix: a licensed title and an original; neither available is **Limited** (originals
    only), otherwise the catalog region.
  - IP location: the country and network owner `ipinfo.io` reports for the exit, the view many
    "VPN" checks use.
- **Custom URL.** Any other `http(s)` URL: an explicit region refusal (HTTP 451 or a "not
  available in your country" text) is **Blocked**; HTTP 401, 403 or 429 without a reason is
  **Refused**, since site firewalls often refuse scripted clients that browsers pass; any other
  answer **Opens**.
- **Bot checks.** A challenge page (Cloudflare's `cf-mitigated: challenge` header, "Just a
  moment", the challenge platform, other vendors' interstitials) is **Bot check** for every
  service: the site is up and a browser normally passes it.
- **Region.** A service that reports where it places the exit shows that code. For a
  Cloudflare-served site without one, the test reads `/cdn-cgi/trace` on the same host.
- **Verdicts.** Works, Opens, Limited, Bot check, Refused, Blocked, CAPTCHA (IP flagged), No
  answer or Unknown; the page explains each one. A page a detector does not recognize is
  **Unknown**, never "works".
- **Details.** Each cell shows the address family and time; the single-service grid adds the
  HTTP status, the final host and path and the detector's note.
- **Destinations.** Only public addresses are dialed. Private, loopback, link-local, ULA and the
  node's own addresses are refused.
- **From the Rules page.** The site check there links to the test with the domain filled in.

## IPv6 site bridge

Client tunnels and egress routing are IPv4-only. Google often refuses WARP's shared IPv4 exits
(search CAPTCHA; Gemini "not available") but accepts the same tunnel's IPv6 exit. **Open websites
over IPv6** (outbound form, off by default) bridges that gap for one outbound
([ADR-0018](../decisions/ADR-0018-ipv6-site-bridge.md)):

- **Availability.** An outbound with an IPv6 tunnel address can use it:
  - WARP registrations store the address Cloudflare assigns.
  - WARP outbounds registered earlier fetch it with `GET /reg/{id}` the first time the bridge is
    turned on.
  - Imported configs need an IPv6 `Address`; re-importing a config without one turns the bridge
    off.
- **Link.** The peer's AllowedIPs gain `::/0`. The link gets the IPv6 address
  (`ip -6 addr replace`). The slot's table gets the same IPv6 rule and routes as IPv4: `ip -6
  rule fwmark … table 51800+slot priority 5180`, `default dev wgoN` and the unreachable guard.
  If the host refuses IPv6 on the link, the card says so and the outbound keeps working over
  IPv4 without the bridge.
- **Redirect.** The `site_bridge` chain (nat prerouting) redirects client TCP connections to
  ports 80 and 443 that carry the outbound's mark to `<gateway>:51800+slot`. `block_forward`
  refuses UDP 443 with that mark (ICMP port unreachable), so browsers fall back from QUIC to TCP.
  The redirect is rendered only while the bridge listens on every gateway. A clean shutdown
  flushes it with `dns_redirect`.
- **Relay** (`internal/egress/sitebridge`):
  1. Read the original destination (`SO_ORIGINAL_DST`).
  2. Read the site name from the TLS client hello or the HTTP `Host` header. Nothing is
     decrypted, and the bridge never answers the handshake.
  3. Connect through the outbound's mark to the name's IPv6 address, then replay the bytes read.
  4. Relay both directions unchanged, passing half-closes on.

  Without a usable IPv6 address or name, the bridge connects to the client's original IPv4
  destination through the same outbound, so a bridged connection never changes exit.
- **Refused.** Destinations that are not public unicast, ports other than 80 and 443, and
  connections made straight to the listener.
- **Bounds.** 1024 concurrent bridged connections; excess connections are refused.
- **Firewall.** With ufw, boot adds `ufw allow in on awgN to any port 51801:51863 proto tcp`
  next to the DNS rule. Other INPUT firewalls must accept TCP 51801–51863 on the tunnel
  interfaces.
- **Card.** The outbound card shows the bridge state (active, waiting for a device tunnel, IPv6
  refused by the host), the probed IPv6 exit and the bridged connection counts, split into IPv6
  and IPv4 fallback.
- **Limits.** QUIC/HTTP/3 is unavailable through a bridged outbound. Non-web ports keep IPv4.
  Web traffic of that outbound takes one userspace relay hop.

## Domain rules and the built-in resolver

Domain matching needs the names clients look up, so a small forwarding resolver
(`internal/egress/dnsrouter`) runs only while domain rules exist:

- **Listeners.** It listens on UDP and TCP port 53 of each tunnel gateway IP (IP_FREEBIND).
  `dns_redirect` (nat prerouting) sends any tunnel client's port-53 traffic to it, so clients
  with a hard-coded DNS server are also covered.
  - A redirected query is local input, not forwarded traffic. With ufw active, boot adds
    `ufw allow in on awgN to any port 53` next to the forwarding rule
    ([networking](networking.md)); other INPUT firewalls must allow it themselves.
  - A clean shutdown flushes `dns_redirect` before the resolver closes, so clients are not
    redirected to a resolver that no longer listens. After a crash the rules stay until the
    panel starts again.
- **Upstreams.** It forwards to `network.dns_servers` and never to itself or loopback. If none
  are set, it uses 1.1.1.1 and 1.0.0.1.
- **Matching names.** For A records of matching names, the answer addresses are added to that
  rule's `r<id>_d` timeout set before the reply goes out. Additions are batched through `nft -f`.
  - When an answer has an address the feeder has not written (or whose remembered timeout has
    expired), the feeder writes it at once, together with anything already queued. The reply
    waits for that write for at most 300 ms. After that it is sent anyway and counted as `Late`;
    only that first connection then takes the default route. Addresses that are still present
    (including ones due for refresh) never delay the reply.
  - The timeout is the record TTL, clamped to 30 minutes–6 hours, and is refreshed at half-life.
    Each write is `add`, `delete`, `add` of the elements in one transaction. Older kernels do
    not extend the timeout of an existing element on a plain `add`; this form resets it on every
    kernel, and the element is never absent.
  - All sets are written in one transaction. If it fails (for example, one set is full), each
    set is retried in its own transaction, so the other rules keep learning (at most one retry
    per domain rule).
  - When an apply (re)creates a resolver set (first apply, or the health pass rebuilding a
    table someone flushed), the feeder forgets what it wrote to that set. The next answer
    re-adds the addresses. Clients that still have the name cached take the default route
    until they query again. A set emptied externally while the table stays in place is not
    detected; its addresses return with the first query after their half-life.
  - AAAA, SVCB and HTTPS queries for matching names get an empty answer, so dual-stack clients
    use IPv4, which the rule can steer, and connection hints (`ipv4hint`/`ipv6hint`) cannot
    bypass the A answer the rule learns from.
- **Bounds.** At most 512 queries in flight, 128 TCP sessions with 32 queries each, 65 536
  elements per set, and no cache.
- **Encrypted DNS** (on by default, its own card on the Rules page with a live state): to stop
  DNS from bypassing domain rules, the resolver answers NXDOMAIN for DoH and canary names
  (`use-application-dns.net`, iCloud Private Relay, the DoH hosts of Google, Cloudflare, Quad9,
  OpenDNS, AdGuard, NextDNS, CleanBrowsing, Control D, Mullvad and others). Known public DoH
  addresses on 443 and all port-853 traffic (DoT, DoQ) are blocked.
  - It acts only while at least one enabled domain rule exists; the card says "Waiting for a
    domain rule" otherwise.
  - A device set to a fixed private DNS provider (Android "Private DNS provider hostname",
    Firefox "Max protection") has no DNS while it is active, because strict modes do not fall
    back. The card says so in visible text.
  - Encrypted DNS sessions opened before it became active keep their conntrack entry until they
    close.
  - If you turn it off, encrypted DNS bypasses domain rules; IP and geoip rules still apply.
  - Apps with hard-coded encrypted resolvers that are not on the list can still bypass domain
    rules.

## WARP

The panel registers a free WARP account through Cloudflare's client API, using the minimal
calls that [wgcf](https://github.com/ViRb3/wgcf) uses. Reviewed at wgcf `de6bba7`, API `v0a5641`.

- **Calls:**
  - `POST /reg` creates the account.
  - `PUT /reg/{id}/account` applies a WARP+ license.
  - `GET /reg/{id}/account` reads the account.
- **Secrets.** The account token, private key and license are sealed and never logged.
- **Verification.** Registration succeeded against the live API on the Ubuntu 24.04 test node.
  If it fails, a wgcf-generated profile can be imported as a `wireguard` outbound instead.
- **New identity.** **New WARP identity** (`POST /routing/outbounds/{id}/renew`) registers a
  fresh device with new keys for an existing WARP outbound.
  - The outbound keeps its ID, slot, references, MTU, chain, IPv6 site bridge and a chosen
    endpoint. A stored WARP+ license is bound to the new device when Cloudflare accepts it.
  - The panel applies the new identity and probes the exit. While Cloudflare hands out the same
    exit IPv4 address, it registers again, up to 3 registrations in total (registrations are
    rate-limited).
  - The toast names the old and new exit, or says that the address stayed the same.
  - The exit stays in the same country, because the country follows the entry data centre.
- **Endpoints.** The endpoint field suggests known WARP endpoints: `engage.cloudflareclient.com`
  and addresses in `162.159.192.0/24`, `162.159.193.0/24`, `162.159.195.0/24` and
  `188.114.96.0/22`, on 2408 and the alternative ports 500, 1701, 4500, 854, 894 and 8854.
- **Exit country.** Cloudflare chooses the WARP exit from the data centre where the tunnel
  enters its network, normally next to this server. Neither the client API nor the WARP+ account
  has a country or exit-address setting; dedicated egress addresses exist only on Cloudflare's
  paid Zero Trust plans, outside this API. To exit in another country, chain the WARP outbound
  through an outbound in that country. To get another address in the same country, use **New
  WARP identity**. For Google services, the IPv6 site bridge usually helps more than a new IPv4
  address.

## Chained outbounds

A `warp` or `wireguard` outbound can go **through** another one (`Via`, stored in the tunnel
config). The chained link's own encrypted packets carry the other outbound's mark (`FwMark` on
the link), so the existing policy rule sends them through that outbound's tunnel.

- Use: WARP chained through a server in Germany exits from a Cloudflare data centre in Germany,
  with a WARP address. A provider server the node cannot reach directly can be reached through
  another exit.
- One level only: the hop must be a tunneled outbound that is not chained itself, and an
  outbound others go through cannot be chained. An outbound cannot go through itself.
- If the hop is disabled or removed, the chained outbound gets no link (`LinkError` `via`, plan
  warning `via_unavailable`) and its traffic takes the direct route. It never leaves through the
  main route with the tunnel unchained, which would change its exit silently.
- The hop is listed in the chained outbound's "Used by" and cannot be deleted while chained.

## Lifecycle

- **Boot and changes.** Boot step 4b applies egress after the client interfaces exist; a failure
  is a non-fatal finding (`egress`). Every routing change calls the same `Apply`.
- **Saved but not applied.** If applying fails, the panel says so in an error toast, and the
  health pass retries with backoff (see [Health](#health)).
- **Uninstall** deletes `table ip wgguard_egress`, the owned IPv4 and IPv6 `ip rule` entries
  and tables, and
  every `wgoN` link recorded in the database. Unattributed `wgo*` links block uninstall the same
  way unattributed `awg*` links do.

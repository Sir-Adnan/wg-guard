# Egress routing (outbounds, balancers, rules)

Egress routing decides where traffic from tunnel clients leaves the node. It covers chosen sites
(for example only Gemini, or only `geosite:google-deepmind` plus `ipdata.co`) and the default
route. It uses the kernel only: policy routing, nftables and extra AmneziaWG/WireGuard links. It
does not use a proxy engine (no Xray, sing-box or tun2socks). See
[ADR-0016](../decisions/ADR-0016-kernel-egress-routing.md) for the decision.

Status: implemented and covered by unit, namespace and web tests. It has **not** been verified
on a real host; see [status](../development/status.md#egress-routing).

## Objects

| Object | Purpose | Panel page |
|---|---|---|
| Outbound | An exit. `warp` (Cloudflare WARP account registered by the panel), `wireguard` (imported WireGuard/AmneziaWG client config, e.g. a wgcf or provider profile) or `source_ip` (leave directly from another local IPv4 via SNAT). | `/routing/outbounds` |
| Balancer | Optional group of outbounds, with a strategy, per-member weight 1–10 and a fallback for when every member is down. | `/routing/balancers` |
| Rule | An ordered list of presets and/or matchers that sends matching traffic to direct, block, an outbound or a balancer. The first match wins. | `/routing/rules` |
| Default route | Where unmatched traffic goes: direct, an outbound or a balancer (never block). | `/routing/rules` |
| Geodata | Optional `geosite.dat` / `geoip.dat` (v2fly format), downloaded over HTTPS or uploaded. | `/routing/rules` |

Data lives in migration `0017_egress_routing.sql`:

- `egress_outbounds`: each row has a unique link slot from 1 to 63. Private keys, the preshared
  key and the WARP token/license are sealed in `secret_encrypted` with the node secret ring.
- `egress_balancers`, `egress_rules`, and the `egress_settings` singleton.

An outbound or balancer cannot be deleted while a rule, a balancer or the default route uses it.
The error lists those users by name.

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
  Slot `N` gets mark `N<<20`. Direct is `0x7fe<<20` and block is `0x7ff<<20`.
- **Connections stay on one exit.** The chosen mark is saved in the conntrack mark. Flows that
  existed before a table rebuild go to direct. A flow never changes exits mid-connection.
- **Policy routing.** There is one `ip rule fwmark <mark>/0x7ff00000 table <51800+slot>
  priority 5180` per tunneled outbound, and the table holds `default dev wgoN`.
  `source_ip` outbounds need no table; `source_nat` (postrouting priority 99) changes the
  source address to the chosen local IPv4.
- **Tunnel links.** Links are named `wgo1…wgo63` and created through the same `tunnel.Backend`
  as the client interfaces: kernel module first, then userspace fallback
  ([ADR-0003](../decisions/ADR-0003-kernel-first-userspace-fallback.md)).
  - AmneziaWG obfuscation parameters are passed through unchanged.
  - Each link gets `rp_filter=2` and MSS clamping (`mss_clamp`, link MTU − 40).
  - The default MTU is 1280.
  - Links are adopted after a panel restart and removed when their row is deleted or disabled.
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

## Balancer strategies

| Strategy | Unit | nftables | Notes |
|---|---|---|---|
| `sticky` (default) | device | `jhash ip saddr mod N` vmap over weighted buckets | Each device keeps one exit IP. This is the safest choice for Google, AI and banking sites that tie a session to an IP. |
| `round_robin` | connection | `numgen inc mod N` | Even rotation. |
| `random` | connection | `numgen random mod N` | Weighted random. |
| `least_conn` | connection | `numgen random` over 20 adaptive buckets | Every health pass counts conntrack flows per outbound mark (`NETLINK_NETFILTER` dump filtered by `CTA_MARK`/`CTA_MARK_MASK`) and gives new flows to the members carrying fewer. If counting is unavailable, it falls back to the weights. |
| `failover` | — | single jump | Uses the first healthy member in the order listed. |

Strategies that change exit per connection can change the visible IP mid-session; the form warns
about this. When no member is healthy, the balancer uses its fallback (`direct` or `block`). A
single outbound that is unhealthy fails open to direct.

## Health

- Every 30 seconds the scheduler signals the `egress-health` slow worker. It probes each enabled
  outbound by fetching `https://1.1.1.1/cdn-cgi/trace` from a socket with `SO_MARK` set to that
  outbound's mark.
- Each probe has a 6-second timeout, and at most 4 probes run at once. A probe reports latency,
  exit IP and country.
- An outbound is marked down after 2 failures in a row and up after 1 success. A state change
  rebuilds the plan.
- The same pass recreates the table if something removed it, and refreshes the least-connection
  shares.

## Domain rules and the built-in resolver

Domain matching needs the names clients look up, so a small forwarding resolver
(`internal/egress/dnsrouter`) runs only while domain rules exist:

- **Listeners.** It listens on UDP and TCP port 53 of each tunnel gateway IP (IP_FREEBIND).
  `dns_redirect` (nat prerouting) sends any tunnel client's port-53 traffic to it, so clients
  with a hard-coded DNS server are also covered.
- **Upstreams.** It forwards to `network.dns_servers` and never to itself or loopback. If none
  are set, it uses 1.1.1.1 and 1.0.0.1.
- **Matching names.** For A records of matching names, the answer addresses are added to that
  rule's `r<id>_d` timeout set before the reply goes out. Additions are batched through `nft -f`.
  - The timeout is the record TTL, clamped to 30 minutes–6 hours, and is refreshed at half-life.
  - AAAA queries for matching names get an empty answer, so dual-stack clients use IPv4, which
    the rule can steer.
- **Bounds.** At most 512 queries in flight, 128 TCP sessions with 32 queries each, 65 536
  elements per set, and no cache.
- **Encrypted DNS** (on by default): to stop DNS from bypassing domain rules, the resolver
  answers NXDOMAIN for DoH and canary names (`use-application-dns.net`, `mask.icloud.com`,
  well-known DoH hosts). Known public DoH/DoT addresses on 443/853 and all port-853 traffic are
  blocked.
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

## Lifecycle

- **Boot and changes.** Boot step 4b applies egress after the client interfaces exist; a failure
  is a non-fatal finding (`egress`). Every routing change calls the same `Apply`.
- **Saved but not applied.** If applying fails, the panel says the change is saved but not
  applied, and the health pass retries.
- **Uninstall** deletes `table ip wgguard_egress`, the owned `ip rule` entries and tables, and
  every `wgoN` link recorded in the database. Unattributed `wgo*` links block uninstall the same
  way unattributed `awg*` links do.

# ADR-0018 — Opt-in IPv6 site bridge for outbounds

Status: accepted · Date: 2026-10-19 · Amends: [ADR-0016](ADR-0016-kernel-egress-routing.md)
("IPv6 egress is out of scope")

## Context

Client tunnels are IPv4-only, and so is egress routing. WARP's shared IPv4 exits are often
refused by Google services: the search CAPTCHA, and Gemini answering "not available in your
country". The IPv6 exit of the same WARP tunnel is usually accepted. Operators asked for a way to
reach such sites over the outbound's IPv6 without giving devices new configs.

The kernel cannot do this alone. nftables cannot translate an IPv4 connection into an IPv6 one,
and the client never sees the site's IPv6 address.

## Decision

An outbound with an IPv6 tunnel address can turn on the **IPv6 site bridge**. It is off by
default, and it is a deliberate, bounded exception to ADR-0016's kernel-only data path:

- **Scope.** Only client TCP connections that the outbound already carries, and only to ports 80
  and 443. Routing decisions stay in the kernel; the bridge changes how a routed connection
  leaves, not where it goes.
- **Redirect.** The routing table redirects those connections to one listener per outbound on
  the tunnel gateways (port 51800 + slot), in the NAT prerouting hook, matched by the outbound's
  mark. UDP 443 for that outbound is refused, so browsers drop QUIC and retry over TCP.
- **Relay.**
  - The bridge reads the site name from the TLS server name or the HTTP Host header. The standard
    library parses the client hello, and the handshake stops before any reply.
  - It connects to the name's IPv6 address through the outbound (socket mark, IPv6 policy route
    to the link) and replays the bytes it read.
  - It then relays unchanged in both directions. TLS stays end to end; nothing is decrypted.
- **Fallback.** If there is no IPv6 address, no name, or the IPv6 connection fails, the bridge
  connects to the client's original IPv4 destination through the same outbound. A bridged
  connection never changes exit.
- **Gating.** Redirect rules are rendered only for bridges that listen on every gateway, as with
  the DNS resolver. Panel shutdown flushes them.
- **Bounds.** 1024 concurrent bridged connections (excess are refused), a 10-second sniff
  deadline, a 16 KiB sniff limit, and two relay goroutines per connection.
- **Safety.**
  - Destinations must be public unicast. Private, loopback, link-local, ULA and the node's own
    addresses are refused, both for the original destination and for resolved names.
  - A connection made straight to the listener is refused, because its original destination is
    the gateway itself.

## Consequences

- Web traffic of a bridged outbound goes through a userspace hop: one accept, one dial, and a
  kernel splice copy per direction. Other ports and other outbounds keep the kernel-only path.
- ufw needs an input allow for the bridge ports on the tunnel interfaces. It is added next to the
  DNS rule; other firewalls need `tcp dport 51801-51863` accepted on `awg*` input.
- QUIC is unavailable for bridged outbounds. HTTP/3 sites still work over HTTP/2.
- WARP devices registered before this change get their IPv6 address from the client API
  (`GET /reg/{id}`) when the bridge is first turned on.

## Alternatives rejected

- **IPv6 inside the client tunnels (ULA + NAT66).** Every device would need a new config. It also
  would not help: under RFC 6724 a client with only a unique-local address prefers IPv4 for
  global destinations.
- **DNS64/NAT64 on the node.** It needs a stateful translator (Jool/Tayga), a new kernel module or
  daemon, and IPv6 inside the tunnels anyway.
- **A full proxy core.** See ADR-0016.

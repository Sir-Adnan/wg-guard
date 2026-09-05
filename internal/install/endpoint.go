package install

import "net/netip"

// validPublicIP classifies a candidate address; it cannot prove assignment,
// routing, NAT forwarding or remote reachability. The IANA special-purpose
// registries were checked on 2026-09-06 (see deployment.md). Exclude blocks
// marked not globally reachable, deprecated/conditional transition ranges,
// and deprecated IPv4-compatible/site-local IPv6 addresses. Explicit globally
// reachable exceptions within broad IETF protocol blocks remain candidates.
func validPublicIP(s string) bool {
	ip, err := netip.ParseAddr(s)
	if err != nil || ip.Zone() != "" {
		return false
	}
	// Classify IPv4-mapped addresses as IPv4 so they cannot bypass exclusions.
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, prefix := range publicEndpointExceptions {
		if prefix.Contains(ip) {
			return true
		}
	}
	for _, prefix := range excludedEndpointPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

var publicEndpointExceptions = [...]netip.Prefix{
	netip.MustParsePrefix("192.0.0.9/32"),    // PCP anycast
	netip.MustParsePrefix("192.0.0.10/32"),   // TURN anycast
	netip.MustParsePrefix("2001:1::1/128"),   // PCP anycast
	netip.MustParsePrefix("2001:1::2/128"),   // TURN anycast
	netip.MustParsePrefix("2001:1::3/128"),   // DNS-SD anycast
	netip.MustParsePrefix("2001:3::/32"),     // AMT
	netip.MustParsePrefix("2001:4:112::/48"), // AS112
	netip.MustParsePrefix("2001:20::/28"),    // ORCHIDv2
	netip.MustParsePrefix("2001:30::/28"),    // Drone Remote ID
}

var excludedEndpointPrefixes = [...]netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // this network
	netip.MustParsePrefix("100.64.0.0/10"),   // shared / carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // protocol assignments (exceptions above)
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("192.88.99.0/24"),  // deprecated 6to4 relay
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved
	netip.MustParsePrefix("::/96"),           // unspecified / deprecated IPv4-compatible
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local-use translation
	netip.MustParsePrefix("100::/64"),        // discard-only
	netip.MustParsePrefix("100:0:0:1::/64"),  // dummy prefix
	netip.MustParsePrefix("2001::/23"),       // protocol assignments (exceptions above)
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("2002::/16"),       // conditional 6to4 transition
	netip.MustParsePrefix("3fff::/20"),       // documentation
	netip.MustParsePrefix("5f00::/16"),       // SRv6 segment identifiers
	netip.MustParsePrefix("fec0::/10"),       // deprecated site-local
}

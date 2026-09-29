package webhook

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"
)

// Deny non-public, transition and documentation ranges even where Go's
// IsGlobalUnicast reports true. IPv4-mapped IPv6 is normalized before this
// check. Reseller webhooks use only resolved, checked IPs for actual dials.
var blockedWebhookRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func publicWebhookAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	if addr.Is6() && !netip.MustParsePrefix("2000::/3").Contains(addr) {
		return false
	}
	for _, prefix := range blockedWebhookRanges {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// publicWebhookDial resolves once and dials the chosen checked IP, keeping
// TLS's original hostname for certificate verification. It never hands an
// unchecked hostname to net.Dialer or a proxy.
func publicWebhookDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid webhook destination")
	}
	var addresses []netip.Addr
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = []netip.Addr{literal}
	} else {
		addresses, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("webhook destination resolution failed")
		}
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	var tried bool
	for _, addr := range addresses {
		if !publicWebhookAddr(addr) {
			continue
		}
		tried = true
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	if !tried {
		return nil, fmt.Errorf("webhook destination is not public")
	}
	return nil, fmt.Errorf("webhook public destination unreachable")
}

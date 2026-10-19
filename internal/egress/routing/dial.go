package routing

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Dialing through an outbound: every socket carries the outbound's mark, so
// the policy rule of its slot sends it through the link (or the SNAT rule
// rewrites its source). The health prober, the site test and the IPv6 site
// bridge share this dialer, so a test sees exactly the path real traffic
// takes.

// MarkedDialer returns a dialer whose sockets carry mark (0: no mark, the
// node's own route). Setting SO_MARK requires CAP_NET_ADMIN, which the node
// already holds for nftables and links.
func MarkedDialer(mark uint32, timeout time.Duration) *net.Dialer {
	d := &net.Dialer{Timeout: timeout}
	if mark != 0 {
		d.Control = func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) { serr = setMark(int(fd), int(mark)) }); err != nil {
				return err
			}
			return serr
		}
	}
	return d
}

// Lookup resolves host to addresses of one family ("ip4" or "ip6").
type Lookup func(ctx context.Context, network, host string) ([]netip.Addr, error)

// DefaultLookup uses the node's resolver.
func DefaultLookup(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, network, host)
}

// ErrNoRoutableAddress reports that a name resolved only to addresses an
// outbound never carries (private, loopback, link-local, multicast).
var ErrNoRoutableAddress = errors.New("routing: no routable address")

// HostDialer dials names through one outbound. With PreferIPv6 it tries the
// name's IPv6 addresses first and falls back to IPv4, the order the IPv6 site
// bridge uses.
type HostDialer struct {
	Mark       uint32
	PreferIPv6 bool
	Timeout    time.Duration
	Lookup     Lookup
	// FirstFamilyBudget, when set, bounds the time spent on the preferred
	// family before falling back to the other one (the site test sets it so
	// a broken IPv6 path still reaches IPv4 in time; the bridge does not).
	FirstFamilyBudget time.Duration
}

// Dial connects to host:port and reports the address family used.
func (h HostDialer) Dial(ctx context.Context, host string, port uint16) (net.Conn, string, error) {
	lookup := h.Lookup
	if lookup == nil {
		lookup = DefaultLookup
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = ProbeTimeout
	}
	families := []string{"ip4"}
	if h.PreferIPv6 {
		families = []string{"ip6", "ip4"}
	}
	if a, err := netip.ParseAddr(host); err == nil {
		families = []string{familyOf(a)}
	}
	dialer := MarkedDialer(h.Mark, timeout)
	var errs []error
	for i, family := range families {
		fctx := ctx
		if h.FirstFamilyBudget > 0 && i == 0 && len(families) > 1 {
			var cancel context.CancelFunc
			fctx, cancel = context.WithTimeout(ctx, h.FirstFamilyBudget)
			defer cancel()
		}
		addrs, err := resolveFamily(fctx, lookup, family, host)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, a := range addrs {
			conn, err := dialer.DialContext(fctx, "tcp", net.JoinHostPort(a.String(), strconv.Itoa(int(port))))
			if err == nil {
				return conn, family, nil
			}
			errs = append(errs, err)
			if fctx.Err() != nil {
				break
			}
			if ctx.Err() != nil {
				return nil, "", errors.Join(errs...)
			}
		}
	}
	if len(errs) == 0 {
		errs = append(errs, ErrNoRoutableAddress)
	}
	return nil, "", errors.Join(errs...)
}

// maxDialAddresses bounds the addresses tried per family.
const maxDialAddresses = 2

func resolveFamily(ctx context.Context, lookup Lookup, family, host string) ([]netip.Addr, error) {
	if a, err := netip.ParseAddr(host); err == nil {
		if !Routable(a) || localAddress(a) {
			return nil, ErrNoRoutableAddress
		}
		return []netip.Addr{a.Unmap()}, nil
	}
	addrs, err := lookup(ctx, family, host)
	if err != nil {
		return nil, err
	}
	var out []netip.Addr
	for _, a := range addrs {
		a = a.Unmap()
		if familyOf(a) == family && Routable(a) && !localAddress(a) {
			out = append(out, a)
			if len(out) == maxDialAddresses {
				break
			}
		}
	}
	if len(out) == 0 {
		return nil, ErrNoRoutableAddress
	}
	return out, nil
}

func familyOf(a netip.Addr) string {
	if a.Unmap().Is4() {
		return "ip4"
	}
	return "ip6"
}

// Routable reports whether an outbound may carry traffic to a: public
// unicast only. Private, loopback, link-local, multicast, unspecified and
// unique-local destinations stay on the node's own routes, and a name that
// resolves to one of them is never dialed through an outbound (the policy
// rule sits below the local table, so it would otherwise reach the node).
func Routable(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() {
		return false
	}
	if a.Is4() {
		for _, s := range reservedPrefixes {
			if netip.MustParsePrefix(s).Contains(a) {
				return false
			}
		}
	}
	return true
}

// The node's own addresses, cached briefly: a public name may resolve to
// one of them, and the local table answers before any policy rule.
var local struct {
	sync.Mutex
	at    time.Time
	addrs map[netip.Addr]bool
}

const localAddressTTL = 30 * time.Second

func localAddress(a netip.Addr) bool {
	local.Lock()
	defer local.Unlock()
	if local.addrs == nil || time.Since(local.at) > localAddressTTL {
		local.addrs, local.at = map[netip.Addr]bool{}, time.Now()
		if list, err := net.InterfaceAddrs(); err == nil {
			for _, ifa := range list {
				if p, err := netip.ParsePrefix(ifa.String()); err == nil {
					local.addrs[p.Addr().Unmap()] = true
				}
			}
		}
	}
	return local.addrs[a.Unmap()]
}

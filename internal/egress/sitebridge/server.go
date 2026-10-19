// Package sitebridge is the IPv6 site bridge (ADR-0018). Client tunnels are
// IPv4-only, but some services judge an exit by its IPv4 address far more
// harshly than by its IPv6 one (Google treats shared WARP IPv4 exits as
// abusive; the same tunnel's IPv6 exit is usually clean). For an outbound
// with the bridge on, the kernel redirects the client TCP connections it
// would send through that outbound on web ports (80, 443) to a listener here;
// the bridge reads the site name the client asked for (TLS server name or
// HTTP Host), opens the same connection through the outbound over IPv6 when
// the name has an IPv6 address, and relays bytes unchanged. TLS stays end to
// end between the client and the site: nothing is decrypted.
//
// When the name has no usable IPv6 address, or no name can be read, the
// bridge connects to the client's original IPv4 destination through the same
// outbound, so a bridged connection never leaves through another exit.
//
// Bounds: one listener per bridged outbound and gateway, a fixed connection
// budget (excess connections are refused), a sniff deadline and size limit,
// and two relay goroutines per bridged connection.
package sitebridge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
	"github.com/Sir-Adnan/wg-guard/internal/egress/routing"
)

// Limits.
const (
	MaxConnections = 1024
	sniffTimeout   = 10 * time.Second
	dialTimeout    = 8 * time.Second
	maxSniff       = 16 << 10
)

type counters struct {
	ipv6, ipv4, failed, refused atomic.Uint64
	inflight                    atomic.Int64
}

type key struct {
	gateway netip.Addr
	port    uint16
}

type route struct {
	id   string
	mark uint32
}

// Server is the bridge. The zero value is not usable; use New.
type Server struct {
	log *slog.Logger

	mu        sync.Mutex
	listeners map[key]net.Listener
	routes    map[uint16]route // listen port → outbound
	stats     map[string]*counters
	wg        sync.WaitGroup

	slots chan struct{}
	// lookup resolves site names (tests replace it).
	lookup routing.Lookup
	// originalDst reads a redirected connection's destination (tests
	// replace it).
	originalDst func(net.Conn) (netip.AddrPort, error)
	// dial opens the upstream connection (tests replace it).
	dial func(ctx context.Context, d routing.HostDialer, host string, port uint16) (net.Conn, string, error)
}

// New returns a stopped bridge.
func New(log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		log: log, listeners: map[key]net.Listener{}, routes: map[uint16]route{}, stats: map[string]*counters{},
		slots: make(chan struct{}, MaxConnections), lookup: routing.DefaultLookup, originalDst: originalDst,
		dial: func(ctx context.Context, d routing.HostDialer, host string, port uint16) (net.Conn, string, error) {
			return d.Dial(ctx, host, port)
		},
	}
}

// Configure implements routing.Bridge: it listens on BridgePort for every
// bridged outbound on every gateway and closes listeners no longer wanted.
// Connections already relayed are not interrupted.
func (s *Server) Configure(_ context.Context, cfg routing.BridgeConfig) map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := map[key]route{}
	for _, b := range cfg.Bridges {
		for _, g := range cfg.Gateways {
			if g.Is4() {
				want[key{gateway: g, port: b.Port}] = route{id: b.OutboundID, mark: b.Mark}
			}
		}
	}
	for k, ln := range s.listeners {
		if _, ok := want[k]; !ok {
			_ = ln.Close()
			delete(s.listeners, k)
		}
	}
	s.routes = map[uint16]route{}
	active := map[string]bool{}
	failed := map[string]bool{}
	for k, r := range want {
		s.routes[k.port] = r
		if s.stats[r.id] == nil {
			s.stats[r.id] = &counters{}
		}
		if _, live := s.listeners[k]; live {
			continue
		}
		ln, err := listen(k)
		if err != nil {
			s.log.Warn("egress bridge: listen failed", "address", k.gateway.String(), "port", k.port, "err", err)
			failed[r.id] = true
			continue
		}
		s.listeners[k] = ln
		s.wg.Add(1)
		go s.serve(ln, k.port)
	}
	for _, r := range want {
		if !failed[r.id] {
			active[r.id] = true
		}
	}
	for id := range s.stats {
		if !active[id] && !failed[id] {
			delete(s.stats, id)
		}
	}
	return active
}

// Stats implements routing.Bridge.
func (s *Server) Stats() map[string]routing.BridgeStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]routing.BridgeStats, len(s.stats))
	for id, c := range s.stats {
		out[id] = routing.BridgeStats{IPv6: c.ipv6.Load(), IPv4: c.ipv4.Load(), Failed: c.failed.Load(), Refused: c.refused.Load(), Inflight: c.inflight.Load()}
	}
	return out
}

// Close stops every listener. Relayed connections end with their peers.
func (s *Server) Close() error {
	s.mu.Lock()
	for k, ln := range s.listeners {
		_ = ln.Close()
		delete(s.listeners, k)
	}
	s.mu.Unlock()
	s.wg.Wait()
	return nil
}

func listen(k key) (net.Listener, error) {
	lc := net.ListenConfig{Control: freebind}
	return lc.Listen(context.Background(), "tcp4", netip.AddrPortFrom(k.gateway, k.port).String())
}

func (s *Server) serve(ln net.Listener, port uint16) {
	defer s.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		s.mu.Lock()
		r, ok := s.routes[port]
		c := s.stats[r.id]
		s.mu.Unlock()
		if !ok || c == nil {
			conn.Close()
			continue
		}
		select {
		case s.slots <- struct{}{}:
		default:
			c.refused.Add(1)
			conn.Close()
			continue
		}
		c.inflight.Add(1)
		go func() {
			defer func() { <-s.slots; c.inflight.Add(-1) }()
			s.handle(conn, r, c)
		}()
	}
}

func (s *Server) handle(client net.Conn, r route, c *counters) {
	defer client.Close()
	dst, err := s.originalDst(client)
	if err != nil || !bridgeable(dst) {
		c.refused.Add(1)
		return
	}
	_ = client.SetReadDeadline(time.Now().Add(sniffTimeout))
	var host string
	var head []byte
	if dst.Port() == 443 {
		host, head = sniffTLS(client)
	} else {
		host, head = sniffHTTP(client)
	}
	_ = client.SetReadDeadline(time.Time{})
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	upstream, family := s.connect(ctx, r.mark, host, dst)
	cancel()
	if upstream == nil {
		c.failed.Add(1)
		return
	}
	defer upstream.Close()
	if family == "ip6" {
		c.ipv6.Add(1)
	} else {
		c.ipv4.Add(1)
	}
	if len(head) > 0 {
		if _, err := upstream.Write(head); err != nil {
			return
		}
	}
	relay(client, upstream)
}

// connect opens the upstream: the site name over IPv6 first, then the
// client's original IPv4 destination, both through the outbound mark.
func (s *Server) connect(ctx context.Context, mark uint32, host string, dst netip.AddrPort) (net.Conn, string) {
	if name, ok := egress.NormalizeDomain(host); ok {
		d := routing.HostDialer{Mark: mark, Timeout: dialTimeout, Lookup: ipv6Only(s.lookup)}
		if conn, family, err := s.dial(ctx, d, name, dst.Port()); err == nil {
			return conn, family
		}
	}
	d := routing.HostDialer{Mark: mark, Timeout: dialTimeout}
	conn, family, err := s.dial(ctx, d, dst.Addr().String(), dst.Port())
	if err != nil {
		return nil, ""
	}
	return conn, family
}

// ipv6Only restricts a lookup to AAAA answers: the IPv4 path always goes to
// the address the client chose, never to another address of the name.
func ipv6Only(lookup routing.Lookup) routing.Lookup {
	return func(ctx context.Context, network, host string) ([]netip.Addr, error) {
		if network != "ip6" {
			return nil, routing.ErrNoRoutableAddress
		}
		return lookup(ctx, network, host)
	}
}

// bridgeable accepts only redirected web flows to public IPv4 destinations:
// a client that connects to the listener directly has the gateway as its
// original destination and is refused.
func bridgeable(dst netip.AddrPort) bool {
	if !dst.Addr().Unmap().Is4() || !routing.Routable(dst.Addr()) {
		return false
	}
	for _, p := range routing.BridgePorts {
		if dst.Port() == p {
			return true
		}
	}
	return false
}

// relay copies both directions until each side has finished, passing on
// half-closes so request/response protocols end cleanly.
func relay(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	pipe := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}
	go pipe(a, b)
	go pipe(b, a)
	wg.Wait()
}

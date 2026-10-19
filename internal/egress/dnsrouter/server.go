// Package dnsrouter is the built-in resolver that makes domain rules work
// with kernel routing. Client DNS (port 53, any destination) is redirected to
// it on the tunnel gateways; it forwards to the configured upstreams and,
// when the queried name matches a rule, adds the A records of the answer to
// that rule's nftables timeout set before replying (bounded by learnWait).
// The client's following connection to that address is then classified by
// the kernel.
//
// Bounds: one UDP and one TCP listener per gateway, a fixed in-flight query
// budget (excess datagrams are dropped like on any overloaded resolver),
// bounded TCP sessions, one feeder goroutine with a bounded queue, and no
// response cache (upstream resolvers already cache).
package dnsrouter

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/Sir-Adnan/wg-guard/internal/egress/match"
	"github.com/Sir-Adnan/wg-guard/internal/egress/routing"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// Limits.
const (
	MaxInflight     = 512
	maxTCPSessions  = 128
	maxTCPQueries   = 32
	tcpIdle         = 10 * time.Second
	upstreamTimeout = 2500 * time.Millisecond
	maxMessage      = 65535
	udpBuffer       = 4096
)

// DefaultUpstreams is used when no usable upstream is configured.
var DefaultUpstreams = []string{"1.1.1.1", "1.0.0.1"}

// Hostnames whose refusal disables automatic encrypted DNS in common
// clients: Firefox's canary domain, iCloud Private Relay, and the DoH
// templates browsers upgrade to for well-known resolvers.
var encryptedDNSNames = []string{
	"use-application-dns.net", "mask.icloud.com", "mask-h2.icloud.com", "mask-api.icloud.com",
	"cloudflare-dns.com", "dns.google", "dns.google.com", "dns64.dns.google",
	"dns.quad9.net", "dns9.quad9.net", "dns10.quad9.net", "dns11.quad9.net",
	"doh.opendns.com", "doh.familyshield.opendns.com", "doh.sandbox.opendns.com",
	"adguard-dns.com", "dns.adguard.com", "dns.nextdns.io", "doh.cleanbrowsing.org", "doh.dns.sb",
	"dns.controld.com", "freedns.controld.com", "dns.mullvad.net", "dns.alidns.com", "doh.pub",
	"doh.xfinity.com",
}

// Record types that carry addresses or connection hints for a name: AAAA,
// SVCB (64) and HTTPS (65). Their ipv4hint/ipv6hint parameters let a client
// connect without the A answer the rule learns from.
const (
	typeSVCB  dnsmessage.Type = 64
	typeHTTPS dnsmessage.Type = 65
)

type state struct {
	rules          *match.Ruleset
	blockEncrypted bool
	gateways       map[netip.Addr]bool
}

type listener struct {
	udp net.PacketConn
	tcp net.Listener
}

// Server is the resolver. The zero value is not usable; use New.
type Server struct {
	upstreams func(context.Context) []string
	log       *slog.Logger
	feeder    *feeder

	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	listeners map[netip.Addr]*listener
	wg        sync.WaitGroup

	state    atomic.Pointer[state]
	inflight chan struct{}
	sessions chan struct{}
	dropped  atomic.Uint64
	queries  atomic.Uint64
	matched  atomic.Uint64

	// port is 53 in production; tests bind an unprivileged port.
	port int
	// exchange forwards one query (tests replace it).
	exchange func(ctx context.Context, network, server string, query []byte) ([]byte, error)
}

// New returns a stopped resolver. upstreams returns the configured upstream
// addresses (network.dns_servers); run applies nftables element batches.
func New(upstreams func(context.Context) []string, run subprocess.Runner, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		upstreams: upstreams, log: log, port: 53,
		listeners: map[netip.Addr]*listener{},
		inflight:  make(chan struct{}, MaxInflight),
		sessions:  make(chan struct{}, maxTCPSessions),
		exchange:  exchange,
	}
	s.feeder = newFeeder(run, log)
	s.state.Store(&state{rules: &match.Ruleset{}})
	return s
}

// Stats are counters for diagnostics.
type Stats struct {
	Listening bool
	Gateways  int
	Queries   uint64
	Matched   uint64
	Dropped   uint64
	Feeder    FeederStats
}

// Stats returns the counters.
func (s *Server) Stats() Stats {
	s.mu.Lock()
	n := len(s.listeners)
	s.mu.Unlock()
	return Stats{Listening: n > 0, Gateways: n, Queries: s.queries.Load(), Matched: s.matched.Load(),
		Dropped: s.dropped.Load(), Feeder: s.feeder.stats()}
}

// Configure implements routing.Resolver.
func (s *Server) Configure(_ context.Context, cfg routing.ResolverConfig) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	gateways := map[netip.Addr]bool{}
	for _, g := range cfg.Gateways {
		if g.Is4() {
			gateways[g] = true
		}
	}
	rules := cfg.Ruleset
	if rules == nil {
		rules = &match.Ruleset{}
	}
	sets := map[string]bool{}
	for _, e := range rules.Entries {
		sets[e.SetName] = true
	}
	s.feeder.setSets(sets)
	s.state.Store(&state{rules: rules, blockEncrypted: cfg.BlockEncryptedDNS, gateways: gateways})
	if rules.Empty() || len(gateways) == 0 {
		s.stopLocked()
		return false
	}
	if s.ctx == nil {
		s.ctx, s.cancel = context.WithCancel(context.Background())
		s.feeder.start(s.ctx)
	}
	for addr, l := range s.listeners {
		if !gateways[addr] {
			l.close()
			delete(s.listeners, addr)
		}
	}
	ok := true
	for addr := range gateways {
		if _, live := s.listeners[addr]; live {
			continue
		}
		l, err := s.listen(addr)
		if err != nil {
			s.log.Warn("egress resolver: listen failed", "address", addr.String(), "err", err)
			ok = false
			continue
		}
		s.listeners[addr] = l
		s.wg.Add(2)
		go s.serveUDP(l.udp)
		go s.serveTCP(l.tcp)
	}
	return ok
}

// Close stops every listener and the feeder.
func (s *Server) Close() error {
	s.mu.Lock()
	s.stopLocked()
	s.mu.Unlock()
	s.wg.Wait()
	return nil
}

func (s *Server) stopLocked() {
	for addr, l := range s.listeners {
		l.close()
		delete(s.listeners, addr)
	}
	if s.cancel != nil {
		s.cancel()
		s.feeder.wait()
		s.ctx, s.cancel = nil, nil
	}
}

func (l *listener) close() {
	_ = l.udp.Close()
	_ = l.tcp.Close()
}

func (s *Server) listen(addr netip.Addr) (*listener, error) {
	lc := net.ListenConfig{Control: freebind}
	hostport := netip.AddrPortFrom(addr, uint16(s.port)).String()
	udp, err := lc.ListenPacket(context.Background(), "udp4", hostport)
	if err != nil {
		return nil, err
	}
	tcp, err := lc.Listen(context.Background(), "tcp4", hostport)
	if err != nil {
		udp.Close()
		return nil, err
	}
	return &listener{udp: udp, tcp: tcp}, nil
}

func (s *Server) serveUDP(conn net.PacketConn) {
	defer s.wg.Done()
	for {
		buf := make([]byte, udpBuffer)
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		select {
		case s.inflight <- struct{}{}:
		default:
			s.dropped.Add(1)
			continue
		}
		go func(query []byte) {
			defer func() { <-s.inflight }()
			ctx, cancel := context.WithTimeout(s.baseContext(), 2*upstreamTimeout)
			defer cancel()
			if resp := s.handle(ctx, "udp", query); resp != nil {
				_, _ = conn.WriteTo(resp, peer)
			}
		}(buf[:n])
	}
}

func (s *Server) serveTCP(ln net.Listener) {
	defer s.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		select {
		case s.sessions <- struct{}{}:
		default:
			s.dropped.Add(1)
			conn.Close()
			continue
		}
		go func() {
			defer func() { <-s.sessions }()
			defer conn.Close()
			for i := 0; i < maxTCPQueries; i++ {
				_ = conn.SetDeadline(time.Now().Add(tcpIdle))
				query, err := readTCP(conn)
				if err != nil {
					return
				}
				ctx, cancel := context.WithTimeout(s.baseContext(), 2*upstreamTimeout)
				resp := s.handle(ctx, "tcp", query)
				cancel()
				if resp == nil || writeTCP(conn, resp) != nil {
					return
				}
			}
		}()
	}
}

func (s *Server) baseContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func readTCP(conn net.Conn) ([]byte, error) {
	var hdr [2]byte
	if _, err := ioReadFull(conn, hdr[:]); err != nil {
		return nil, err
	}
	n := int(hdr[0])<<8 | int(hdr[1])
	if n < 12 {
		return nil, errors.New("short message")
	}
	msg := make([]byte, n)
	if _, err := ioReadFull(conn, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func writeTCP(conn net.Conn, msg []byte) error {
	if len(msg) > maxMessage {
		return errors.New("message too large")
	}
	out := make([]byte, 2+len(msg))
	out[0], out[1] = byte(len(msg)>>8), byte(len(msg))
	copy(out[2:], msg)
	_, err := conn.Write(out)
	return err
}

func ioReadFull(conn net.Conn, b []byte) (int, error) {
	read := 0
	for read < len(b) {
		n, err := conn.Read(b[read:])
		read += n
		if err != nil {
			return read, err
		}
	}
	return read, nil
}

// handle answers one query; nil drops it (unparseable input).
func (s *Server) handle(ctx context.Context, network string, query []byte) []byte {
	var p dnsmessage.Parser
	hdr, err := p.Start(query)
	if err != nil || hdr.Response {
		return nil
	}
	q, err := p.Question()
	if err != nil {
		return reply(hdr, nil, dnsmessage.RCodeFormatError)
	}
	s.queries.Add(1)
	st := s.state.Load()
	name := strings.TrimSuffix(strings.ToLower(q.Name.String()), ".")
	if st.blockEncrypted && encryptedDNSName(name) {
		return reply(hdr, &q, dnsmessage.RCodeNameError)
	}
	entry, matched := st.rules.Lookup(name)
	if matched && q.Class == dnsmessage.ClassINET && (q.Type == dnsmessage.TypeAAAA || q.Type == typeSVCB || q.Type == typeHTTPS) {
		// Matched services must resolve to IPv4 through the A answer so the
		// client uses the tunnel (client configs route IPv4 only) and the
		// rule learns the address first; the client then falls back to A.
		return reply(hdr, &q, dnsmessage.RCodeSuccess)
	}
	resp, err := s.forward(ctx, network, query, st)
	if err != nil {
		return reply(hdr, &q, dnsmessage.RCodeServerFailure)
	}
	if matched && (q.Type == dnsmessage.TypeA || q.Type == dnsmessage.TypeALL) {
		s.matched.Add(1)
		if done := s.learn(entry.SetName, resp); done != nil {
			wait := time.NewTimer(learnWait)
			select {
			case <-done:
			case <-wait.C:
				s.feeder.late.Add(1)
			case <-ctx.Done():
			}
			wait.Stop()
		}
	}
	return resp
}

func encryptedDNSName(name string) bool {
	for _, n := range encryptedDNSNames {
		if name == n || strings.HasSuffix(name, "."+n) {
			return true
		}
	}
	return false
}

// learn feeds the A records of a successful answer into the rule's set. The
// returned channel (nil: nothing to wait for) closes once addresses the
// kernel did not have yet are in the set.
func (s *Server) learn(set string, resp []byte) <-chan struct{} {
	var p dnsmessage.Parser
	hdr, err := p.Start(resp)
	if err != nil || hdr.RCode != dnsmessage.RCodeSuccess {
		return nil
	}
	if err := p.SkipAllQuestions(); err != nil {
		return nil
	}
	var addrs []netip.Addr
	var ttls []uint32
	for {
		h, err := p.AnswerHeader()
		if err != nil {
			break
		}
		if h.Type != dnsmessage.TypeA || h.Class != dnsmessage.ClassINET {
			if err := p.SkipAnswer(); err != nil {
				break
			}
			continue
		}
		a, err := p.AResource()
		if err != nil {
			break
		}
		addrs, ttls = append(addrs, netip.AddrFrom4(a.A)), append(ttls, h.TTL)
	}
	if len(addrs) == 0 {
		return nil
	}
	return s.feeder.learn(set, addrs, ttls)
}

// Forget implements routing.Resolver: the named sets were (re)created empty.
func (s *Server) Forget(sets []string) { s.feeder.forget(sets) }

func (s *Server) upstreamList(ctx context.Context, st *state) []string {
	var out []string
	if s.upstreams != nil {
		for _, u := range s.upstreams(ctx) {
			a, err := netip.ParseAddr(strings.TrimSpace(u))
			// Never forward to ourselves (a gateway configured as client DNS).
			if err != nil || !a.Is4() || st.gateways[a] || a.IsLoopback() {
				continue
			}
			out = append(out, a.String())
		}
	}
	if len(out) == 0 {
		out = DefaultUpstreams
	}
	return out
}

func (s *Server) forward(ctx context.Context, network string, query []byte, st *state) ([]byte, error) {
	var last error
	for _, up := range s.upstreamList(ctx, st) {
		resp, err := s.exchange(ctx, network, net.JoinHostPort(up, "53"), query)
		if err == nil {
			return resp, nil
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	if last == nil {
		last = errors.New("no upstream")
	}
	return nil, last
}

// exchange sends one query and returns the raw response (same ID).
func exchange(ctx context.Context, network, server string, query []byte) ([]byte, error) {
	d := net.Dialer{Timeout: upstreamTimeout}
	conn, err := d.DialContext(ctx, network, server)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline := time.Now().Add(upstreamTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)
	if network == "tcp" {
		if err := writeTCP(conn, query); err != nil {
			return nil, err
		}
		return readTCP(conn)
	}
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, maxMessage)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		if n >= 2 && buf[0] == query[0] && buf[1] == query[1] {
			return buf[:n:n], nil
		}
	}
}

// reply builds a minimal response for the query header and question.
func reply(q dnsmessage.Header, question *dnsmessage.Question, rcode dnsmessage.RCode) []byte {
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID: q.ID, Response: true, OpCode: q.OpCode, RecursionDesired: q.RecursionDesired,
		RecursionAvailable: true, RCode: rcode,
	})
	if question != nil {
		if err := b.StartQuestions(); err == nil {
			_ = b.Question(*question)
		}
	}
	out, err := b.Finish()
	if err != nil {
		return nil
	}
	return out
}

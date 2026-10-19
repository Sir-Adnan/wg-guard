package sitebridge

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/egress/routing"
)

func TestSniffTLSReadsServerNameAndKeepsBytes(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() {
		c := tls.Client(client, &tls.Config{ServerName: "gemini.google.com", InsecureSkipVerify: true})
		_ = c.SetDeadline(time.Now().Add(2 * time.Second))
		_ = c.Handshake() // never completes: the bridge does not answer
	}()
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	name, head := sniffTLS(server)
	if name != "gemini.google.com" {
		t.Fatalf("server name %q", name)
	}
	if len(head) < 5 || head[0] != 22 { // a TLS handshake record, replayable as is
		t.Fatalf("hello bytes not kept: %d", len(head))
	}
}

func TestSniffHTTPReadsHost(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	req := "GET / HTTP/1.1\r\nHost: example.com:80\r\nUser-Agent: t\r\n\r\n"
	go func() { _, _ = io.WriteString(client, req) }()
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	host, head := sniffHTTP(server)
	if host != "example.com" || string(head) != req {
		t.Fatalf("host %q head %q", host, head)
	}
}

// A bridged connection goes to the site name over IPv6 first and replays
// what was read; without IPv6 it goes to the client's original address.
func TestHandleBridgesIPv6ThenFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ipv6     bool
		wantHost string
		wantFam  string
	}{
		{"ipv6", true, "example.com", "ip6"},
		{"fallback", false, "142.250.185.78", "ip4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(nil)
			s.originalDst = func(net.Conn) (netip.AddrPort, error) {
				return netip.MustParseAddrPort("142.250.185.78:80"), nil
			}
			upClient, upServer := net.Pipe()
			var dialed string
			var mark uint32
			s.dial = func(_ context.Context, d routing.HostDialer, host string, _ uint16) (net.Conn, string, error) {
				if d.Lookup != nil && !tc.ipv6 {
					return nil, "", routing.ErrNoRoutableAddress
				}
				dialed, mark = host, d.Mark
				if d.Lookup != nil {
					return upClient, "ip6", nil
				}
				return upClient, "ip4", nil
			}
			client, conn := net.Pipe()
			c := &counters{}
			done := make(chan struct{})
			go func() { s.handle(conn, route{id: "o", mark: 0x100000}, c); close(done) }()
			req := "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"
			go func() { _, _ = io.WriteString(client, req) }()
			got, err := bufio.NewReader(upServer).ReadString('\n')
			if err != nil || got != "GET / HTTP/1.1\r\n" {
				t.Fatalf("upstream got %q %v", got, err)
			}
			if dialed != tc.wantHost || mark != 0x100000 {
				t.Fatalf("dialed %q mark %#x", dialed, mark)
			}
			client.Close()
			upServer.Close()
			<-done
			if tc.wantFam == "ip6" && c.ipv6.Load() != 1 || tc.wantFam == "ip4" && c.ipv4.Load() != 1 {
				t.Fatalf("counters ipv6=%d ipv4=%d", c.ipv6.Load(), c.ipv4.Load())
			}
		})
	}
}

// A direct connection to the listener (its original destination is the
// gateway itself) or a non-web port is refused.
func TestHandleRefusesUnbridgeableDestinations(t *testing.T) {
	for _, dst := range []string{"10.8.0.1:51801", "142.250.185.78:22"} {
		s := New(nil)
		s.originalDst = func(net.Conn) (netip.AddrPort, error) { return netip.MustParseAddrPort(dst), nil }
		s.dial = func(context.Context, routing.HostDialer, string, uint16) (net.Conn, string, error) {
			t.Fatalf("dialed for %s", dst)
			return nil, "", nil
		}
		_, conn := net.Pipe()
		c := &counters{}
		s.handle(conn, route{id: "o", mark: 0x100000}, c)
		if c.refused.Load() != 1 {
			t.Fatalf("%s not refused", dst)
		}
	}
}

func TestConfigureListensAndStops(t *testing.T) {
	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback:", err)
	}
	port := uint16(probe.Addr().(*net.TCPAddr).Port)
	probe.Close()
	s := New(nil)
	defer s.Close()
	gw := netip.MustParseAddr("127.0.0.1")
	active := s.Configure(context.Background(), routing.BridgeConfig{Gateways: []netip.Addr{gw},
		Bridges: []routing.BridgeSpec{{OutboundID: "o", Mark: 0x100000, Port: port}}})
	if !active["o"] {
		t.Fatal("bridge not active")
	}
	conn, err := net.Dial("tcp4", netip.AddrPortFrom(gw, port).String())
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if got := s.Configure(context.Background(), routing.BridgeConfig{}); len(got) != 0 {
		t.Fatalf("still active: %v", got)
	}
	if _, err := net.DialTimeout("tcp4", netip.AddrPortFrom(gw, port).String(), time.Second); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("listener left open: %v", err)
	}
}

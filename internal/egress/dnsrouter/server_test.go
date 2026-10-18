package dnsrouter

import (
	"context"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
	"github.com/Sir-Adnan/wg-guard/internal/egress/match"
	"github.com/Sir-Adnan/wg-guard/internal/egress/routing"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

type captureRunner struct {
	mu      sync.Mutex
	scripts []string
}

func (c *captureRunner) Run(_ context.Context, argv []string) (subprocess.Result, error) {
	if len(argv) == 3 && argv[0] == "nft" && argv[1] == "-f" {
		data, _ := os.ReadFile(argv[2])
		c.mu.Lock()
		c.scripts = append(c.scripts, string(data))
		c.mu.Unlock()
	}
	return subprocess.Result{}, nil
}

func (c *captureRunner) joined() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.scripts, "")
}

func query(t *testing.T, name string, typ dnsmessage.Type) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 0x4242, RecursionDesired: true})
	if err := b.StartQuestions(); err != nil {
		t.Fatal(err)
	}
	if err := b.Question(dnsmessage.Question{Name: dnsmessage.MustNewName(name), Type: typ, Class: dnsmessage.ClassINET}); err != nil {
		t.Fatal(err)
	}
	out, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// fakeUpstream answers every A query with two addresses behind a CNAME.
func fakeUpstream(_ context.Context, _, server string, q []byte) ([]byte, error) {
	var p dnsmessage.Parser
	hdr, err := p.Start(q)
	if err != nil {
		return nil, err
	}
	question, err := p.Question()
	if err != nil {
		return nil, err
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: hdr.ID, Response: true, RecursionAvailable: true})
	_ = b.StartQuestions()
	_ = b.Question(question)
	_ = b.StartAnswers()
	if question.Type == dnsmessage.TypeA {
		target := dnsmessage.MustNewName("edge.example.net.")
		_ = b.CNAMEResource(dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET, TTL: 60}, dnsmessage.CNAMEResource{CNAME: target})
		_ = b.AResource(dnsmessage.ResourceHeader{Name: target, Class: dnsmessage.ClassINET, TTL: 60}, dnsmessage.AResource{A: [4]byte{142, 250, 1, 1}})
		_ = b.AResource(dnsmessage.ResourceHeader{Name: target, Class: dnsmessage.ClassINET, TTL: 7200}, dnsmessage.AResource{A: [4]byte{142, 250, 1, 2}})
	}
	return b.Finish()
}

func rcodeOf(t *testing.T, resp []byte) (dnsmessage.RCode, int) {
	t.Helper()
	var p dnsmessage.Parser
	hdr, err := p.Start(resp)
	if err != nil {
		t.Fatal(err)
	}
	_ = p.SkipAllQuestions()
	answers, _ := p.AllAnswers()
	return hdr.RCode, len(answers)
}

func ruleset(t *testing.T) *match.Ruleset {
	ms, err := egress.ParseMatchers("gemini.google.com\nipdata.co")
	if err != nil {
		t.Fatal(err)
	}
	return &match.Ruleset{Entries: []match.Entry{{RuleID: "r1", SetName: "r1_d", Domains: match.NewDomains(ms)}}}
}

func TestResolverFeedsMatchedAnswersOverUDPAndTCP(t *testing.T) {
	run := &captureRunner{}
	s := New(func(context.Context) []string { return []string{"10.8.0.1", "9.9.9.9"} }, run, nil)
	s.exchange = fakeUpstream
	probe, _ := net.ListenPacket("udp4", "127.0.0.1:0")
	s.port = probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()
	gw := netip.MustParseAddr("127.0.0.1")
	if !s.Configure(context.Background(), routing.ResolverConfig{Gateways: []netip.Addr{gw}, Ruleset: ruleset(t), BlockEncryptedDNS: true}) {
		t.Fatal("resolver did not start")
	}
	defer s.Close()
	addr := netip.AddrPortFrom(gw, uint16(s.port)).String()

	udp, err := net.Dial("udp4", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	exchangeUDP := func(q []byte) []byte {
		_ = udp.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := udp.Write(q); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 4096)
		n, err := udp.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		return buf[:n]
	}
	if rc, n := rcodeOf(t, exchangeUDP(query(t, "Gemini.Google.com.", dnsmessage.TypeA))); rc != dnsmessage.RCodeSuccess || n != 3 {
		t.Fatalf("matched A: %v %d", rc, n)
	}
	// Learned before the reply went out: no waiting for a feeder tick.
	if !strings.Contains(run.joined(), "142.250.1.2 timeout 7200s") {
		t.Fatalf("reply sent before the set update:\n%s", run.joined())
	}
	if rc, n := rcodeOf(t, exchangeUDP(query(t, "gemini.google.com.", dnsmessage.TypeAAAA))); rc != dnsmessage.RCodeSuccess || n != 0 {
		t.Fatalf("matched AAAA must be empty: %v %d", rc, n)
	}
	if rc, _ := rcodeOf(t, exchangeUDP(query(t, "use-application-dns.net.", dnsmessage.TypeA))); rc != dnsmessage.RCodeNameError {
		t.Fatalf("canary domain must be refused: %v", rc)
	}
	if rc, n := rcodeOf(t, exchangeUDP(query(t, "example.org.", dnsmessage.TypeA))); rc != dnsmessage.RCodeSuccess || n != 3 {
		t.Fatalf("unmatched names are forwarded: %v %d", rc, n)
	}

	tcp, err := net.Dial("tcp4", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	_ = tcp.SetDeadline(time.Now().Add(3 * time.Second))
	if err := writeTCP(tcp, query(t, "api.ipdata.co.", dnsmessage.TypeA)); err != nil {
		t.Fatal(err)
	}
	resp, err := readTCP(tcp)
	if err != nil {
		t.Fatal(err)
	}
	if rc, n := rcodeOf(t, resp); rc != dnsmessage.RCodeSuccess || n != 3 {
		t.Fatalf("tcp: %v %d", rc, n)
	}

	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(run.joined(), "142.250.1.2 timeout 7200s") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	got := run.joined()
	if !strings.Contains(got, "add element ip "+routing.TableName+" r1_d { 142.250.1.1 timeout 1800s, 142.250.1.2 timeout 7200s }") {
		t.Fatalf("unexpected feed:\n%s", got)
	}
	if strings.Count(got, "142.250.1.1 timeout") != 1 {
		t.Fatalf("repeated answers within half the timeout must not be re-written:\n%s", got)
	}
	st := s.Stats()
	if !st.Listening || st.Matched < 2 || st.Queries < 5 {
		t.Fatalf("stats: %+v", st)
	}

	// An empty ruleset stops the resolver.
	if s.Configure(context.Background(), routing.ResolverConfig{}) {
		t.Fatal("empty ruleset must stop the resolver")
	}
	if s.Stats().Listening {
		t.Fatal("listeners survived stop")
	}
}

func TestUpstreamListNeverLoopsToGateway(t *testing.T) {
	s := New(func(context.Context) []string { return []string{"10.8.0.1", "127.0.0.53", "bogus", "8.8.8.8"} }, nil, nil)
	st := &state{gateways: map[netip.Addr]bool{netip.MustParseAddr("10.8.0.1"): true}}
	if got := s.upstreamList(context.Background(), st); len(got) != 1 || got[0] != "8.8.8.8" {
		t.Fatalf("upstreams: %v", got)
	}
	s = New(func(context.Context) []string { return []string{"10.8.0.1"} }, nil, nil)
	if got := s.upstreamList(context.Background(), st); len(got) != 2 || got[0] != "1.1.1.1" {
		t.Fatalf("fallback upstreams: %v", got)
	}
}

func TestTimeoutBounds(t *testing.T) {
	if timeoutFor(5) != minTimeout || timeoutFor(1<<30) != maxTimeout || timeoutFor(7200) != 2*time.Hour {
		t.Fatal("timeout clamp")
	}
}

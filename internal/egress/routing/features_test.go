package routing

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/egress"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
)

func newRoutingStore(t *testing.T) *egress.Service {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "e.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	return egress.NewService(db, ring)
}

type fakeBridge struct {
	mu   sync.Mutex
	last BridgeConfig
	fail map[string]bool
}

func (b *fakeBridge) Configure(_ context.Context, cfg BridgeConfig) map[string]bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.last = cfg
	out := map[string]bool{}
	for _, s := range cfg.Bridges {
		if !b.fail[s.OutboundID] && len(cfg.Gateways) > 0 {
			out[s.OutboundID] = true
		}
	}
	return out
}

func (b *fakeBridge) Stats() map[string]BridgeStats { return nil }

func (h *hostRunner) ran(prefix string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, a := range h.argv {
		if strings.HasPrefix(strings.Join(a, " "), prefix) {
			return true
		}
	}
	return false
}

// An outbound with the IPv6 site bridge gets its IPv6 address and routes,
// the redirect and QUIC refusal render only for a listening bridge, and an
// exit that stops carrying traffic has its connections drained.
func TestManagerBridgesIPv6AndDrains(t *testing.T) {
	ctx := context.Background()
	store := newRoutingStore(t)
	conf := "[Interface]\nPrivateKey = " + keyA + "\nAddress = 172.16.0.2/32, 2606:4700:110:8a36::2/128\n[Peer]\nPublicKey = " + keyB + "\nEndpoint = 198.51.100.4:51820\n"
	ob, err := store.CreateWireGuard(ctx, "WARP-ish", conf)
	if err != nil {
		t.Fatal(err)
	}
	on := true
	if _, err := store.UpdateOutbound(ctx, ob.ID, egress.OutboundInput{IPv6Sites: &on}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRule(ctx, egress.RuleInput{Name: "AI", Enabled: true,
		Target: egress.Target{Kind: egress.TargetOutbound, ID: ob.ID}, Presets: []string{"gemini"}}); err != nil {
		t.Fatal(err)
	}
	host := &hostRunner{rules: map[string]bool{}}
	bridge := &fakeBridge{}
	var drained []uint32
	m := &Manager{Store: store, Backend: fake.New(), Run: host, Resolver: &nullResolver{}, Prober: &fixedProber{ok: map[uint32]bool{}},
		Bridge: bridge, Drain: func(mark uint32) error { drained = append(drained, mark); return nil }}
	if err := m.Apply(ctx, []Interface{{Name: "awg0", Pools: []string{"10.8.0.0/24"}}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ip -6 addr replace 2606:4700:110:8a36::2/128 dev wgo1",
		"ip -6 route replace default dev wgo1 table 51801",
		"ip -6 rule add priority 5180 fwmark 0x100000/0x7ff00000 table 51801",
	} {
		if !host.ran(want) {
			t.Fatalf("missing %q in %v", want, host.argv)
		}
	}
	script := host.lastScript()
	if !strings.Contains(script, "meta l4proto tcp th dport { 80, 443 } redirect to :51801") ||
		!strings.Contains(script, "== 0x00100000 udp dport 443 reject") {
		t.Fatalf("bridge rules missing:\n%s", script)
	}
	if len(bridge.last.Bridges) != 1 || bridge.last.Bridges[0].Port != 51801 || len(bridge.last.Gateways) != 1 {
		t.Fatalf("bridge config: %+v", bridge.last)
	}
	if o := m.Status().Outbounds[ob.ID]; !o.IPv6 || o.Bridge != "active" {
		t.Fatalf("status: %+v", o)
	}
	if len(drained) != 0 {
		t.Fatalf("drained on the first apply: %v", drained)
	}

	// A bridge that cannot listen gets no redirect: client connections are
	// never sent to a port nobody serves.
	bridge.fail = map[string]bool{ob.ID: true}
	if err := m.Reapply(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(host.lastScript(), "redirect to :51801") || m.Status().Outbounds[ob.ID].Bridge != "idle" {
		t.Fatalf("redirect kept for a bridge that does not listen:\n%s", host.lastScript())
	}

	off := false
	if _, err := store.UpdateOutbound(ctx, ob.ID, egress.OutboundInput{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if err := m.Reapply(ctx); err != nil {
		t.Fatal(err)
	}
	if len(drained) != 1 || drained[0] != SlotMark(ob.Slot) {
		t.Fatalf("drained %v, want the disabled outbound's mark", drained)
	}
	if !host.ran("ip -j -6 rule show") {
		t.Fatal("stale IPv6 rules are not cleaned up")
	}
}

// movedBackend reports a live endpoint address the name no longer has.
type movedBackend struct {
	*fake.Backend
	live string
}

func (b movedBackend) Dump(ctx context.Context, name string) (tunnel.InterfaceState, error) {
	st, err := b.Backend.Dump(ctx, name)
	if err == nil && len(st.Peers) == 1 {
		st.Peers[0].Endpoint = b.live
	}
	return st, err
}

// A down link whose endpoint name moved is set again; a name that still
// points at the live address, or a recent check, leaves it alone.
func TestReresolveMovedEndpoint(t *testing.T) {
	ctx := context.Background()
	store := newRoutingStore(t)
	conf := "[Interface]\nPrivateKey = " + keyA + "\nAddress = 10.7.0.2/32\n[Peer]\nPublicKey = " + keyB + "\nEndpoint = vpn.example.net:51820\n"
	ob, err := store.CreateWireGuard(ctx, "DDNS", conf)
	if err != nil {
		t.Fatal(err)
	}
	backend := movedBackend{Backend: fake.New(), live: "198.51.100.4:51820"}
	resolved := "198.51.100.4"
	m := &Manager{Store: store, Backend: backend, Run: &hostRunner{rules: map[string]bool{}}, Resolver: &nullResolver{},
		Lookup: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr(resolved)}, nil
		}}
	if err := m.Apply(ctx, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < failuresDown; i++ {
		m.record(ob.ID, ProbeResult{Error: "timeout"})
	}
	now := time.Now()
	if m.reresolve(ctx, now) {
		t.Fatal("unchanged name triggered a re-set")
	}
	resolved = "203.0.113.9"
	if m.reresolve(ctx, now.Add(time.Minute)) {
		t.Fatal("re-resolved before the pacing interval")
	}
	if !m.reresolve(ctx, now.Add(reresolveEvery+time.Second)) || m.applied["wgo1"] != "" {
		t.Fatal("moved endpoint not scheduled for a peer re-set")
	}
}

// warpSequence registers a new device per call.
type warpSequence struct{ n int }

func (w *warpSequence) Register(context.Context, string) (egress.WARPRegistration, error) {
	w.n++
	return egress.WARPRegistration{DeviceID: fmt.Sprintf("d%d", w.n), Token: "t", AccountType: "free", PeerPublicKey: keyB,
		Endpoint: "engage.cloudflareclient.com:2408", AddressV4: fmt.Sprintf("172.16.0.%d", 1+w.n), AddressV6: "2606:4700:110:8a36::2"}, nil
}

func (w *warpSequence) ApplyLicense(context.Context, string, string, string) (string, bool, error) {
	return "free", false, nil
}

type exitProber struct {
	mu   sync.Mutex
	exit []string
}

func (p *exitProber) Probe(context.Context, uint32) ProbeResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	ip := p.exit[0]
	if len(p.exit) > 1 {
		p.exit = p.exit[1:]
	}
	return ProbeResult{OK: true, ExitIP: ip}
}

// Rotation re-registers while Cloudflare hands out the same exit address.
func TestRotateWARPRetriesUntilExitChanges(t *testing.T) {
	ctx := context.Background()
	store := newRoutingStore(t)
	store.WARP = &warpSequence{}
	ob, err := store.CreateWARP(ctx, "WARP", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if ob.Tunnel.Address6 != "2606:4700:110:8a36::2/128" {
		t.Fatalf("IPv6 address not stored: %q", ob.Tunnel.Address6)
	}
	prober := &exitProber{exit: []string{"104.28.0.1", "104.28.0.1", "104.28.0.7"}}
	m := &Manager{Store: store, Backend: fake.New(), Run: &hostRunner{rules: map[string]bool{}}, Resolver: &nullResolver{}, Prober: prober}
	if err := m.Apply(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Test(ctx, ob.ID); err != nil {
		t.Fatal(err)
	}
	res, err := m.RotateWARP(ctx, ob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Attempts != 2 || !res.Changed || res.OldIP != "104.28.0.1" || res.NewIP != "104.28.0.7" {
		t.Fatalf("rotation: %+v", res)
	}
}

func TestSiteJudges(t *testing.T) {
	page := func(status int, rawURL, body string, header ...string) *SitePage {
		u, _ := url.Parse(rawURL)
		h := http.Header{}
		for i := 0; i+1 < len(header); i += 2 {
			h.Set(header[i], header[i+1])
		}
		return &SitePage{Status: status, Host: u.Host, Path: u.Path, Header: h, Body: []byte(body)}
	}
	judge := func(id string, main, second *SitePage) SiteJudgement {
		svc, ok := SiteServiceByID(id)
		if !ok {
			t.Fatalf("no service %s", id)
		}
		return svc.judge(main, second)
	}
	if j := judge("gemini", page(200, "https://gemini.google.com/app", `x WIZ_global_data 45631641,null,true ,2,1,200,"DEU"`), nil); j.Verdict != SiteAvailable || j.Region != "DEU" {
		t.Fatalf("gemini available: %+v", j)
	}
	// The current page: the flag is gone and the region decides.
	gem := `WIZ_global_data {\"45631641\":false} "",2,1,200,"%s",null`
	if j := judge("gemini", page(200, "https://gemini.google.com/", fmt.Sprintf(gem, "USA")), nil); j.Verdict != SiteAvailable || j.Region != "USA" {
		t.Fatalf("gemini supported region: %+v", j)
	}
	if j := judge("gemini", page(200, "https://gemini.google.com/", fmt.Sprintf(gem, "IRN")), nil); j.Verdict != SiteBlocked || j.Region != "IRN" {
		t.Fatalf("gemini unsupported region: %+v", j)
	}
	if j := judge("gemini", page(200, "https://gemini.google.com/", `WIZ_global_data`), nil); j.Verdict != SiteUnknown {
		t.Fatalf("gemini without flag or region: %+v", j)
	}
	if j := judge("gemini", page(200, "https://gemini.google.com/", `<html>maintenance</html>`), nil); j.Verdict != SiteUnknown {
		t.Fatalf("unrecognized page must not be judged: %+v", j)
	}
	if j := judge("google", page(429, "https://www.google.com/sorry/index", ""), nil); j.Verdict != SiteCaptcha {
		t.Fatalf("google sorry page: %+v", j)
	}
	// Claude reads Cloudflare's country from the trace endpoint.
	trace := "fl=1\nh=claude.ai\nip=203.0.113.9\nloc=%s\nwarp=off\n"
	if j := judge("claude", page(200, "https://claude.ai/cdn-cgi/trace", fmt.Sprintf(trace, "DE")), nil); j.Verdict != SiteAvailable || j.Region != "DE" {
		t.Fatalf("claude supported: %+v", j)
	}
	if j := judge("claude", page(200, "https://claude.ai/cdn-cgi/trace", fmt.Sprintf(trace, "IR")), nil); j.Verdict != SiteBlocked || j.Region != "IR" {
		t.Fatalf("claude unsupported: %+v", j)
	}
	if j := judge("claude", page(200, "https://claude.ai/cdn-cgi/trace", fmt.Sprintf(trace, "PR")), nil); j.Verdict != SiteAvailable {
		t.Fatalf("claude territory of a listed country: %+v", j)
	}
	if j := judge("claude", page(403, "https://claude.ai/cdn-cgi/trace", "<html>"), nil); j.Verdict != SiteUnknown {
		t.Fatalf("claude without trace: %+v", j)
	}
	if j := judge("chatgpt", page(403, "https://api.openai.com/", `{"error":{"code":"unsupported_country"}}`), nil); j.Verdict != SiteBlocked {
		t.Fatalf("openai region: %+v", j)
	}
	licensed := page(200, "https://www.netflix.com/title/81280792", `"id":"US","countryName":"United States"`)
	original := page(200, "https://www.netflix.com/title/70143836", `ok`)
	if j := judge("netflix", licensed, original); j.Verdict != SiteAvailable || j.Region != "US" {
		t.Fatalf("netflix full: %+v", j)
	}
	if j := judge("netflix", page(200, licensed.Host, "Oh no!"), page(200, original.Host, "Oh no!")); j.Verdict != SiteLimited {
		t.Fatalf("netflix originals only: %+v", j)
	}
	if j := judge("iplocation", page(200, "https://ipinfo.io/json", `{"country":"de","city":"Frankfurt","org":"AS13335 Cloudflare"}`), nil); j.Verdict != SiteAvailable || j.Region != "DE" || j.Note != "AS13335 Cloudflare · Frankfurt" {
		t.Fatalf("ipinfo: %+v", j)
	}
	// Custom URLs: an explicit region refusal is Blocked, a bare 403 is the
	// site firewall refusing the client, and a challenge page is a bot check.
	if j := judgeCustom(page(451, "https://example.com/", ""), nil); j.Verdict != SiteBlocked {
		t.Fatalf("451: %+v", j)
	}
	if j := judgeCustom(page(200, "https://example.com/", "This service is not available in your country"), nil); j.Verdict != SiteBlocked {
		t.Fatalf("region phrase: %+v", j)
	}
	if j := judgeCustom(page(403, "https://example.com/", "Access denied"), nil); j.Verdict != SiteRefused {
		t.Fatalf("bare 403: %+v", j)
	}
	if j := judgeCustom(page(404, "https://example.com/x", ""), nil); j.Verdict != SiteReachable {
		t.Fatalf("404 still answers: %+v", j)
	}
	if !botCheck(page(403, "https://claude.ai/", "<title>Just a moment...</title>", "Cf-Mitigated", "challenge")) {
		t.Fatal("cloudflare challenge header")
	}
	if !botCheck(page(503, "https://example.com/", `<script src="/cdn-cgi/challenge-platform/h/b/orchestrate"></script>`)) {
		t.Fatal("challenge platform marker")
	}
	if botCheck(page(403, "https://example.com/", "Access denied")) {
		t.Fatal("plain 403 is not a challenge")
	}
	if !cloudflareServed(page(200, "https://example.com/", "", "Server", "cloudflare")) {
		t.Fatal("cloudflare server header")
	}
	for _, bad := range []string{"", "ftp://x", "https://user:pw@example.com/", "https://"} {
		if _, err := ParseSiteURL(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if got, err := ParseSiteURL("gemini.google.com"); err != nil || got.String() != "https://gemini.google.com/" {
		t.Fatalf("bare host: %v %v", got, err)
	}
}

// The dialer never sends private, loopback or local destinations through an
// outbound, and prefers IPv6 only when asked.
func TestResolveFamilyFiltersDestinations(t *testing.T) {
	lookup := func(_ context.Context, network, _ string) ([]netip.Addr, error) {
		if network == "ip6" {
			return []netip.Addr{netip.MustParseAddr("fd00::1"), netip.MustParseAddr("2a00:1450:4001:80b::200e")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("142.250.185.78")}, nil
	}
	got, err := resolveFamily(context.Background(), lookup, "ip6", "gemini.google.com")
	if err != nil || len(got) != 1 || got[0].String() != "2a00:1450:4001:80b::200e" {
		t.Fatalf("ip6: %v %v", got, err)
	}
	got, err = resolveFamily(context.Background(), lookup, "ip4", "gemini.google.com")
	if err != nil || len(got) != 1 || got[0].String() != "142.250.185.78" {
		t.Fatalf("ip4: %v %v", got, err)
	}
	if _, err := resolveFamily(context.Background(), lookup, "ip4", "127.0.0.1"); err == nil {
		t.Fatal("loopback literal accepted")
	}
	for _, a := range []string{"100.64.1.1", "192.168.1.1", "fe80::1", "::1"} {
		if Routable(netip.MustParseAddr(a)) {
			t.Fatalf("%s routable", a)
		}
	}
}

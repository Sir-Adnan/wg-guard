package routing

import (
	"bytes"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
)

const (
	keyA = "eLGK3fnLmMcRvJyWdMVT9ULJ2WA4xGuefpmm34dPnHI="
	keyB = "3FlgEEAdrHAeVfmj7bJyMnaKQsHajUQd/Nd0iRKzwQ8="
)

func tunnelOutbound(id string, slot int, enabled bool) egress.OutboundState {
	return egress.OutboundState{
		Outbound: &egress.Outbound{ID: id, Name: id, Kind: egress.KindWARP, Slot: slot, Enabled: enabled,
			Tunnel: &egress.TunnelConfig{Address: "172.16.0.2/32", MTU: 1280, PeerPublicKey: keyB,
				Endpoint: "engage.cloudflareclient.com:2408", Keepalive: 25}},
		Secret: egress.Secret{PrivateKey: keyA},
	}
}

func sourceOutbound(id string, slot int) egress.OutboundState {
	return egress.OutboundState{Outbound: &egress.Outbound{ID: id, Name: id, Kind: egress.KindSourceIP, Slot: slot, Enabled: true, SourceIP: "203.0.113.7"}}
}

type fakeGeo struct {
	sites map[string][]egress.Matcher
	ips   map[string][]netip.Prefix
}

func (g fakeGeo) Domains(w []string) (map[string][]egress.Matcher, error) { return g.sites, nil }
func (g fakeGeo) Prefixes(w []string) (map[string][]netip.Prefix, error)  { return g.ips, nil }

var ifaces = []Interface{{Name: "awg0", Pools: []string{"10.8.0.0/24", "10.9.0.0/24"}}, {Name: "awg1", Pools: []string{"10.8.1.0/24"}}}

func scenario() *egress.Snapshot {
	return &egress.Snapshot{
		Outbounds: []egress.OutboundState{
			tunnelOutbound("o-warp", 1, true), tunnelOutbound("o-nl", 2, true), sourceOutbound("o-ip", 3),
			tunnelOutbound("o-off", 4, false),
		},
		Balancers: []*egress.Balancer{{ID: "00000000-0000-7000-8000-00000000000b", Name: "AI", Strategy: egress.StrategySticky,
			Fallback: egress.FallbackBlock, Enabled: true,
			Members: []egress.Member{{OutboundID: "o-warp", Weight: 2}, {OutboundID: "o-nl", Weight: 1}, {OutboundID: "o-off", Weight: 1}}}},
		Rules: []*egress.Rule{
			{ID: "00000000-0000-7000-8000-000000000001", Name: "Iran direct", Enabled: true, Target: egress.Direct, Matchers: "geoip:ir\ndomain:ir"},
			{ID: "00000000-0000-7000-8000-000000000002", Name: "AI", Enabled: true,
				Target:  egress.Target{Kind: egress.TargetBalancer, ID: "00000000-0000-7000-8000-00000000000b"},
				Presets: []string{"gemini"}, Matchers: "geosite:google-deepmind\nipdata.co\n8.8.4.0/24"},
			{ID: "00000000-0000-7000-8000-000000000003", Name: "Spotify", Enabled: true,
				Target: egress.Target{Kind: egress.TargetOutbound, ID: "o-ip"}, Presets: []string{"spotify"}},
			{ID: "00000000-0000-7000-8000-000000000004", Name: "Disabled", Enabled: false, Target: egress.Target{Kind: egress.TargetBlock}, Matchers: "example.com"},
			{ID: "00000000-0000-7000-8000-000000000005", Name: "Ads", Enabled: true, Target: egress.Target{Kind: egress.TargetBlock}, Matchers: "keyword:doubleclick"},
		},
		Settings: egress.Settings{DefaultTarget: egress.Target{Kind: egress.TargetOutbound, ID: "o-warp"}, BlockEncryptedDNS: true},
	}
}

var geo = fakeGeo{
	sites: map[string][]egress.Matcher{"google-deepmind": {{Type: egress.MatchDomain, Value: "deepmind.google"}}},
	ips:   map[string][]netip.Prefix{"ir": {netip.MustParsePrefix("5.160.0.0/13")}},
}

func TestCompileScenario(t *testing.T) {
	p := Compile(scenario(), ifaces, Inputs{Health: Health{}}, geo)
	if len(p.Links) != 2 || p.Links[0].Name != "wgo1" || p.Links[1].Name != "wgo2" {
		t.Fatalf("links: %+v", p.Links)
	}
	if len(p.Routes) != 2 || p.Routes[0].Table != 51801 || p.Routes[0].Mark != 0x00100000 {
		t.Fatalf("routes: %+v", p.Routes)
	}
	if len(p.SNAT) != 1 || p.SNAT[0].Address != "203.0.113.7" || p.SNAT[0].Mark != 0x00300000 {
		t.Fatalf("snat: %+v", p.SNAT)
	}
	if len(p.Gateways) != 3 || p.Gateways[1] != netip.MustParseAddr("10.9.0.1") {
		t.Fatalf("gateways: %v", p.Gateways)
	}
	if p.DefaultChain != "t_o1" || !p.BlockEncryptedDNS {
		t.Fatalf("default %q block %v", p.DefaultChain, p.BlockEncryptedDNS)
	}
	// Order: rule 1 (domains, prefixes), rule 2 (domains, prefixes), rule 3, rule 5.
	var got []string
	for _, c := range p.Classify {
		got = append(got, c.Set+">"+c.Chain)
	}
	want := []string{
		"r00000000000070008000000000000001_d>t_direct", "r00000000000070008000000000000001_c>t_direct",
		"r00000000000070008000000000000002_d>t_b0000000000007000800000000000000b", "r00000000000070008000000000000002_c>t_b0000000000007000800000000000000b",
		"r00000000000070008000000000000003_d>t_o3", "r00000000000070008000000000000005_d>t_block",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("classify:\n%v\nwant\n%v", got, want)
	}
	bal := p.Chains["t_b0000000000007000800000000000000b"]
	if bal != "jhash ip saddr mod 3 seed 0x57474744 vmap { 0 : goto t_o1, 1 : goto t_o1, 2 : goto t_o2 }" {
		t.Fatalf("weighted sticky balancer: %q (disabled member must be excluded)", bal)
	}
	if e, ok := p.Ruleset.Lookup("gemini.google.com"); !ok || e.RuleID != "00000000-0000-7000-8000-000000000002" {
		t.Fatal("gemini preset not in resolver ruleset")
	}
	if e, ok := p.Ruleset.Lookup("api.deepmind.google"); !ok || e.RuleID != "00000000-0000-7000-8000-000000000002" {
		t.Fatal("geosite category not expanded")
	}
	if e, ok := p.Ruleset.Lookup("shop.digikala.ir"); !ok || e.RuleID != "00000000-0000-7000-8000-000000000001" {
		t.Fatal("first rule must win for .ir")
	}
	if _, ok := p.Ruleset.Lookup("example.com"); ok {
		t.Fatal("disabled rule compiled")
	}
	if p.MSS["wgo1"] != 1240 {
		t.Fatalf("mss: %v", p.MSS)
	}
}

func TestCompileHealthFallbacks(t *testing.T) {
	snap := scenario()
	// WARP down: default fails open to direct, the balancer keeps NL only.
	p := Compile(snap, ifaces, Inputs{Health: Health{"o-warp": false}}, geo)
	if p.DefaultChain != "t_direct" {
		t.Fatalf("unhealthy default must fail open: %q", p.DefaultChain)
	}
	for _, c := range p.Classify {
		if c.RuleID == "00000000-0000-7000-8000-000000000002" && c.Chain != "t_o2" {
			t.Fatalf("balancer with one live member collapses to it: %q", c.Chain)
		}
	}
	// Every member down: the balancer's block fallback applies.
	p = Compile(snap, ifaces, Inputs{Health: Health{"o-warp": false}, Down: map[string]bool{"o-nl": true}}, geo)
	for _, c := range p.Classify {
		if c.RuleID == "00000000-0000-7000-8000-000000000002" && c.Chain != "t_block" {
			t.Fatalf("all members down must use fallback: %q", c.Chain)
		}
	}
	if _, ok := p.Chains["t_o2"]; ok {
		t.Fatal("unreferenced target chains must be pruned")
	}
	// Failover picks the first live member in order.
	snap.Balancers[0].Strategy = egress.StrategyFailover
	p = Compile(snap, ifaces, Inputs{Health: Health{}}, geo)
	for _, c := range p.Classify {
		if c.RuleID == "00000000-0000-7000-8000-000000000002" && c.Chain != "t_o1" {
			t.Fatalf("failover: %q", c.Chain)
		}
	}
}

func TestCompileWarningsAndNoTunnels(t *testing.T) {
	p := Compile(scenario(), ifaces, Inputs{Health: Health{}}, fakeGeo{})
	kinds := map[string]string{}
	for _, w := range p.Warnings {
		kinds[w.Kind] = w.Detail
	}
	if kinds["geosite_missing"] != "google-deepmind" || kinds["geoip_missing"] != "ir" {
		t.Fatalf("warnings: %+v", p.Warnings)
	}
	p = Compile(scenario(), nil, Inputs{Health: Health{}}, geo)
	if len(p.Classify) != 0 || len(p.Links) != 2 || p.Classifies() != true {
		t.Fatalf("without client tunnels only links/SNAT/MSS remain: %+v", p)
	}
}

// nftAvailable runs nft inside an unprivileged user+network namespace so the
// rendered transaction is checked by the real parser and kernel.
func nftNamespace(t *testing.T) func(script string) string {
	t.Helper()
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nft not installed")
	}
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("unshare not installed")
	}
	if out, err := exec.Command("unshare", "-rn", "nft", "list", "ruleset").CombinedOutput(); err != nil {
		t.Skipf("no user network namespace: %v %s", err, out)
	}
	dir := t.TempDir()
	return func(steps string) string {
		t.Helper()
		sh := filepath.Join(dir, "steps.sh")
		if err := os.WriteFile(sh, []byte("set -e\n"+steps), 0o700); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("unshare", "-rn", "sh", sh)
		cmd.Dir = dir
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("nft failed: %v\n%s", err, out.String())
		}
		return out.String()
	}
}

func TestRenderedTransactionsApplyAndPreserveResolvedSets(t *testing.T) {
	run := nftNamespace(t)
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	first := Compile(scenario(), ifaces, Inputs{Health: Health{}}, geo)
	s1 := write("one.nft", RenderTable(first, Inventory{}, true))

	// Second pass: one rule removed, the balancer replaced by failover and
	// WARP down — obsolete chains and sets must be deleted in the same
	// transaction while the surviving resolver set keeps its element.
	snap := scenario()
	snap.Rules = append(snap.Rules[:2], snap.Rules[3:]...)
	snap.Balancers[0].Strategy = egress.StrategyFailover
	second := Compile(snap, ifaces, Inputs{Health: Health{"o-warp": false}}, geo)
	inv := Inventory{Present: true, Chains: map[string]bool{}, Sets: map[string]bool{}}
	for _, n := range []string{"prerouting", "classify", "block_forward", "mss_clamp", "dns_redirect", "source_nat", "t_direct", "t_block", "t_o1", "t_o2", "t_o3", "t_b0000000000007000800000000000000b"} {
		inv.Chains[n] = true
	}
	for _, n := range []string{"reserved", "encrypted_dns", "r00000000000070008000000000000001_c", "r00000000000070008000000000000001_d",
		"r00000000000070008000000000000002_c", "r00000000000070008000000000000002_d", "r00000000000070008000000000000003_d", "r00000000000070008000000000000005_d"} {
		inv.Sets[n] = true
	}
	s2 := write("two.nft", RenderTable(second, inv, true))
	empty := Compile(&egress.Snapshot{Settings: egress.Settings{DefaultTarget: egress.Direct}}, ifaces, Inputs{Health: Health{}}, geo)
	if empty.Classifies() {
		t.Fatal("empty state must not need a table")
	}
	out := run("nft -f " + s1 + "\n" +
		"nft add element ip " + TableName + " r00000000000070008000000000000002_d '{ 142.250.1.1 timeout 2h }'\n" +
		"nft -f " + s2 + "\n" +
		"nft -t list table ip " + TableName + "\n" +
		"nft list set ip " + TableName + " r00000000000070008000000000000002_d\n")
	if !strings.Contains(out, "142.250.1.1") {
		t.Fatalf("resolver-fed element lost across apply:\n%s", out)
	}
	if strings.Contains(out, "r00000000000070008000000000000003_d") || strings.Contains(out, "chain t_b0000") {
		t.Fatalf("obsolete objects survived:\n%s", out)
	}
	if !strings.Contains(out, "goto t_o2") || !strings.Contains(out, "redirect to :53") || !strings.Contains(out, "snat to 203.0.113.7") {
		t.Fatalf("unexpected second table:\n%s", out)
	}
}

func TestBalancerStrategiesRender(t *testing.T) {
	run := nftNamespace(t)
	dir := t.TempDir()
	for _, tc := range []struct {
		strategy egress.Strategy
		want     string
	}{
		{egress.StrategySticky, "jhash ip saddr mod 3 seed 0x57474744 vmap"},
		{egress.StrategyRoundRobin, "numgen inc mod 3 vmap"},
		{egress.StrategyRandom, "numgen random mod 3 vmap"},
		{egress.StrategyLeastConn, "numgen random mod "},
	} {
		snap := scenario()
		snap.Balancers[0].Strategy = tc.strategy
		in := Inputs{Health: Health{}, Load: map[string]int64{"o-warp": 900, "o-nl": 10}}
		p := Compile(snap, ifaces, in, geo)
		body := p.Chains["t_b0000000000007000800000000000000b"]
		if !strings.HasPrefix(body, tc.want) {
			t.Fatalf("%s: %q", tc.strategy, body)
		}
		if tc.strategy == egress.StrategyLeastConn {
			// The loaded member keeps one bucket; the idle one takes the rest.
			if strings.Count(body, "goto t_o1") != 1 || strings.Count(body, "goto t_o2") < 10 {
				t.Fatalf("least_conn shares: %q", body)
			}
		}
		path := filepath.Join(dir, string(tc.strategy)+".nft")
		if err := os.WriteFile(path, RenderTable(p, Inventory{}, true), 0o600); err != nil {
			t.Fatal(err)
		}
		run("nft -f " + path + "\nnft delete table ip " + TableName + "\n")
	}
}

func TestLeastConnShares(t *testing.T) {
	if got := leastConnShares([]int{1, 1}, []int64{0, 0}); got[0] != got[1] {
		t.Fatalf("equal load keeps equal shares: %v", got)
	}
	if got := leastConnShares([]int{3, 1}, []int64{0, 0}); got[0] <= got[1] {
		t.Fatalf("weights apply at equal load: %v", got)
	}
	if got := leastConnShares([]int{1, 1}, []int64{1000, 0}); got[0] != 1 || got[1] < 15 {
		t.Fatalf("busy member must receive few new flows: %v", got)
	}
}

func TestMarkRulesKeepForeignBits(t *testing.T) {
	if MarkMask != 0x7ff00000 || markKeep != 0x800fffff {
		t.Fatalf("mark field: mask %#x keep %#x", MarkMask, markKeep)
	}
	p := Compile(scenario(), ifaces, Inputs{Health: Health{}}, geo)
	if got := p.Chains["t_o1"]; got != "meta mark set meta mark and 0x800fffff or 0x00100000 ct mark set ct mark and 0x800fffff or 0x00100000" {
		t.Fatalf("mark statements: %q", got)
	}
	table := string(RenderTable(p, Inventory{}, true))
	// Restore covers every enabled outbound (o-ip has no rule of its own)
	// plus direct and block, each with constant operands only.
	for _, want := range []string{
		"add rule ip " + TableName + " prerouting ct mark and 0x7ff00000 vmap { 0x7ff00000 : goto k_block, 0x7fe00000 : goto k_direct, 0x00100000 : goto k_o1, 0x00200000 : goto k_o2, 0x00300000 : goto k_o3 }",
		"add rule ip " + TableName + " k_o3 meta mark set meta mark and 0x800fffff or 0x00300000",
		"add rule ip " + TableName + " k_direct meta mark set meta mark and 0x800fffff or 0x7fe00000",
	} {
		if !strings.Contains(table, want) {
			t.Fatalf("missing %q in:\n%s", want, table)
		}
	}
	if strings.Contains(table, "k_o4") || strings.Contains(table, "meta mark set ct mark") {
		t.Fatalf("disabled outbound restored or foreign bits overwritten:\n%s", table)
	}
}

// A chained outbound's link carries the mark of the outbound it goes
// through; a broken chain gets no link and its traffic is not sent to it.
func TestCompileChainedOutbound(t *testing.T) {
	warp := tunnelOutbound("o-warp", 1, true)
	warp.Tunnel.Via = "o-de"
	de := tunnelOutbound("o-de", 2, true)
	snap := &egress.Snapshot{
		Outbounds: []egress.OutboundState{warp, de},
		Settings:  egress.Settings{DefaultTarget: egress.Target{Kind: egress.TargetOutbound, ID: "o-warp"}},
	}
	plan := Compile(snap, ifaces, Inputs{}, fakeGeo{})
	var link LinkSpec
	for _, l := range plan.Links {
		if l.OutboundID == "o-warp" {
			link = l
		}
	}
	if link.Config.Fwmark != "0x200000" || link.fwmark() != SlotMark(2) || plan.DefaultChain != "t_o1" {
		t.Fatalf("chained link: %+v default %s", link.Config, plan.DefaultChain)
	}
	plain := linkSpec(tunnelOutbound("o-warp", 1, true), 0)
	if plain.Hash == link.Hash {
		t.Fatal("the chain must change the link hash")
	}

	de.Enabled = false
	snap.Outbounds = []egress.OutboundState{warp, de}
	plan = Compile(snap, ifaces, Inputs{}, fakeGeo{})
	if len(plan.Links) != 0 || plan.Unlinked["o-warp"] != "via" || plan.DefaultChain != chainDirect {
		t.Fatalf("broken chain: links %v unlinked %v default %s", plan.Links, plan.Unlinked, plan.DefaultChain)
	}
	if len(plan.Warnings) != 1 || plan.Warnings[0].Kind != "via_unavailable" || plan.Warnings[0].Detail != "o-warp" {
		t.Fatalf("warnings: %+v", plan.Warnings)
	}
}

// A bridged outbound: its link carries IPv6, it has IPv6 routes and a
// bridge spec, an unbridged link keeps its hash, and the redirect and QUIC
// refusal load in the real nft.
func TestBridgedOutboundPlanAndRender(t *testing.T) {
	snap := scenario()
	var before string
	for i, o := range snap.Outbounds {
		if o.ID == "o-warp" {
			before = linkSpec(o, 0).Hash
			t2 := *o.Tunnel
			t2.Address6, t2.IPv6Sites = "2606:4700:110:8a36::2/128", true
			o.Outbound = &egress.Outbound{ID: o.ID, Name: o.Name, Kind: o.Kind, Slot: o.Slot, Enabled: true, Tunnel: &t2}
			snap.Outbounds[i] = o
		}
	}
	p := Compile(snap, ifaces, Inputs{Health: Health{}}, geo)
	if len(p.Bridges) != 1 || p.Bridges[0].OutboundID != "o-warp" || len(p.Routes6) != 1 {
		t.Fatalf("bridges %+v routes6 %+v", p.Bridges, p.Routes6)
	}
	for _, l := range p.Links {
		if l.OutboundID != "o-warp" {
			continue
		}
		if l.Address6 == "" || len(l.Config.Peers[0].AllowedIPs) != 2 || l.Hash == before {
			t.Fatalf("bridged link: %+v", l)
		}
	}
	unbridged := tunnelOutbound("x", 9, true)
	if linkSpec(unbridged, 0).Hash != linkSpec(tunnelOutbound("x", 9, true), 0).Hash {
		t.Fatal("hash not stable")
	}
	script := RenderTable(p, Inventory{}, true)
	if !strings.Contains(string(script), "chain ip "+TableName+" site_bridge") {
		t.Fatalf("no bridge chain:\n%s", script)
	}
	run := nftNamespace(t)
	path := filepath.Join(t.TempDir(), "b.nft")
	if err := os.WriteFile(path, script, 0o600); err != nil {
		t.Fatal(err)
	}
	out := run("nft -f " + path + "\nnft list table ip " + TableName + "\n")
	if !strings.Contains(out, "redirect to :") || !strings.Contains(out, "udp dport 443 reject") {
		t.Fatalf("bridge rules not loaded:\n%s", out)
	}
}

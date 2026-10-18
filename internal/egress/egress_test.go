package egress

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
)

const (
	testPriv = "eLGK3fnLmMcRvJyWdMVT9ULJ2WA4xGuefpmm34dPnHI="
	testPub  = "3FlgEEAdrHAeVfmj7bJyMnaKQsHajUQd/Nd0iRKzwQ8="
)

func newService(t *testing.T) *Service {
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
	return NewService(db, ring)
}

func reason(t *testing.T, err error) string {
	t.Helper()
	v, ok := AsValidation(err)
	if !ok {
		t.Fatalf("not a validation error: %v", err)
	}
	return v.Reason
}

func TestParseMatchers(t *testing.T) {
	got, err := ParseMatchers("# AI\nGemini.Google.com.\n*.openai.com\nfull:chat.com\nkeyword:claude\nregexp:^.+\\.ai$\n" +
		"geosite:google-deepmind\ngeosite:google@cn\n1.2.3.4\ncidr:10.0.0.0/8\ngeoip:IR\nexample.com # trailing comment\nexample.com\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Matcher{
		{MatchDomain, "gemini.google.com"}, {MatchDomain, "openai.com"}, {MatchFull, "chat.com"},
		{MatchKeyword, "claude"}, {MatchRegexp, `^.+\.ai$`}, {MatchGeosite, "google-deepmind"},
		{MatchGeosite, "google@cn"}, {MatchCIDR, "1.2.3.4/32"}, {MatchCIDR, "10.0.0.0/8"},
		{MatchGeoIP, "ir"}, {MatchDomain, "example.com"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d matchers: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("matcher %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	for line, r := range map[string]string{
		"bad domain!":   "matcher_invalid",
		"regexp:(":      "matcher_invalid",
		"2001:db8::/32": "matcher_ipv6",
		"0.0.0.0/0":     "matcher_everything",
		"unknown:x":     "matcher_invalid",
		"geosite:":      "matcher_invalid",
	} {
		_, err := ParseMatchers(line)
		if got := reason(t, err); got != r {
			t.Errorf("%q: reason %q, want %q", line, got, r)
		}
		if v, _ := AsValidation(err); r == "matcher_invalid" && v.Detail != line {
			t.Errorf("%q: detail %q", line, v.Detail)
		}
	}
	many := strings.Repeat("regexp:a\n", 0)
	for i := 0; i <= maxRegexpPerRule; i++ {
		many += "regexp:a" + strings.Repeat("b", i) + "\n"
	}
	if reason(t, func() error { _, err := ParseMatchers(many); return err }()) != "matchers_too_many_regexp" {
		t.Fatal("regexp budget not enforced")
	}
}

func TestPresetsAreValidAndUnique(t *testing.T) {
	ids := map[string]bool{}
	for _, p := range Presets() {
		if ids[p.ID] {
			t.Fatalf("duplicate preset %s", p.ID)
		}
		ids[p.ID] = true
		if _, err := ParseMatchers(strings.Join(p.Lines, "\n")); err != nil {
			t.Fatalf("preset %s: %v", p.ID, err)
		}
		if len(PresetMatchers([]string{p.ID})) == 0 {
			t.Fatalf("preset %s expands to nothing", p.ID)
		}
	}
	if got := PresetMatchers([]string{"gemini", "retired-preset"}); len(got) == 0 {
		t.Fatal("unknown preset IDs must be skipped, not fail")
	}
}

func TestParseClientConfig(t *testing.T) {
	conf := "[Interface]\nPrivateKey = " + testPriv + "\nAddress = 10.66.66.2/32, fd00::2/128\nDNS = 1.1.1.1\nMTU = 1380\n" +
		"Jc = 4\nJmin = 40\nJmax = 70\nS1 = 0\nS2 = 0\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n\n" +
		"[Peer]\nPublicKey = " + testPub + "\nPresharedKey = " + testPriv + "\nAllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = vpn.example.net:51820\nPersistentKeepalive = 15\n"
	p, err := ParseClientConfig(conf)
	if err != nil {
		t.Fatal(err)
	}
	if p.Tunnel.Address != "10.66.66.2/32" || p.Tunnel.MTU != 1380 || p.Tunnel.Endpoint != "vpn.example.net:51820" ||
		p.Tunnel.Keepalive != 15 || !p.Tunnel.Obfuscation.Enabled || p.Tunnel.Obfuscation.Jc != 4 {
		t.Fatalf("tunnel: %+v", p.Tunnel)
	}
	if p.Secret.PrivateKey != testPriv || p.Secret.PresharedKey != testPriv {
		t.Fatal("secret half")
	}
	plain := "[Interface]\nPrivateKey = " + testPriv + "\nAddress = 172.16.0.2/32\n[Peer]\nPublicKey = " + testPub + "\nEndpoint = 162.159.192.1:2408\n"
	if p, err := ParseClientConfig(plain); err != nil || p.Tunnel.MTU != DefaultTunnelMTU || p.Tunnel.Obfuscation.Enabled || p.Tunnel.Keepalive != 25 {
		t.Fatalf("plain: %+v %v", p, err)
	}
	if p, err := ParseClientConfig(plain + "PersistentKeepalive = 0\n"); err != nil || p.Tunnel.Keepalive != 0 {
		t.Fatalf("explicit PersistentKeepalive = 0 must stay off: %+v %v", p.Tunnel, err)
	}
	for conf, r := range map[string]string{
		"[Interface]\nPrivateKey = nope\n":                                            "config_key",
		plain + "[Peer]\nPublicKey = " + testPub + "\n":                               "config_multiple_peers",
		"[Interface]\nPrivateKey = " + testPriv + "\n":                                "config_incomplete",
		strings.Replace(plain, "Endpoint = 162.159.192.1:2408", "Endpoint = host", 1): "endpoint_invalid",
		plain + "[Interface]\nPostUp = x\nBogus = 1\n":                                "config_unsupported_key",
		strings.Replace(plain, "Address = 172.16.0.2/32", "Address = fd00::2/128", 1): "config_address",
	} {
		_, err := ParseClientConfig(conf)
		if got := reason(t, err); got != r {
			t.Errorf("reason %q, want %q for\n%s", got, r, conf)
		}
		if err != nil && strings.Contains(err.Error(), testPriv) {
			t.Fatal("error leaks key material")
		}
	}
}

type fakeWARP struct {
	licenseErr error
}

func (f fakeWARP) Register(context.Context, string) (WARPRegistration, error) {
	return WARPRegistration{DeviceID: "dev", Token: "tok", AccountType: "free", PeerPublicKey: testPub,
		Endpoint: "engage.cloudflareclient.com:2408", AddressV4: "172.16.0.2"}, nil
}

func (f fakeWARP) ApplyLicense(context.Context, string, string, string) (string, bool, error) {
	return "unlimited", true, f.licenseErr
}

func TestServiceLifecycleAndReferences(t *testing.T) {
	svc := newService(t)
	svc.WARP = fakeWARP{}
	ctx := context.Background()

	warp, err := svc.CreateWARP(ctx, "WARP", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if warp.Slot != 1 || warp.LinkName() != "wgo1" || warp.Tunnel.Address != "172.16.0.2/32" || warp.WARP.Plus {
		t.Fatalf("warp: %+v %+v", warp, warp.Tunnel)
	}
	sec, err := svc.OutboundSecret(ctx, warp.ID)
	if err != nil || sec.WARPToken != "tok" || sec.PrivateKey == "" {
		t.Fatalf("secret: %+v %v", sec, err)
	}
	var raw []byte
	if err := svc.db.QueryRow(`SELECT secret_encrypted FROM egress_outbounds WHERE id = ?`, warp.ID).Scan(&raw); err != nil || strings.Contains(string(raw), "tok") {
		t.Fatal("secret must be sealed at rest")
	}
	if _, err := svc.CreateWARP(ctx, "WARP", "", ""); reason(t, err) != "name_taken" {
		t.Fatal("duplicate name")
	}
	plus, err := svc.UpdateWARPLicense(ctx, warp.ID, "a1b2c3d4-e5f6a7b8-c9d0e1f2")
	if err != nil || !plus.WARP.Plus || plus.WARP.AccountType != "unlimited" {
		t.Fatalf("license: %+v %v", plus, err)
	}
	if _, err := svc.UpdateWARPLicense(ctx, warp.ID, "short"); reason(t, err) != "license_invalid" {
		t.Fatal("license syntax")
	}

	conf := "[Interface]\nPrivateKey = " + testPriv + "\nAddress = 10.7.0.2/32\n[Peer]\nPublicKey = " + testPub + "\nEndpoint = 198.51.100.4:51820\n"
	nl, err := svc.CreateWireGuard(ctx, "Netherlands", conf)
	if err != nil || nl.Slot != 2 {
		t.Fatalf("wireguard: %+v %v", nl, err)
	}
	src, err := svc.CreateSourceIP(ctx, "Second IP", "203.0.113.7")
	if err != nil || src.Slot != 3 {
		t.Fatalf("source ip: %+v %v", src, err)
	}
	if _, err := svc.CreateSourceIP(ctx, "Bad", "10.0.0.300"); reason(t, err) != "source_ip_invalid" {
		t.Fatal("source ip validation")
	}

	bal, err := svc.CreateBalancer(ctx, BalancerInput{Name: "AI pool", Strategy: StrategySticky, Enabled: true,
		Members: []Member{{OutboundID: warp.ID}, {OutboundID: nl.ID, Weight: 3}}})
	if err != nil || bal.Fallback != FallbackDirect || bal.Members[0].Weight != 1 {
		t.Fatalf("balancer: %+v %v", bal, err)
	}
	if _, err := svc.CreateBalancer(ctx, BalancerInput{Name: "Dup", Strategy: StrategySticky,
		Members: []Member{{OutboundID: warp.ID}, {OutboundID: warp.ID}}}); reason(t, err) != "members_duplicate" {
		t.Fatal("duplicate member")
	}

	r1, err := svc.CreateRule(ctx, RuleInput{Name: "AI", Enabled: true, Target: Target{Kind: TargetBalancer, ID: bal.ID},
		Presets: []string{"openai", "gemini", "gemini"}, Matchers: "geosite:google-deepmind\r\nipdata.co"})
	if err != nil || len(r1.Presets) != 2 || r1.Presets[0] != "gemini" || r1.Position != 1 || strings.Contains(r1.Matchers, "\r") {
		t.Fatalf("rule: %+v %v", r1, err)
	}
	r2, err := svc.CreateRule(ctx, RuleInput{Name: "Spotify", Enabled: true, Target: Target{Kind: TargetOutbound, ID: src.ID}, Presets: []string{"spotify"}})
	if err != nil || r2.Position != 2 {
		t.Fatalf("rule 2: %+v %v", r2, err)
	}
	if _, err := svc.CreateRule(ctx, RuleInput{Name: "Empty", Target: Direct}); reason(t, err) != "rule_empty" {
		t.Fatal("empty rule")
	}
	if _, err := svc.CreateRule(ctx, RuleInput{Name: "Ghost", Target: Target{Kind: TargetOutbound, ID: "missing"}, Matchers: "x.com"}); reason(t, err) != "target_unknown" {
		t.Fatal("unknown target")
	}
	if err := svc.MoveRule(ctx, r2.ID, -1); err != nil {
		t.Fatal(err)
	}
	rules, _ := svc.ListRules(ctx)
	if rules[0].ID != r2.ID || rules[1].ID != r1.ID {
		t.Fatal("move")
	}

	if err := svc.UpdateSettings(ctx, Settings{DefaultTarget: Target{Kind: TargetOutbound, ID: warp.ID}, BlockEncryptedDNS: true}); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateSettings(ctx, Settings{DefaultTarget: Target{Kind: TargetBlock}}); reason(t, err) != "target_invalid" {
		t.Fatal("blocking everything by default must be refused")
	}
	if err := svc.UpdateSettings(ctx, Settings{DefaultTarget: Direct, GeositeURL: "http://insecure.example/x.dat"}); reason(t, err) != "url_invalid" {
		t.Fatal("http data URL must be refused")
	}

	err = svc.DeleteOutbound(ctx, warp.ID)
	if v, ok := AsValidation(err); !ok || v.Reason != "in_use" || !strings.Contains(v.Detail, "AI pool") || !strings.Contains(v.Detail, "default") {
		t.Fatalf("referenced outbound deleted: %v", err)
	}
	if err := svc.DeleteBalancer(ctx, bal.ID); reason(t, err) != "in_use" {
		t.Fatal("referenced balancer deleted")
	}
	if err := svc.DeleteRule(ctx, r1.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBalancer(ctx, bal.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateSettings(ctx, Settings{DefaultTarget: Direct}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteOutbound(ctx, warp.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteOutbound(ctx, warp.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	// Freed slots are reused.
	again, err := svc.CreateSourceIP(ctx, "Third IP", "203.0.113.8")
	if err != nil || again.Slot != 1 {
		t.Fatalf("slot reuse: %+v %v", again, err)
	}

	snap, err := svc.LoadSnapshot(ctx)
	if err != nil || len(snap.Outbounds) != 3 || len(snap.Rules) != 1 {
		t.Fatalf("snapshot: %+v %v", snap, err)
	}
	for _, o := range snap.Outbounds {
		if (o.ID == nl.ID) != (o.Secret.PrivateKey == testPriv) {
			t.Fatalf("snapshot secret for %s: %+v", o.Name, o.Secret)
		}
	}
}

func TestLinkNames(t *testing.T) {
	for name, want := range map[string]bool{"wgo1": true, "wgo63": true, "wgo64": false, "wgo0": false, "wgo01": false, "awg0": false, "wgo": false, "wgo1a": false} {
		if IsLinkName(name) != want {
			t.Errorf("IsLinkName(%q) != %v", name, want)
		}
	}
	for _, s := range []string{"direct", "block", "outbound:abc", "balancer:x"} {
		tgt, err := ParseTarget(s)
		if err != nil || tgt.String() != s {
			t.Fatalf("target %q: %v", s, err)
		}
	}
	for _, s := range []string{"", "direct:x", "outbound:", "proxy:1"} {
		if _, err := ParseTarget(s); err == nil {
			t.Fatalf("accepted target %q", s)
		}
	}
}

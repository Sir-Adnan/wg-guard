package egress

import (
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
)

const testHPK = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

// AmneziaWG configs as the official clients and servers export them parse
// losslessly, with the values the pinned runtimes accept.
func TestParseClientConfigAmneziaWG(t *testing.T) {
	awg2 := "\uFEFF[Interface]\r\nPrivateKey = " + testPriv + "\r\nAddress = 10.8.1.2/32\r\nDNS = 1.1.1.1, 1.0.0.1\r\n" +
		"Jc = 6\r\nJmin = 20\r\nJmax = 80\r\nS1 = 32\r\nS2 = 64\r\nS3 = 24\r\nS4 = 32\r\n" +
		"H1 = 100000-200000\r\nH2 = 300000 - 400000\r\nH3 = 500000\r\nH4 = 600000-700000\r\n" +
		"I1 = <b 0xc70000000108ce1bf31eec7d93360000449e227e4596ed7f75c4d35ce31880b4133107c822c6355d51f0d7c1bba96d5c210a48aca01885fed0871cfc37d59137d73b506dc013bb4a13c060ca5b04b7ae215af71e37d6e8ff1db235f9fe0c25cb8b492471054a7c8d0d6077d430d07f6e87a8699287f6e69f54263c7334a8e144a29851429bf2e350e519445172d36953e96085110ce1fb641e5efad42c0feb4711ece959b72cc4d6f3c1e83251adb572b921534f6ac4b10927167f41fe50040a75acef62f45bded67c0b45b9d655ce374589cad6f6b6b16b22be3f4c1b15ee6f1f04c3b4f2c4f6b1d8a6b2f9c5d8><r 16><t>  # QUIC initial\r\n" +
		"I2 = <b 0xf6ab3267fa><c><rc 8><rd 4>\r\nHeaderProtectionKey = " + testHPK + "\r\n" +
		"ContentPaddingAddition = 10-35\r\nRekeyAfterTime = 100-120\r\nRekeyTimeout = 3-7\r\nRejectAfterTime = 150-180\r\n" +
		"KeepaliveTimeout = 5-15\r\nMaxHandshakeAttempts = 15-20\r\nRandomTrailers = on\r\nDisableCookies = 1\r\n\r\n" +
		"[Peer]\r\nPublicKey = " + testPub + "\r\nPresharedKey = " + testPriv + "\r\nAllowedIPs = 0.0.0.0/0, ::/0\r\n" +
		"Endpoint = 203.0.113.7:51820\r\nPersistentKeepalive = 25-35\r\nAdvancedSecurity = on\r\n"
	p, err := ParseClientConfig(awg2)
	if err != nil {
		t.Fatal(err)
	}
	o := p.Tunnel.Obfuscation
	h2, _ := awgparam.NewU32Range(300000, 400000)
	if !o.Enabled || o.Jc != 6 || o.Jmin != 20 || o.Jmax != 80 || o.S1 != 32 || o.S2 != 64 || o.S3 != 24 || o.S4 != 32 ||
		o.H2 != h2 || o.H3 != awgparam.ScalarU32(500000) || o.HeaderProtectionKey != testHPK ||
		!o.RandomTrailers || !o.DisableCookies || o.ContentPaddingAddition.String() != "10-35" || o.MaxHandshakeAttempts.String() != "15-20" {
		t.Fatalf("obfuscation: %+v", o)
	}
	if !strings.HasPrefix(o.I1, "<b 0xc7") || !strings.HasSuffix(o.I1, "<r 16><t>") || o.I2 != "<b 0xf6ab3267fa><c><rc 8><rd 4>" {
		t.Fatalf("signature packets: %q %q", o.I1, o.I2)
	}
	if p.Tunnel.Keepalive != 25 || p.Tunnel.KeepaliveMax != 35 || p.Tunnel.Endpoint != "203.0.113.7:51820" {
		t.Fatalf("peer: %+v", p.Tunnel)
	}
	if strings.Join(p.Ignored, ",") != "DNS,AllowedIPs,AdvancedSecurity" {
		t.Fatalf("ignored: %v", p.Ignored)
	}

	// AmneziaWG 1.5 WARP-style profile: junk only, legacy keys, no H lines.
	legacy := "[Interface]\nPrivateKey=" + testPriv + "\nAddress=172.16.0.2/32,2606:4700:110:8a36::2/128\n" +
		"Jc=4\nJmin=40\nJmax=70\nJ1=<r 2><b 0x858000010001000000000669636c6f756404636f6d0000010001c00c000100010000105a00044d583737>\nItime=0\n" +
		"[Peer]\nPublicKey=" + testPub + "\nEndpoint=engage.cloudflareclient.com:2408\n"
	p, err = ParseClientConfig(legacy)
	if err != nil {
		t.Fatal(err)
	}
	o = p.Tunnel.Obfuscation
	if !o.Enabled || o.Jc != 4 || o.H1 != awgparam.ScalarU32(1) || o.H4 != awgparam.ScalarU32(4) || p.Tunnel.Address != "172.16.0.2/32" {
		t.Fatalf("junk-only profile must get the stock headers: %+v", o)
	}
	if strings.Join(p.Ignored, ",") != "J1,Itime" {
		t.Fatalf("ignored: %v", p.Ignored)
	}

	// Jc = 0 with padding is still an obfuscated set; all-stock values are plain.
	padded := strings.Replace(legacy, "Jc=4", "Jc=0\nS1=15\nS2=64", 1)
	if p, err = ParseClientConfig(padded); err != nil || !p.Tunnel.Obfuscation.Enabled || p.Tunnel.Obfuscation.S2 != 64 {
		t.Fatalf("Jc=0 with padding: %+v %v", p.Tunnel.Obfuscation, err)
	}
	stock := "[Interface]\nPrivateKey = " + testPriv + "\nAddress = 172.16.0.2/32\nJc = 0\nJmin = 0\nJmax = 0\nS1 = 0\nS2 = 0\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n" +
		"[Peer]\nPublicKey = " + testPub + "\nEndpoint = 162.159.192.1:2408\nPersistentKeepalive = off\n"
	if p, err = ParseClientConfig(stock); err != nil || p.Tunnel.Obfuscation.Enabled || p.Tunnel.Keepalive != 0 {
		t.Fatalf("stock values are plain WireGuard: %+v %v", p.Tunnel, err)
	}
}

// Rejections name the key, never a value.
func TestParseClientConfigRejections(t *testing.T) {
	base := "[Interface]\nPrivateKey = " + testPriv + "\nAddress = 10.0.0.2/32\n%s\n[Peer]\nPublicKey = " + testPub + "\nEndpoint = 198.51.100.1:443\n"
	for extra, want := range map[string][2]string{
		"H1 = 5-10\nH2 = 8-20":             {"config_headers_overlap", "H1, H2"},
		"Jc = 3\nJmin = 90\nJmax = 40":     {"config_junk_range", "Jmin, Jmax"},
		"Jc = 200\nJmin = 1\nJmax = 2":     {"config_obfuscation", "Jc"},
		"S1 = 70000":                       {"config_obfuscation", "S1"},
		"H3 = 0":                           {"config_obfuscation", "H3"},
		"I1 = <x 12>":                      {"config_obfuscation", "I1"},
		"I1 = 0xdeadbeef":                  {"config_obfuscation", "I1"},
		"HeaderProtectionKey = c2hvcnQ=":   {"config_obfuscation", "HeaderProtectionKey"},
		"HeaderProtectionKey = " + testHPK: {"config_header_protection", "S1–S4"},
		"RandomTrailers = maybe":           {"config_obfuscation", "RandomTrailers"},
		"MTU = 1380\nMTU = 1280":           {"config_duplicate_key", "MTU"},
		"Bogus = 1":                        {"config_unsupported_key", "bogus"},
	} {
		_, err := ParseClientConfig(strings.Replace(base, "%s", extra, 1))
		v, ok := AsValidation(err)
		if !ok || v.Reason != want[0] || v.Detail != want[1] {
			t.Errorf("%q: got %v, want %s (%s)", extra, err, want[0], want[1])
		}
		if err != nil && strings.Contains(err.Error(), testPriv) {
			t.Fatal("error leaks key material")
		}
	}
	if _, err := ParseClientConfig(strings.Replace(base, "%s", "", 1) + "PersistentKeepalive = 0-9\n"); reason(t, err) != "config_value" {
		t.Errorf("a keepalive range from 0 is invalid: %v", err)
	}
	if _, err := ParseClientConfig("[Interface]\nPrivateKey = " + testPriv + "\nAddress = 10.0.0.2/32\n"); reason(t, err) != "config_missing" {
		t.Errorf("missing [Peer]: %v", err)
	}
	if _, err := ParseClientConfig("[Interfaces]\n"); reason(t, err) != "config_unknown_section" {
		t.Errorf("unknown section: %v", err)
	}
}

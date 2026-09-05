package amneziawg

import (
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

const (
	testPriv = "6BqSrxh0chPmL0pD5t62Lh7cSQtU4hS37xjKORTLLkc="
	testPub  = "eR0MzG2HjgbJHmBEnpjVACJ0mmC4WdLwYfUY3ZiT7Uw="
)

func obf() tunnel.Obfuscation {
	return tunnel.Obfuscation{
		Enabled: true,
		Jc:      5, Jmin: 40, Jmax: 70, S1: 86, S2: 61,
		H1: awgparam.ScalarU32(1234567), H2: awgparam.ScalarU32(2345678),
		H3: awgparam.ScalarU32(3456789), H4: awgparam.ScalarU32(4567890),
	}
}

func testU32Range(t *testing.T, text string) awgparam.U32Range {
	t.Helper()
	value, err := awgparam.ParseU32Range(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func testU16Range(t *testing.T, text string) awgparam.U16Range {
	t.Helper()
	value, err := awgparam.ParseU16Range(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestRenderSetconfObfuscated(t *testing.T) {
	out := string(renderSetconf(tunnel.InterfaceConfig{
		PrivateKey:  testPriv,
		ListenPort:  39411,
		Obfuscation: obf(),
	}))
	for _, want := range []string{
		"[Interface]\n",
		"PrivateKey = " + testPriv + "\n",
		"ListenPort = 39411\n",
		"Jc = 5\n", "Jmin = 40\n", "Jmax = 70\n",
		"S1 = 86\n", "S2 = 61\n",
		"H1 = 1234567\n", "H2 = 2345678\n", "H3 = 3456789\n", "H4 = 4567890\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "I1 =") {
		t.Errorf("I1–I5 must be omitted when unset:\n%s", out)
	}
}

func TestRenderSetconfPlainWritesExplicitZeros(t *testing.T) {
	out := string(renderSetconf(tunnel.InterfaceConfig{
		PrivateKey: testPriv,
		ListenPort: 40000,
	}))
	// The pinned runtime rejects explicit zeros with EINVAL, so a plain
	// profile must omit the obfuscation block entirely.
	for _, forbidden := range []string{"Jc =", "Jmin =", "Jmax =", "S1 =", "S2 =", "H1 =", "H4 ="} {
		if strings.Contains(out, forbidden) {
			t.Errorf("plain profile must omit %q — runtime rejects zeros:\n%s", forbidden, out)
		}
	}
	if !strings.Contains(out, "ListenPort = 40000\n") {
		t.Errorf("listen port missing:\n%s", out)
	}
}

func TestRenderSetconfIParamsOptIn(t *testing.T) {
	o := obf()
	o.I1 = "aabbccdd"
	o.I3 = "1122"
	out := string(renderSetconf(tunnel.InterfaceConfig{PrivateKey: testPriv, Obfuscation: o}))
	if !strings.Contains(out, "I1 = aabbccdd\n") || !strings.Contains(out, "I3 = 1122\n") {
		t.Fatalf("set I params missing:\n%s", out)
	}
	if strings.Contains(out, "I2 =") || strings.Contains(out, "I4 =") || strings.Contains(out, "I5 =") {
		t.Fatalf("unset I params must be omitted:\n%s", out)
	}
}

func TestRenderSetconfFwmark(t *testing.T) {
	out := string(renderSetconf(tunnel.InterfaceConfig{PrivateKey: testPriv, ListenPort: 1, Fwmark: "0x1234"}))
	if !strings.Contains(out, "FwMark = 0x1234\n") {
		t.Fatalf("fwmark missing:\n%s", out)
	}
}

func TestRenderSetconfPeers(t *testing.T) {
	out := string(renderSetconf(tunnel.InterfaceConfig{
		PrivateKey: testPriv,
		ListenPort: 39411,
		Peers: []tunnel.PeerConfig{
			{
				PublicKey:           testPub,
				PresharedKey:        "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
				AllowedIPs:          []string{"10.8.0.2/32", "10.8.0.3/32"},
				Endpoint:            "203.0.113.7:51820",
				PersistentKeepalive: testU16Range(t, "25-35"),
			},
			{PublicKey: testPub, AllowedIPs: []string{"10.8.0.4/32"}},
		},
	}))
	if !strings.Contains(out, "PresharedKey = AAAAAAAA") {
		t.Fatalf("psk missing:\n%s", out)
	}
	if !strings.Contains(out, "AllowedIPs = 10.8.0.2/32, 10.8.0.3/32\n") {
		t.Fatalf("allowed ips missing:\n%s", out)
	}
	if !strings.Contains(out, "Endpoint = 203.0.113.7:51820\n") ||
		!strings.Contains(out, "PersistentKeepalive = 25-35\n") {
		t.Fatalf("endpoint/keepalive missing:\n%s", out)
	}
	// Second peer omits optional fields entirely (syncconf keeps stored values).
	if strings.Count(out, "PresharedKey =") != 1 || strings.Count(out, "Endpoint =") != 1 {
		t.Fatalf("optional fields must be omitted when unset:\n%s", out)
	}
}

func TestRenderSyncconfPeersOnly(t *testing.T) {
	out := string(renderSyncconf([]tunnel.PeerConfig{
		{PublicKey: testPub, AllowedIPs: []string{"10.8.0.2/32"}},
	}))
	if strings.Contains(out, "[Interface]") || strings.Contains(out, "PrivateKey") {
		t.Fatalf("syncconf file must not touch the interface section:\n%s", out)
	}
	if !strings.Contains(out, "[Peer]\nPublicKey = "+testPub+"\n") {
		t.Fatalf("peer missing:\n%s", out)
	}
}

func TestRenderSyncconfEmptyRemovesAllPeers(t *testing.T) {
	if out := string(renderSyncconf(nil)); out != "" {
		t.Fatalf("empty peer list must render an empty file, got %q", out)
	}
}

func TestComposeSyncconfPreservesCompleteInterfaceSection(t *testing.T) {
	current := []byte("[Interface]\nPrivateKey = " + testPriv +
		"\nListenPort = 39411\nJc = 5\nH1 = 11-20\n\n" +
		"[Peer]\nPublicKey = stale-peer-key=\nAllowedIPs = 10.8.0.9/32\n")
	out, err := composeSyncconf(current, []tunnel.PeerConfig{
		{PublicKey: testPub, AllowedIPs: []string{"10.8.0.2/32"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, want := range []string{
		"[Interface]\n", "PrivateKey = " + testPriv + "\n", "ListenPort = 39411\n",
		"Jc = 5\n", "H1 = 11-20\n", "[Peer]\nPublicKey = " + testPub + "\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("composed syncconf missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "stale-peer-key=") {
		t.Fatalf("stale peer survived composition:\n%s", text)
	}
}

func TestRenderSetconfGatedParams(t *testing.T) {
	cfg := tunnel.InterfaceConfig{
		PrivateKey: "8N8eM9uMx9WcXWvOHbiu4B9kB8eSvbG3wfZugvwtCWU=",
		ListenPort: 39001,
		Obfuscation: tunnel.Obfuscation{
			Enabled: true,
			Jc:      4, Jmin: 40, Jmax: 70, S1: 15, S2: 64,
			H1: testU32Range(t, "11-20"), H2: awgparam.ScalarU32(22),
			H3: awgparam.ScalarU32(33), H4: awgparam.ScalarU32(44),
			S3:                     40,
			S4:                     48,
			HeaderProtectionKey:    "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			ContentPaddingAddition: testU16Range(t, "10-20"),
			RekeyAfterTime:         testU16Range(t, "120-180"),
			RekeyTimeout:           testU16Range(t, "15-25"),
			RejectAfterTime:        awgparam.ScalarU16(90),
			KeepaliveTimeout:       testU16Range(t, "30-45"),
			MaxHandshakeAttempts:   testU16Range(t, "4-8"),
			RandomTrailers:         true,
			DisableCookies:         true,
		},
	}
	out := string(renderSetconf(cfg))
	for _, want := range []string{
		"H1 = 11-20\n", "S3 = 40\n", "S4 = 48\n", "HeaderProtectionKey = AAAAAAAA", "ContentPaddingAddition = 10-20\n",
		"RekeyAfterTime = 120-180\n", "RekeyTimeout = 15-25\n", "RejectAfterTime = 90\n",
		"KeepaliveTimeout = 30-45\n", "MaxHandshakeAttempts = 4-8\n",
		"RandomTrailers = on\n", "DisableCookies = on\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q", want)
		}
	}
	// Plain profiles omit the gated block entirely (explicit zeros are
	// rejected by the runtime).
	plain := string(renderSetconf(tunnel.InterfaceConfig{
		PrivateKey: cfg.PrivateKey, Obfuscation: tunnel.Obfuscation{Enabled: false},
	}))
	if strings.Contains(plain, "RandomTrailers") || strings.Contains(plain, "S3") {
		t.Error("plain profile must not carry gated keys")
	}
}

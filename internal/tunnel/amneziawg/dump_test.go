package amneziawg

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// The fixture was captured from the pinned userspace daemon (WSL2, 2026-08-29)
// and is the golden input for the parser.
const fixturePath = "../../../docs/integrations/fixtures/dump-awg0-userspace.txt"

func loadFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return data
}

func TestParseDumpFixture(t *testing.T) {
	st, err := parseDump("awg0", loadFixture(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if st.Name != "awg0" {
		t.Fatalf("name = %q", st.Name)
	}
	if st.PublicKey != "HmFweuq2rTVwdSFIV60UeFB3CRHafUv9T1jdAW4vQVw=" {
		t.Fatalf("public key = %q", st.PublicKey)
	}
	if st.ListenPort != 39411 {
		t.Fatalf("port = %d", st.ListenPort)
	}
	want := tunnel.Obfuscation{
		Enabled: true,
		Jc:      5, Jmin: 40, Jmax: 70, S1: 86, S2: 61,
		H1: awgparam.ScalarU32(1234567), H2: awgparam.ScalarU32(2345678),
		H3: awgparam.ScalarU32(3456789), H4: awgparam.ScalarU32(4567890),
	}
	if st.Obfuscation != want {
		t.Fatalf("obfuscation = %+v, want %+v", st.Obfuscation, want)
	}
	if len(st.Peers) != 1 {
		t.Fatalf("peers = %d", len(st.Peers))
	}
	p := st.Peers[0]
	if p.PublicKey != "HSg3u5yrUGweFJcJ0giKX1/swvDP5LBohvXihss3Mi8=" {
		t.Fatalf("peer key = %q", p.PublicKey)
	}
	if p.PresharedKeySet {
		t.Fatal("peer psk must be unset")
	}
	if p.Endpoint != "" || p.AllowedIPs == nil || len(p.AllowedIPs) != 1 || p.AllowedIPs[0] != "10.8.0.2/32" {
		t.Fatalf("peer endpoint/allowedips = %q %v", p.Endpoint, p.AllowedIPs)
	}
	if !p.LastHandshake.IsZero() {
		t.Fatalf("handshake = %v, want zero", p.LastHandshake)
	}
	if p.RXBytes != 0 || p.TXBytes != 0 || !p.PersistentKeepalive.IsZero() {
		t.Fatalf("counters = %d/%d ka=%s", p.RXBytes, p.TXBytes, p.PersistentKeepalive)
	}
}

func TestParseDumpActivePeerFields(t *testing.T) {
	hs := time.Now().Add(-time.Minute).Truncate(time.Second)
	line := strings.Join([]string{
		"HSg3u5yrUGweFJcJ0giKX1/swvDP5LBohvXihss3Mi8=",
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", // psk set
		"203.0.113.7:51820",
		"10.8.0.2/32, 10.8.0.9/32",
		"0", // replaced below
		"1234",
		"5678",
		"25-35",
	}, "\t")
	// Replace the handshake field with real unix seconds.
	f := strings.Split(line, "\t")
	f[4] = strconv.FormatInt(hs.Unix(), 10)
	line = strings.Join(f, "\t")

	ifLine := strings.Join([]string{
		"SFMhedNboAgpzEeKzfGmz1hC0iKD+R2Odzwlb5izG1A=", "HmFweuq2rTVwdSFIV60UeFB3CRHafUv9T1jdAW4vQVw=",
		"39411", "5", "40", "70", "86", "61", "0", "0",
		"1234567", "2345678", "3456789", "4567890",
		"(null)", "(null)", "(null)", "(null)", "(null)", "(none)",
		"0", "0", "0", "0", "0", "0", "off", "off", "off",
	}, "\t")

	st, err := parseDump("awg0", []byte(ifLine+"\n"+line+"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p := st.Peers[0]
	if !p.PresharedKeySet || p.Endpoint != "203.0.113.7:51820" {
		t.Fatalf("psk/endpoint = %v %q", p.PresharedKeySet, p.Endpoint)
	}
	if len(p.AllowedIPs) != 2 || p.AllowedIPs[1] != "10.8.0.9/32" {
		t.Fatalf("allowedips = %v", p.AllowedIPs)
	}
	if !p.LastHandshake.Equal(hs) {
		t.Fatalf("handshake = %v want %v", p.LastHandshake, hs)
	}
	if p.RXBytes != 1234 || p.TXBytes != 5678 || p.PersistentKeepalive != testU16Range(t, "25-35") {
		t.Fatalf("counters = %d/%d ka=%s", p.RXBytes, p.TXBytes, p.PersistentKeepalive)
	}
}

func TestParseDumpHeaderRangePreserved(t *testing.T) {
	ifLine := strings.Join([]string{
		"priv", "pub", "39411", "5", "40", "70", "86", "61", "0", "0",
		"1234567-7654321", "2345678", "3456789", "4567890",
		"(null)", "(null)", "(null)", "(null)", "(null)", "(none)",
		"0", "0", "0", "0", "0", "0", "off", "off", "0x0",
	}, "\t")
	st, err := parseDump("awg0", []byte(ifLine+"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if st.Obfuscation.H1 != testU32Range(t, "1234567-7654321") {
		t.Fatalf("H1 = %s, want exact range", st.Obfuscation.H1)
	}
}

func TestParseDumpRejectsMalformedRangesAtWireBoundary(t *testing.T) {
	base := []string{
		"priv", "pub", "39411", "5", "40", "70", "86", "61", "0", "0",
		"100-110", "200-210", "300-310", "400-410",
		"(null)", "(null)", "(null)", "(null)", "(null)", "(none)",
		"0", "0", "0", "0", "0", "0", "off", "off", "off",
	}
	for _, tc := range []struct {
		name      string
		field     int
		value     string
		wantError string
	}{
		{"inverted header", 10, "110-100", "field H1"},
		{"overflow header", 13, "4294967296", "field H4"},
		{"inverted timer", 20, "20-10", "field ContentPaddingAddition"},
		{"overflow timer", 25, "65536", "field MaxHandshakeAttempts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := append([]string(nil), base...)
			fields[tc.field] = tc.value
			_, err := parseDump("awg0", []byte(strings.Join(fields, "\t")+"\n"))
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("want %q error, got %v", tc.wantError, err)
			}
		})
	}

	peer := "pub\t(none)\t(none)\t10.8.0.2/32\t0\t0\t0\t25-65536"
	_, err := parseDump("awg0", []byte(strings.Join(base, "\t")+"\n"+peer+"\n"))
	if err == nil || !strings.Contains(err.Error(), "field persistent_keepalive") {
		t.Fatalf("want peer keepalive error, got %v", err)
	}
}

func TestParseDumpRejectsUnknownFormats(t *testing.T) {
	// Stock wireguard dump (4 fields): loud, specific refusal.
	wg := "priv\tpub\t39411\toff"
	_, err := parseDump("awg0", []byte(wg+"\n"))
	if err == nil || !strings.Contains(err.Error(), "stock-WireGuard") {
		t.Fatalf("want stock-wg rejection, got %v", err)
	}
	// Wrong field count: unknown format.
	junk := strings.Repeat("a\t", 20) + "a"
	_, err = parseDump("awg0", []byte(junk+"\n"))
	if err == nil || !strings.Contains(err.Error(), "21 interface fields") {
		t.Fatalf("want unknown-format rejection, got %v", err)
	}
	// Peer line with wrong count.
	ifLine := strings.Join([]string{
		"priv", "pub", "39411", "5", "40", "70", "86", "61", "0", "0",
		"1", "2", "3", "4", "(null)", "(null)", "(null)", "(null)", "(null)", "(none)",
		"0", "0", "0", "0", "0", "0", "off", "off", "off",
	}, "\t")
	_, err = parseDump("awg0", []byte(ifLine+"\nshort\tpeer\tline\n"))
	if err == nil || !strings.Contains(err.Error(), "3 fields") {
		t.Fatalf("want peer-line rejection, got %v", err)
	}
}

func TestParseDumpEmpty(t *testing.T) {
	if _, err := parseDump("awg0", nil); err == nil {
		t.Fatal("empty dump must error")
	}
}

func TestParseDumpPlainInterface(t *testing.T) {
	ifLine := strings.Join([]string{
		"priv", "pub", "51820", "0", "0", "0", "0", "0", "0", "0",
		"0", "0", "0", "0", "(null)", "(null)", "(null)", "(null)", "(null)", "(none)",
		"0", "0", "0", "0", "0", "0", "off", "off", "off",
	}, "\t")
	st, err := parseDump("awg0", []byte(ifLine+"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if st.Obfuscation.Enabled {
		t.Fatal("all-zero obfuscation must parse as plain")
	}
}

func TestParseDumpGatedFields(t *testing.T) {
	// 29-field interface line with gated values (field order per amneziawg.md).
	f := make([]string, 29)
	for i := range f {
		f[i] = "0"
	}
	f[0] = "(none)"
	f[1] = "8N8eM9uMx9WcXWvOHbiu4B9kB8eSvbG3wfZugvwtCWU="
	f[2] = "39001"
	f[3] = "4"  // Jc
	f[4] = "40" // Jmin
	f[5] = "70" // Jmax
	f[6] = "15" // S1
	f[7] = "64" // S2
	f[8] = "40" // S3
	f[9] = "0"  // S4 unset
	f[10], f[11], f[12], f[13] = "11", "22", "33", "44"
	for i := 14; i <= 18; i++ {
		f[i] = "(null)"
	}
	f[19] = "(none)"
	f[20] = "10-20" // content padding
	f[21] = "120-180"
	f[25] = "5"
	f[26] = "on"
	f[27] = "off"
	f[28] = "off"

	st, err := parseDump("awg0", []byte(strings.Join(f, "\t")+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	o := st.Obfuscation
	if o.S3 != 40 || o.S4 != 0 {
		t.Fatalf("S3/S4: %d %d", o.S3, o.S4)
	}
	if o.HeaderProtectionKey != "" {
		t.Fatalf("hpk: %q", o.HeaderProtectionKey)
	}
	if o.ContentPaddingAddition != testU16Range(t, "10-20") ||
		o.RekeyAfterTime != testU16Range(t, "120-180") ||
		o.MaxHandshakeAttempts != awgparam.ScalarU16(5) {
		t.Fatalf("ranges: %s %s %s", o.ContentPaddingAddition, o.RekeyAfterTime, o.MaxHandshakeAttempts)
	}
	if !o.RandomTrailers || o.DisableCookies {
		t.Fatalf("flags: %v %v", o.RandomTrailers, o.DisableCookies)
	}
}

// TestParseDumpKernelPlainBaseline is the regression for the VPS finding
// (2026-08-31): the kernel module dumps H1..H4 = 1,2,3,4 on a fresh plain
// interface (userspace dumps zeros). The parser normalizes that inert
// default so verify-after-apply and drift comparison see the applied plain
// profile, not the link's cosmetic baseline.
func TestParseDumpKernelPlainBaseline(t *testing.T) {
	line := "priv\tpub\t39101\t0\t0\t0\t0\t0\t0\t0\t1\t2\t3\t4\t(null)\t(null)\t(null)\t(null)\t(null)\t(none)\t0\t0\t0\t0\t0\t0\toff\toff\t0x0"
	st, err := parseDump("awg0", []byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if st.Obfuscation.Enabled {
		t.Fatal("plain profile parsed as enabled")
	}
	if !st.Obfuscation.H1.IsZero() || !st.Obfuscation.H2.IsZero() ||
		!st.Obfuscation.H3.IsZero() || !st.Obfuscation.H4.IsZero() {
		t.Fatalf("kernel plain baseline not normalized: %+v", st.Obfuscation)
	}
	// With junk packets configured the kernel values are real and must stay.
	lineObf := "priv\tpub\t39101\t5\t20\t80\t10\t140\t0\t0\t1\t2\t3\t4\t(null)\t(null)\t(null)\t(null)\t(null)\t(none)\t0\t0\t0\t0\t0\t0\toff\toff\t0x0"
	st2, err := parseDump("awg0", []byte(lineObf))
	if err != nil {
		t.Fatal(err)
	}
	if !st2.Obfuscation.Enabled || st2.Obfuscation.H1 != awgparam.ScalarU32(1) ||
		st2.Obfuscation.H4 != awgparam.ScalarU32(4) {
		t.Fatalf("enabled profile headers must be preserved: %+v", st2.Obfuscation)
	}
}

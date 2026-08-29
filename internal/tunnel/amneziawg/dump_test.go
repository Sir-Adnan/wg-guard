package amneziawg

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

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
		H1: 1234567, H2: 2345678, H3: 3456789, H4: 4567890,
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
	if p.RXBytes != 0 || p.TXBytes != 0 || p.KeepaliveSeconds != 0 {
		t.Fatalf("counters = %d/%d ka=%d", p.RXBytes, p.TXBytes, p.KeepaliveSeconds)
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
		"25",
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
	if p.RXBytes != 1234 || p.TXBytes != 5678 || p.KeepaliveSeconds != 25 {
		t.Fatalf("counters = %d/%d ka=%d", p.RXBytes, p.TXBytes, p.KeepaliveSeconds)
	}
}

func TestParseDumpHeaderRangeTolerated(t *testing.T) {
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
	if st.Obfuscation.H1 != 1234567 {
		t.Fatalf("H1 = %d, want low bound of range", st.Obfuscation.H1)
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

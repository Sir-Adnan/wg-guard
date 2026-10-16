package clientconf

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

// qUncompress mirrors the app side: base64url (no padding), 4-byte length, zlib.
func decodeAmneziaKey(t *testing.T, key string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(key, "vpn://"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 4 {
		t.Fatal("short payload")
	}
	want := binary.BigEndian.Uint32(raw[:4])
	zr, err := zlib.NewReader(bytes.NewReader(raw[4:]))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if uint32(len(out)) != want {
		t.Fatalf("length prefix %d, payload %d", want, len(out))
	}
	return string(out)
}

func TestAmneziaVPNKeyRoundTrip(t *testing.T) {
	conf := "[Interface]\nPrivateKey = a\nAddress = 10.8.0.2/32\nJc = 4\n\n[Peer]\nPublicKey = b\nEndpoint = vpn.example.com:39001\n"
	key, err := AmneziaVPNKey(conf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "vpn://") || strings.ContainsAny(key[6:], "+/=") {
		t.Fatalf("key must be base64url without padding: %s", key)
	}
	if got := decodeAmneziaKey(t, key); got != conf {
		t.Fatalf("round trip:\n%q\n%q", got, conf)
	}
	if _, err := AmneziaVPNKey(""); err == nil {
		t.Fatal("empty config accepted")
	}
}

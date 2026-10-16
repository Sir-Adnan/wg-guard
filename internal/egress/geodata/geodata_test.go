package geodata

import (
	"net/netip"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
)

// Minimal protobuf writer for fixtures (the production reader is independent).
func pbVarint(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func pbBytes(num int, b []byte) []byte {
	out := pbVarint(uint64(num<<3 | 2))
	out = append(out, pbVarint(uint64(len(b)))...)
	return append(out, b...)
}

func pbUint(num int, v uint64) []byte {
	return append(pbVarint(uint64(num<<3)), pbVarint(v)...)
}

func domainMsg(typ uint64, value string, attrs ...string) []byte {
	msg := append(pbUint(1, typ), pbBytes(2, []byte(value))...)
	for _, a := range attrs {
		msg = append(msg, pbBytes(3, pbBytes(1, []byte(a)))...)
	}
	return msg
}

func site(code string, domains ...[]byte) []byte {
	msg := pbBytes(1, []byte(code))
	for _, d := range domains {
		msg = append(msg, pbBytes(2, d)...)
	}
	return pbBytes(1, msg)
}

func cidr(ip []byte, bits uint64) []byte {
	return append(pbBytes(1, ip), pbUint(2, bits)...)
}

func geoip(code string, reverse bool, cidrs ...[]byte) []byte {
	msg := pbBytes(1, []byte(code))
	for _, c := range cidrs {
		msg = append(msg, pbBytes(2, c)...)
	}
	if reverse {
		msg = append(msg, pbUint(3, 1)...)
	}
	return pbBytes(1, msg)
}

func TestGeositeMaterializesRequestedCategoriesOnly(t *testing.T) {
	var data []byte
	data = append(data, site("GOOGLE-DEEPMIND",
		domainMsg(2, "deepmind.com"), domainMsg(3, "gemini.google.com"),
		domainMsg(0, "bard"), domainMsg(1, `^.+\.deepmind\.google$`), domainMsg(2, "cn-only.example", "cn"))...)
	data = append(data, site("SPOTIFY", domainMsg(2, "spotify.com"))...)
	names, err := Categories(data)
	if err != nil || len(names) != 2 || names[0] != "google-deepmind" || names[1] != "spotify" {
		t.Fatalf("categories: %v %v", names, err)
	}
	got, err := Geosite(data, []string{"google-deepmind", "google-deepmind@cn", "google-deepmind@!cn", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	all := got["google-deepmind"]
	if len(all) != 5 {
		t.Fatalf("all: %+v", all)
	}
	want := []egress.Matcher{
		{Type: egress.MatchDomain, Value: "deepmind.com"},
		{Type: egress.MatchFull, Value: "gemini.google.com"},
		{Type: egress.MatchKeyword, Value: "bard"},
		{Type: egress.MatchRegexp, Value: `^.+\.deepmind\.google$`},
	}
	for i, w := range want {
		if all[i] != w {
			t.Fatalf("matcher %d = %+v, want %+v", i, all[i], w)
		}
	}
	if cn := got["google-deepmind@cn"]; len(cn) != 1 || cn[0].Value != "cn-only.example" {
		t.Fatalf("attribute filter: %+v", cn)
	}
	if notCN := got["google-deepmind@!cn"]; len(notCN) != 4 {
		t.Fatalf("negated attribute filter: %+v", notCN)
	}
	if _, ok := got["missing"]; ok {
		t.Fatal("missing category must be absent")
	}
	if _, ok := got["spotify"]; ok {
		t.Fatal("unrequested category materialized")
	}
}

func TestGeoIPSkipsIPv6AndReverseCategories(t *testing.T) {
	var data []byte
	data = append(data, geoip("IR", false, cidr([]byte{5, 160, 0, 0}, 13), cidr(make([]byte, 16), 32), cidr([]byte{2, 176, 0, 9}, 16))...)
	data = append(data, geoip("PRIVATE", true, cidr([]byte{10, 0, 0, 0}, 8))...)
	got, err := GeoIP(data, []string{"ir", "private"})
	if err != nil {
		t.Fatal(err)
	}
	ir := got["ir"]
	if len(ir) != 2 || ir[0] != netip.MustParsePrefix("5.160.0.0/13") || ir[1] != netip.MustParsePrefix("2.176.0.0/16") {
		t.Fatalf("ir: %v", ir)
	}
	if len(got["private"]) != 0 {
		t.Fatal("reverse-match categories are not supported and must be skipped")
	}
}

func TestMalformedInputIsRejected(t *testing.T) {
	for _, data := range [][]byte{{0x0a, 0xff}, {0x0a, 0x05, 0x01}, {0xff}} {
		if _, err := Categories(data); err == nil {
			t.Fatalf("accepted malformed %x", data)
		}
	}
}

func TestStoreInstallCachesAndValidates(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Install(KindGeosite, []byte("not a dat file")); err == nil {
		t.Fatal("invalid data installed")
	}
	data := site("OPENAI", domainMsg(2, "openai.com"))
	n, err := s.Install(KindGeosite, data)
	if err != nil || n != 1 {
		t.Fatalf("install: %d %v", n, err)
	}
	got, err := s.Domains([]string{"openai"})
	if err != nil || len(got["openai"]) != 1 {
		t.Fatalf("domains: %v %v", got, err)
	}
	again, _ := s.Domains([]string{"openai"})
	if &again["openai"][0] != &got["openai"][0] {
		t.Fatal("unchanged file and request should hit the cache")
	}
	info := s.Info()
	if !info[0].Present || info[0].Categories != 1 || info[1].Present {
		t.Fatalf("info: %+v", info)
	}
	if err := s.Remove(KindGeosite); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Domains([]string{"openai"}); err != nil || got != nil {
		t.Fatalf("removed file: %v %v", got, err)
	}
}

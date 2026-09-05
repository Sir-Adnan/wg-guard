package install

import (
	"context"
	"fmt"
	"io"
	"testing"
)

// Public-address examples are parsed against an in-memory host, never contacted.
func TestEndpointRejectsSpecialUseExplicitInput(t *testing.T) {
	for _, address := range []string{
		"100.64.0.0", "100.64.1.2", "100.127.255.255", "::ffff:100.64.1.2", "::ffff:6440:102",
		"0.1.2.3", "10.0.0.1", "127.0.0.1", "169.254.1.2", "172.16.1.2", "192.168.1.2",
		"192.0.0.1", "192.0.2.1", "192.88.99.1", "198.18.0.1", "198.19.255.255", "198.51.100.1", "203.0.113.1", "240.0.0.1", "224.0.0.1",
		"::ffff:198.18.0.1", "::ffff:203.0.113.1", "::1", "::", "::192.0.2.1", "fc00::1", "fe80::1", "fec0::1", "ff02::1",
		"64:ff9b:1::1", "100::1", "100:0:0:1::1", "2001:2::1", "2001:db8::1", "2002:6440:102::1", "3fff::1", "5f00::1",
	} {
		t.Run(address, func(t *testing.T) {
			p := Defaults()
			p.PublicIP = address
			if _, err := p.Resolve(); err == nil {
				t.Fatal("special-use endpoint accepted")
			}
		})
	}
}

func TestEndpointAcceptsOrdinaryUnicastCandidates(t *testing.T) {
	for _, address := range []string{"8.8.8.8", "100.63.255.255", "100.128.0.0", "::ffff:8.8.8.8", "2606:4700:4700::1111", "192.0.0.9", "192.0.0.10", "2001:1::1", "2001:3::1", "2001:4:112::1"} {
		p := Defaults()
		p.PublicIP = address
		if _, err := p.Resolve(); err != nil {
			t.Errorf("ordinary candidate %s rejected: %v", address, err)
		}
	}
}

func TestEndpointDiscoverySkipsCGNATAndSpecialUse(t *testing.T) {
	for _, address := range []string{"100.64.1.2", "::ffff:100.64.1.2", "203.0.113.7", "198.18.1.2", "2001:db8::1"} {
		t.Run(address, func(t *testing.T) {
			h := newMemHost()
			h.output["ip"] = fmt.Sprintf(`[{"addr_info":[{"local":%q}]}]`, address)
			_, err := Install(context.Background(), h, InstallOptions{Plan: Defaults(), Yes: true, Stdout: io.Discard})
			if err == nil {
				t.Error("special-use-only host accepted")
			}
			if _, ok := h.files[ConfigPath]; ok {
				t.Error("special-use endpoint reached deployment writes")
			}
			h.output["ip"] = fmt.Sprintf(`[{"addr_info":[{"local":%q},{"local":"8.8.8.8"}]}]`, address)
			p := Defaults()
			if err := resolveEndpoint(context.Background(), h, &p); err != nil || p.PublicIP != "8.8.8.8" {
				t.Fatalf("did not skip special-use address: %s %v", p.PublicIP, err)
			}
		})
	}
}

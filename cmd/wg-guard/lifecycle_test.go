package main

import (
	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestParseLifecycleSources(t *testing.T) {
	for _, args := range [][]string{{"--release", "v1", "--yes"}, {"--commit", "main", "--yes"}} {
		o, err := parseInstallOptions(args)
		if err != nil {
			t.Fatal(err)
		}
		if o.Selection.Channel == "" {
			t.Fatal("explicit source lost")
		}
	}
	if _, err := parseInstallOptions([]string{"--release", "v1", "--commit", "main", "--yes"}); err == nil {
		t.Fatal("ambiguous source accepted")
	}
}

func TestParseSecureExposureFlagsAndProtectedCloudflareToken(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "cloudflare-token")
	const token = "synthetic_cloudflare_token_123456"
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tokenFile, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	o, err := parseInstallOptions([]string{
		"--yes", "--domain", "panel.example.com", "--exposure", "nginx",
		"--certificate", "cloudflare-dns", "--cloudflare-token-file", tokenFile,
		"--acme-email", "ops@example.com", "--https-port", "8443",
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.Plan.Exposure != install.ExposureNginx || o.Plan.Certificate != install.CertificateCloudflareDNS || o.Plan.CloudflareToken != token || o.Plan.ACMEEmail != "ops@example.com" || o.Plan.PublicPort != 8443 {
		t.Fatalf("secure exposure flags lost: %+v", o.Plan)
	}
	if strings.Contains(o.Plan.CloudflareTokenFile, token) {
		t.Fatal("token content confused with its protected path")
	}

	for _, args := range [][]string{
		{"--tls", "acme", "--exposure", "direct"},
		{"--exposure", "public-http"},
		{"--certificate", "unknown"},
		{"--cloudflare-token-file", tokenFile},
	} {
		if _, err := parseInstallOptions(args); err == nil {
			t.Fatalf("invalid secure-access flags accepted: %v", args)
		}
	}
}
func TestUpdateSelectionIsFreshAndExplicit(t *testing.T) {
	o, err := parseUpdateOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.Selection != (distribution.Selection{Channel: "release", Ref: "latest"}) {
		t.Fatalf("default source %+v", o.Selection)
	}
	o, err = parseUpdateOptions([]string{"--binary", "/tmp/candidate", "--image", "local:test", "--local-image"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.LocalImage || o.Selection.Channel != "" {
		t.Fatal("local path lost")
	}
	if _, err = parseUpdateOptions([]string{"--rollback", "--commit", "main"}); err == nil {
		t.Fatal("rollback mixed with acquisition")
	}
}

func TestUpdateCommandClassificationKeepsLegacyPanelFlags(t *testing.T) {
	cases := []struct {
		args        []string
		interactive bool
		kind        string
		rest        []string
	}{
		{nil, true, "menu", nil},
		{nil, false, "panel", nil},
		{[]string{"manager", "--commit", "main"}, false, "manager", []string{"--commit", "main"}},
		{[]string{"panel", "--release", "v1"}, false, "panel", []string{"--release", "v1"}},
		{[]string{"core", "--yes"}, false, "core", []string{"--yes"}},
		{[]string{"all", "--commit", "main", "--yes"}, false, "all", []string{"--commit", "main", "--yes"}},
		{[]string{"status"}, false, "status", nil},
		{[]string{"--rollback"}, false, "panel", []string{"--rollback"}},
	}
	for _, tc := range cases {
		kind, rest, err := classifyUpdateCommand(tc.args, tc.interactive)
		if err != nil || kind != tc.kind || !reflect.DeepEqual(rest, tc.rest) {
			t.Fatalf("args %v interactive=%v => %q %v, %v", tc.args, tc.interactive, kind, rest, err)
		}
	}
	if _, _, err := classifyUpdateCommand([]string{"everything"}, false); err == nil {
		t.Fatal("unknown update component accepted")
	}
}

func TestComponentUpdateOptionsAreExplicitAndSafe(t *testing.T) {
	manager, err := parseManagerUpdateOptions([]string{"--commit", "main", "--refresh"})
	if err != nil || manager.Selection != (distribution.Selection{Channel: "commit", Ref: "main"}) || !manager.Refresh {
		t.Fatalf("manager options = %+v, %v", manager, err)
	}
	manager, err = parseManagerUpdateOptions(nil)
	if err != nil || manager.Selection != (distribution.Selection{Channel: "release", Ref: "latest"}) {
		t.Fatalf("manager default = %+v, %v", manager, err)
	}
	core, err := parseCoreUpdateOptions([]string{"--bundle", "recommended", "--yes"})
	if err != nil || core.Bundle != "recommended" || !core.Yes {
		t.Fatalf("core options = %+v, %v", core, err)
	}
	all, err := parseAllUpdateOptions([]string{"--commit", "main", "--bundle", "latest-compatible", "--yes"})
	if err != nil || all.Selection.Ref != "main" || all.Bundle != "latest-compatible" || !all.Yes {
		t.Fatalf("all options = %+v, %v", all, err)
	}
	for _, args := range [][]string{{"--release", "v1", "--commit", "main"}, {"unexpected"}} {
		if _, err := parseManagerUpdateOptions(args); err == nil {
			t.Fatalf("unsafe manager options accepted: %v", args)
		}
	}
}

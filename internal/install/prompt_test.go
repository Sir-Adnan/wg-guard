package install

import (
	"errors"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
	"strings"
	"testing"
)

func TestWizardReviewIsEnglishAndEnterProceeds(t *testing.T) {
	var out strings.Builder
	q := newPrompt(strings.NewReader("\n"), &out, false)
	q.ui.Locale = i18n.Fa
	p := Defaults()
	p.TelegramToken = "synthetic-hidden-token"
	if err := q.confirm(p); err != nil {
		t.Fatalf("recommended review default did not proceed: %v", err)
	}
	if strings.Contains(out.String(), "synthetic-hidden-token") || strings.Contains(out.String(), "بررسی") {
		t.Fatal("review exposed a secret or non-English terminal copy")
	}
	q = newPrompt(strings.NewReader("back\n"), &out, false)
	if _, err := q.askChoice("menu", []string{"one"}, 1); !errors.Is(err, terminal.ErrBack) {
		t.Fatal(err)
	}
}

func TestAdvancedOverrideDetectionIncludesACMEPort(t *testing.T) {
	p := Defaults()
	p.Mode = ""
	if advancedSettingsRequested(&p) {
		t.Fatal("default interactive plan should use the recommended path")
	}
	p.ACMEHTTPPort = 8081
	if !advancedSettingsRequested(&p) {
		t.Fatal("custom ACME challenge port must open advanced setup")
	}
}

func TestWizardReviewEffectiveNetworkDefaults(t *testing.T) {
	for _, locale := range []i18n.Locale{i18n.En, i18n.Fa} {
		for _, custom := range []bool{false, true} {
			p := Defaults()
			want := []string{"10.8.0.0/24", "1420", "1.1.1.1, 1.0.0.1"}
			if custom {
				p.VPNSubnet = "10.42.0.0/24"
				p.MTU = 1380
				p.ClientDNS = "9.9.9.9"
				want = []string{"10.42.0.0/24", "1380", "9.9.9.9"}
			}
			var out strings.Builder
			q := newPrompt(strings.NewReader("yes\n"), &out, false)
			q.advanced = true
			q.ui.Locale = locale
			if err := q.confirm(p); err != nil {
				t.Fatal(err)
			}
			for _, value := range want {
				if !strings.Contains(out.String(), value) {
					t.Fatalf("review omitted effective network value %s", value)
				}
			}
		}
	}
}

func TestWizardRecommendedAccessUsesHostFactsAndEnterDefaults(t *testing.T) {
	cases := []struct {
		name  string
		input string
		host  func() *memHost
		mode  ExposureMode
		cert  CertificateSource
	}{
		{"blank domain private", "\n\n", newMemHost, ExposurePrivate, ""},
		{"free domain direct ACME", "panel.example.com\n\n", newMemHost, ExposureDirect, CertificateBuiltin},
		{"existing nginx webroot", "panel.example.com\n\n", func() *memHost {
			h := newMemHost()
			h.portFree = func(addr string) bool { return addr == "127.0.0.1:8081" }
			h.output["systemctl is-active nginx.service"] = "active\n"
			h.output["nginx -T"] = "http { include /etc/nginx/conf.d/*.conf; }\n"
			return h
		}, ExposureNginx, CertificateWebroot},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			q := newPrompt(strings.NewReader(tc.input), &out, false)
			p := Defaults()
			p.Mode = ""
			if err := q.plan(&p, tc.host()); err != nil {
				t.Fatal(err)
			}
			if p.Mode != ModeDocker || p.Exposure != tc.mode || p.Certificate != tc.cert {
				t.Fatalf("recommended plan = %+v", p)
			}
			if containsNonEnglishTerminalScript(out.String()) {
				t.Fatalf("wizard rendered non-English text:\n%s", out.String())
			}
		})
	}
}

func TestWizardAdvancedPublicIPAndCloudflareSecrets(t *testing.T) {
	// Blank domain, customize, Docker, public-IP HTTPS, IP, panel port,
	// challenge port, optional email, network defaults, Telegram later.
	var out strings.Builder
	q := newPrompt(strings.NewReader("\nyes\n\n2\n8.8.8.8\n\n\n\n\n\n\n\n"), &out, false)
	p := Defaults()
	p.Mode = ""
	if err := q.plan(&p, newMemHost()); err != nil {
		t.Fatal(err)
	}
	if p.Exposure != ExposureDirect || p.Certificate != CertificateIP || p.PublicIP != "8.8.8.8" {
		t.Fatalf("IP HTTPS choice lost: %+v", p)
	}

	const token = "synthetic_cloudflare_token_123456"
	out.Reset()
	q = newPrompt(strings.NewReader("panel.example.com\nyes\n\n3\n2\n"+token+"\n\n\n\n\n\n\n\n\n"), &out, false)
	p = Defaults()
	p.Mode = ""
	if err := q.plan(&p, newMemHost()); err != nil {
		t.Fatal(err)
	}
	if p.Certificate != CertificateCloudflareDNS || p.CloudflareToken != token || strings.Contains(out.String(), token) {
		t.Fatalf("Cloudflare secret flow failed: %+v\n%s", p, out.String())
	}
}

func containsNonEnglishTerminalScript(s string) bool {
	for _, r := range s {
		if r >= '\u0600' && r <= '\u06ff' {
			return true
		}
	}
	return false
}

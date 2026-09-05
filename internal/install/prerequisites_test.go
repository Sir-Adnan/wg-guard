package install

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/config"
)

func TestNetworkRejectsUnsafeDomainsAndPortConflict(t *testing.T) {
	for _, domain := range []string{"x\n.example.com", "x example.com", "-x.example.com", "x..example.com", "203.0.113.7"} {
		t.Run(domain, func(t *testing.T) {
			p := Defaults()
			p.Domain = domain
			if _, err := p.Resolve(); err == nil {
				t.Error("accepted unsafe ACME domain")
			}
		})
	}
	p := Defaults()
	p.Domain = "panel.example.com"
	p.PanelPort = 80
	if _, err := p.Resolve(); err == nil {
		t.Error("accepted conflicting panel and HTTP-01 TCP ports")
	}
}

func TestLoopbackSummaryMatchesListener(t *testing.T) {
	p := Defaults()
	p.TLSMode = config.TLSModeProxy
	if got := p.PanelURL(); got != "http://127.0.0.1:8080" {
		t.Errorf("unreachable panel URL %s", got)
	}
	var out strings.Builder
	printSummary(&out, p, &State{})
	if !strings.Contains(out.String(), "ssh -N -L 8080:127.0.0.1:8080") {
		t.Error("missing SSH tunnel instructions")
	}
}

func TestTelegramGroupIDAccepted(t *testing.T) {
	q := newPrompt(strings.NewReader("y\ntoken\n-1001234567890\n03:30\n"), io.Discard, false)
	p := Defaults()
	if err := q.planTelegram(&p); err != nil {
		t.Fatalf("valid negative group ID rejected: %v", err)
	}
	if p.TelegramChat != "-1001234567890" {
		t.Error("lost group ID")
	}
}

func TestInstallPrerequisiteFailureBeforeDeploymentWrites(t *testing.T) {
	for _, tool := range []string{"ip", "tc", "nft", "awg", "systemctl", "sysctl"} {
		t.Run(tool, func(t *testing.T) {
			h := newMemHost()
			h.failCmd[tool] = fmt.Errorf("missing")
			p := Defaults()
			p.Mode = ModeNative
			_, err := Install(context.Background(), h, InstallOptions{Plan: p, Yes: true, Stdin: strings.NewReader(""), Stdout: io.Discard})
			if err == nil {
				t.Error("missing prerequisite accepted")
			}
			if _, ok := h.files[ConfigPath]; ok {
				t.Error("wrote deployment config before checking prerequisites")
			}
		})
	}
	h := newMemHost()
	h.failCmd["docker"] = fmt.Errorf("missing compose")
	_, _ = Install(context.Background(), h, InstallOptions{Plan: Defaults(), Yes: true, Stdin: strings.NewReader(""), Stdout: io.Discard})
	if _, ok := h.files[ConfigPath]; ok {
		t.Error("wrote config before Docker/Compose check")
	}
}

func TestSkipModuleDoesNotMutateHostModule(t *testing.T) {
	h := newMemHost()
	p := Defaults()
	p.PanelPort = healthServer(t, 200)
	_, err := Install(context.Background(), h, InstallOptions{Plan: p, Yes: true, SkipModule: true, Stdin: strings.NewReader(""), Stdout: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if h.ran("modprobe") || h.ran("apt-get") {
		t.Error("skip-module still changes host dependencies/module")
	}
	if _, ok := h.files[ModuleAutoLoadPath]; ok {
		t.Error("skip-module writes module persistence")
	}
}

func TestExplicitTLS8080AndEndpoint(t *testing.T) {
	for _, mode := range []config.TLSMode{config.TLSModeACME, config.TLSModeManual} {
		p := Defaults()
		p.TLSMode = mode
		p.Domain = "panel.example.com"
		p.CertFile = "/x/cert"
		p.KeyFile = "/x/key"
		p.PanelPortExplicit = true
		got, err := p.Resolve()
		if err != nil || got.PanelPort != 8080 {
			t.Fatalf("explicit 8080 lost: %d, %v", got.PanelPort, err)
		}
	}
	p := Defaults()
	p.PublicIP = "203.0.113.7"
	if p.VPNEndpoint() != "203.0.113.7" || p.PanelURL() != "http://127.0.0.1:8080" {
		t.Fatal("VPN endpoint coupled to panel bind")
	}
	seeds := planSeeds(p)
	if len(seeds) != 1 || seeds[0].argv[4] != "203.0.113.7" {
		t.Fatal("missing IP endpoint seed")
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "::1", "0.0.0.0", "example.com", "203.0.113.1\n"} {
		p.PublicIP = ip
		if _, err := p.Resolve(); err == nil {
			t.Errorf("accepted invalid public IP %q", ip)
		}
	}
}

func TestUnsupportedHostStopsBeforeWrites(t *testing.T) {
	h := newMemHost()
	h.files["/etc/os-release"] = memFile{data: []byte("ID=ubuntu\nVERSION_ID=24.04\n")}
	h.output["uname"] = "FreeBSD"
	_, err := Install(context.Background(), h, InstallOptions{Plan: Defaults(), Yes: true, Stdout: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "Linux") {
		t.Fatalf("unsupported OS result: %v", err)
	}
	if _, ok := h.files[ConfigPath]; ok {
		t.Fatal("unsupported OS wrote deployment config")
	}
}

func TestEndpointRequiredBeforeWrites(t *testing.T) {
	h := newMemHost()
	h.output["ip"] = ""
	_, err := Install(context.Background(), h, InstallOptions{Plan: Defaults(), Yes: true, Stdout: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "public-ip") {
		t.Fatalf("endpoint-less install result: %v", err)
	}
	if _, ok := h.files[ConfigPath]; ok {
		t.Error("endpoint failure wrote config")
	}
}

func TestPromptExplicitTLS8080SurvivesResolve(t *testing.T) {
	p := Defaults()
	p.Domain = "panel.example.com"
	p.TLSMode = config.TLSModeACME
	q := newPrompt(strings.NewReader("\n8080\n80\nn\nn\n\n"), io.Discard, false)
	if err := q.plan(&p, newMemHost()); err != nil {
		t.Fatal(err)
	}
	resolved, err := p.Resolve()
	if err != nil || resolved.PanelPort != 8080 {
		t.Fatalf("prompt lost explicit port: %d %v", resolved.PanelPort, err)
	}
}

func TestManualTLSPreflightAndContainerMounts(t *testing.T) {
	p := Defaults()
	p.TLSMode = config.TLSModeManual
	p.CertFile = "/etc/certs/panel.pem"
	p.KeyFile = "/etc/certs/panel.key"
	p.PublicIP = "203.0.113.7"
	if err := preflight(context.Background(), newMemHost(), p, io.Discard); err == nil {
		t.Fatal("missing manual TLS files accepted")
	}
	compose := RenderCompose(p)
	if strings.Contains(compose, "-k https://") {
		t.Fatal("manual health probe combines curl flag and URL into one argument")
	}
	if !strings.Contains(compose, "/etc/certs/panel.pem:/etc/certs/panel.pem:ro") || !strings.Contains(compose, "/etc/certs/panel.key:/etc/certs/panel.key:ro") {
		t.Fatal("manual TLS files inaccessible inside container")
	}
	p.CertFile = "/x\n  privileged: true"
	if _, err := p.Resolve(); err == nil {
		t.Fatal("unsafe certificate path accepted")
	}
}

func TestManualTLSRetainsDomainForReadinessRetry(t *testing.T) {
	p := Defaults()
	p.TLSMode = config.TLSModeManual
	p.Domain = "panel.example.com"
	if p.BootConfig().TLS.Domain != "panel.example.com" {
		t.Fatal("manual TLS verification name lost from persistent config")
	}
}

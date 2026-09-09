package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

type accessPromptHost struct{ install.Host }

func (accessPromptHost) PortFree(string) bool { return true }
func (accessPromptHost) LookPath(string) (string, error) {
	return "", fs.ErrNotExist
}

func privateAccessPlan() install.Plan {
	p := install.Defaults()
	p.Exposure = install.ExposurePrivate
	p.Certificate = ""
	p.TLSMode = config.TLSModeProxy
	p.PublicIP = "8.8.8.8"
	return p
}

func TestAccessWizardDefaultsToAutomaticDomainHTTPS(t *testing.T) {
	for _, width := range []int{40, 48, 80} {
		var out strings.Builder
		u := terminal.New(strings.NewReader("2\npanel.example.com\n\n\n\n\n"), &out, terminal.Options{Locale: i18n.En, Width: width})
		p, err := promptAccessPlan(context.Background(), u, accessPromptHost{}, privateAccessPlan())
		if err != nil {
			t.Fatal(err)
		}
		if p.Exposure != install.ExposureDirect || p.Certificate != install.CertificateBuiltin || p.PublicURL() != "https://panel.example.com" {
			t.Fatalf("automatic access plan = %+v", p)
		}
		if containsRTLScript(out.String()) {
			t.Fatal("access wizard rendered non-English terminal text")
		}
		for _, line := range strings.Split(out.String(), "\n") {
			if len([]rune(line)) > width {
				t.Fatalf("width=%d overflow: %q", width, line)
			}
		}
	}
}

func TestAccessWizardNeverEchoesCloudflareToken(t *testing.T) {
	const token = "synthetic_cloudflare_token_123456"
	var out strings.Builder
	u := terminal.New(strings.NewReader("2\npanel.example.com\n2\n"+token+"\n\n\n\n"), &out, terminal.Options{Locale: i18n.En, Width: 48})
	p, err := promptAccessPlan(context.Background(), u, accessPromptHost{}, privateAccessPlan())
	if err != nil {
		t.Fatal(err)
	}
	if p.Certificate != install.CertificateCloudflareDNS || p.CloudflareToken != token {
		t.Fatalf("Cloudflare plan = %+v", p)
	}
	if strings.Contains(out.String(), token) {
		t.Fatal("Cloudflare token appeared in terminal output")
	}
}

func TestAccessWizardEnterKeepsCurrentSettings(t *testing.T) {
	current := privateAccessPlan()
	var out strings.Builder
	u := terminal.New(strings.NewReader("\n"), &out, terminal.Options{Locale: i18n.En, Width: 40})
	got, err := promptAccessPlan(context.Background(), u, accessPromptHost{}, current)
	if err != nil {
		t.Fatal(err)
	}
	if got.Exposure != current.Exposure || got.PanelPort != current.PanelPort {
		t.Fatalf("Enter changed current access: %+v", got)
	}
}

func TestInteractiveAccessOptionsAcceptPrivateStateDefaults(t *testing.T) {
	p, yes, err := accessOptions(nil, accessPromptHost{}, privateAccessPlan(), io.Discard)
	if err != nil || yes || p.Certificate != install.CertificateAuto || p.ACMEHTTPPort != 80 {
		t.Fatalf("private access defaults = %+v yes=%t err=%v", p, yes, err)
	}
}

func TestAccessOptionsHelpIsVisibleAndDistinctFromInvalidArguments(t *testing.T) {
	var out strings.Builder
	_, _, err := accessOptions([]string{"--help"}, accessPromptHost{}, privateAccessPlan(), &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("configure help error = %v", err)
	}
	if !strings.Contains(out.String(), "-exposure") || !strings.Contains(out.String(), "-certificate") {
		t.Fatalf("configure help was hidden:\n%s", out.String())
	}
}

func TestExposureStatusIsCompactEnglish(t *testing.T) {
	var out strings.Builder
	p := privateAccessPlan()
	report := &install.ExposureHealthReport{Plan: p, Checks: []install.ExposureHealthCheck{{Name: "panel-access", Status: install.ExposureHealthPass}}}
	showExposureStatus(terminal.New(nil, &out, terminal.Options{Locale: i18n.En, Width: 40}), report, &install.State{TLSReadiness: "not-applicable"})
	if containsRTLScript(out.String()) || !strings.Contains(strings.ToLower(out.String()), "ssh") || !strings.Contains(out.String(), "private") {
		t.Fatalf("unexpected access status:\n%s", out.String())
	}
}

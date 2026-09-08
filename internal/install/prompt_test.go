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

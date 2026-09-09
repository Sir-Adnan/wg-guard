package install

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

func TestInstallerProgressUsesSemanticTTYColors(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	var out bytes.Buffer
	u := terminal.New(nil, &out, terminal.Options{Locale: i18n.En, TTY: true, Color: true})
	p := &progressOutput{Writer: &out, ui: u}
	progress(p, "healthy", "http://127.0.0.1:8080")
	progress(p, "dns_pending", "panel.example.com")
	progress(p, "shim", "/tmp/wg-guard", BinPath)
	text := out.String()
	for _, code := range []string{"\x1b[32;1m", "\x1b[33;1m", "\x1b[36m"} {
		if !strings.Contains(text, code) {
			t.Fatalf("progress tone %q missing: %q", code, text)
		}
	}
}

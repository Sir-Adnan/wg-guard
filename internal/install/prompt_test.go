package install

import (
	"errors"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
	"strings"
	"testing"
)

func TestWizardLocalizedReviewAndSafeBlank(t *testing.T) {
	var out strings.Builder
	q := newPrompt(strings.NewReader("\n"), &out, false)
	q.ui.Locale = i18n.Fa
	p := Defaults()
	p.TelegramToken = "synthetic-hidden-token"
	if err := q.confirm(p); !errors.Is(err, terminal.ErrCanceled) {
		t.Fatalf("blank review permitted: %v", err)
	}
	if strings.Contains(out.String(), "synthetic-hidden-token") || !strings.Contains(out.String(), "بررسی") {
		t.Fatal("review secret/locale failure")
	}
	q = newPrompt(strings.NewReader("back\n"), &out, false)
	if _, err := q.askChoice("menu", []string{"one"}, 1); !errors.Is(err, terminal.ErrBack) {
		t.Fatal(err)
	}
}

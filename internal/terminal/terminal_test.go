package terminal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
)

func TestCanceledInputAndSecretBytes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	u := New(strings.NewReader("yes\n"), &bytes.Buffer{}, Options{Context: ctx})
	if _, err := u.Ask("confirm", ""); !errors.Is(err, ErrCanceled) {
		t.Fatal("cancellation ignored")
	}
	u = New(strings.NewReader("  ۱۲٣ secret  \n"), &bytes.Buffer{}, Options{})
	got, err := u.Secret("hidden")
	if err != nil || got != "  ۱۲٣ secret  " {
		t.Fatal("secret bytes normalized")
	}
}

func TestInputRetryBackAndCancellation(t *testing.T) {
	for _, v := range []string{"۲\n", "٢\n"} {
		n, err := New(strings.NewReader(v), &bytes.Buffer{}, Options{}).Choose("number", []string{"one", "two"}, 0)
		if err != nil || n != 2 {
			t.Fatalf("localized number: %d %v", n, err)
		}
	}
	for _, ending := range []string{"0\n", "", "1"} {
		ui := New(strings.NewReader(ending), &bytes.Buffer{}, Options{Locale: i18n.En})
		_, err := ui.Choose("menu", []string{"one"}, 0)
		if ending == "0\n" {
			if !errors.Is(err, ErrBack) {
				t.Fatal(err)
			}
		} else if !errors.Is(err, ErrCanceled) {
			t.Fatalf("%q: %v", ending, err)
		}
	}
	var quitOut bytes.Buffer
	ui := New(strings.NewReader("q\n0\n"), &quitOut, Options{Locale: i18n.En})
	_, err := ui.Choose("menu", []string{"one"}, 0)
	if !errors.Is(err, ErrBack) {
		t.Fatalf("q should be ordinary invalid menu input before 0 goes back: %v", err)
	}
	if strings.Contains(quitOut.String(), "q  ") || strings.Contains(quitOut.String(), "q to") {
		t.Fatalf("menu still advertises q:\n%s", quitOut.String())
	}
	ui = New(strings.NewReader("q\n"), &bytes.Buffer{}, Options{Locale: i18n.En})
	if got, err := ui.Ask("value", ""); err != nil || got != "q" {
		t.Fatalf("q remained a hidden cancel command: %q, %v", got, err)
	}
	var out bytes.Buffer
	ui = New(strings.NewReader("bad\n9\n2\n"), &out, Options{Locale: i18n.En})
	n, err := ui.Choose("menu", []string{"one", "two"}, 0)
	if err != nil || n != 2 || !strings.Contains(out.String(), "1–2") {
		t.Fatalf("%d %v %s", n, err, out.String())
	}
	ui = New(strings.NewReader("\n"), &out, Options{Locale: i18n.En})
	ok, err := ui.Confirm("delete")
	if err != nil || ok {
		t.Fatalf("blank consent: %v %v", ok, err)
	}
}

func TestConfirmationUsesCompactYNDefaultsAndAcceptsCommonForms(t *testing.T) {
	cases := []struct {
		input string
		def   bool
		want  bool
		hint  string
	}{
		{"\n", true, true, "[Y/n]"},
		{"\n", false, false, "[y/N]"},
		{"y\n", false, true, "[y/N]"},
		{"Y\n", false, true, "[y/N]"},
		{"yes\n", false, true, "[y/N]"},
		{"YES\n", false, true, "[y/N]"},
		{"n\n", true, false, "[Y/n]"},
		{"N\n", true, false, "[Y/n]"},
		{"no\n", true, false, "[Y/n]"},
		{"No\n", true, false, "[Y/n]"},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		got, err := New(strings.NewReader(tc.input), &out, Options{Locale: i18n.En}).ConfirmDefault("Continue?", tc.def)
		if err != nil || got != tc.want {
			t.Fatalf("input %q default %v = %v, %v", tc.input, tc.def, got, err)
		}
		if !strings.Contains(out.String(), tc.hint) || strings.Contains(out.String(), "yes/no") {
			t.Fatalf("input %q rendered a verbose confirmation:\n%s", tc.input, out.String())
		}
	}
}

func TestConfirmationDoesNotTreatQAsCancellation(t *testing.T) {
	var out bytes.Buffer
	got, err := New(strings.NewReader("q\nn\n"), &out, Options{Locale: i18n.En}).Confirm("Continue?")
	if err != nil || got {
		t.Fatalf("q should retry a y/n confirmation: %v, %v", got, err)
	}
	if strings.Count(out.String(), "Enter y or n.") != 1 {
		t.Fatalf("q did not produce one validation retry:\n%s", out.String())
	}
}

func TestSemanticColorsAreTTYOnly(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	var out bytes.Buffer
	ui := New(strings.NewReader(""), &out, Options{Locale: i18n.En, TTY: true, Color: true})
	ui.Success("installed")
	ui.Warning("review this")
	ui.Failure("failed")
	ui.Info("working")
	ui.Result(nil)
	ui.Result(errors.New("synthetic"))
	text := out.String()
	for _, code := range []string{"\x1b[32;1m", "\x1b[33;1m", "\x1b[31;1m", "\x1b[36m"} {
		if !strings.Contains(text, code) {
			t.Fatalf("semantic color %q missing from TTY output: %q", code, text)
		}
	}

	out.Reset()
	ui = New(strings.NewReader(""), &out, Options{Locale: i18n.En, TTY: false, Color: true})
	ui.Success("installed")
	ui.Warning("review this")
	ui.Failure("failed")
	ui.Info("working")
	if strings.Contains(out.String(), "\x1b") {
		t.Fatalf("redirected output contains terminal escapes: %q", out.String())
	}
}

func TestStatusCardsUseStateTone(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	for _, tc := range []struct {
		status string
		code   string
	}{
		{"READY", "\x1b[32;1m"},
		{"ACTION REQUIRED", "\x1b[33;1m"},
		{"FAILED", "\x1b[31;1m"},
	} {
		var out bytes.Buffer
		New(nil, &out, Options{TTY: true, Color: true}).StatusCard(tc.status, "Node", nil)
		if !strings.Contains(out.String(), tc.code) {
			t.Fatalf("status %q missing semantic tone: %q", tc.status, out.String())
		}
	}
}

func TestMenuHighlightsOnlyTheRecommendedDefault(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	var out bytes.Buffer
	got, err := New(strings.NewReader("\n"), &out, Options{TTY: true, Color: true}).Choose("Setup", []string{"Recommended", "Advanced"}, 1)
	if err != nil || got != 1 {
		t.Fatalf("default menu choice = %d, %v", got, err)
	}
	if !strings.Contains(out.String(), "\x1b[32;1m  1  Recommended") || strings.Contains(out.String(), "\x1b[32;1m  2  Advanced") {
		t.Fatalf("recommended menu hierarchy missing: %q", out.String())
	}
}

func TestRootMenuUsesExitFooter(t *testing.T) {
	var out bytes.Buffer
	ui := New(strings.NewReader("q\n0\n"), &out, Options{Locale: i18n.En})
	_, err := ui.ChooseRoot("Management", []string{"Install & updates"}, 0)
	if !errors.Is(err, ErrBack) {
		t.Fatalf("exit choice: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "0  Exit") || !strings.Contains(text, "0 to exit") || strings.Contains(text, "0  Back") || strings.Contains(text, "q to") || strings.Contains(text, "q  ") {
		t.Fatalf("root footer is ambiguous:\n%s", text)
	}
}

func TestWidthColorAndUntrustedDisplay(t *testing.T) {
	for _, width := range []int{48, 80, 120} {
		for _, locale := range []i18n.Locale{i18n.En, i18n.Fa} {
			var out bytes.Buffer
			ui := New(strings.NewReader(""), &out, Options{Locale: locale, Width: width, Color: true})
			ui.Section(strings.Repeat("عنوان ", 30))
			ui.Field("build", strings.Repeat("a", 140)+"\x1b[31mBAD\x1b]52;c;payload\a\rINJECT\u202e")
			if strings.ContainsAny(out.String(), "\x1b\r\a\u202e") {
				t.Fatalf("control escaped: %q", out.String())
			}
			for _, line := range strings.Split(out.String(), "\n") {
				if utf8.RuneCountInString(line) > width {
					t.Fatalf("width%d: %q", width, line)
				}
			}
		}
	}
	for _, env := range []struct{ no, term string }{{"1", "xterm"}, {"", "dumb"}} {
		t.Setenv("NO_COLOR", env.no)
		t.Setenv("TERM", env.term)
		var out bytes.Buffer
		New(strings.NewReader(""), &out, Options{TTY: true, Color: true}).Section("hello")
		if strings.Contains(out.String(), "\x1b") {
			t.Fatal(out.String())
		}
	}
}

func TestSecretDoesNotPrefetchOrEcho(t *testing.T) {
	var out bytes.Buffer
	ui := New(strings.NewReader("name\nsynthetic-secret\nnext\n"), &out, Options{Locale: i18n.En})
	name, _ := ui.Ask("name", "")
	secret, err := ui.Secret("password")
	next, _ := ui.Ask("next", "")
	if name != "name" || secret != "synthetic-secret" || next != "next" || err != nil {
		t.Fatalf("input sequencing failed: %v", err)
	}
	if strings.Contains(out.String(), "synthetic-secret") {
		t.Fatal("secret echoed")
	}
	if !strings.Contains(out.String(), "password: ") || strings.Contains(out.String(), "password\n> ") {
		t.Fatalf("secret prompt is not compact:\n%s", out.String())
	}
	_, err = New(strings.NewReader(strings.Repeat("x", 4097)+"\n"), &out, Options{}).Ask("bounded", "")
	if err == nil {
		t.Fatal("unbounded input")
	}
}

func TestCompactPromptAndSummaryLayout(t *testing.T) {
	var out bytes.Buffer
	ui := New(strings.NewReader("\n"), &out, Options{Locale: i18n.En, Width: 80})
	got, err := ui.Ask("Panel domain", "auto-detect")
	if err != nil || got != "auto-detect" {
		t.Fatalf("default answer = %q, %v", got, err)
	}
	ui.Field("Panel", "https://vpn.example.com")
	text := out.String()
	if !strings.Contains(text, "Panel domain [auto-detect]: ") || strings.Contains(text, "\n> ") {
		t.Fatalf("prompt is not compact:\n%s", text)
	}
	if !strings.Contains(text, "Panel: https://vpn.example.com\n") {
		t.Fatalf("summary field is not compact:\n%s", text)
	}
}

func TestWideSectionUsesReadableMeasure(t *testing.T) {
	var out bytes.Buffer
	New(strings.NewReader(""), &out, Options{Width: 120}).Section("Connection")
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("section lines = %q", lines)
	}
	if got := utf8.RuneCountInString(lines[1]); got > 72 {
		t.Fatalf("divider is %d columns; want at most 72", got)
	}
}

func TestHeaderKeepsBrandAndPurposeTogether(t *testing.T) {
	var out bytes.Buffer
	New(strings.NewReader(""), &out, Options{Width: 80}).Header("WG-GUARD", "Secure AmneziaWG node setup")
	text := out.String()
	if !strings.Contains(text, "WG-GUARD\nSecure AmneziaWG node setup\n") {
		t.Fatalf("header hierarchy missing:\n%s", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if utf8.RuneCountInString(line) > 72 {
			t.Fatalf("header line exceeds readable measure: %q", line)
		}
	}
}

func TestStatusCardIsCompactAtSupportedSSHWidths(t *testing.T) {
	for _, width := range []int{40, 48, 80} {
		var out bytes.Buffer
		ui := New(strings.NewReader(""), &out, Options{Locale: i18n.En, Width: width})
		ui.StatusCard("READY", "Local manager", []StatusField{
			{Label: "Build", Value: "v1.2.3 · 0123456789ab"},
			{Label: "Next", Value: "Choose Install to configure this server"},
		})
		text := out.String()
		if !strings.Contains(text, "READY · Local manager") || !strings.Contains(text, "Build") {
			t.Fatalf("width %d card hierarchy missing:\n%s", width, text)
		}
		for _, line := range strings.Split(text, "\n") {
			if utf8.RuneCountInString(line) > width {
				t.Fatalf("width %d overflow: %q", width, line)
			}
		}
	}
}

func ExampleUI_Section() {
	var out bytes.Buffer
	New(strings.NewReader(""), &out, Options{Width: 48}).Section("WG-Guard")
	fmt.Print(out.String())
	// Output:
	// WG-Guard
	// ────────────────────────────────────────────────
}

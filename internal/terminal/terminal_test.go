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
	for _, ending := range []string{"0\n", "q\n", "", "1"} {
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
	var out bytes.Buffer
	ui := New(strings.NewReader("bad\n9\n2\n"), &out, Options{Locale: i18n.En})
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
	_, err = New(strings.NewReader(strings.Repeat("x", 4097)+"\n"), &out, Options{}).Ask("bounded", "")
	if err == nil {
		t.Fatal("unbounded input")
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

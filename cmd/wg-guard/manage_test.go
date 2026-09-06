package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestBootstrapManagementEntryPreservesAcquiredBuild(t *testing.T) {
	want := []string{"--build-metadata", "/private/build.json", "--lang", "fa"}
	if got := managementSetup(nil, "/private/build.json", "fa"); !reflect.DeepEqual(got, want) {
		t.Fatalf("fresh bootstrap source/locale lost: %v", got)
	}
	if got := managementSetup(&install.State{Mode: install.ModeDocker}, "/private/build.json", "fa"); got != nil {
		t.Fatal("installed bootstrap tried reinstall")
	}
	if got := managementSetup(nil, "", "en"); got != nil {
		t.Fatal("ordinary management opening started setup")
	}
}

func TestManagerActionCommandsAndSecretTransport(t *testing.T) {
	cases := []struct {
		script string
		args   []string
		secret string
	}{
		{"2\n1\nq\n", []string{"status"}, ""},
		{"2\n2\nq\n", []string{"doctor"}, ""},
		{"2\n6\nyes\nq\n", []string{"restart", "--yes"}, ""},
		{"1\n3\nyes\nq\n", []string{"update", "--rollback"}, ""},
		{"1\n5\nyes\n", []string{"uninstall", "--yes"}, ""},
		{"3\n1\nyes\nq\n", []string{"backup", "create"}, ""},
		{"3\n3\n/private/archive.wgg.age\nyes\nsynthetic-archive-password\nyes\nq\n", []string{"restore", "/private/archive.wgg.age", "--yes", "--password"}, "synthetic-archive-password\n"},
		{"3\n4\n۰۳:۳۰\nyes\nq\n", []string{"backup", "schedule-add", "--kind", "daily", "--time", "03:30"}, ""},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		calls := 0
		m := manager{ui: terminal.New(strings.NewReader(tc.script), &out, terminal.Options{Locale: i18n.Fa}), catalog: menuCatalog{}, run: func(_ context.Context, args []string, in io.Reader) error {
			calls++
			if !reflect.DeepEqual(args, tc.args) {
				t.Fatalf("args: %v", args)
			}
			if in != nil {
				b, _ := io.ReadAll(in)
				if string(b) != tc.secret {
					t.Fatal("secret stdin altered")
				}
			}
			return nil
		}}
		_ = m.loop(context.Background())
		if calls != 1 {
			t.Fatalf("%q calls=%d", tc.script, calls)
		}
		if tc.secret != "" && strings.Contains(out.String(), strings.TrimSpace(tc.secret)) {
			t.Fatal("secret in menu output")
		}
	}
}

type menuCatalog struct{ empty bool }

func (c menuCatalog) Releases(context.Context) ([]distribution.Release, error) {
	if c.empty {
		return nil, nil
	}
	return []distribution.Release{{Tag: "v1.2.3", PublishedAt: "2026-09-05T10:00:00Z"}}, nil
}
func (c menuCatalog) Resolve(_ context.Context, s distribution.Selection) (distribution.Build, error) {
	return distribution.Build{Channel: s.Channel, Ref: s.Ref, Commit: strings.Repeat("a", 40), Version: "v1.2.3"}, nil
}

func TestManagerNavigationNoStartupMutationAndSafeReview(t *testing.T) {
	for _, script := range []string{"q\n", "0\n", "", "1\n0\nq\n", "1\n3\n\n0\nq\n"} {
		var out bytes.Buffer
		calls := 0
		m := manager{ui: terminal.New(strings.NewReader(script), &out, terminal.Options{Locale: i18n.En}), catalog: menuCatalog{}, run: func(context.Context, []string, io.Reader) error { calls++; return nil }}
		if err := m.loop(context.Background()); err != nil && !errors.Is(err, terminal.ErrCanceled) {
			t.Fatal(err)
		}
		if calls != 0 {
			t.Fatalf("%q mutated on startup/back/blank review", script)
		}
	}
}
func TestSourcePickerMetadataAndExplicitDevelopment(t *testing.T) {
	for _, tc := range []struct {
		script       string
		empty        bool
		channel, ref string
	}{{"1\nyes\n", false, "release", "v1.2.3"}, {"2\n1\nyes\n", false, "release", "v1.2.3"}, {"3\nyes\n", true, "commit", strings.Repeat("a", 40)}, {"4\n" + strings.Repeat("a", 40) + "\nyes\n", true, "commit", strings.Repeat("a", 40)}} {
		var out bytes.Buffer
		u := terminal.New(strings.NewReader(tc.script), &out, terminal.Options{Locale: i18n.En})
		s, err := pickSource(context.Background(), u, menuCatalog{tc.empty}, "installed-v0")
		if err != nil || s.Channel != tc.channel || s.Ref != tc.ref {
			t.Fatalf("selection %+v %v", s, err)
		}
		if !strings.Contains(out.String(), "installed-v0") || !strings.Contains(out.String(), "v1.2.3") {
			t.Fatal("metadata missing")
		}
	}
	var out bytes.Buffer
	if _, err := pickSource(context.Background(), terminal.New(strings.NewReader("1\nq\n"), &out, terminal.Options{}), menuCatalog{true}, ""); !errors.Is(err, terminal.ErrCanceled) {
		t.Fatalf("empty stable silently fell back: %v", err)
	}
}

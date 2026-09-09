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
	"unicode"
)

func containsRTLScript(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		return unicode.In(r, unicode.Arabic) || unicode.In(r, unicode.Hebrew)
	}) >= 0
}

func TestTerminalCommandsAlwaysSelectEnglish(t *testing.T) {
	t.Setenv("WGG_LANG", "fa")
	t.Setenv("LANG", "fa_IR.UTF-8")
	if got := terminalLocale(); got != "en" {
		t.Fatalf("terminal locale = %q, want en", got)
	}

	o, err := parseInstallOptions([]string{"--yes", "--lang", "fa"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Locale != i18n.En {
		t.Fatalf("legacy install language selected %q", o.Locale)
	}
	b, err := parseBackupFlags("list", []string{"--lang", "fa"})
	if err != nil {
		t.Fatal(err)
	}
	if b.lang != string(i18n.En) {
		t.Fatalf("legacy backup language selected %q", b.lang)
	}
}

func TestTerminalManagerHasNoLanguageSwitcher(t *testing.T) {
	var out bytes.Buffer
	m := manager{
		ui:      terminal.New(strings.NewReader("0\n"), &out, terminal.Options{Locale: i18n.Fa}),
		catalog: menuCatalog{},
		run:     func(context.Context, []string, io.Reader) error { return nil },
	}
	if err := m.loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if containsRTLScript(out.String()) {
		t.Fatalf("terminal manager exposed a non-English language option:\n%s", out.String())
	}
}

func TestManagerRootMenusAreStateAware(t *testing.T) {
	cases := []struct {
		state managerView
		first string
		want  []string
	}{
		{managerFresh, "install_cached", []string{"install_cached", "install_choose", "readiness", "help_short"}},
		{managerInstalled, "lifecycle", []string{"lifecycle", "access", "backups", "operations", "uninstall"}},
		{managerInstallRecovery, "cleanup_install", []string{"cleanup_install", "readiness"}},
		{managerUninstallRecovery, "uninstall_resume", []string{"uninstall_resume"}},
		{managerRecovery, "recover_now", []string{"recover_now", "lifecycle", "access", "backups", "operations"}},
	}
	for _, tc := range cases {
		menu := managerRootMenu(tc.state)
		if len(menu.items) != len(tc.want) || menu.items[0] != tc.first || !reflect.DeepEqual(menu.items, tc.want) {
			t.Fatalf("state %d menu = %+v, want %v", tc.state, menu, tc.want)
		}
	}
}

func TestInterruptedUninstallGetsDedicatedRecoveryView(t *testing.T) {
	st := &install.State{Schema: install.StateSchema, Mode: install.ModeDocker, ConfigPath: install.ConfigPath, DataDir: install.DataDir, ComposePath: install.ComposePth, BinPath: install.BinPath}
	j := &install.Journal{Schema: 1, Operation: "uninstall", Stage: "recovery-required", Before: st}
	if got := classifyManagerView(st, j); got != managerUninstallRecovery {
		t.Fatalf("interrupted uninstall view = %v", got)
	}
	if got := classifyManagerView(nil, j); got != managerUninstallRecovery {
		t.Fatalf("interrupted uninstall after state removal view = %v", got)
	}
}

func TestInterruptedUninstallOverviewDoesNotReadMissingBootConfig(t *testing.T) {
	var out bytes.Buffer
	m := manager{ui: terminal.New(strings.NewReader(""), &out, terminal.Options{Locale: i18n.En})}
	st := &install.State{Schema: install.StateSchema, Mode: install.ModeDocker, ConfigPath: install.ConfigPath, DataDir: install.DataDir, ComposePath: install.ComposePth, BinPath: install.BinPath}
	j := &install.Journal{Schema: 1, Operation: "uninstall", Stage: "recovery-required", Before: st}
	if handled := m.prepareOverview(st, j); !handled {
		t.Fatal("uninstall recovery fell through to runtime config and health probing")
	}
	text := out.String()
	if !strings.Contains(text, "Uninstall did not finish") || !strings.Contains(text, "Continue removal or choose a full reset") {
		t.Fatalf("uninstall recovery guidance missing:\n%s", text)
	}
}

func TestInterruptedInitialSetupGetsOnlySafeCleanupAndDiagnostics(t *testing.T) {
	st := &install.State{Schema: install.StateSchema, Mode: install.ModeDocker, Recovery: "install-incomplete", ConfigPath: install.ConfigPath, DataDir: install.DataDir, ComposePath: install.ComposePth, BinPath: install.BinPath}
	j := &install.Journal{Schema: 1, Operation: "install", Stage: "recovery-required", After: st}
	if got := classifyManagerView(st, j); got != managerInstallRecovery {
		t.Fatalf("safe interrupted setup view = %v", got)
	}
	j.DataMayHaveChanged = true
	if got := classifyManagerView(st, j); got != managerInstallRecovery {
		t.Fatalf("interrupted initial setup lost its guided reset view: %v", got)
	}

	var got []string
	m := manager{
		ui:                 terminal.New(strings.NewReader("y\n"), io.Discard, terminal.Options{Locale: i18n.En}),
		view:               managerInstallRecovery,
		installCleanupSafe: true,
		run: func(_ context.Context, args []string, _ io.Reader) error {
			got = append([]string(nil), args...)
			return nil
		},
	}
	if err := m.rootAction(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if want := []string{"recover-install", "--yes"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("safe cleanup args = %v, want %v", got, want)
	}

	got = nil
	m.installCleanupSafe = false
	m.ui = terminal.New(strings.NewReader("1\ny\n"), io.Discard, terminal.Options{Locale: i18n.En})
	if err := m.rootAction(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if want := []string{"uninstall", "--yes"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mutated setup reset args = %v, want %v", got, want)
	}
}

func TestManagerRootMenusFitNarrowEnglishTerminalsWithoutMutation(t *testing.T) {
	for _, width := range []int{40, 48, 80} {
		for _, view := range []managerView{managerFresh, managerInstalled, managerUninstallRecovery, managerRecovery} {
			var out bytes.Buffer
			calls := 0
			m := manager{
				ui:   terminal.New(strings.NewReader("0\n"), &out, terminal.Options{Locale: i18n.Fa, Width: width}),
				view: view,
				run:  func(context.Context, []string, io.Reader) error { calls++; return nil },
			}
			if err := m.loop(context.Background()); err != nil {
				t.Fatal(err)
			}
			if calls != 0 || containsRTLScript(out.String()) {
				t.Fatalf("width=%d view=%d mutated or rendered RTL:\n%s", width, view, out.String())
			}
			for _, line := range strings.Split(out.String(), "\n") {
				if len([]rune(line)) > width {
					t.Fatalf("width=%d view=%d overflow: %q", width, view, line)
				}
			}
		}
	}
}

func TestInterruptedUninstallRecoveryOffersSafeRemovalOrFullReset(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script string
		want   []string
	}{
		{name: "keep data", script: "1\ny\n", want: []string{"uninstall", "--yes"}},
		{name: "full reset", script: "2\ny\n", want: []string{"uninstall", "--purge-data", "--purge-packages", "--yes"}},
		{name: "complete removal", script: "3\ny\n", want: []string{"uninstall", "--purge-all", "--yes"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			m := manager{
				ui:   terminal.New(strings.NewReader(tc.script), io.Discard, terminal.Options{Locale: i18n.En}),
				view: managerUninstallRecovery,
				run: func(_ context.Context, args []string, _ io.Reader) error {
					got = append([]string(nil), args...)
					return nil
				},
			}
			if err := m.rootAction(context.Background(), 1); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("uninstall args = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCompleteRemovalExitsManagerAfterDeletingItsCache(t *testing.T) {
	var got []string
	overviews := 0
	m := manager{
		ui: terminal.New(strings.NewReader("5\n3\ny\n"), io.Discard, terminal.Options{Locale: i18n.En}),
		overview: func() error {
			overviews++
			return nil
		},
		view: managerInstalled,
		run: func(_ context.Context, args []string, _ io.Reader) error {
			got = append([]string(nil), args...)
			return nil
		},
	}
	if err := m.loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"uninstall", "--purge-all", "--yes"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("complete removal args = %v, want %v", got, want)
	}
	if overviews != 1 {
		t.Fatalf("manager reopened %d times after deleting itself", overviews)
	}
}

func TestCertificateRecoveryUsesTheRecordedLineage(t *testing.T) {
	var got []string
	m := manager{
		ui:   terminal.New(strings.NewReader("yes\n"), io.Discard, terminal.Options{Locale: i18n.En}),
		view: managerRecovery, journalOperation: "certificate",
		recoveryLineage: "/etc/letsencrypt/live/wg-guard-123456789abc",
		run: func(_ context.Context, args []string, _ io.Reader) error {
			got = append([]string(nil), args...)
			return nil
		},
	}
	if err := m.rootAction(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	want := []string{"certificate-sync", "--lineage", "/etc/letsencrypt/live/wg-guard-123456789abc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recovery args = %v, want %v", got, want)
	}
}

func TestExposureRecoveryUsesTheDedicatedRecoveryCommand(t *testing.T) {
	var got []string
	m := manager{
		ui:   terminal.New(strings.NewReader("yes\n"), io.Discard, terminal.Options{Locale: i18n.En}),
		view: managerRecovery, journalOperation: "exposure",
		run: func(_ context.Context, args []string, _ io.Reader) error {
			got = append([]string(nil), args...)
			return nil
		},
	}
	if err := m.rootAction(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if want := []string{"exposure", "recover"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recovery args = %v, want %v", got, want)
	}
}

func TestCommandHelpPresentsEnglishOnlyTerminal(t *testing.T) {
	if strings.Contains(usage, "--lang fa") || strings.Contains(usage, "fa|en") || containsRTLScript(usage) {
		t.Fatalf("command help advertises a non-English terminal mode:\n%s", usage)
	}
	if !strings.Contains(usage, "wg-guard                 Open the local manager") {
		t.Fatalf("command help does not foreground the rerun command:\n%s", usage)
	}
}

func TestFreshManagerUsesCachedBuildOnlyAfterInstallSelection(t *testing.T) {
	var out bytes.Buffer
	calls := 0
	m := manager{
		ui:                terminal.New(strings.NewReader("\n0\n"), &out, terminal.Options{Locale: i18n.En}),
		catalog:           panicCatalog{},
		bootstrapMetadata: "/var/cache/wg-guard/manager-build.json",
		run: func(_ context.Context, args []string, _ io.Reader) error {
			calls++
			want := []string{"install", "--build-metadata", "/var/cache/wg-guard/manager-build.json", "--lang", "en"}
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("install args = %v, want %v", args, want)
			}
			return nil
		},
	}
	if err := m.loop(context.Background()); err != nil && !errors.Is(err, terminal.ErrCanceled) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("manager performed %d mutations; want one explicit Install", calls)
	}

	calls = 0
	m.ui = terminal.New(strings.NewReader("0\n"), &out, terminal.Options{Locale: i18n.En})
	if err := m.loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("fresh manager auto-started installation")
	}
}

func TestPendingLifecycleStopsUpdateBeforeSourceCatalog(t *testing.T) {
	calls := 0
	m := manager{
		ui:      terminal.New(strings.NewReader("1\n1\n0\n0\n"), io.Discard, terminal.Options{Locale: i18n.En}),
		catalog: panicCatalog{}, view: managerInstalled,
		lifecycleReady: func() error {
			calls++
			return errors.New("install recovery is required")
		},
		run: func(context.Context, []string, io.Reader) error {
			t.Fatal("update ran with a pending lifecycle operation")
			return nil
		},
	}
	if err := m.loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("lifecycle guard calls = %d, want 1", calls)
	}
}

type panicCatalog struct{}

func (panicCatalog) Releases(context.Context) ([]distribution.Release, error) {
	panic("fresh cached install contacted the release catalog")
}
func (panicCatalog) Resolve(context.Context, distribution.Selection) (distribution.Build, error) {
	panic("fresh cached install resolved a network build")
}

func TestManagerLegacyReadinessIsExplicitlyUnknown(t *testing.T) {
	for _, locale := range []i18n.Locale{i18n.En, i18n.Fa} {
		var out strings.Builder
		u := terminal.New(strings.NewReader(""), &out, terminal.Options{Locale: locale})
		showRecordedReadiness(u, &install.State{Schema: 1})
		want := "Unknown / not recorded"
		if locale == i18n.Fa {
			want = "نامشخص / ثبت نشده"
		}
		if strings.Count(out.String(), want) != 2 {
			t.Fatal("legacy readiness missing explicit unknown", out.String())
		}
		out.Reset()
		showRecordedReadiness(u, &install.State{TLSReadiness: "verified", Core: install.CoreReport{Requested: install.CoreBundle{ID: "awg-2026-08"}}})
		if !strings.Contains(out.String(), "verified") || !strings.Contains(out.String(), "awg-2026-08") || strings.Contains(out.String(), want) {
			t.Fatal("known readiness changed", out.String())
		}
	}
}

func TestManagerActionCommandsAndSecretTransport(t *testing.T) {
	cases := []struct {
		script string
		args   []string
		secret string
	}{
		{"4\n1\n0\n0\n", []string{"status"}, ""},
		{"4\n2\n0\n0\n", []string{"doctor"}, ""},
		{"1\n1\n0\n0\n", []string{"update"}, ""},
		{"1\n4\nyes\n0\n0\n", []string{"restart", "--yes"}, ""},
		{"1\n2\nyes\n0\n0\n", []string{"update", "--rollback"}, ""},
		{"5\n1\ny\n0\n", []string{"uninstall", "--yes"}, ""},
		{"3\n1\nyes\n0\n0\n", []string{"backup", "create"}, ""},
		{"3\n3\n/private/archive.wgg.age\nyes\nsynthetic-archive-password\n0\n0\n", []string{"restore", "/private/archive.wgg.age", "--password"}, "synthetic-archive-password\n"},
		{"3\n4\n2\ndaily\n1\n۰۳:۳۰\n0\nyes\nyes\n0\n0\n", []string{"backup", "schedule-add", "--name", "daily", "--kind", "daily", "--time", "03:30", "--retention", "0"}, ""},
		{"2\n1\n0\n0\n", []string{"exposure", "status"}, ""},
		{"2\n2\n0\n0\n", []string{"exposure", "configure"}, ""},
		{"2\n3\nyes\n0\n0\n", []string{"exposure", "renew"}, ""},
		{"2\n4\nyes\n0\n0\n", []string{"exposure", "private", "--yes"}, ""},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		calls := 0
		m := manager{ui: terminal.New(strings.NewReader(tc.script), &out, terminal.Options{Locale: i18n.Fa}), catalog: menuCatalog{}, view: managerInstalled, installed: "v1", run: func(_ context.Context, args []string, in io.Reader) error {
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
	for _, script := range []string{"0\n", "", "1\n0\n0\n", "1\n3\n\n0\n0\n"} {
		var out bytes.Buffer
		calls := 0
		m := manager{ui: terminal.New(strings.NewReader(script), &out, terminal.Options{Locale: i18n.En}), catalog: menuCatalog{}, view: managerInstalled, installed: "v1", run: func(context.Context, []string, io.Reader) error { calls++; return nil }}
		if err := m.loop(context.Background()); err != nil && !errors.Is(err, terminal.ErrCanceled) {
			t.Fatal(err)
		}
		if calls != 0 {
			t.Fatalf("%q mutated on startup/back/blank review", script)
		}
	}
}

func TestNestedManagerBackReturnsWithoutTechnicalErrorOrFalseSuccess(t *testing.T) {
	for _, script := range []string{"1\n0\n0\n", "1\n1\n0\n0\n"} {
		var out bytes.Buffer
		m := manager{
			ui:   terminal.New(strings.NewReader(script), &out, terminal.Options{Locale: i18n.En}),
			view: managerInstalled,
			run:  func(context.Context, []string, io.Reader) error { return nil },
		}
		if err := m.loop(context.Background()); err != nil {
			t.Fatalf("script %q returned technical cancellation: %v", script, err)
		}
		if strings.Contains(out.String(), "Done.") {
			t.Fatalf("script %q printed false success after opening/canceling a menu:\n%s", script, out.String())
		}
	}
}
func TestSourcePickerMetadataAndExplicitDevelopment(t *testing.T) {
	for _, tc := range []struct {
		script       string
		empty        bool
		channel, ref string
	}{{"\n\n", false, "release", "v1.2.3"}, {"2\n1\nyes\n", false, "release", "v1.2.3"}, {"3\nyes\n", true, "commit", strings.Repeat("a", 40)}, {"4\n" + strings.Repeat("a", 40) + "\nyes\n", true, "commit", strings.Repeat("a", 40)}} {
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
	if _, err := pickSource(context.Background(), terminal.New(strings.NewReader("1\n0\n"), &out, terminal.Options{}), menuCatalog{true}, ""); !errors.Is(err, terminal.ErrBack) {
		t.Fatalf("empty stable silently fell back: %v", err)
	}
}

package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

func TestInstallArchiveFlagsPreserveSourceAccessAndKeepPasswordsOutOfArgs(t *testing.T) {
	o, err := parseInstallOptions([]string{"--from-backup", "/private/recovery.wgg", "--backup-password-file", "/private/password", "--yes"})
	if err != nil || o.ArchivePath != "/private/recovery.wgg" || o.ArchivePasswordFile != "/private/password" {
		t.Fatal("archive options", err)
	}
	for _, args := range [][]string{{"--backup-password"}, {"--backup-password-file", "/private/password"}, {"--from-backup", "archive.wgg", "--owner-username", "replacement"}, {"--from-backup", "archive.wgg", "--backup-password", "--backup-password-file", "/private/password"}} {
		if _, err := parseInstallOptions(args); err == nil {
			t.Fatal("conflicting archive/access options accepted")
		}
	}
}

func TestFreshManagerArchiveJourneyUsesSelectedManagerWithoutSecretTransport(t *testing.T) {
	var out bytes.Buffer
	var got []string
	m := manager{ui: terminal.New(strings.NewReader("/private/recovery.wgg\n"), &out, terminal.Options{}), bootstrapMetadata: "/private/verified-manager.json", run: func(_ context.Context, args []string, input io.Reader) error {
		got = append([]string(nil), args...)
		if input != nil {
			t.Fatal("archive password requested by parent menu")
		}
		return nil
	}}
	if err := m.freshAction(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "install --build-metadata /private/verified-manager.json --lang en --from-backup /private/recovery.wgg" {
		t.Fatal("selected manager/archive identity not retained")
	}
	m.ui = terminal.New(strings.NewReader("\n"), io.Discard, terminal.Options{})
	got = nil
	if err := m.freshAction(context.Background(), 3); err != terminal.ErrBack || got != nil {
		t.Fatal("blank archive path started setup")
	}
}

func TestDomainWizardReturnsOnlyReviewedIntentAndCancelDoesNotExecute(t *testing.T) {
	var out bytes.Buffer
	m := manager{ui: terminal.New(strings.NewReader("https://SUB.example.test/\n3\n\n\ny\n"), &out, terminal.Options{})}
	args, err := m.domainForm("subscription")
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(args, " ")
	if !strings.Contains(line, "--origin https://sub.example.test") || !strings.Contains(line, "--method manual") {
		t.Fatal("domain intent was not normalized/reviewed")
	}
	m.ui = terminal.New(strings.NewReader("https://sub.example.test\n1\n\nn\n"), io.Discard, terminal.Options{})
	if args, err := m.domainForm("subscription"); err != terminal.ErrBack || args != nil {
		t.Fatal("canceled domain change produced executable intent")
	}
}

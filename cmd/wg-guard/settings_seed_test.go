package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
)

// TestSettingsSetStdin: `settings set KEY -stdin` stores the piped value
// (one trailing newline stripped); a secret key reads back as <set>, and the
// exact value round-trips through the registry encryption.
func TestSettingsSetStdin(t *testing.T) {
	cfgPath := testTokenConfig(t)
	const secret = "777000:AAE_settings_stdin_test"

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(secret + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	out := captureStdout(t, func() {
		if err := runSettings([]string{"set", "-config", cfgPath, "backup.telegram_token", "-stdin"}); err != nil {
			t.Fatalf("settings set: %v", err)
		}
	})
	if !strings.Contains(out, "updated") {
		t.Fatalf("output: %q", out)
	}

	env, err := loadCLIEnv(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	ctx := context.Background()
	got, err := env.Reg.GetSecret(ctx, "backup.telegram_token")
	if err != nil {
		t.Fatal(err)
	}
	if got != secret {
		t.Fatalf("stored value = %q, want the piped value without the newline", got)
	}

	// get/list never echo the secret.
	out = captureStdout(t, func() {
		if err := runSettings([]string{"get", "-config", cfgPath, "backup.telegram_token"}); err != nil {
			t.Fatalf("settings get: %v", err)
		}
	})
	if strings.TrimSpace(out) != "<set>" || strings.Contains(out, secret) {
		t.Fatalf("get output leaks or is wrong: %q", out)
	}
}

// TestBackupScheduleAdd: the installer-facing verb creates an enabled daily
// schedule row; invalid kinds are refused.
func TestBackupScheduleAdd(t *testing.T) {
	cfgPath := testTokenConfig(t)
	out := captureStdout(t, func() {
		if err := runBackup([]string{"schedule-add", "-config", cfgPath}); err != nil {
			t.Fatalf("schedule-add: %v", err)
		}
	})
	if !strings.Contains(out, "created") {
		t.Fatalf("output: %q", out)
	}

	env, err := loadCLIEnv(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	scheds, err := env.newBackupService().Schedules(context.Background())
	if err != nil || len(scheds) != 1 {
		t.Fatalf("schedules after add: %d %v", len(scheds), err)
	}
	s := scheds[0]
	if s.Kind != backup.KindDaily || s.TimeOfDay != "03:30" || !s.Enabled || s.Name != "installer-daily" {
		t.Fatalf("schedule shape: %+v", s)
	}
	if s.NextRunAt.IsZero() {
		t.Fatal("next run not computed")
	}
	if err := runBackup([]string{"schedule-add", "-config", cfgPath, "-kind", "monthly"}); err == nil {
		t.Fatal("invalid kind must be refused")
	}
}

package main

import (
	"context"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
	"io"
	"strings"
	"testing"
)

func TestBackupMissingFlagValuesDoNotPanic(t *testing.T) {
	for _, args := range [][]string{{"schedule-add", "--hours"}, {"schedule-add", "--config"}, {"create", "--output"}, {"list", "--config"}, {"telegram-test", "--config"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			defer func() {
				if recover() != nil {
					t.Error("missing value panicked")
				}
			}()
			if err := runBackup(args); err == nil {
				t.Fatal("missing value accepted")
			}
		})
	}
}

func TestScheduleCLICompleteLifecycle(t *testing.T) {
	cfg := testTokenConfig(t)
	run := func(args ...string) {
		t.Helper()
		captureStdout(t, func() {
			if err := runBackup(append(args, "--config", cfg)); err != nil {
				t.Fatal(err)
			}
		})
	}
	run("schedule-add", "--name", "interval", "--days", "2", "--retention", "7")
	e, err := loadCLIEnv(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	s := e.newBackupService()
	ctx := context.Background()
	rows, err := s.Schedules(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatal("missing created schedule")
	}
	id := rows[0].ID
	if rows[0].IntervalHours != 48 || rows[0].RetentionCount != 7 {
		t.Fatal("days or retention lost")
	}
	run("schedule-disable", "--id", id)
	sc, _ := s.GetSchedule(ctx, id)
	if sc.Enabled {
		t.Fatal("disable lost")
	}
	run("schedule-enable", "--id", id)
	sc, _ = s.GetSchedule(ctx, id)
	if !sc.Enabled {
		t.Fatal("enable lost")
	}
	run("schedule-update", "--id", id, "--name", "changed", "--kind", "interval", "--hours", "6")
	sc, _ = s.GetSchedule(ctx, id)
	if sc.IntervalHours != 6 || sc.Name != "changed" {
		t.Fatal("update lost")
	}
	run("schedule-list")
	run("schedule-delete", "--id", id)
	rows, _ = s.Schedules(ctx)
	if len(rows) != 0 {
		t.Fatal("delete lost")
	}
	for _, days := range []string{"0", "-1", "8", "9999999999999999999"} {
		if _, err := parseBackupFlags("schedule-add", []string{"--days", days}); err == nil {
			t.Fatal("invalid interval days accepted")
		}
	}
	u := terminal.New(strings.NewReader("yes"), io.Discard, terminal.Options{})
	if ok, err := u.Confirm("apply"); err == nil || ok {
		t.Fatal("EOF accepted as consent")
	}
}

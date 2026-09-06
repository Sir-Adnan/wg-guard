package install

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Model systemd's absent-unit distinction and interruption at the filesystem
// boundary, keeping the real lifecycle/state/journal implementation in use.
type nativeCleanupHost struct {
	*memHost
	failSeed         bool
	failRemoveBinary bool
	stopErr          error
	queryErr         error
	queryOutput      string
}

func (h *nativeCleanupHost) Run(ctx context.Context, args []string, d time.Duration) error {
	if h.failSeed && len(args) > 1 && args[0] == BinPath && args[1] == "settings" {
		return errors.New("seed failed before unit creation")
	}
	if len(args) > 1 && args[0] == "systemctl" && (args[1] == "stop" || args[1] == "disable") {
		h.commands = append(h.commands, memCmd{argv: args})
		if _, ok := h.files[UnitPath]; !ok {
			return errors.New("unit not loaded")
		}
		if args[1] == "stop" && h.stopErr != nil {
			return h.stopErr
		}
		return nil
	}
	return h.memHost.Run(ctx, args, d)
}
func (h *nativeCleanupHost) Output(ctx context.Context, args []string, d time.Duration) (string, error) {
	if len(args) > 1 && args[0] == "systemctl" && args[1] == "show" {
		h.commands = append(h.commands, memCmd{argv: args})
		if h.queryErr != nil {
			return "LoadState=not-found\nActiveState=inactive\n", h.queryErr
		}
		if h.queryOutput != "" {
			return h.queryOutput, nil
		}
		if strings.Join(args, " ") != "systemctl show wg-guard.service --property=LoadState --property=ActiveState" {
			return "", errors.New("unexpected unit query")
		}
		if _, ok := h.files[UnitPath]; !ok {
			return "LoadState=not-found\nActiveState=inactive\n", nil
		}
		return "LoadState=loaded\nActiveState=inactive\n", nil
	}
	return h.memHost.Output(ctx, args, d)
}
func (h *nativeCleanupHost) Remove(p string) error {
	if p == BinPath && h.failRemoveBinary {
		h.failRemoveBinary = false
		return errors.New("interrupted after unit removal")
	}
	return h.memHost.Remove(p)
}

func TestNativeCleanupBeforeUnitCreation(t *testing.T) {
	h := &nativeCleanupHost{memHost: newMemHost(), failSeed: true}
	p := Defaults()
	p.Mode = ModeNative
	p.PanelPort = healthServer(t, http.StatusOK)
	if _, err := Install(context.Background(), h, InstallOptions{Plan: p, Yes: true, Stdout: io.Discard}); err == nil {
		t.Fatal("injected seed failure accepted")
	}
	if _, ok := h.files[UnitPath]; ok {
		t.Fatal("fixture reached unit creation")
	}
	if _, ok := h.files[BinPath]; !ok {
		t.Fatal("fixture did not leave partial binary")
	}
	h.commands = nil
	rep, err := Uninstall(context.Background(), h, UninstallOptions{Yes: true, Stdout: io.Discard})
	if err != nil {
		t.Fatalf("confirmed absent unit blocked partial-install cleanup: %v", err)
	}
	if !rep.Stopped || rep.KeptData != DataDir {
		t.Fatal("partial cleanup lost stopped/data-preserved result")
	}
	for _, p := range []string{BinPath, ConfigPath, StatePath} {
		if _, ok := h.files[p]; ok {
			t.Fatalf("partial artifact retained: %s", p)
		}
	}
	if h.ran("systemctl", "stop") || h.ran("systemctl", "disable") {
		t.Fatal("attempted service mutation for confirmed absent unit")
	}
}
func TestNativeCleanupResumesAfterUnitRemoval(t *testing.T) {
	h := &nativeCleanupHost{memHost: installedFixture(t, ModeNative), failRemoveBinary: true}
	if _, err := Uninstall(context.Background(), h, UninstallOptions{Yes: true, Stdout: io.Discard}); err == nil {
		t.Fatal("injected removal failure accepted")
	}
	if _, ok := h.files[UnitPath]; ok {
		t.Fatal("fixture did not remove unit before interruption")
	}
	h.commands = nil
	if _, err := Uninstall(context.Background(), h, UninstallOptions{Yes: true, Stdout: io.Discard}); err != nil {
		t.Fatalf("absent unit blocked resumed uninstall: %v", err)
	}
	if _, ok := h.files[StatePath]; ok {
		t.Fatal("resumed uninstall retained state")
	}
	j, err := LoadJournal(h)
	if err != nil || j.Stage != "complete" {
		t.Fatalf("resumed uninstall not committed: %v", err)
	}
	if h.ran("systemctl", "stop") || h.ran("systemctl", "disable") {
		t.Fatal("resumed uninstall mutated absent unit")
	}
}
func TestNativeCleanupRejectsUnprovenStop(t *testing.T) {
	for _, tc := range []struct {
		name, output      string
		queryErr, stopErr error
	}{
		{name: "query failure", queryErr: errors.New("systemd unavailable")},
		{name: "stop failure", stopErr: errors.New("access denied")},
		{name: "missing activity", output: "LoadState=not-found\n"},
		{name: "absent but active", output: "LoadState=not-found\nActiveState=active\n"},
		{name: "unknown load", output: "LoadState=error\nActiveState=inactive\n"},
		{name: "still active after stop", output: "LoadState=loaded\nActiveState=active\n"},
		{name: "duplicate state", output: "LoadState=loaded\nActiveState=active\nActiveState=inactive\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &nativeCleanupHost{memHost: installedFixture(t, ModeNative), queryErr: tc.queryErr, stopErr: tc.stopErr, queryOutput: tc.output}
			if _, err := Uninstall(context.Background(), h, UninstallOptions{Yes: true, PurgeData: true, Stdout: io.Discard}); err == nil {
				t.Fatal("unproven stop accepted")
			}
			for _, p := range []string{UnitPath, BinPath, ConfigPath, StatePath} {
				if _, ok := h.files[p]; !ok {
					t.Fatalf("removed artifact without proven stop: %s", p)
				}
			}
			if !h.dirs[DataDir] {
				t.Fatal("purged data without proven stop")
			}
		})
	}
}

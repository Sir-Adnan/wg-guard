package install

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestComposeImageAddsOrRepairsOwnedLogPolicy(t *testing.T) {
	base := `name: wg-guard
services:
  wg-guard:
    image: old
    container_name: wg-guard
    logging:
      driver: json-file
      options:
        max-size: "unbounded"
    volumes:
      - /etc/wg-guard/wg-guard.toml:/etc/wg-guard/wg-guard.toml:ro
`
	got, err := composeImage([]byte(base), "sha256:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if strings.Count(text, "    logging:\n") != 1 || strings.Contains(text, "json-file") || strings.Contains(text, "unbounded") {
		t.Fatalf("old logging policy survived:\n%s", text)
	}
	for _, want := range []string{"driver: local", `max-size: "16m"`, `max-file: "8"`, `compress: "true"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("updated compose missing %q:\n%s", want, text)
		}
	}

	second, err := composeImage(got, "sha256:"+strings.Repeat("b", 64))
	if err != nil || strings.Count(string(second), "    logging:\n") != 1 {
		t.Fatalf("policy update is not idempotent: %v\n%s", err, second)
	}
}

func TestNativeUninstallRemovesOwnedJournalPolicy(t *testing.T) {
	h := &nativeCleanupHost{memHost: installedFixture(t, ModeNative)}
	state, err := LoadState(h)
	if err != nil {
		t.Fatal(err)
	}
	state.ExtraFiles = append(state.ExtraFiles, JournalRetentionPath)
	h.files[JournalRetentionPath] = memFile{data: []byte("owned"), perm: 0o644}
	h.files[OperationRetentionPath] = memFile{data: []byte(RenderOperationRetention()), perm: 0o644}
	state.ExtraFiles = addUnique(state.ExtraFiles, OperationRetentionPath)
	h.dirs[JournalRetentionDir] = true
	if err := saveState(h, state); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(context.Background(), h, UninstallOptions{Yes: true, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.files[JournalRetentionPath]; ok {
		t.Fatal("native journal retention policy survived uninstall")
	}
	if _, ok := h.files[OperationRetentionPath]; ok {
		t.Fatal("operation retention policy survived uninstall")
	}
	if !h.ran("systemctl", "daemon-reload") || !h.ran("systemctl", "try-restart", "systemd-journald@wg-guard.service") {
		t.Fatalf("systemd did not forget the removed unit/policy: %v", h.ranCommands())
	}
}

func TestUpdateMigratesExactOperationRetentionWithoutOverwritingForeignFile(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		h := installedFixture(t, ModeDocker)
		state, err := LoadState(h)
		if err != nil {
			t.Fatal(err)
		}
		state.ExtraFiles = slices.DeleteFunc(state.ExtraFiles, func(path string) bool { return path == OperationRetentionPath })
		delete(h.files, OperationRetentionPath)
		if err := saveState(h, state); err != nil {
			t.Fatal(err)
		}
		contractFixture(h)
		if err := Update(context.Background(), h, UpdateOptions{Image: "image:new", BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: io.Discard}); err != nil {
			t.Fatal(err)
		}
		updated, err := LoadState(h)
		if err != nil || !contains(updated.ExtraFiles, OperationRetentionPath) {
			t.Fatalf("operation retention ownership not migrated: %v %+v", err, updated)
		}
		if string(h.files[OperationRetentionPath].data) != RenderOperationRetention() || !h.ran("systemd-tmpfiles", "--create", OperationRetentionPath) {
			t.Fatalf("operation retention was not applied: %v", h.ranCommands())
		}
	})

	t.Run("foreign conflict", func(t *testing.T) {
		h := installedFixture(t, ModeDocker)
		state, err := LoadState(h)
		if err != nil {
			t.Fatal(err)
		}
		state.ExtraFiles = slices.DeleteFunc(state.ExtraFiles, func(path string) bool { return path == OperationRetentionPath })
		h.files[OperationRetentionPath] = memFile{data: []byte("foreign\n"), perm: 0o644}
		if err := saveState(h, state); err != nil {
			t.Fatal(err)
		}
		contractFixture(h)
		if err := Update(context.Background(), h, UpdateOptions{Image: "image:new", BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: io.Discard}); err == nil {
			t.Fatal("foreign tmpfiles policy was overwritten")
		}
		if got := string(h.files[OperationRetentionPath].data); got != "foreign\n" {
			t.Fatalf("foreign policy changed: %q", got)
		}
	})
}

func TestOperationRetentionMigrationRollsBackWhenStateCommitFails(t *testing.T) {
	base := installedFixture(t, ModeDocker)
	state, err := LoadState(base)
	if err != nil {
		t.Fatal(err)
	}
	state.ExtraFiles = slices.DeleteFunc(state.ExtraFiles, func(path string) bool { return path == OperationRetentionPath })
	delete(base.files, OperationRetentionPath)
	if err := saveState(base, state); err != nil {
		t.Fatal(err)
	}
	h := &faultHost{memHost: base, failRename: StatePath}
	if err := ensureOperationRetention(context.Background(), h, state, true); err == nil {
		t.Fatal("state commit failure accepted")
	}
	if _, ok := base.files[OperationRetentionPath]; ok {
		t.Fatal("failed ownership commit left an unowned tmpfiles policy")
	}
	loaded, err := LoadState(base)
	if err != nil || contains(loaded.ExtraFiles, OperationRetentionPath) {
		t.Fatalf("failed migration changed state: %v %+v", err, loaded)
	}
}

func TestFreshInstallRefusesForeignOperationPolicyBeforePrerequisites(t *testing.T) {
	h := newMemHost()
	h.files[OperationRetentionPath] = memFile{data: []byte("foreign\n"), perm: 0o644}
	plan := Defaults()
	plan.PanelPort = healthServer(t, 200)
	if _, err := Install(context.Background(), h, InstallOptions{Plan: plan, Yes: true, Stdout: io.Discard}); err == nil {
		t.Fatal("fresh install overwrote a foreign operation retention policy")
	}
	if h.ran("modprobe") || h.ran("systemd-tmpfiles") {
		t.Fatalf("foreign policy was discovered after host mutation: %v", h.ranCommands())
	}
	if got := string(h.files[OperationRetentionPath].data); got != "foreign\n" {
		t.Fatalf("foreign policy changed: %q", got)
	}
}

func legacyNativeLogFixture(t *testing.T) *memHost {
	t.Helper()
	h := installedFixture(t, ModeNative)
	state, err := LoadState(h)
	if err != nil {
		t.Fatal(err)
	}
	state.ExtraFiles = slices.DeleteFunc(state.ExtraFiles, func(path string) bool { return path == JournalRetentionPath })
	delete(h.files, JournalRetentionPath)
	unit := strings.ReplaceAll(string(h.files[UnitPath].data), "LogNamespace=wg-guard\n", "")
	h.files[UnitPath] = memFile{data: []byte(unit), perm: 0o644}
	if err := saveState(h, state); err != nil {
		t.Fatal(err)
	}
	h.commands = nil
	contractFixture(h)
	return h
}

func TestNativeUpdateMigratesUnitAndRetentionTransactionally(t *testing.T) {
	h := legacyNativeLogFixture(t)
	if err := Update(context.Background(), h, UpdateOptions{BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(h.files[UnitPath].data), "LogNamespace=wg-guard") {
		t.Fatal("updated native unit did not select its journal namespace")
	}
	if _, ok := h.files[JournalRetentionPath]; !ok {
		t.Fatal("updated native install has no retention policy")
	}
	state, err := LoadState(h)
	if err != nil || !contains(state.ExtraFiles, JournalRetentionPath) {
		t.Fatalf("updated state does not own retention policy: %v %+v", err, state)
	}
	if state.Previous == nil || state.Previous.Unit == "" || state.Current == nil || state.Current.Unit == "" {
		t.Fatalf("unit snapshots missing from lifecycle artifacts: %+v", state)
	}
	if !h.ran("systemctl", "daemon-reload") || !h.ran("systemctl", "restart", "wg-guard") {
		t.Fatalf("updated unit was not reloaded before restart: %v", h.ranCommands())
	}
	if !h.ran("systemctl", "try-restart", "systemd-journald@wg-guard.service") {
		t.Fatalf("existing journal namespace did not reload its bounds: %v", h.ranCommands())
	}
}

func TestNativeUpdateFailureRestoresLegacyUnitAndOwnership(t *testing.T) {
	base := legacyNativeLogFixture(t)
	legacyUnit := string(base.files[UnitPath].data)
	h := &faultHost{memHost: base, failRun: "systemctl restart"}
	err := Update(context.Background(), h, UpdateOptions{BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: io.Discard})
	if err == nil {
		t.Fatal("restart failure accepted")
	}
	if got := string(base.files[UnitPath].data); got != legacyUnit {
		t.Fatalf("failed update did not restore legacy unit:\n%s", got)
	}
	if _, ok := base.files[JournalRetentionPath]; ok {
		t.Fatal("failed update retained an unowned journal policy")
	}
	state, loadErr := LoadState(base)
	if loadErr != nil || contains(state.ExtraFiles, JournalRetentionPath) {
		t.Fatalf("failed update changed retention ownership: %v %+v", loadErr, state)
	}
}

func TestNativeUpdateRefusesUnownedJournalPolicy(t *testing.T) {
	h := legacyNativeLogFixture(t)
	h.files[JournalRetentionPath] = memFile{data: []byte("foreign\n"), perm: 0o644}
	before := string(h.files[UnitPath].data)
	if err := Update(context.Background(), h, UpdateOptions{BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: io.Discard}); err == nil {
		t.Fatal("unowned journal policy was overwritten")
	}
	if got := string(h.files[JournalRetentionPath].data); got != "foreign\n" || string(h.files[UnitPath].data) != before {
		t.Fatal("unowned journal policy refusal mutated the host")
	}
}

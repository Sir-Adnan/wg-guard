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

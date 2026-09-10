package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestOperationJournalPrunesSevenDaysAndKeepsFixedPrivateRecords(t *testing.T) {
	now := time.Date(2026, 9, 10, 14, 30, 0, 0, time.UTC)
	h := newMemHost()
	h.dirs[OperationLogDir] = true
	h.files[OperationLogDir+"/operations-2026-09-03.jsonl"] = memFile{data: []byte("old\n"), perm: 0o600}
	h.files[OperationLogDir+"/operations-2026-09-04.jsonl"] = memFile{data: []byte("corrupt\npartial"), perm: 0o600}
	h.files[OperationLogDir+"/operator-notes.txt"] = memFile{data: []byte("foreign"), perm: 0o600}
	h.files[DataDir+"/operations-2026-09-03.jsonl"] = memFile{data: []byte("outside"), perm: 0o600}

	journal := operationJournal{host: h, now: func() time.Time { return now }, maxBytes: OperationLogMaxBytes}
	if err := journal.record(operationInstall, operationStarted, ModeDocker); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.files[OperationLogDir+"/operations-2026-09-03.jsonl"]; ok {
		t.Fatal("expired owned operation log survived")
	}
	for _, keep := range []string{
		OperationLogDir + "/operations-2026-09-04.jsonl",
		OperationLogDir + "/operator-notes.txt",
		DataDir + "/operations-2026-09-03.jsonl",
	} {
		if _, ok := h.files[keep]; !ok {
			t.Fatalf("pruning crossed ownership boundary: %s", keep)
		}
	}
	current := h.files[OperationLogDir+"/operations-2026-09-10.jsonl"]
	if current.perm != 0o600 {
		t.Fatalf("operation log mode = %o", current.perm)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(current.data), &record); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"msg": "lifecycle", "source": "operations", "operation": "install",
		"outcome": "started", "mode": "docker",
	} {
		if record[key] != want {
			t.Fatalf("record[%s] = %v, want %q", key, record[key], want)
		}
	}
	for _, forbidden := range []string{"error", "argv", "prompt", "stdin", "password", "token", "config"} {
		if _, ok := record[forbidden]; ok {
			t.Fatalf("operation record contains free-form field %q", forbidden)
		}
	}
}

func TestOperationJournalEnforcesAggregateCapOldestFirst(t *testing.T) {
	now := time.Date(2026, 9, 10, 14, 30, 0, 0, time.UTC)
	h := newMemHost()
	h.dirs[OperationLogDir] = true
	h.files[OperationLogDir+"/operations-2026-09-08.jsonl"] = memFile{data: bytes.Repeat([]byte("a"), 90), perm: 0o600}
	h.files[OperationLogDir+"/operations-2026-09-09.jsonl"] = memFile{data: bytes.Repeat([]byte("b"), 90), perm: 0o600}
	journal := operationJournal{host: h, now: func() time.Time { return now }, maxBytes: 220}
	if err := journal.record(operationUpdate, operationSucceeded, ModeNative); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.files[OperationLogDir+"/operations-2026-09-08.jsonl"]; ok {
		t.Fatal("oldest file was not pruned for aggregate cap")
	}
	if _, ok := h.files[OperationLogDir+"/operations-2026-09-10.jsonl"]; !ok {
		t.Fatal("new outcome was lost during cap enforcement")
	}
	var total int
	for path, file := range h.files {
		if strings.HasPrefix(path, OperationLogDir+"/operations-") {
			total += len(file.data)
		}
	}
	if total > 220 {
		t.Fatalf("operation logs use %d bytes over cap", total)
	}
}

func TestOperationJournalRejectsNonFixedMetadata(t *testing.T) {
	h := newMemHost()
	journal := operationJournal{host: h, now: time.Now, maxBytes: OperationLogMaxBytes}
	if err := journal.record(operationAction("install password=leak"), operationSucceeded, ModeDocker); err == nil {
		t.Fatal("free-form operation accepted")
	}
	if err := journal.record(operationInstall, operationOutcome("failed: token=leak"), ModeDocker); err == nil {
		t.Fatal("free-form outcome accepted")
	}
	if _, ok := h.dirs[OperationLogDir]; ok {
		t.Fatal("invalid metadata created an operation directory")
	}
}

func TestOperationJournalRepairsInterruptedFinalLine(t *testing.T) {
	now := time.Date(2026, 9, 10, 14, 30, 0, 0, time.UTC)
	h := newMemHost()
	h.dirs[OperationLogDir] = true
	path := operationLogPath(now)
	h.files[path] = memFile{data: []byte(`{"time":"interrupted"`), perm: 0o600}
	journal := operationJournal{host: h, now: func() time.Time { return now }, maxBytes: OperationLogMaxBytes}
	if err := journal.record(operationUpdate, operationFailed, ModeDocker); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(h.files[path].data, []byte("\n{")) {
		t.Fatalf("interrupted line was not terminated before the next record: %q", h.files[path].data)
	}
	var out bytes.Buffer
	if err := StreamLogs(context.Background(), h, nil, LogOptions{
		Source: LogSourceOperations, Tail: 10, Since: now.Add(-time.Hour),
	}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"operation":"update"`) || strings.Contains(out.String(), "interrupted") {
		t.Fatalf("repaired operation stream = %q", out.String())
	}
}

func TestOperationLogSourceSkipsCorruptOversizedAndPartialLines(t *testing.T) {
	now := time.Date(2026, 9, 10, 14, 30, 0, 0, time.UTC)
	h := newMemHost()
	h.dirs[OperationLogDir] = true
	validOld := `{"time":"2026-09-10T12:00:00Z","level":"INFO","msg":"lifecycle","source":"operations","operation":"install","outcome":"started","mode":"docker"}` + "\n"
	validNew := `{"time":"2026-09-10T13:00:00Z","level":"INFO","msg":"lifecycle","source":"operations","operation":"install","outcome":"succeeded","mode":"docker"}` + "\n"
	h.files[OperationLogDir+"/operations-2026-09-10.jsonl"] = memFile{data: []byte(
		"not-json\n" + validOld + strings.Repeat("x", maxLogLineBytes+10) + "\n" + validNew + `{"time":"partial"}`,
	), perm: 0o600}
	var out bytes.Buffer
	err := StreamLogs(context.Background(), h, nil, LogOptions{
		Source: "operations", Tail: 1, Since: now.Add(-24 * time.Hour),
	}, &out, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != validNew {
		t.Fatalf("operation output = %q", out.String())
	}
	if len(h.commands) != 0 {
		t.Fatalf("operation journal unexpectedly invoked a subprocess: %v", h.ranCommands())
	}
}

func TestOperationLogSourceToleratesMissingDirectory(t *testing.T) {
	h := newMemHost()
	err := StreamLogs(context.Background(), h, nil, LogOptions{
		Source: "operations", Tail: 200, Since: time.Now().Add(-time.Hour),
	}, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
}

func clearOperationLogs(host *memHost) {
	for path := range host.files {
		if strings.HasPrefix(path, OperationLogDir+"/") {
			delete(host.files, path)
		}
	}
	delete(host.dirs, OperationLogDir)
}

func recordedOutcomes(t *testing.T, host *memHost) []string {
	t.Helper()
	var paths []string
	for path := range host.files {
		if strings.HasPrefix(path, OperationLogDir+"/operations-") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	var outcomes []string
	for _, path := range paths {
		for _, line := range bytes.Split(host.files[path].data, []byte{'\n'}) {
			record, _, ok := parseOperationRecord(line)
			if ok {
				outcomes = append(outcomes, record.Operation+":"+record.Outcome)
			}
		}
	}
	return outcomes
}

func requireOutcomes(t *testing.T, host *memHost, want ...string) {
	t.Helper()
	if got := recordedOutcomes(t, host); !slices.Equal(got, want) {
		t.Fatalf("operation outcomes = %v, want %v", got, want)
	}
}

func TestLifecycleCommandsRecordOnlyFixedOutcomes(t *testing.T) {
	t.Run("install success", func(t *testing.T) {
		h := newMemHost()
		plan := Defaults()
		plan.PanelPort = healthServer(t, http.StatusOK)
		if _, err := Install(context.Background(), h, InstallOptions{Plan: plan, Yes: true, Stdout: io.Discard}); err != nil {
			t.Fatal(err)
		}
		requireOutcomes(t, h, "install:started", "install:succeeded")
	})

	t.Run("install failure", func(t *testing.T) {
		base := newMemHost()
		h := &faultHost{memHost: base, failRun: " up -d"}
		plan := Defaults()
		plan.PanelPort = healthServer(t, http.StatusOK)
		if _, err := Install(context.Background(), h, InstallOptions{Plan: plan, Yes: true, Stdout: io.Discard}); err == nil {
			t.Fatal("injected install failure accepted")
		}
		requireOutcomes(t, base, "install:started", "install:failed")
	})

	t.Run("update success and failure", func(t *testing.T) {
		base := installedFixture(t, ModeNative)
		clearOperationLogs(base)
		contractFixture(base)
		if err := Update(context.Background(), base, UpdateOptions{BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: io.Discard}); err != nil {
			t.Fatal(err)
		}
		requireOutcomes(t, base, "update:started", "update:succeeded")

		failedBase := installedFixture(t, ModeNative)
		clearOperationLogs(failedBase)
		contractFixture(failedBase)
		failed := &faultHost{memHost: failedBase, failRun: "systemctl restart"}
		if err := Update(context.Background(), failed, UpdateOptions{BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: io.Discard}); err == nil {
			t.Fatal("injected update failure accepted")
		}
		requireOutcomes(t, failedBase, "update:started", "update:failed")
	})

	t.Run("rollback success", func(t *testing.T) {
		h := installedFixture(t, ModeNative)
		contractFixture(h)
		if err := Update(context.Background(), h, UpdateOptions{BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: io.Discard}); err != nil {
			t.Fatal(err)
		}
		clearOperationLogs(h)
		if err := Update(context.Background(), h, UpdateOptions{Rollback: true, Stdout: io.Discard}); err != nil {
			t.Fatal(err)
		}
		requireOutcomes(t, h, "rollback:started", "rollback:succeeded")

		failed := installedFixture(t, ModeNative)
		clearOperationLogs(failed)
		if err := Update(context.Background(), failed, UpdateOptions{Rollback: true, Stdout: io.Discard}); err == nil {
			t.Fatal("rollback without a previous build was accepted")
		}
		requireOutcomes(t, failed, "rollback:started", "rollback:failed")
	})

	t.Run("uninstall success and failure", func(t *testing.T) {
		base := installedFixture(t, ModeDocker)
		clearOperationLogs(base)
		if _, err := Uninstall(context.Background(), base, UninstallOptions{Yes: true, Stdout: io.Discard}); err != nil {
			t.Fatal(err)
		}
		requireOutcomes(t, base, "uninstall:started", "uninstall:succeeded")

		failedBase := installedFixture(t, ModeNative)
		clearOperationLogs(failedBase)
		failed := &nativeCleanupHost{memHost: failedBase, stopErr: errors.New("password=must-not-be-recorded")}
		if _, err := Uninstall(context.Background(), failed, UninstallOptions{Yes: true, Stdout: io.Discard}); err == nil {
			t.Fatal("injected uninstall failure accepted")
		}
		requireOutcomes(t, failedBase, "uninstall:started", "uninstall:failed")
		for _, file := range failedBase.files {
			if strings.Contains(string(file.data), "must-not-be-recorded") {
				t.Fatal("free-form lifecycle error entered operation log")
			}
		}
	})
}

func TestPurgingDataDoesNotRecreateOperationJournal(t *testing.T) {
	h := installedFixture(t, ModeDocker)
	if _, err := Uninstall(context.Background(), h, UninstallOptions{Yes: true, PurgeData: true, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	for path := range h.files {
		if path == DataDir || strings.HasPrefix(path, DataDir+"/") {
			t.Fatalf("purged data was recreated by outcome logging: %s", path)
		}
	}
	if h.dirs[DataDir] || h.dirs[OperationLogDir] {
		t.Fatal("purged data directories were recreated by outcome logging")
	}
}

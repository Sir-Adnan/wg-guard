package install

import (
	"context"
	"io"
	"testing"
)

func interruptedInitialInstall(t *testing.T, h *memHost, dataChanged bool) {
	t.Helper()
	st := &State{
		Schema: StateSchema, Mode: ModeDocker, Recovery: "install-incomplete",
		ConfigPath: ConfigPath, DataDir: DataDir, ComposePath: ComposePth, BinPath: BinPath,
	}
	if err := saveState(h, st); err != nil {
		t.Fatal(err)
	}
	j := &Journal{Schema: 1, ID: transactionID(), Operation: "install", Stage: "recovery-required", After: st, DataMayHaveChanged: dataChanged}
	if err := j.save(h, "recovery-required"); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupIncompleteInitialInstallPreservesManagerCacheAndData(t *testing.T) {
	h := newMemHost()
	interruptedInitialInstall(t, h, false)
	h.files[BinPath] = memFile{data: []byte("manager"), perm: 0o755}
	h.files[ManagerBuildPath] = memFile{data: []byte("build receipt"), perm: 0o600}
	h.dirs[DataDir] = true

	if err := CleanupIncompleteInstall(context.Background(), h, true, io.Discard); err != nil {
		t.Fatal(err)
	}
	if st, err := LoadState(h); err != nil || st != nil {
		t.Fatalf("incomplete state remains: %+v %v", st, err)
	}
	j, err := LoadJournal(h)
	if err != nil || j == nil || j.Stage != "aborted" {
		t.Fatalf("journal not closed: %+v %v", j, err)
	}
	if string(h.files[BinPath].data) != "manager" || string(h.files[ManagerBuildPath].data) != "build receipt" || !h.dirs[DataDir] {
		t.Fatal("safe cleanup removed the persistent manager, cached build, or data")
	}
}

func TestCleanupIncompleteInitialInstallRefusesPossibleRuntimeChanges(t *testing.T) {
	h := newMemHost()
	interruptedInitialInstall(t, h, true)
	beforeState := string(h.files[StatePath].data)
	beforeJournal := string(h.files[JournalPath].data)

	if err := CleanupIncompleteInstall(context.Background(), h, true, io.Discard); err == nil {
		t.Fatal("unsafe automatic cleanup accepted")
	}
	if string(h.files[StatePath].data) != beforeState || string(h.files[JournalPath].data) != beforeJournal {
		t.Fatal("refused cleanup changed recovery evidence")
	}
}

func TestCleanupAcceptsAbruptInterruptionBeforePrerequisitesComplete(t *testing.T) {
	h := newMemHost()
	st := &State{Schema: StateSchema, Mode: ModeDocker, ConfigPath: ConfigPath, DataDir: DataDir, ComposePath: ComposePth, BinPath: BinPath}
	j := &Journal{Schema: 1, ID: transactionID(), Operation: "install", After: st}
	if err := j.save(h, "prerequisites"); err != nil {
		t.Fatal(err)
	}
	if err := CleanupIncompleteInstall(context.Background(), h, true, io.Discard); err != nil {
		t.Fatalf("safe abrupt interruption could not be cleared: %v", err)
	}
	closed, err := LoadJournal(h)
	if err != nil || closed == nil || closed.Stage != "aborted" {
		t.Fatalf("abrupt interruption journal not closed: %+v %v", closed, err)
	}
}

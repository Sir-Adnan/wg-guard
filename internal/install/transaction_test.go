package install

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLifecycleLockContention(t *testing.T) {
	h := newMemHost()
	unlock, err := h.LockLifecycle()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := h.LockLifecycle(); err == nil {
		t.Fatal("second writer acquired lifecycle lock")
	}
}
func TestAtomicStateWriteKeepsPreviousOnFailure(t *testing.T) {
	m := installedFixture(t, ModeDocker)
	before := string(m.files[StatePath].data)
	h := &faultHost{memHost: m, failRename: StatePath}
	st, _ := LoadState(h)
	st.Version = "new"
	if err := saveState(h, st); err == nil {
		t.Fatal("write failure ignored")
	}
	if string(m.files[StatePath].data) != before {
		t.Fatal("lost old state")
	}
}
func contractFixture(h *memHost) {
	h.output["systemctl show wg-guard.service --property=LoadState --property=ActiveState"] = "LoadState=not-found\nActiveState=inactive\n"
	h.output["docker inspect --format {{.Image}} "+Container] = "sha256:" + strings.Repeat("a", 64)
	h.output["docker run --rm --network none --entrypoint sha256sum sha256:"+strings.Repeat("b", 64)+" "+BinPath] = fmt.Sprintf("%x  %s", sha256.Sum256([]byte("candidate")), BinPath)
	b, _ := json.Marshal(CurrentContract())
	h.output["/tmp/candidate installer-contract"] = string(b)
	h.output[BinPath+" installer-contract"] = string(b)
	h.output["docker run --rm --network none --entrypoint "+BinPath+" sha256:"+strings.Repeat("b", 64)+" installer-contract"] = string(b)
	h.output["docker image inspect --format {{.Id}} image:new"] = "sha256:" + strings.Repeat("b", 64)
	h.output["docker image inspect --format {{.Id}} "+DefaultImage] = "sha256:" + strings.Repeat("a", 64)
	h.output["docker run --rm --network none --entrypoint "+BinPath+" sha256:"+strings.Repeat("a", 64)+" installer-contract"] = string(b)
	h.files["/tmp/candidate"] = memFile{data: []byte("candidate"), perm: 0755}
}
func TestFailedRemotePullDoesNotMutateActive(t *testing.T) {
	m := installedFixture(t, ModeDocker)
	contractFixture(m)
	before := string(m.files[ComposePth].data)
	h := &faultHost{memHost: m, failRun: "docker pull"}
	err := Update(context.Background(), h, UpdateOptions{Image: "image:new", BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: io.Discard})
	if err == nil {
		t.Fatal("failed pull accepted")
	}
	if string(m.files[ComposePth].data) != before {
		t.Fatal("pull failure changed compose")
	}
}
func TestRestartFailureRestoresArtifact(t *testing.T) {
	for _, mode := range []Mode{ModeDocker} {
		t.Run(string(mode), func(t *testing.T) {
			m := installedFixture(t, mode)
			contractFixture(m)
			old := string(m.files[BinPath].data)
			h := &faultHost{memHost: m, failRun: " up -d"}

			h.failRun = " up -d"

			err := Update(context.Background(), h, UpdateOptions{Image: "image:new", BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: io.Discard})
			if err == nil {
				t.Fatal("restart failure accepted")
			}
			if string(m.files[BinPath].data) != old {
				t.Fatal("binary not recovered")
			}
			j, e := LoadJournal(h)
			if e != nil || j == nil || j.Stage != "rolled-back" {
				t.Fatalf("recovery not durably recorded: %v %+v", e, j)
			}
		})
	}
}
func TestHealthyUpdateThenRollbackRetainsPrevious(t *testing.T) {
	for _, mode := range []Mode{ModeDocker} {
		t.Run(string(mode), func(t *testing.T) {
			m := installedFixture(t, mode)
			contractFixture(m)
			old := string(m.files[BinPath].data)
			var out strings.Builder
			if err := Update(context.Background(), m, UpdateOptions{Image: "image:new", BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: &out}); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Preparing lifecycle change", "Applying selected build", "Update complete"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("successful update omitted %q progress:\n%s", want, out.String())
				}
			}
			if string(m.files[BinPath].data) != "candidate" {
				t.Fatal("host binary not synchronized")
			}
			if err := Update(context.Background(), m, UpdateOptions{Rollback: true, Stdout: io.Discard}); err != nil {
				t.Fatal(err)
			}
			if string(m.files[BinPath].data) != old {
				t.Fatal("successful update lost previous artifact")
			}
		})
	}
}

type faultHost struct {
	*memHost
	readErr    error
	failRun    string
	failRename string
	cancel     context.CancelFunc
	archive    bool
}

func (h *faultHost) Open(p string) (io.ReadCloser, error) {
	if p == StatePath && h.readErr != nil {
		return nil, h.readErr
	}
	return h.memHost.Open(p)
}

func (h *faultHost) Output(ctx context.Context, args []string, d time.Duration) (string, error) {
	if h.archive {
		return (archiveHost{h.memHost, "age-encryption.org/v1\nfixture payload", "created wg-guard-test.wgg (1 KiB, age-encrypted)\n"}).Output(ctx, args, d)
	}
	return h.memHost.Output(ctx, args, d)
}

func (h *faultHost) ReadFile(p string) ([]byte, error) {
	if p == StatePath && h.readErr != nil {
		return nil, h.readErr
	}
	return h.memHost.ReadFile(p)
}
func (h *faultHost) Run(ctx context.Context, a []string, d time.Duration) error {
	if h.cancel != nil && strings.Contains(strings.Join(a, " "), " up -d") {
		cancel := h.cancel
		h.cancel = nil
		cancel()
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.Contains(strings.Join(a, " "), h.failRun) && h.failRun != "" {
		h.failRun = ""
		return errors.New("injected command failure")
	}
	return h.memHost.Run(ctx, a, d)
}

func TestStateCommitFailureRecovers(t *testing.T) {
	m := installedFixture(t, ModeDocker)
	contractFixture(m)
	h := &faultHost{memHost: m, failRename: StatePath}
	old := string(m.files[BinPath].data)
	if err := Update(context.Background(), h, UpdateOptions{Image: "image:new", BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: io.Discard}); err == nil {
		t.Fatal("state commit failure accepted")
	}
	if string(m.files[BinPath].data) != old {
		t.Fatal("state failure did not restore binary")
	}
	j, err := LoadJournal(h)
	if err != nil || j.Stage != "rolled-back" {
		t.Fatalf("recovery journal %v %+v", err, j)
	}
}
func TestCanceledUpdateUsesIndependentRecoveryContext(t *testing.T) {
	m := installedFixture(t, ModeDocker)
	contractFixture(m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &faultHost{memHost: m, cancel: cancel}
	old := string(m.files[BinPath].data)
	if err := Update(ctx, h, UpdateOptions{Image: "image:new", BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: io.Discard}); err == nil {
		t.Fatal("cancellation accepted")
	}
	if string(m.files[BinPath].data) != old {
		t.Fatal("canceled recovery failed to restore")
	}
	j, _ := LoadJournal(h)
	if j.Stage != "rolled-back" {
		t.Fatalf("stage %s", j.Stage)
	}
}
func TestIncompatibleUpdateNeverRestartsOldCode(t *testing.T) {
	m := installedFixture(t, ModeDocker)
	contractFixture(m)
	m.output["docker run --rm --network none --entrypoint "+BinPath+" sha256:"+strings.Repeat("a", 64)+" installer-contract"] = ""
	h := &faultHost{memHost: m, failRun: " up -d", archive: true}
	err := Update(context.Background(), h, UpdateOptions{Image: "image:new", BinaryPath: "/tmp/candidate", Stdout: io.Discard})
	if err == nil {
		t.Fatal("missing old contract accepted")
	}
	j, e := LoadJournal(h)
	if e != nil || j.Stage != "restore-required" {
		t.Fatalf("restore not required: %v %+v", e, j)
	}
	if m.ran("systemctl", "restart", "wg-guard") {
		t.Fatal("old code restarted against uncertain data")
	}
}
func TestInterruptedJournalRecovers(t *testing.T) {
	m := installedFixture(t, ModeDocker)
	contractFixture(m)
	st, _ := LoadState(m)
	previous, err := retainCurrent(context.Background(), m, st)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := stageCandidate(context.Background(), m, st, UpdateOptions{Image: "image:new", BinaryPath: "/tmp/candidate"})
	if err != nil {
		t.Fatal(err)
	}
	j := &Journal{Schema: JournalSchema, ID: strings.Repeat("c", 32), Operation: "update", Before: st, Previous: previous, Candidate: candidate, DataMayHaveChanged: true}
	if err := j.save(m, "started"); err != nil {
		t.Fatal(err)
	}
	if err := deployArtifact(m, st, candidate); err != nil {
		t.Fatal(err)
	}
	if err := Update(context.Background(), m, UpdateOptions{Recover: true, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if string(m.files[BinPath].data) != "/src/wg-guard" {
		t.Fatal("interrupted swap not recovered")
	}
}
func TestInstallHonorsLifecycleLock(t *testing.T) {
	h := newMemHost()
	unlock, err := h.LockLifecycle()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := Install(context.Background(), h, InstallOptions{Plan: Defaults(), Yes: true, Stdout: io.Discard}); err == nil {
		t.Fatal("concurrent install accepted")
	}
	if h.ran("modprobe") {
		t.Fatal("concurrent install mutated prerequisites")
	}
}
func TestInstallFailureJournalsPartialOwnership(t *testing.T) {
	h := &faultHost{memHost: newMemHost(), failRun: "systemctl enable"}
	p := Defaults()

	p.PanelPort = healthServer(t, http.StatusOK)
	if _, err := Install(context.Background(), h, InstallOptions{Plan: p, Yes: true, Stdout: io.Discard}); err == nil {
		t.Fatal("start failure accepted")
	}
	j, err := LoadJournal(h)
	if err != nil || j == nil || j.After == nil || j.After.BinPath != BinPath || j.Stage != "recovery-required" {
		t.Fatalf("partial install not journaled: %v %+v", err, j)
	}
}

type archiveHost struct {
	*memHost
	archiveText string
	summary     string
}

func (h archiveHost) Output(ctx context.Context, args []string, timeout time.Duration) (string, error) {
	if strings.Contains(strings.Join(args, " "), "backup create --reason pre-upgrade --output") {
		dir := args[len(args)-1]
		h.files[dir+"/wg-guard-test.wgg"] = memFile{data: []byte(h.archiveText), perm: 0600}
		return h.summary, nil
	}
	return h.memHost.Output(ctx, args, timeout)
}
func TestBackupRecordsActualLocalEncryptedArchive(t *testing.T) {
	m := installedFixture(t, ModeDocker)
	st, _ := LoadState(m)
	body := "age-encryption.org/v1\nfixture encrypted payload"
	h := archiveHost{m, body, "created wg-guard-test.wgg (1 KiB, age-encrypted)\n"}
	b, err := createBackup(context.Background(), h, st, strings.Repeat("d", 32))
	if err != nil {
		t.Fatal(err)
	}
	if !b.Encrypted || b.Path != "/var/lib/wg-guard/backups/lifecycle-"+strings.Repeat("d", 32)+"/wg-guard-test.wgg" || b.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(body))) {
		t.Fatal("archive identity not recorded")
	}
}
func TestBackupRejectsClaimWithoutLocalArchive(t *testing.T) {
	m := installedFixture(t, ModeDocker)
	st, _ := LoadState(m)
	h := archiveHost{m, "fixture", "created absent.wgg (1 KiB)\n"}
	if _, err := createBackup(context.Background(), h, st, strings.Repeat("d", 32)); err == nil {
		t.Fatal("nonexistent archive accepted")
	}
}
func TestOwnerPreparationFailurePreventsListenerStart(t *testing.T) {
	h := newMemHost()
	p := Defaults()

	p.PanelPort = healthServer(t, http.StatusOK)
	_, err := Install(context.Background(), h, InstallOptions{Plan: p, Yes: true, Stdout: io.Discard, BeforeStart: func(context.Context, Host, Plan, *State) error { return errors.New("owner preparation failed") }})
	if err == nil {
		t.Fatal("owner failure accepted")
	}
	if h.ran("docker", "compose", "-f", ComposePth, "up", "-d") {
		t.Fatal("listener started before owner prepared")
	}
}

func TestLoadStateAcceptsInterruptedExposureMigrationSentinel(t *testing.T) {
	h := installedFixture(t, ModeDocker)
	st, err := LoadState(h)
	if err != nil {
		t.Fatal(err)
	}
	st.Schema = StateSchema
	st.Exposure = ExposureState{}
	if err := saveState(h, st); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadState(h)
	if err != nil || loaded == nil {
		t.Fatalf("interrupted migration became unreadable: state=%+v err=%v", loaded, err)
	}
	if loaded.Exposure != (ExposureState{}) {
		t.Fatalf("read-only load rewrote migration sentinel: %+v", loaded.Exposure)
	}
}

func TestIncompatibleUpdateCannotSkipBackup(t *testing.T) {
	m := installedFixture(t, ModeDocker)
	contractFixture(m)
	m.output["docker run --rm --network none --entrypoint "+BinPath+" sha256:"+strings.Repeat("a", 64)+" installer-contract"] = ""
	before := string(m.files[BinPath].data)
	if err := Update(context.Background(), m, UpdateOptions{Image: "image:new", BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: io.Discard}); err == nil {
		t.Fatal("unsafe update allowed without backup")
	}
	if string(m.files[BinPath].data) != before {
		t.Fatal("unsafe backup override mutated binary")
	}
}
func (h *faultHost) Rename(a, b string) error {
	if b == h.failRename {
		h.failRename = ""
		return errors.New("injected rename failure")
	}
	return h.memHost.Rename(a, b)
}
func installedFixture(t *testing.T, mode Mode) *memHost {
	t.Helper()
	h := newMemHost()
	p := Defaults()

	p.PanelPort = healthServer(t, http.StatusOK)
	if _, err := Install(context.Background(), h, InstallOptions{Plan: p, Yes: true, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestStateReadFailureIsNotAbsence(t *testing.T) {
	h := &faultHost{memHost: newMemHost(), readErr: fs.ErrPermission}
	if _, err := LoadState(h); err == nil {
		t.Fatal("permission failure treated as absent")
	}
}
func TestCorruptStateRefusesInstall(t *testing.T) {
	h := newMemHost()
	h.files[StatePath] = memFile{data: []byte("{")}
	if _, err := Install(context.Background(), h, InstallOptions{Plan: Defaults(), Yes: true, Stdout: io.Discard}); err == nil {
		t.Fatal("corrupt state accepted")
	}
	if h.ran("modprobe") {
		t.Fatal("mutated host with corrupt state")
	}
}
func TestUnsupportedStateRefused(t *testing.T) {
	h := newMemHost()
	h.files[StatePath] = memFile{data: []byte(`{"schema":99,"mode":"docker"}`)}
	if _, err := LoadState(h); err == nil {
		t.Fatal("future schema accepted")
	}
}
func TestUninstallRejectsStatePaths(t *testing.T) {
	for _, field := range []string{"data", "extra", "binary", "compose"} {
		t.Run(field, func(t *testing.T) {
			h := installedFixture(t, ModeDocker)
			st, _ := LoadState(h)
			switch field {
			case "data":
				st.DataDir = "/"
			case "extra":
				st.ExtraFiles = []string{"/etc/passwd"}
			case "binary":
				st.BinPath = "/bin/sh"
			case "compose":
				st.ComposePath = "/tmp/foreign.yaml"
			}
			if err := saveState(h, st); err != nil {
				return
			}
			if _, err := Uninstall(context.Background(), h, UninstallOptions{Yes: true, PurgeData: true, Stdout: io.Discard}); err == nil {
				t.Fatal("unsafe deletion accepted")
			}
		})
	}
}
func TestUninstallStopFailurePreservesArtifacts(t *testing.T) {
	for _, mode := range []Mode{ModeDocker} {
		t.Run(string(mode), func(t *testing.T) {
			m := installedFixture(t, mode)
			h := &faultHost{memHost: m, failRun: " down"}

			_, err := Uninstall(context.Background(), h, UninstallOptions{Yes: true, PurgeData: true, Stdout: io.Discard})
			if err == nil {
				t.Fatal("stop failure ignored")
			}
			if _, ok := m.files[BinPath]; !ok {
				t.Fatal("removed binary after failed stop")
			}
			if !m.dirs[DataDir] {
				t.Fatal("purged running data")
			}
		})
	}
}

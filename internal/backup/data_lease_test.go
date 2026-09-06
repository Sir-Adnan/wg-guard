package backup

import (
	"context"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// A separate OS process exercises kernel ownership, including the same inode
// reached through a second path (the bind-volume property used by Docker).
func TestDataLeaseProcess(t *testing.T) {
	if dir := os.Getenv("WGG_TEST_LEASE_DIR"); dir != "" {
		s := &Service{Cfg: &config.Config{DataDir: dir, DatabasePath: filepath.Join(dir, "wg-guard.db"), MasterKeyFile: filepath.Join(dir, "master.key")}}
		if os.Getenv("WGG_TEST_LEASE_RESTORE_CRASH") == "1" {
			if _, err := s.ApplyStaged(&exitRestoreContext{Context: context.Background()}); err != nil {
				t.Fatal(err)
			}
			t.Fatal("restore did not reach process-death seam")
		}
		lease, err := s.OpenData(os.Getenv("WGG_TEST_LEASE_EXCLUSIVE") == "1")
		if err != nil {
			os.Exit(23)
		}
		defer lease.Close()
		if os.Getenv("WGG_TEST_LEASE_HOLD") == "1" {
			os.Stdout.Write([]byte("ready\n"))
			var b [1]byte
			os.Stdin.Read(b[:])
		}
		return
	}
	for _, exclusive := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared", true: "exclusive"}[exclusive], func(t *testing.T) {
			s, dir := newService(t)
			lease, err := s.OpenData(exclusive)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			for _, childExclusive := range []bool{false, true} {
				cmd := exec.Command(os.Args[0], "-test.run=^TestDataLeaseProcess$")
				cmd.Env = append(os.Environ(), "WGG_TEST_LEASE_DIR="+dir, "WGG_TEST_LEASE_EXCLUSIVE="+map[bool]string{false: "0", true: "1"}[childExclusive])
				err := cmd.Run()
				if (err != nil) != (exclusive || childExclusive) {
					t.Fatalf("parent exclusive=%v child exclusive=%v: %v", exclusive, childExclusive, err)
				}
			}
			lease.Close()
			next, err := s.OpenData(true)
			if err != nil {
				t.Fatal("release", err)
			}
			next.Close()
		})
	}
}

type exitRestoreContext struct {
	context.Context
	calls int
}

func (c *exitRestoreContext) Err() error {
	c.calls++
	if c.calls == 3 {
		os.Exit(29)
	} // DB replaced, matching key still not replaced.
	return nil
}

func TestDataLeaseProcessDeathRecoversOriginalPair(t *testing.T) {
	src, _ := newService(t)
	arc, err := src.Create(context.Background(), CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	target, dir := newService(t)
	if err := target.Reg.SetRaw(context.Background(), "backup.telegram_token", "synthetic-target-token"); err != nil {
		t.Fatal(err)
	}
	target.DB.Close()
	p, _, err := target.Stage(context.Background(), arc.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.Approve(p.PreviewID()); err != nil {
		t.Fatal(err)
	}
	beforeDB, beforeKey := fileHash(target.Cfg.DatabasePath), fileHash(target.Cfg.MasterKeyFile)
	cmd := exec.Command(os.Args[0], "-test.run=^TestDataLeaseProcess$")
	cmd.Env = append(os.Environ(), "WGG_TEST_LEASE_DIR="+dir, "WGG_TEST_LEASE_RESTORE_CRASH=1")
	if err := cmd.Run(); err == nil || cmd.ProcessState.ExitCode() != 29 {
		t.Fatalf("expected death between pair members: %v", err)
	}
	if l, err := target.OpenData(false); err == nil {
		l.Close()
		t.Fatal("interrupted pair admitted reader")
	}
	if err := target.RecoverInterrupted(); err == nil {
		t.Fatal("recovery must require operator review")
	}
	if fileHash(target.Cfg.DatabasePath) != beforeDB || fileHash(target.Cfg.MasterKeyFile) != beforeKey {
		t.Fatal("process-death recovery changed original pair")
	}
	l, err := target.OpenData(false)
	if err != nil {
		t.Fatal("dead process leaked ownership", err)
	}
	l.Close()
}

func TestDataLeaseAdmissionAndVolumeAlias(t *testing.T) {
	s, dir := newService(t)
	alias := filepath.Join(t.TempDir(), "volume")
	if runtime.GOOS == "linux" {
		if err := os.Symlink(dir, alias); err != nil {
			t.Fatal(err)
		}
	} else {
		alias = dir
	}
	lease, err := s.lockData(true)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	run := func(exclusive bool) error {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDataLeaseProcess$")
		cmd.Env = append(os.Environ(), "WGG_TEST_LEASE_DIR="+alias, "WGG_TEST_LEASE_EXCLUSIVE="+map[bool]string{false: "0", true: "1"}[exclusive])
		return cmd.Run()
	}
	// Model the portable downgrade interval: the data byte is momentarily
	// unlocked, but admission must still reject another process through an alias.
	if err := leaseUnlock(lease.file, 1); err != nil {
		t.Fatal(err)
	}
	if err := run(false); err == nil {
		t.Fatal("admission crossed exclusive conversion")
	}
	if err := leaseLock(lease.file, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := lease.Share(); err != nil {
		t.Fatal(err)
	}
	if err := run(false); err != nil {
		t.Fatal("downgrade did not admit shared reader", err)
	}
	if err := run(true); err == nil {
		t.Fatal("downgrade lost lifetime protection")
	}
}

func TestAdmittedDataLeaseBlocksPairReplacement(t *testing.T) {
	s, _ := newService(t)
	arc, err := s.Create(context.Background(), CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := s.Stage(context.Background(), arc.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Approve(p.PreviewID()); err != nil {
		t.Fatal(err)
	}
	lease, err := s.OpenData(false)
	if err != nil {
		t.Fatal(err)
	}
	before := fileHash(s.Cfg.MasterKeyFile)
	lockBefore, err := os.Stat(filepath.Join(s.Cfg.DataDir, dataLeaseName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyStaged(context.Background()); err == nil {
		t.Fatal("restore replaced an admitted owner's data")
	}
	if fileHash(s.Cfg.MasterKeyFile) != before {
		t.Fatal("key changed on contention")
	}
	lease.Close()
	s.DB.Close()
	if _, err = s.ApplyStaged(context.Background()); err != nil {
		t.Fatal("restore after release", err)
	}
	lockAfter, err := os.Stat(filepath.Join(s.Cfg.DataDir, dataLeaseName))
	if err != nil || !os.SameFile(lockBefore, lockAfter) {
		t.Fatal("pair swap replaced shared-volume lock inode", err)
	}
}

func TestDataLeaseChecksMarkerUnderOwnership(t *testing.T) {
	s, dir := newService(t)
	if err := os.WriteFile(filepath.Join(dir, RestoreGuardName), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if lease, err := s.OpenData(false); err == nil {
		lease.Close()
		t.Fatal("marker admitted data opener")
	}
	// Failed admission must release the kernel lock for offline recovery.
	lease, err := s.lockData(true)
	if err != nil {
		t.Fatal(err)
	}
	lease.Close()
}

func TestDataLeaseRejectsSplitLayoutAndAcceptsDirectoryAlias(t *testing.T) {
	for _, field := range []string{"database", "key"} {
		t.Run(field, func(t *testing.T) {
			s, _ := newService(t)
			other := filepath.Join(t.TempDir(), "outside")
			if field == "database" {
				s.Cfg.DatabasePath = other
			} else {
				s.Cfg.MasterKeyFile = other
			}
			if l, err := s.OpenData(false); err == nil {
				l.Close()
				t.Fatal("split DB/key layout admitted")
			}
		})
	}
	s, dir := newService(t)
	s.Cfg.DatabasePath = filepath.Join(dir, "custom.db")
	s.Cfg.MasterKeyFile = filepath.Join(dir, "custom.key")
	l, err := s.OpenData(false)
	if err != nil {
		t.Fatal("custom direct-child filenames rejected", err)
	}
	l.Close()
	if runtime.GOOS != "linux" {
		return
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	s.Cfg.DatabasePath = filepath.Join(alias, "custom.db")
	l, err = s.OpenData(false)
	if err != nil {
		t.Fatal("same-directory alias rejected", err)
	}
	l.Close()
	outside := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(outside, make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, s.Cfg.MasterKeyFile); err != nil {
		t.Fatal(err)
	}
	if l, err := s.OpenData(false); err == nil {
		l.Close()
		t.Fatal("key symlink escape admitted")
	}
}

func TestDataLeaseMissingKeyKeepsInitializerExclusive(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Complete()
	s := &Service{Cfg: cfg}
	first, err := s.OpenKeys(false)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if l, err := s.OpenKeys(false); err == nil {
		l.Close()
		t.Fatal("two key initializers admitted")
	}
}

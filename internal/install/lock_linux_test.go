//go:build linux

package install

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLockReleasedOnProcessDeath(t *testing.T) {
	if p := os.Getenv("WGG_LOCK_TEST_PATH"); p != "" {
		unlock, err := lockFile(p)
		if err != nil {
			os.Exit(2)
		}
		defer unlock()
		os.Stdout.WriteString("locked\n")
		time.Sleep(30 * time.Second)
		return
	}
	p := filepath.Join(t.TempDir(), "lifecycle.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockReleasedOnProcessDeath$")
	cmd.Env = append(os.Environ(), "WGG_LOCK_TEST_PATH="+p)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("lock child did not start: %v", err)
	}
	if unlock, err := lockFile(p); err == nil {
		unlock()
		t.Fatal("concurrent process acquired lock")
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	unlock, err := lockFile(p)
	if err != nil {
		t.Fatalf("dead process retained lock: %v", err)
	}
	unlock()
}
func TestAtomicWriteAndCopyRejectSymlinkTargets(t *testing.T) {
	dir := t.TempDir()
	foreign := filepath.Join(dir, "foreign")
	if err := os.WriteFile(foreign, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(foreign, link); err != nil {
		t.Fatal(err)
	}
	h := realHost{}
	if err := h.AtomicWrite(link, []byte("changed"), 0600); err == nil {
		t.Fatal("symlink write allowed")
	}
	if err := h.CopyFile(foreign, link, 0755); err == nil {
		t.Fatal("symlink copy allowed")
	}
	b, _ := os.ReadFile(foreign)
	if string(b) != "preserve" {
		t.Fatal("foreign data changed")
	}
	target := filepath.Join(dir, "state.json")
	if err := h.AtomicWrite(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.AtomicWrite(target, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(target)
	if string(b) != "new" {
		t.Fatal("atomic replace failed")
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm() != 0600 {
		t.Fatal("private state permissions lost")
	}
}

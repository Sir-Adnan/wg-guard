package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLifecycleStagingIgnoresTemporaryRAMFilesystem(t *testing.T) {
	base := t.TempDir()
	cache := filepath.Join(base, "cache", "staging")
	// If any code accidentally uses the global temporary directory, it fails.
	t.Setenv("TMPDIR", filepath.Join(base, "not-present"))
	t.Setenv("TEMP", filepath.Join(base, "not-present"))
	t.Setenv("TMP", filepath.Join(base, "not-present"))
	stage, err := newLifecycleStageAt(cache)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stage) })
	if filepath.Dir(stage) != cache {
		t.Fatal("staging escaped the managed cache")
	}
	if info, err := os.Stat(stage); err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatal("staging must stay private")
	}
	if err := os.WriteFile(filepath.Join(stage, "candidate"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(stage); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatal("cleanup removed cache authority")
	}
}

func TestLifecycleStagingRejectsUnsafeRoot(t *testing.T) {
	if _, err := newLifecycleStageAt("relative"); err == nil {
		t.Fatal("relative root admitted")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes/symlinks")
	}
	base := t.TempDir()
	public := filepath.Join(base, "public")
	if err := os.Mkdir(public, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(public, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := newLifecycleStageAt(public); err == nil {
		t.Fatal("public root admitted")
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(base, link); err != nil {
		t.Fatal(err)
	}
	if _, err := newLifecycleStageAt(link); err == nil {
		t.Fatal("symlink root admitted")
	}
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Sir-Adnan/wg-guard/internal/layout"
)

// Acquisition needs disk space for source, toolchain, modules and Go's private
// caches. Do not inherit a small RAM-backed /tmp from the host or its environment.
func newLifecycleStage() (string, error) {
	if err := os.MkdirAll(layout.ManagerCache, 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(layout.ManagerCache)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || runtime.GOOS != "windows" && info.Mode().Perm()&0o022 != 0 {
		return "", fmt.Errorf("acquisition: unsafe cache directory")
	}
	if err := checkStagingOwner(info); err != nil {
		return "", err
	}
	return newLifecycleStageAt(layout.BuildStaging)
}

func newLifecycleStageAt(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("acquisition: absolute staging root required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("acquisition: staging root must be a private directory")
	}
	if err := checkStagingOwner(info); err != nil {
		return "", err
	}
	return os.MkdirTemp(root, "wg-guard-lifecycle-")
}

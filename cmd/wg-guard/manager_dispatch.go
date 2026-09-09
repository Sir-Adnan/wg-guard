package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/install"
)

const managerActiveEnv = "WGG_MANAGER_ACTIVE"

func managerDelegationTarget(command, current string, cached distribution.Build) string {
	if command != "manage" && command != "update" || cached.BinaryPath != install.ManagerBinaryPath || current == install.ManagerBinaryPath {
		return ""
	}
	return cached.BinaryPath
}

// maybeDelegateManager keeps the active service/shim binary stable while the
// operator-facing manager can advance independently. The cache is fully
// verified before execution and the marker prevents recursive dispatch.
func maybeDelegateManager() {
	if os.Getenv(managerActiveEnv) == "1" || len(os.Args) < 2 {
		return
	}
	current, err := os.Executable()
	if err != nil {
		return
	}
	b, err := install.LoadManagerBuild(context.Background(), install.NewRealHost())
	if err != nil {
		return
	}
	target := managerDelegationTarget(os.Args[1], current, b)
	if target == "" {
		return
	}
	cmd := exec.Command(target, os.Args[1:]...) //nolint:gosec // fixed verified manager path
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), managerActiveEnv+"=1")
	err = cmd.Run()
	if err == nil {
		os.Exit(0)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	fmt.Fprintf(os.Stderr, "wg-guard: start verified manager: %v\n", err)
}

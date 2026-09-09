package main

import (
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/install"
)

func TestManagerDelegationUsesIndependentCacheOnlyForManagementCommands(t *testing.T) {
	b := distribution.Build{BinaryPath: install.ManagerBinaryPath}
	for _, command := range []string{"manage", "update"} {
		if got := managerDelegationTarget(command, install.BinPath, b); got != install.ManagerBinaryPath {
			t.Fatalf("%s delegation target = %q", command, got)
		}
	}
	for _, tc := range []struct{ command, current string }{
		{"serve", install.BinPath},
		{"status", install.BinPath},
		{"manage", install.ManagerBinaryPath},
	} {
		if got := managerDelegationTarget(tc.command, tc.current, b); got != "" {
			t.Fatalf("%s from %s delegated to %q", tc.command, tc.current, got)
		}
	}
}

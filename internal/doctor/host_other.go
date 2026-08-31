//go:build !unix

package doctor

import (
	"fmt"
	"os"
	"runtime"
)

// platformPrivileges is only meaningful on unix servers.
func platformPrivileges() (Status, string) {
	return StatusSkip, "privilege check is a unix server check (" + runtime.GOOS + ")"
}

func checkPerm(string, os.FileMode) string { return "" }

func checkPermFile(string, os.FileInfo) string { return "" }

func kernelModuleLoaded(name string) (bool, string) {
	return false, "module state unavailable on " + runtime.GOOS
}

func readIPForward() (string, bool) { return "", false }

func diskUsage(string) (uint64, uint64, bool) { return 0, 0, false }

var _ = fmt.Sprintf

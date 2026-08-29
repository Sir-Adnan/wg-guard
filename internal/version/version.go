// Package version holds the build identity of the wg-guard binary.
// Values are overridden at link time via -ldflags -X.
package version

import (
	"fmt"
	"runtime"
)

var (
	Version = "0.1.0-dev"
	Commit  = "none"
	Date    = "unknown"
)

func String() string {
	return fmt.Sprintf("wg-guard %s (commit %s, built %s, %s/%s)",
		Version, Commit, Date, runtime.GOOS, runtime.GOARCH)
}

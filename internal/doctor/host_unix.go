//go:build unix

package doctor

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// platformPrivileges reports the effective user (root is the designed
// service identity on Linux).
func platformPrivileges() (Status, string) {
	if os.Geteuid() == 0 {
		return StatusPass, "running as root"
	}
	return StatusWarn, fmt.Sprintf("running as uid %d (not root)", os.Geteuid())
}

// checkPerm returns a note when the directory is group/world accessible.
func checkPerm(path string, forbidden os.FileMode) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if st.Mode().Perm()&forbidden != 0 {
		return fmt.Sprintf("%s is mode %04o", path, st.Mode().Perm())
	}
	return ""
}

// checkPermFile returns a note unless the file is owner-only.
func checkPermFile(path string, st os.FileInfo) string {
	if st.Mode().Perm()&0o177 != 0 {
		return fmt.Sprintf("%s is mode %04o (want 0600)", path, st.Mode().Perm())
	}
	return ""
}

// kernelModuleLoaded reads /proc/modules.
func kernelModuleLoaded(name string) (bool, string) {
	b, err := os.ReadFile("/proc/modules")
	if err != nil {
		return false, "/proc/modules unavailable"
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == name {
			return true, "kernel module " + name + " loaded"
		}
	}
	return false, "kernel module " + name + " not loaded"
}

// readIPForward reads the live sysctl value.
func readIPForward() (string, bool) {
	b, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// diskUsage returns free/total bytes of the filesystem holding path.
func diskUsage(path string) (free, total uint64, ok bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	return st.Bavail * uint64(st.Bsize), st.Blocks * uint64(st.Bsize), true
}

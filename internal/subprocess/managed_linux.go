//go:build linux

package subprocess

import "syscall"

func managedProcessAttrs() *syscall.SysProcAttr {
	// A native node crash must not leave an unowned TUN daemon behind.
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}

//go:build !linux

package subprocess

import "syscall"

func managedProcessAttrs() *syscall.SysProcAttr { return nil }

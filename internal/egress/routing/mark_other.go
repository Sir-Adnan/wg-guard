//go:build !linux

package routing

import "syscall"

func setMark(int, int) error { return syscall.EPERM }

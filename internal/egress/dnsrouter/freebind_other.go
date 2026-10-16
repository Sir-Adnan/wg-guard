//go:build !linux

package dnsrouter

import "syscall"

func freebind(_, _ string, _ syscall.RawConn) error { return nil }

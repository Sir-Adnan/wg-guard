//go:build linux

package dnsrouter

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// freebind lets the resolver bind a gateway address before the tunnel link
// that carries it exists (interfaces can be recreated by reconcile).
func freebind(_, _ string, c syscall.RawConn) error {
	var serr error
	if err := c.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.SOL_IP, unix.IP_FREEBIND, 1)
	}); err != nil {
		return err
	}
	return serr
}

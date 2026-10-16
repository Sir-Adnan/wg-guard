//go:build linux

package routing

import "golang.org/x/sys/unix"

func setMark(fd, mark int) error {
	return unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK, mark)
}

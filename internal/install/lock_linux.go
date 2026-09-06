//go:build linux

package install

import (
	"golang.org/x/sys/unix"
	"os"
)

func (realHost) LockLifecycle() (func(), error) { return lockFile("/run/lock/wg-guard-lifecycle.lock") }
func lockFile(p string) (func(), error) {
	fd, err := unix.Open(p, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), p)
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, terminalError("install.error.lock")
	}
	return func() { _ = f.Close() }, nil
}

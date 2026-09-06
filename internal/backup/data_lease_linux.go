//go:build linux

package backup

import (
	"golang.org/x/sys/unix"
	"os"
)

func openLeaseFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
func leaseLock(f *os.File, offset int64, exclusive bool) error {
	kind := int16(unix.F_RDLCK)
	if exclusive {
		kind = unix.F_WRLCK
	}
	return unix.FcntlFlock(f.Fd(), unix.F_OFD_SETLK, &unix.Flock_t{Type: kind, Whence: 0, Start: offset, Len: 1})
}
func leaseUnlock(f *os.File, offset int64) error {
	return unix.FcntlFlock(f.Fd(), unix.F_OFD_SETLK, &unix.Flock_t{Type: unix.F_UNLCK, Whence: 0, Start: offset, Len: 1})
}

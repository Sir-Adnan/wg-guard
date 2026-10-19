//go:build linux

package routing

import (
	"errors"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

var conntrackSeq atomic.Uint32

// ErrDrainUnavailable reports that connections could not be flushed.
var ErrDrainUnavailable = errors.New("routing: connection drain unavailable")

// FlushConnections deletes every tracked flow whose conntrack mark carries
// the outbound mark. The mark filter is mandatory: a zero mark would match
// unmarked flows (the node's own and every client's), so it is refused.
func FlushConnections(mark uint32) error {
	if mark&MarkMask == 0 {
		return ErrDrainUnavailable
	}
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_NETFILTER)
	if err != nil {
		return ErrDrainUnavailable
	}
	defer unix.Close(fd)
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return ErrDrainUnavailable
	}
	tv := unix.NsecToTimeval(int64(3 * time.Second))
	_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
	seq := conntrackSeq.Add(1)
	if err := unix.Sendto(fd, conntrackFlushRequest(seq, mark), 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return ErrDrainUnavailable
	}
	buf := make([]byte, 4096)
	n, _, err := unix.Recvfrom(fd, buf, 0)
	if err != nil {
		return ErrDrainUnavailable
	}
	// The acknowledgement is an error message whose code is 0 on success;
	// ENOENT means nothing carried the mark.
	if code, ok := ackCode(buf[:n], seq); !ok || (code != 0 && code != -int32(unix.ENOENT)) {
		return ErrDrainUnavailable
	}
	return nil
}

// CountConnections returns the number of tracked flows whose conntrack mark
// carries the outbound mark (requires CAP_NET_ADMIN, which the node holds).
func CountConnections(mark uint32) (int64, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_NETFILTER)
	if err != nil {
		return 0, ErrCountUnavailable
	}
	defer unix.Close(fd)
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return 0, ErrCountUnavailable
	}
	tv := unix.NsecToTimeval(int64(3 * time.Second))
	_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
	seq := conntrackSeq.Add(1)
	if err := unix.Sendto(fd, conntrackDumpRequest(seq, mark), 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return 0, ErrCountUnavailable
	}
	buf := make([]byte, 64<<10)
	var total int64
	for total < maxCountedFlows {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			return total, ErrCountUnavailable
		}
		count, done, err := countDumpMessages(buf[:n], seq)
		total += count
		if err != nil || done {
			return total, err
		}
	}
	return total, nil
}

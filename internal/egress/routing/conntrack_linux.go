//go:build linux

package routing

import (
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

var conntrackSeq atomic.Uint32

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

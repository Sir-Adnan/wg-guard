//go:build linux

package sitebridge

import (
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"syscall"

	"golang.org/x/sys/unix"
)

// soOriginalDst is SO_ORIGINAL_DST (linux/netfilter_ipv4.h).
const soOriginalDst = 80

// originalDst reads the destination a redirected connection was addressed
// to before nftables redirected it.
func originalDst(conn net.Conn) (netip.AddrPort, error) {
	tc, ok := conn.(*net.TCPConn)
	if !ok {
		return netip.AddrPort{}, errors.New("sitebridge: not a TCP connection")
	}
	raw, err := tc.SyscallConn()
	if err != nil {
		return netip.AddrPort{}, err
	}
	var addr *unix.IPv6Mreq
	var serr error
	if err := raw.Control(func(fd uintptr) {
		// sockaddr_in fits the 20-byte IPv6Mreq buffer the helper reads.
		addr, serr = unix.GetsockoptIPv6Mreq(int(fd), unix.IPPROTO_IP, soOriginalDst)
	}); err != nil {
		return netip.AddrPort{}, err
	}
	if serr != nil {
		return netip.AddrPort{}, serr
	}
	b := addr.Multiaddr
	port := binary.BigEndian.Uint16(b[2:4])
	ip := netip.AddrFrom4([4]byte{b[4], b[5], b[6], b[7]})
	return netip.AddrPortFrom(ip, port), nil
}

// freebind lets the bridge listen on a gateway address before the tunnel
// link that carries it exists (interfaces can be recreated by reconcile).
func freebind(_, _ string, c syscall.RawConn) error {
	var serr error
	if err := c.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.SOL_IP, unix.IP_FREEBIND, 1)
	}); err != nil {
		return err
	}
	return serr
}

//go:build !linux

package sitebridge

import (
	"errors"
	"net"
	"net/netip"
	"syscall"
)

func originalDst(net.Conn) (netip.AddrPort, error) {
	return netip.AddrPort{}, errors.New("sitebridge: unsupported platform")
}

func freebind(string, string, syscall.RawConn) error { return nil }

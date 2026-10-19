//go:build linux

package sitebridge

import (
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestOriginalDstInNamespace redirects a loopback connection with nft and
// reads the pre-redirect destination back through SO_ORIGINAL_DST.
func TestOriginalDstInNamespace(t *testing.T) {
	if os.Getenv("WGG_BRIDGE_NETNS") == "1" {
		fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		ifr, _ := unix.NewIfreq("lo")
		_ = unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, ifr)
		ifr.SetUint16(ifr.Uint16() | unix.IFF_UP)
		if err := unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, ifr); err != nil {
			t.Fatal(err)
		}
		unix.Close(fd)
		for _, rule := range []string{
			"add table ip n",
			"add chain ip n out { type nat hook output priority -100; }",
			"add rule ip n out ip daddr 127.0.0.2 tcp dport 443 redirect to :40443",
		} {
			if out, err := exec.Command("nft", strings.Fields(rule)...).CombinedOutput(); err != nil {
				t.Fatalf("nft %s: %v %s", rule, err, out)
			}
		}
		ln, err := net.Listen("tcp4", "127.0.0.1:40443")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		c, err := net.Dial("tcp4", "127.0.0.2:443")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		conn, err := ln.Accept()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		dst, err := originalDst(conn)
		if err != nil || dst.String() != "127.0.0.2:443" {
			t.Fatalf("original destination %v %v", dst, err)
		}
		return
	}
	for _, tool := range []string{"unshare", "nft"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not installed")
		}
	}
	if out, err := exec.Command("unshare", "-rn", "true").CombinedOutput(); err != nil {
		t.Skipf("no user network namespace: %v %s", err, out)
	}
	cmd := exec.Command("unshare", "-rn", os.Args[0], "-test.run", "^TestOriginalDstInNamespace$")
	cmd.Env = append(os.Environ(), "WGG_BRIDGE_NETNS=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("namespace run: %v\n%s", err, out)
	}
}

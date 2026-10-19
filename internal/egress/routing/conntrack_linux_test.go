//go:build linux

package routing

import (
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// loopbackUp brings lo up in a fresh network namespace.
func loopbackUp(t *testing.T) {
	t.Helper()
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	ifr, err := unix.NewIfreq("lo")
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, ifr); err != nil {
		t.Fatal(err)
	}
	ifr.SetUint16(ifr.Uint16() | unix.IFF_UP)
	if err := unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, ifr); err != nil {
		t.Fatal(err)
	}
}

// TestFlushConnectionsInNamespace marks two loopback flows with different
// outbound marks and checks that a flush removes exactly one of them: the
// kernel's mark filter, not a whole-table flush.
func TestFlushConnectionsInNamespace(t *testing.T) {
	if os.Getenv("WGG_EGRESS_FLUSH") == "1" {
		loopbackUp(t)
		for _, rule := range []string{
			"add table ip t",
			"add chain ip t out { type filter hook output priority 0; }",
			"add rule ip t out tcp dport 40001 ct mark set 0x00100000",
			"add rule ip t out tcp dport 40002 ct mark set 0x00200000",
		} {
			if out, err := exec.Command("nft", strings.Fields(rule)...).CombinedOutput(); err != nil {
				t.Fatalf("nft %s: %v %s", rule, err, out)
			}
		}
		for _, port := range []string{"40001", "40002"} {
			ln, err := net.Listen("tcp4", "127.0.0.1:"+port)
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			c, err := net.Dial("tcp4", "127.0.0.1:"+port)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
		}
		if n, err := CountConnections(0x00100000); err != nil || n != 1 {
			t.Fatalf("before flush: %d %v", n, err)
		}
		if err := FlushConnections(0x00100000); err != nil {
			t.Fatal(err)
		}
		a, _ := CountConnections(0x00100000)
		b, _ := CountConnections(0x00200000)
		if a != 0 || b != 1 {
			t.Fatalf("after flush: marked %d (want 0), other %d (want 1)", a, b)
		}
		if err := FlushConnections(0); err == nil {
			t.Fatal("a zero mark must be refused")
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
	cmd := exec.Command("unshare", "-rn", os.Args[0], "-test.run", "^TestFlushConnectionsInNamespace$", "-test.v")
	cmd.Env = append(os.Environ(), "WGG_EGRESS_FLUSH=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("namespace run: %v\n%s", err, out)
	}
}

package routing

import (
	"encoding/binary"
	"os"
	"os/exec"
	"testing"
)

func nlmsg(typ uint16, seq uint32, payload []byte) []byte {
	b := make([]byte, nlmsgHdrLen, nlmsgHdrLen+len(payload))
	binary.NativeEndian.PutUint32(b[0:4], uint32(nlmsgHdrLen+len(payload)))
	binary.NativeEndian.PutUint16(b[4:6], typ)
	binary.NativeEndian.PutUint32(b[8:12], seq)
	b = append(b, payload...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func TestConntrackDumpEncodingAndCounting(t *testing.T) {
	req := conntrackDumpRequest(7, 0x00300000)
	if binary.NativeEndian.Uint16(req[4:6]) != 0x0101 || binary.NativeEndian.Uint16(req[6:8]) != nlmFRequest|nlmFDump {
		t.Fatalf("header: %x", req[:16])
	}
	if binary.BigEndian.Uint32(req[24:28]) != 0x00300000 || binary.BigEndian.Uint32(req[32:36]) != MarkMask {
		t.Fatalf("mark filter: %x", req[16:])
	}
	entry := nlmsg(0x0100, 7, []byte{2, 0, 0, 0, 1})
	foreign := nlmsg(0x0100, 9, nil)
	buf := append(append(append([]byte{}, entry...), entry...), foreign...)
	if n, done, err := countDumpMessages(buf, 7); n != 2 || done || err != nil {
		t.Fatalf("count: %d %v %v", n, done, err)
	}
	if n, done, err := countDumpMessages(append(entry, nlmsg(nlmsgDone, 7, []byte{0, 0, 0, 0})...), 7); n != 1 || !done || err != nil {
		t.Fatalf("done: %d %v %v", n, done, err)
	}
	errPayload := make([]byte, 4)
	binary.NativeEndian.PutUint32(errPayload, uint32(0xffffffff)) // -1 EPERM
	if _, _, err := countDumpMessages(nlmsg(nlmsgError, 7, errPayload), 7); err == nil {
		t.Fatal("netlink error not reported")
	}
}

// TestCountConnectionsInNamespace runs the real netlink dump inside an
// unprivileged network namespace (empty conntrack table).
func TestCountConnectionsInNamespace(t *testing.T) {
	if os.Getenv("WGG_EGRESS_NETNS") == "1" {
		n, err := CountConnections(0x00100000)
		if err != nil || n != 0 {
			t.Fatalf("count in empty namespace: %d %v", n, err)
		}
		return
	}
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("unshare not installed")
	}
	if out, err := exec.Command("unshare", "-rn", "true").CombinedOutput(); err != nil {
		t.Skipf("no user network namespace: %v %s", err, out)
	}
	cmd := exec.Command("unshare", "-rn", os.Args[0], "-test.run", "^TestCountConnectionsInNamespace$")
	cmd.Env = append(os.Environ(), "WGG_EGRESS_NETNS=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("conntrack netlink unavailable in this kernel/namespace: %v %s", err, out)
	}
}

//go:build integration

package shaper

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// TestIntegrationTcHTBApplies exercises the rendered tc batch against the
// real iproute2 tc on a dummy interface: apply → qdisc/classes/filters
// visible → idempotent re-apply → limit change rebuilds → cleanup removes
// the qdisc. Requires root and tc (run inside WSL2/CI with the
// `integration` tag).
func TestIntegrationTcHTBApplies(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("integration test requires root")
	}
	run := subprocess.NewSystem()
	ctx := context.Background()

	// tc has no reliable --version flag across iproute2 versions; presence
	// on PATH is what matters (the manager itself fails loudly if absent).
	if _, err := exec.LookPath("tc"); err != nil {
		t.Skip("tc not available")
	}

	const ifc = "wgshap0"
	cleanup := func() {
		_, _ = run.Run(ctx, []string{"ip", "link", "del", ifc})
	}
	_, err := run.Run(ctx, []string{"ip", "link", "add", ifc, "type", "dummy"})
	if err != nil {
		t.Skipf("cannot create dummy interface (no NET_ADMIN?): %v", err)
	}
	t.Cleanup(cleanup)

	m := New(run)

	// Apply: two users, three device IPs, one shared class per user.
	groups := []Group{
		{InterfaceName: ifc, UserID: "u1", IPs: []string{"10.8.0.2/32", "10.8.0.3/32"}, DownKbps: 20480, UpKbps: 4096},
		{InterfaceName: ifc, UserID: "u2", IPs: []string{"10.8.0.9/32"}, DownKbps: 512},
	}
	applied, err := m.Ensure(ctx, ifc, groups)
	if err != nil || !applied {
		t.Fatalf("apply: applied=%v err=%v", applied, err)
	}

	qdiscOut := tcShow(t, run, "qdisc", ifc)
	if !strings.Contains(qdiscOut, "htb") {
		t.Fatalf("no htb qdisc: %s", qdiscOut)
	}
	classOut := tcShow(t, run, "class", ifc)
	if got := strings.Count(classOut, "htb"); got != 2 {
		t.Fatalf("want 2 classes, got %d: %s", got, classOut)
	}
	// One filter line per device IP; count by IP so the assertion survives
	// iproute2 formatting differences across versions.
	filterOut := tcShow(t, run, "filter", ifc)
	// One "flowid 1:N" per filter (the match line encodes the IP as hex and
	// header lines repeat, so both are fragile counts).
	if got := strings.Count(filterOut, "flowid 1:"); got != 3 {
		t.Fatalf("want 3 filters, got %d: %s", got, filterOut)
	}

	// Identical desired state: no subprocess at all.
	applied, err = m.Ensure(ctx, ifc, groups)
	if err != nil || applied {
		t.Fatalf("re-apply must be a no-op: applied=%v err=%v", applied, err)
	}

	// Changing a limit rebuilds the tree: same class count, different rate.
	groups[1].DownKbps = 1024
	if applied, err = m.Ensure(ctx, ifc, groups); err != nil || !applied {
		t.Fatalf("rebuild: applied=%v err=%v", applied, err)
	}
	classOut2 := tcShow(t, run, "class", ifc)
	if got := strings.Count(classOut2, "htb"); got != 2 {
		t.Fatalf("want 2 classes after rebuild, got %d: %s", got, classOut2)
	}
	if classOut2 == classOut {
		t.Fatalf("class output must change when the rate changes:\n%s", classOut2)
	}
	if got := strings.Count(tcShow(t, run, "filter", ifc), "flowid 1:"); got != 3 {
		t.Fatalf("rebuild duplicated filters: %d", got)
	}

	// Cleanup: empty desired state removes the qdisc.
	if applied, err = m.Ensure(ctx, ifc, nil); err != nil || !applied {
		t.Fatalf("cleanup: applied=%v err=%v", applied, err)
	}
	if out := tcShow(t, run, "qdisc", ifc); strings.Contains(out, "htb") {
		t.Fatalf("qdisc not removed: %s", out)
	}
}

// TestIntegrationIngressIFBShaping exercises the upload direction end-to-end
// against real iproute2: the ingress qdisc, the mirred redirect, and the HTB
// tree on the IFB mirror device (kernel ifb support verified in WSL2,
// 2026-08-30 — see docs/architecture/networking.md).
func TestIntegrationIngressIFBShaping(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("integration test requires root")
	}
	run := subprocess.NewSystem()
	ctx := context.Background()
	if _, err := exec.LookPath("tc"); err != nil {
		t.Skip("tc not available")
	}

	const ifc = "wgshap1"
	ifb := IFBName(ifc)
	_, err := run.Run(ctx, []string{"ip", "link", "add", ifc, "type", "dummy"})
	if err != nil {
		t.Skipf("cannot create dummy interface (no NET_ADMIN?): %v", err)
	}
	t.Cleanup(func() {
		_, _ = run.Run(ctx, []string{"ip", "link", "del", ifc})
		_, _ = run.Run(ctx, []string{"ip", "link", "del", ifb})
	})

	m := New(run)
	groups := []Group{
		{InterfaceName: ifc, UserID: "u1", IPs: []string{"10.8.0.2/32", "10.8.0.3/32"}, UpKbps: 4096},
	}
	if applied, err := m.Ensure(ctx, ifc, groups); err != nil || !applied {
		t.Fatalf("apply: applied=%v err=%v", applied, err)
	}

	// The tunnel interface carries the ingress qdisc with the redirect.
	// (Ingress filters live under parent ffff:; this iproute2 lists them
	// only with the parent given explicitly — verified live 2026-08-30.)
	qdiscOut := tcShow(t, run, "qdisc", ifc)
	if !strings.Contains(qdiscOut, "ingress") {
		t.Fatalf("no ingress qdisc on %s: %s", ifc, qdiscOut)
	}
	filterOut := tcShow(t, run, "filter", ifc, "parent", "ffff:")
	if !strings.Contains(filterOut, "mirred") || !strings.Contains(filterOut, ifb) {
		t.Fatalf("no mirred redirect into %s: %s", ifb, filterOut)
	}

	// The IFB device carries the HTB tree: 1 class, 2 source filters.
	classOut := tcShow(t, run, "class", ifb)
	if got := strings.Count(classOut, "htb"); got != 1 {
		t.Fatalf("want 1 class on %s, got %d: %s", ifb, got, classOut)
	}
	if got := strings.Count(tcShow(t, run, "filter", ifb), "flowid 1:"); got != 2 {
		t.Fatalf("want 2 source filters on %s, got %d", ifb, got)
	}
	if _, err := run.Run(ctx, []string{"ip", "link", "show", ifb}); err != nil {
		t.Fatalf("ifb device missing: %v", err)
	}

	// Identical desired state: no-op.
	if applied, err := m.Ensure(ctx, ifc, groups); err != nil || applied {
		t.Fatalf("re-apply must be a no-op: applied=%v err=%v", applied, err)
	}

	// Cleanup: the ingress tree and the IFB device are removed.
	if applied, err := m.Ensure(ctx, ifc, nil); err != nil || !applied {
		t.Fatalf("cleanup: applied=%v err=%v", applied, err)
	}
	if out := tcShow(t, run, "qdisc", ifc); strings.Contains(out, "ingress") {
		t.Fatalf("ingress qdisc not removed: %s", out)
	}
	if _, err := run.Run(ctx, []string{"ip", "link", "show", ifb}); err == nil {
		t.Fatal("ifb device must be removed on cleanup")
	}
}

func tcShow(t *testing.T, run *subprocess.System, kind, ifc string, extra ...string) string {
	t.Helper()
	args := append([]string{"tc", kind, "show", "dev", ifc}, extra...)
	res, err := run.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("tc %s show: %v", kind, err)
	}
	return string(res.Stdout)
}

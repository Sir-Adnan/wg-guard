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
		{InterfaceName: ifc, UserID: "u1", IPs: []string{"10.8.0.2/32", "10.8.0.3/32"}, Kbps: 20480},
		{InterfaceName: ifc, UserID: "u2", IPs: []string{"10.8.0.9/32"}, Kbps: 512},
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
	groups[1].Kbps = 1024
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

func tcShow(t *testing.T, run *subprocess.System, kind, ifc string) string {
	t.Helper()
	res, err := run.Run(context.Background(), []string{"tc", kind, "show", "dev", ifc})
	if err != nil {
		t.Fatalf("tc %s show: %v", kind, err)
	}
	return string(res.Stdout)
}

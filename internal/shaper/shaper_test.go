package shaper

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// fakeRunner captures tc batch files at call time and can fail like the real
// runner would (missing binary, non-zero exit).
type fakeRunner struct {
	mu      sync.Mutex
	calls   [][]string
	batches []string // tc -b files, in capture order
	err     error
}

func (f *fakeRunner) Run(_ context.Context, argv []string) (subprocess.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), argv...))
	if len(argv) >= 3 && argv[0] == "tc" && argv[1] == "-b" {
		if data, err := os.ReadFile(argv[2]); err == nil {
			f.batches = append(f.batches, string(data))
		}
	}
	return subprocess.Result{}, f.err
}

func (f *fakeRunner) lastBatch(t *testing.T) string {
	t.Helper()
	if len(f.batches) == 0 {
		t.Fatal("no tc batch captured")
	}
	return f.batches[len(f.batches)-1]
}

func TestRenderScriptGolden(t *testing.T) {
	got, err := RenderScript("awg0", []Group{
		{InterfaceName: "awg0", UserID: "u2", IPs: []string{"10.8.0.3/32", "10.8.0.2/32"}, Kbps: 20480},
		{InterfaceName: "awg0", UserID: "u1", IPs: []string{"10.8.0.10/32"}, Kbps: 1024},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `qdisc add dev awg0 root handle 1: htb default 0
class add dev awg0 parent 1: classid 1:10 htb rate 1024kbit ceil 1024kbit
filter add dev awg0 parent 1: protocol ip pref 100 u32 match ip dst 10.8.0.10/32 flowid 1:10
class add dev awg0 parent 1: classid 1:11 htb rate 20480kbit ceil 20480kbit
filter add dev awg0 parent 1: protocol ip pref 101 u32 match ip dst 10.8.0.2/32 flowid 1:11
filter add dev awg0 parent 1: protocol ip pref 102 u32 match ip dst 10.8.0.3/32 flowid 1:11
`
	if got != want {
		t.Fatalf("script:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderScriptEmptyRendersNothing(t *testing.T) {
	got, err := RenderScript("awg1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("empty desired state must render nothing: %q", got)
	}
}

func TestRenderScriptRejectsZeroRate(t *testing.T) {
	if _, err := RenderScript("awg0", []Group{{InterfaceName: "awg0", UserID: "u", IPs: []string{"10.8.0.2/32"}}}); err == nil {
		t.Fatal("zero rate must be rejected")
	}
}

func TestRenderScriptIsDeterministic(t *testing.T) {
	groups := []Group{
		{InterfaceName: "awg0", UserID: "b", IPs: []string{"10.8.0.5/32", "10.8.0.4/32"}, Kbps: 100},
		{InterfaceName: "awg0", UserID: "a", IPs: []string{"10.8.0.9/32"}, Kbps: 200},
	}
	first, err := RenderScript("awg0", groups)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := RenderScript("awg0", groups)
		if err != nil || again != first {
			t.Fatalf("render not deterministic at %d: %v", i, err)
		}
	}
}

func TestEnsureAppliesFirstAndSkipsUnchanged(t *testing.T) {
	r := &fakeRunner{}
	m := New(r)
	ctx := context.Background()
	groups := []Group{{InterfaceName: "awg0", UserID: "u1", IPs: []string{"10.8.0.2/32"}, Kbps: 5000}}

	applied, err := m.Ensure(ctx, "awg0", groups)
	if err != nil || !applied {
		t.Fatalf("first ensure: applied=%v err=%v", applied, err)
	}
	if got := r.lastBatch(t); !strings.Contains(got, "rate 5000kbit") {
		t.Fatalf("batch: %s", got)
	}

	// Identical desired state: no subprocess at all.
	r.batches, r.calls = nil, nil
	applied, err = m.Ensure(ctx, "awg0", groups)
	if err != nil || applied || len(r.calls) != 0 {
		t.Fatalf("unchanged ensure must be a no-op (applied=%v err=%v calls=%v)", applied, err, r.calls)
	}

	// Changing one IP rebuilds the whole tree.
	groups[0].IPs = append(groups[0].IPs, "10.8.0.3/32")
	applied, err = m.Ensure(ctx, "awg0", groups)
	if err != nil || !applied {
		t.Fatalf("rebuild: applied=%v err=%v", applied, err)
	}
	if got := r.lastBatch(t); !strings.Contains(got, "10.8.0.3/32") || !strings.Contains(got, "qdisc add") {
		t.Fatalf("batch: %s", got)
	}
}

func TestEnsureEmptyStateCleansUpOnce(t *testing.T) {
	r := &fakeRunner{}
	m := New(r)
	ctx := context.Background()
	groups := []Group{{InterfaceName: "awg0", UserID: "u1", IPs: []string{"10.8.0.2/32"}, Kbps: 5000}}

	if _, err := m.Ensure(ctx, "awg0", groups); err != nil {
		t.Fatal(err)
	}
	// Same manager, empty desired → one best-effort cleanup call.
	applied, err := m.Ensure(ctx, "awg0", nil)
	if err != nil || !applied {
		t.Fatalf("empty-state cleanup: applied=%v err=%v", applied, err)
	}
	last := r.calls[len(r.calls)-1]
	if strings.Join(last, " ") != "tc qdisc del dev awg0 root" {
		t.Fatalf("cleanup call: %v", last)
	}
	// Repeated empty state: no-op.
	r.calls = nil
	applied, err = m.Ensure(ctx, "awg0", nil)
	if err != nil || applied || len(r.calls) != 0 {
		t.Fatalf("second empty ensure must be a no-op (applied=%v err=%v)", applied, err)
	}

	// Restart recovery shape: fresh manager, empty desired → one cleanup.
	r.calls = nil
	m3 := New(r)
	applied, err = m3.Ensure(ctx, "awg0", nil)
	if err != nil || !applied || len(r.calls) != 1 {
		t.Fatalf("fresh-manager cleanup: applied=%v err=%v calls=%v", applied, err, r.calls)
	}
}

func TestEnsureMissingTCOnlyToleratedWithoutLimits(t *testing.T) {
	notFound := &subprocessError{err: exec.ErrNotFound}
	r := &fakeRunner{err: notFound}
	m := New(r)
	ctx := context.Background()

	// No limits desired: cleanup is best-effort.
	applied, err := m.Ensure(ctx, "awg0", nil)
	if err != nil || applied {
		t.Fatalf("tc-less host with no limits: applied=%v err=%v", applied, err)
	}
	// Limits desired: hard error — an unenforced limit would be a lie.
	_, err = m.Ensure(ctx, "awg0", []Group{{InterfaceName: "awg0", UserID: "u1", IPs: []string{"10.8.0.2/32"}, Kbps: 100}})
	if err == nil {
		t.Fatal("missing tc with desired limits must fail")
	}
}

// subprocessError wraps exec.ErrNotFound the way subprocess.System does.
type subprocessError struct{ err error }

func (e *subprocessError) Error() string { return "subprocess: run tc: " + e.err.Error() }
func (e *subprocessError) Unwrap() error { return e.err }

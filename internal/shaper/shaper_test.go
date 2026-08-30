package shaper

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// fakeRunner captures tc batch files and subprocess calls, and can fail like
// the real runner would (missing binary, non-zero exit, targeted failures).
type fakeRunner struct {
	mu      sync.Mutex
	calls   [][]string
	batches []string // tc -b files, in capture order
	err     error
	failOn  func(argv []string) error // optional per-call failure
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
	if f.failOn != nil {
		if err := f.failOn(argv); err != nil {
			return subprocess.Result{}, err
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

func TestRenderEgressGolden(t *testing.T) {
	got, err := RenderEgress("awg0", []Group{
		{InterfaceName: "awg0", UserID: "u2", IPs: []string{"10.8.0.3/32", "10.8.0.2/32"}, DownKbps: 20480, UpKbps: 1024},
		{InterfaceName: "awg0", UserID: "u1", IPs: []string{"10.8.0.10/32"}, DownKbps: 1024},
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

func TestRenderIngressGolden(t *testing.T) {
	got, err := RenderIngress("awg0", []Group{
		{InterfaceName: "awg0", UserID: "u2", IPs: []string{"10.8.0.3/32", "10.8.0.2/32"}, UpKbps: 2048},
		{InterfaceName: "awg0", UserID: "u1", IPs: []string{"10.8.0.10/32"}, DownKbps: 1024}, // down-only: not in ingress
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `qdisc add dev awg0 handle ffff: ingress
filter add dev awg0 parent ffff: protocol ip pref 1 u32 match u32 0 0 action mirred egress redirect dev ifb-awg0
qdisc add dev ifb-awg0 root handle 1: htb default 0
class add dev ifb-awg0 parent 1: classid 1:10 htb rate 2048kbit ceil 2048kbit
filter add dev ifb-awg0 parent 1: protocol ip pref 100 u32 match ip src 10.8.0.2/32 flowid 1:10
filter add dev ifb-awg0 parent 1: protocol ip pref 101 u32 match ip src 10.8.0.3/32 flowid 1:10
`
	if got != want {
		t.Fatalf("script:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderDirectionIndependence(t *testing.T) {
	groups := []Group{{InterfaceName: "awg0", UserID: "u1", IPs: []string{"10.8.0.2/32"}, UpKbps: 512}}
	if got, err := RenderEgress("awg0", groups); err != nil || got != "" {
		t.Fatalf("up-only group must render no egress tree: %q %v", got, err)
	}
	groups[0].UpKbps = 0
	groups[0].DownKbps = 512
	if got, err := RenderIngress("awg0", groups); err != nil || got != "" {
		t.Fatalf("down-only group must render no ingress tree: %q %v", got, err)
	}
	if got, err := RenderEgress("awg1", nil); err != nil || got != "" {
		t.Fatalf("empty desired state must render nothing: %q %v", got, err)
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	groups := []Group{
		{InterfaceName: "awg0", UserID: "b", IPs: []string{"10.8.0.5/32", "10.8.0.4/32"}, DownKbps: 100, UpKbps: 100},
		{InterfaceName: "awg0", UserID: "a", IPs: []string{"10.8.0.9/32"}, DownKbps: 200},
	}
	for _, fn := range []struct {
		name string
		run  func() (string, error)
	}{
		{"egress", func() (string, error) { return RenderEgress("awg0", groups) }},
		{"ingress", func() (string, error) { return RenderIngress("awg0", groups) }},
	} {
		first, err := fn.run()
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ {
			again, err := fn.run()
			if err != nil || again != first {
				t.Fatalf("%s render not deterministic at %d: %v", fn.name, i, err)
			}
		}
	}
}

func TestEnsureAppliesFirstAndSkipsUnchanged(t *testing.T) {
	r := &fakeRunner{}
	m := New(r)
	ctx := context.Background()
	groups := []Group{{InterfaceName: "awg0", UserID: "u1", IPs: []string{"10.8.0.2/32"}, DownKbps: 5000}}

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

func TestEnsureBothDirectionsIndependent(t *testing.T) {
	r := &fakeRunner{}
	m := New(r)
	ctx := context.Background()
	groups := []Group{{InterfaceName: "awg0", UserID: "u1", IPs: []string{"10.8.0.2/32"}, DownKbps: 5000, UpKbps: 1000}}

	if _, err := m.Ensure(ctx, "awg0", groups); err != nil {
		t.Fatal(err)
	}
	batches := r.batches
	if len(batches) != 2 {
		t.Fatalf("want egress+ingress batches, got %d: %q", len(batches), batches)
	}
	if !strings.Contains(batches[0], "match ip dst") || !strings.Contains(batches[1], "mirred egress redirect") {
		t.Fatalf("batch order/content wrong: %q", batches)
	}

	// Changing ONLY the upload rate rebuilds ONLY the ingress tree.
	r.batches, r.calls = nil, nil
	groups[0].UpKbps = 1500
	if _, err := m.Ensure(ctx, "awg0", groups); err != nil {
		t.Fatal(err)
	}
	if len(r.batches) != 1 || !strings.Contains(r.batches[0], "mirred egress redirect") {
		t.Fatalf("upload-rate change must touch only the ingress tree: %q", r.batches)
	}
	for _, c := range r.calls {
		if strings.Join(c, " ") == "tc qdisc del dev awg0 root" {
			t.Fatal("egress tree must not be rebuilt while its render is unchanged")
		}
	}
}

func TestEnsureEmptyStateCleansUpOnce(t *testing.T) {
	r := &fakeRunner{}
	m := New(r)
	ctx := context.Background()
	groups := []Group{{InterfaceName: "awg0", UserID: "u1", IPs: []string{"10.8.0.2/32"}, DownKbps: 5000, UpKbps: 1000}}

	if _, err := m.Ensure(ctx, "awg0", groups); err != nil {
		t.Fatal(err)
	}
	// Same manager, empty desired → best-effort cleanup of both directions.
	applied, err := m.Ensure(ctx, "awg0", nil)
	if err != nil || !applied {
		t.Fatalf("empty-state cleanup: applied=%v err=%v", applied, err)
	}
	joined := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		joined = append(joined, strings.Join(c, " "))
	}
	for _, want := range []string{
		"tc qdisc del dev awg0 root",
		"tc qdisc del dev awg0 ingress",
		"tc qdisc del dev ifb-awg0 root",
		"ip link del ifb-awg0",
	} {
		found := false
		for _, j := range joined {
			if j == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("cleanup missing %q in %q", want, joined)
		}
	}
	// Repeated empty state: no-op.
	r.calls = nil
	applied, err = m.Ensure(ctx, "awg0", nil)
	if err != nil || applied || len(r.calls) != 0 {
		t.Fatalf("second empty ensure must be a no-op (applied=%v err=%v)", applied, err)
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
	// Download limit desired: hard error — an unenforced limit would be a lie.
	_, err = m.Ensure(ctx, "awg0", []Group{{InterfaceName: "awg0", UserID: "u1", IPs: []string{"10.8.0.2/32"}, DownKbps: 100}})
	if err == nil {
		t.Fatal("missing tc with desired limits must fail")
	}
}

func TestEnsureIFBFailureConfinedToUpload(t *testing.T) {
	r := &fakeRunner{
		failOn: func(argv []string) error {
			// Simulate a kernel without ifb support: creating fails AND the
			// device does not exist afterwards.
			if argv[0] == "ip" && (argv[1] == "link" || argv[1] == "del") && len(argv) > 3 && argv[3] == "ifb-awg0" {
				return &subprocessError{err: errors.New("RTNETLINK answers: Operation not permitted")}
			}
			return nil
		},
	}
	m := New(r)
	ctx := context.Background()
	groups := []Group{{InterfaceName: "awg0", UserID: "u1", IPs: []string{"10.8.0.2/32"}, DownKbps: 5000, UpKbps: 1000}}

	applied, err := m.Ensure(ctx, "awg0", groups)
	if err == nil {
		t.Fatal("ifb failure must surface")
	}
	if !strings.Contains(err.Error(), "ifb") {
		t.Fatalf("error must name the ifb cause: %v", err)
	}
	if !applied {
		t.Fatal("the download tree must still have been applied")
	}
	// The egress batch ran; the ingress batch did not.
	if len(r.batches) != 1 || !strings.Contains(r.batches[0], "match ip dst") {
		t.Fatalf("egress tree must be enforced despite ifb failure: %q", r.batches)
	}
}

// subprocessError wraps errors the way subprocess.System does.
type subprocessError struct{ err error }

func (e *subprocessError) Error() string { return "subprocess: run: " + e.err.Error() }
func (e *subprocessError) Unwrap() error { return e.err }

package dnsrouter

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/Sir-Adnan/wg-guard/internal/egress/routing"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// scriptRunner records `nft -f` scripts; fail rejects matching scripts and
// gate, when set, blocks each run until it is closed.
type scriptRunner struct {
	mu      sync.Mutex
	scripts []string
	fail    func(script string) bool
	gate    chan struct{}
}

func (r *scriptRunner) Run(ctx context.Context, argv []string) (subprocess.Result, error) {
	data, _ := os.ReadFile(argv[2])
	if r.gate != nil {
		select {
		case <-r.gate:
		case <-ctx.Done():
			return subprocess.Result{}, ctx.Err()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scripts = append(r.scripts, string(data))
	if r.fail != nil && r.fail(string(data)) {
		return subprocess.Result{}, errors.New("set full")
	}
	return subprocess.Result{}, nil
}

func (r *scriptRunner) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.scripts...)
}

func startedFeeder(t *testing.T, run subprocess.Runner, sets ...string) *feeder {
	t.Helper()
	f := newFeeder(run, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	live := map[string]bool{}
	for _, s := range sets {
		live[s] = true
	}
	f.setSets(live)
	ctx, cancel := context.WithCancel(context.Background())
	f.start(ctx)
	t.Cleanup(func() { cancel(); f.wait() })
	return f
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("learn ticket never completed")
	}
}

func TestLearnWaitsOnlyForAddressesNotInTheKernel(t *testing.T) {
	run := &scriptRunner{}
	f := startedFeeder(t, run, "r1_d")
	a := netip.MustParseAddr("142.250.1.1")

	done := f.learn("r1_d", []netip.Addr{a}, []uint32{60})
	if done == nil {
		t.Fatal("a new address must be waited for")
	}
	waitDone(t, done)
	t0 := "ip " + routing.TableName
	want := "add element " + t0 + " r1_d { 142.250.1.1 }\ndelete element " + t0 + " r1_d { 142.250.1.1 }\nadd element " + t0 + " r1_d { 142.250.1.1 timeout 1800s }\n"
	if got := run.all(); len(got) != 1 || got[0] != want {
		t.Fatalf("write is not add/delete/add in one transaction:\n%q", got)
	}
	if f.learn("r1_d", []netip.Addr{a}, []uint32{60}) != nil || f.stats().Skipped != 1 {
		t.Fatal("a fresh element must be neither waited for nor rewritten")
	}

	// Past half-life but before expiry the element is still in the kernel:
	// the refresh is written asynchronously, the reply does not wait.
	key := memKey{"r1_d", a}
	f.mu.Lock()
	f.remembered[key] = memory{refresh: time.Now().Add(-time.Second), expires: time.Now().Add(time.Minute)}
	f.mu.Unlock()
	if f.learn("r1_d", []netip.Addr{a}, []uint32{60}) != nil {
		t.Fatal("a due refresh must not hold the reply")
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(run.all()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := run.all(); len(got) != 2 || got[1] != want {
		t.Fatalf("refresh not rewritten with delete+add:\n%q", got)
	}
	f.mu.Lock()
	m := f.remembered[key]
	f.mu.Unlock()
	if time.Until(m.expires) < 29*time.Minute || time.Until(m.refresh) < 14*time.Minute {
		t.Fatalf("refresh did not move the remembered expiry: %+v", m)
	}

	// Remembered past its expiry: gone from the kernel, wait again.
	f.mu.Lock()
	f.remembered[key] = memory{refresh: time.Now().Add(-time.Hour), expires: time.Now().Add(-time.Second)}
	f.mu.Unlock()
	if done := f.learn("r1_d", []netip.Addr{a}, []uint32{60}); done == nil {
		t.Fatal("an expired element must be waited for")
	} else {
		waitDone(t, done)
	}
	if f.learn("other_d", []netip.Addr{a}, []uint32{60}) != nil {
		t.Fatal("unknown sets are ignored")
	}
}

func TestReplyWaitIsBoundedWhenNftStalls(t *testing.T) {
	run := &scriptRunner{gate: make(chan struct{})}
	defer close(run.gate)
	s := New(nil, run, nil)
	s.exchange = fakeUpstream
	s.state.Store(&state{rules: ruleset(t)})
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); s.feeder.wait() }()
	s.feeder.setSets(map[string]bool{"r1_d": true})
	s.feeder.start(ctx)

	start := time.Now()
	resp := s.handle(context.Background(), "udp", query(t, "gemini.google.com.", dnsmessage.TypeA))
	took := time.Since(start)
	if rc, n := rcodeOf(t, resp); rc != dnsmessage.RCodeSuccess || n != 3 {
		t.Fatalf("answer: %v %d", rc, n)
	}
	if took < learnWait-50*time.Millisecond || took > learnWait+time.Second {
		t.Fatalf("reply waited %v, want about %v", took, learnWait)
	}
	if st := s.feeder.stats(); st.Late != 1 {
		t.Fatalf("late reply not counted: %+v", st)
	}
}

func TestFlushRetriesEachSetWhenOneFails(t *testing.T) {
	run := &scriptRunner{fail: func(script string) bool { return strings.Contains(script, "full_d") }}
	f := newFeeder(run, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	f.setSets(map[string]bool{"full_d": true, "ok_d": true})
	a := netip.MustParseAddr("203.0.113.9")
	done := make(chan struct{})
	f.flush(context.Background(), []element{{set: "full_d", addr: a, ttl: 60}, {set: "ok_d", addr: a, ttl: 60, done: done}})
	select {
	case <-done:
	default:
		t.Fatal("flush must complete its tickets")
	}
	if got := run.all(); len(got) != 3 || strings.Contains(got[2], "full_d") || !strings.Contains(got[2], "ok_d") {
		t.Fatalf("want one combined try then one transaction per set:\n%q", got)
	}
	f.mu.Lock()
	_, okKnown := f.remembered[memKey{"ok_d", a}]
	_, fullKnown := f.remembered[memKey{"full_d", a}]
	f.mu.Unlock()
	if !okKnown || fullKnown {
		t.Fatalf("memory: ok=%v full=%v", okKnown, fullKnown)
	}
	if st := f.stats(); st.Added != 1 || st.Failed != 1 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestForgetDropsMemoryOfRecreatedSets(t *testing.T) {
	run := &scriptRunner{}
	s := New(nil, run, nil)
	s.feeder.setSets(map[string]bool{"a_d": true, "b_d": true})
	addr := netip.MustParseAddr("198.51.100.1")
	s.feeder.flush(context.Background(), []element{{set: "a_d", addr: addr, ttl: 60}, {set: "b_d", addr: addr, ttl: 60}})
	s.Forget([]string{"a_d"})
	s.feeder.mu.Lock()
	_, aKnown := s.feeder.remembered[memKey{"a_d", addr}]
	_, bKnown := s.feeder.remembered[memKey{"b_d", addr}]
	s.feeder.mu.Unlock()
	if aKnown || !bKnown {
		t.Fatalf("forget: a=%v b=%v", aKnown, bKnown)
	}
}

// TestRefreshScriptResetsTimeoutInKernel applies setScript with the real nft
// parser and kernel in an unprivileged user+network namespace.
func TestRefreshScriptResetsTimeoutInKernel(t *testing.T) {
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nft not installed")
	}
	if out, err := exec.Command("unshare", "-rn", "nft", "list", "ruleset").CombinedOutput(); err != nil {
		t.Skipf("no user network namespace: %v %s", err, out)
	}
	dir := t.TempDir()
	tbl := "ip " + routing.TableName
	script := setScript("r1_d", []pendingElement{
		{addr: netip.MustParseAddr("192.0.2.1"), timeout: 30 * time.Minute},
		{addr: netip.MustParseAddr("192.0.2.2"), timeout: 2 * time.Hour},
	})
	if err := os.WriteFile(filepath.Join(dir, "feed.nft"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	steps := "set -e\nnft add table " + tbl + "\n" +
		"nft add set " + tbl + " r1_d '{ type ipv4_addr; flags timeout; size 65536; }'\n" +
		"nft add element " + tbl + " r1_d '{ 192.0.2.1 timeout 60s }'\n" +
		"nft -f feed.nft\nnft list set " + tbl + " r1_d\n"
	if err := os.WriteFile(filepath.Join(dir, "steps.sh"), []byte(steps), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("unshare", "-rn", "sh", "steps.sh")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("nft failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "192.0.2.1 timeout 30m") || !strings.Contains(string(out), "192.0.2.2 timeout 2h") {
		t.Fatalf("existing element not refreshed or new one not added:\n%s", out)
	}
}

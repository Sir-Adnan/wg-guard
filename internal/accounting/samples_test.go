package accounting

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/shaper"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

func newShaperForTest(run subprocess.Runner) *shaper.Manager { return shaper.New(run) }

// ---------------------------------------------------------------------------
// Samples, rollups, pruning
// ---------------------------------------------------------------------------

func TestFlushSamplesAndRollups(t *testing.T) {
	e := newEnv(t)
	uid := e.seedUser(t, "vera", "active", nil, "immediate")
	e.seedDevice(t, uid, "phone", keyA, 0, 0)

	// Two cycles of traffic within one 5-minute bucket.
	e.setActivity(t, keyA, e.now.Add(-time.Second), 1000, 200)
	cycle(t, e)
	e.now = e.now.Add(time.Minute)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 1500, 200)
	cycle(t, e)

	n, err := e.svc.FlushSamples(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 { // one device, one bucket (both deltas accumulate)
		t.Fatalf("flushed rows: %d", n)
	}
	var rx, tx int64
	var ts string
	if err := e.db.QueryRow(`SELECT ts, rx_delta, tx_delta FROM traffic_samples WHERE device_id = 'dev-phone'`).
		Scan(&ts, &rx, &tx); err != nil {
		t.Fatal(err)
	}
	if rx != 1500 || tx != 200 {
		t.Fatalf("sample: %d/%d", rx, tx)
	}
	wantBucket := e.now.Add(-time.Minute).Truncate(300 * time.Second).UTC().Format(time.RFC3339Nano)
	if ts != wantBucket {
		t.Fatalf("bucket = %v, want %v", ts, wantBucket)
	}

	// Rollups: hourly and daily accumulations exist and match.
	for _, g := range []string{"hourly", "daily"} {
		var grx, gtx int64
		if err := e.db.QueryRow(`SELECT rx, tx FROM traffic_rollups WHERE device_id = 'dev-phone' AND granularity = ?`, g).
			Scan(&grx, &gtx); err != nil {
			t.Fatalf("%s rollup: %v", g, err)
		}
		if grx != 1500 || gtx != 200 {
			t.Fatalf("%s rollup: %d/%d", g, grx, gtx)
		}
	}

	// A second flush landing in the same bucket must ACCUMULATE (upsert),
	// never overwrite or duplicate.
	e.now = e.now.Add(2 * time.Minute) // still in the same 5-minute bucket
	e.setActivity(t, keyA, e.now.Add(-time.Second), 2000, 300)
	cycle(t, e)
	n, err = e.svc.FlushSamples(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("second flush: %d %v", n, err)
	}
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM traffic_samples WHERE device_id = 'dev-phone'`).Scan(&rx); err != nil {
		t.Fatal(err)
	}
	if rx != 1 {
		t.Fatalf("sample rows: %d", rx)
	}
	if err := e.db.QueryRow(`SELECT rx_delta, tx_delta FROM traffic_samples WHERE device_id = 'dev-phone'`).
		Scan(&rx, &tx); err != nil {
		t.Fatal(err)
	}
	if rx != 2000 || tx != 300 {
		t.Fatalf("accumulated sample: %d/%d, want 2000/300", rx, tx)
	}
	var grx int64
	if err := e.db.QueryRow(`SELECT rx FROM traffic_rollups WHERE device_id = 'dev-phone' AND granularity = 'hourly'`).
		Scan(&grx); err != nil {
		t.Fatal(err)
	}
	if grx != 2000 {
		t.Fatalf("accumulated hourly rollup: %d", grx)
	}
}

func TestSampleBucketSpansHours(t *testing.T) {
	e := newEnv(t)
	uid := e.seedUser(t, "will", "active", nil, "immediate")
	e.seedDevice(t, uid, "phone", keyA, 0, 0)

	// Traffic in two different hourly buckets → two sample rows, each rollup
	// bucket carrying its own share.
	e.setActivity(t, keyA, e.now.Add(-time.Second), 100, 0)
	cycle(t, e)
	e.now = e.now.Add(2 * time.Hour)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 300, 0)
	cycle(t, e)
	if _, err := e.svc.FlushSamples(context.Background()); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM traffic_samples WHERE device_id = 'dev-phone'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("sample rows across buckets: %d", n)
	}
	var hours int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM traffic_rollups WHERE device_id = 'dev-phone' AND granularity = 'hourly'`).Scan(&hours); err != nil {
		t.Fatal(err)
	}
	if hours != 2 {
		t.Fatalf("hourly rollup rows: %d", hours)
	}
	var daily int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM traffic_rollups WHERE device_id = 'dev-phone' AND granularity = 'daily'`).Scan(&daily); err != nil {
		t.Fatal(err)
	}
	if daily != 1 {
		t.Fatalf("both hours are the same UTC day: %d daily rows", daily)
	}
}

func TestPruneRespectsRetention(t *testing.T) {
	e := newEnv(t)
	uid := e.seedUser(t, "yann", "active", nil, "immediate")
	e.seedDevice(t, uid, "phone", keyA, 0, 0)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 100, 0)
	cycle(t, e)
	if _, err := e.svc.FlushSamples(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Age the rows beyond every retention window and add fresh ones.
	old := e.now.Add(-400 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	for _, q := range []string{
		`INSERT INTO traffic_samples (device_id, ts, rx_delta, tx_delta) VALUES ('dev-phone', ?, 1, 1)`,
		`INSERT INTO traffic_rollups (device_id, granularity, bucket_start, rx, tx) VALUES ('dev-phone', 'hourly', ?, 1, 1)`,
		`INSERT INTO traffic_rollups (device_id, granularity, bucket_start, rx, tx) VALUES ('dev-phone', 'daily', ?, 1, 1)`,
	} {
		if _, err := e.db.Exec(q, old); err != nil {
			t.Fatal(err)
		}
	}

	rep, err := e.svc.Prune(context.Background(), e.now)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Samples != 1 || rep.Hourly != 1 || rep.Daily != 1 {
		t.Fatalf("prune: %+v", rep)
	}
	var n int
	// The fresh rows are one hourly + one daily rollup row and one sample.
	for _, q := range []string{
		`SELECT COUNT(*) FROM traffic_samples`,
		`SELECT COUNT(*) FROM traffic_rollups WHERE granularity = 'hourly'`,
		`SELECT COUNT(*) FROM traffic_rollups WHERE granularity = 'daily'`,
	} {
		if err := e.db.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("fresh rows must survive %q: %d", q, n)
		}
	}
}

// ---------------------------------------------------------------------------
// Shaper wiring: the cycle applies rendered tc state for limited users
// ---------------------------------------------------------------------------

type scriptRunner struct {
	mu      sync.Mutex
	calls   [][]string
	batches []string
}

func (r *scriptRunner) Run(_ context.Context, argv []string) (subprocess.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string(nil), argv...))
	if len(argv) >= 3 && argv[0] == "tc" && argv[1] == "-b" {
		r.batches = append(r.batches, "tc-batch") // content asserted via shaper's own tests
	}
	return subprocess.Result{}, nil
}

func TestCycleAppliesShaper(t *testing.T) {
	e := newEnv(t)
	uid := e.seedUser(t, "zara", "active", nil, "immediate")
	if _, err := e.db.Exec(`UPDATE users SET speed_limit_kbps = 10240 WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	e.seedDevice(t, uid, "phone", keyA, 0, 0)

	runner := &scriptRunner{}
	e.svc.shaper = newShaperForTest(runner)

	// Cycle 1: shaping applied for the limited user (del + rebuild batch).
	rep := cycle(t, e)
	if !rep.ShaperApplied || rep.ShaperError != "" {
		t.Fatalf("cycle 1 shaper: applied=%v err=%q", rep.ShaperApplied, rep.ShaperError)
	}
	if len(runner.calls) != 2 || runner.calls[1][0] != "tc" {
		t.Fatalf("del + tc batch expected: %v", runner.calls)
	}
	// Cycle 2 (no state change): zero subprocesses.
	e.now = e.now.Add(time.Minute)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 100, 0)
	rep = cycle(t, e)
	if rep.ShaperApplied {
		t.Fatal("unchanged shaping must not re-apply")
	}
	if len(runner.calls) != 2 {
		t.Fatalf("no tc calls expected: %v", runner.calls)
	}

	// Limit removed → cleanup call on the next cycle.
	if _, err := e.db.Exec(`UPDATE users SET speed_limit_kbps = NULL WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	rep = cycle(t, e)
	if !rep.ShaperApplied {
		t.Fatal("limit removal must clean up the qdisc")
	}
	if len(runner.calls) != 3 || strings.Join(runner.calls[2], " ") != "tc qdisc del dev awg0 root" {
		t.Fatalf("cleanup call expected: %v", runner.calls)
	}
}

func TestCycleShaperErrorIsReportedNotFatal(t *testing.T) {
	e := newEnv(t)
	uid := e.seedUser(t, "abel", "active", nil, "immediate")
	if _, err := e.db.Exec(`UPDATE users SET speed_limit_kbps = 1024 WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	e.seedDevice(t, uid, "phone", keyA, 0, 0)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 10, 0)

	e.svc.shaper = newShaperForTest(&failingRunner{})
	rep := cycle(t, e)
	if rep.ShaperError == "" {
		t.Fatal("shaper failure must be reported")
	}
	if !strings.Contains(rep.ShaperError, "shaper") {
		t.Fatalf("shaper error: %q", rep.ShaperError)
	}
	// The accounting itself still completed.
	if rep.Deltas != 1 {
		t.Fatalf("cycle must survive a shaper failure: %+v", rep)
	}
}

type failingRunner struct{}

func (failingRunner) Run(context.Context, []string) (subprocess.Result, error) {
	return subprocess.Result{}, &subprocess.ExitError{Name: "tc", ExitCode: 1, Stderr: "simulated tc failure"}
}

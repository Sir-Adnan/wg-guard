package hoststats

import (
	"testing"
	"time"
)

// CPU utilization comes purely from counter deltas — testable everywhere.
func TestCPUDeltaAcrossPlatforms(t *testing.T) {
	r := New(t.TempDir())
	t0 := time.Unix(1700000000, 0)

	if p := r.cpuDelta(t0, cpuTimes{idle: 100, total: 200}); p != nil {
		t.Fatalf("first sample must have no percentage, got %v", *p)
	}
	// 40 idle jiffies of 100 total → 60% busy.
	p := r.cpuDelta(t0.Add(time.Second), cpuTimes{idle: 140, total: 300})
	if p == nil || *p < 59.9 || *p > 60.1 {
		t.Fatalf("cpu%% = %v, want 60", p)
	}
	// Back-to-back call (< 500 ms) reuses the previous value.
	p2 := r.cpuDelta(t0.Add(1200*time.Millisecond), cpuTimes{idle: 1000, total: 1100})
	if p2 == nil || *p != *p2 {
		t.Fatalf("rapid call must reuse last value: %v vs %v", p, p2)
	}
}

func TestSnapshotNonLinuxUnavailable(t *testing.T) {
	if snapshotOKOnThisPlatform {
		t.Skip("platform has host metrics")
	}
	r := New(t.TempDir())
	s := r.Snapshot(time.Now())
	if s.OK {
		t.Fatal("OK must be false off Linux")
	}
	if s.CPUPercent != nil || s.MemTotal != 0 || s.Uptime != 0 {
		t.Fatal("off-Linux snapshot must carry no metrics")
	}
}

func TestMemUsed(t *testing.T) {
	s := Snapshot{MemTotal: 100, MemAvail: 25}
	if s.MemUsed() != 75 {
		t.Fatalf("MemUsed = %d", s.MemUsed())
	}
	// Inconsistent /proc reads must not produce negative usage.
	s = Snapshot{MemTotal: 100, MemAvail: 150}
	if s.MemUsed() != 0 {
		t.Fatalf("MemUsed with avail>total = %d", s.MemUsed())
	}
}

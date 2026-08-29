package scheduler

import (
	"container/heap"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// runStep processes at most one due job at the given time.
func runStep(s *Scheduler, at time.Time) {
	s.step(context.Background(), at)
}

func TestStepRunsDueJobAndReschedules(t *testing.T) {
	s := New(nil)
	var runs int
	base := time.Now()
	s.Every("job", time.Hour, func(context.Context) error { runs++; return nil })

	// Registered next = base+1h (registration time ≈ base).
	runStep(s, base.Add(59*time.Minute))
	if runs != 0 {
		t.Fatalf("ran before due: %d", runs)
	}
	runStep(s, base.Add(90*time.Minute))
	if runs != 1 {
		t.Fatalf("want 1 run, got %d", runs)
	}
	// Catch-up: two intervals missed → exactly one run, next anchored at
	// step time + interval (not stacked).
	runStep(s, base.Add(4*time.Hour))
	if runs != 2 {
		t.Fatalf("catch-up must run once per step, got %d total", runs)
	}
}

func TestHeapOrdersByNextThenName(t *testing.T) {
	// Equal next times: the name tie-break keeps processing deterministic.
	// (Registration anchors are nanoseconds apart in practice, so this only
	// matters for exact ties.)
	h := &entryHeap{}
	same := time.Now()
	heap.Push(h, &entry{name: "b", next: same})
	heap.Push(h, &entry{name: "a", next: same})
	heap.Push(h, &entry{name: "c", next: same.Add(-time.Minute)})
	if h.peek().name != "c" {
		t.Fatalf("earliest next must win: %v", h.peek().name)
	}
	heap.Pop(h) // c
	if h.peek().name != "a" {
		t.Fatalf("tie must order by name: %v", h.peek().name)
	}
}

func TestOneShotRunsOnceAndIsRemoved(t *testing.T) {
	s := New(nil)
	var runs int
	base := time.Now()
	s.At("once", base.Add(time.Minute), func(context.Context) error { runs++; return nil })

	runStep(s, base.Add(30*time.Second))
	if runs != 0 {
		t.Fatalf("ran early: %d", runs)
	}
	runStep(s, base.Add(time.Minute))
	runStep(s, base.Add(2*time.Minute))
	if runs != 1 {
		t.Fatalf("one-shot must run exactly once, got %d", runs)
	}
	s.mu.Lock()
	_, exists := s.entries["once"]
	s.mu.Unlock()
	if exists {
		t.Fatal("consumed one-shot must be removed")
	}
}

func TestPanicRecovered(t *testing.T) {
	s := New(nil)
	var good int
	base := time.Now()
	s.Every("boom", time.Hour, func(context.Context) error { panic("job exploded") })
	s.Every("fine", time.Hour, func(context.Context) error { good++; return nil })
	// Align schedules exactly: the registration anchors are nanoseconds
	// apart, and step() runs one job per pass.
	s.mu.Lock()
	s.entries["boom"].next = base.Add(1 * time.Hour)
	s.entries["fine"].next = base.Add(90 * time.Minute)
	s.mu.Unlock()

	for i := 1; i <= 10; i++ {
		runStep(s, base.Add(time.Duration(i)*30*time.Minute))
	}
	if good < 3 {
		t.Fatalf("scheduler must survive a panicking job; fine ran %d times", good)
	}
	s.mu.Lock()
	_, boomAlive := s.entries["boom"]
	_, fineAlive := s.entries["fine"]
	s.mu.Unlock()
	if !boomAlive || !fineAlive {
		t.Fatalf("jobs must stay registered after a panic (boom=%v fine=%v)", boomAlive, fineAlive)
	}
}

// nextOf reads a job's currently scheduled run time (tests register with
// relative intervals, so the absolute anchor comes from the scheduler).
func nextOf(t *testing.T, s *Scheduler, name string) time.Time {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[name]
	if !ok {
		t.Fatalf("job %q not registered", name)
	}
	return e.next
}

func TestSetIntervalFromWithinJob(t *testing.T) {
	s := New(nil)
	var runs int
	s.Every("self", time.Hour, func(context.Context) error {
		runs++
		if runs == 1 {
			// Live-apply: the accounting job does exactly this.
			s.SetInterval("self", 2*time.Hour)
		}
		return nil
	})

	due := nextOf(t, s, "self")
	runStep(s, due) // run 1; SetInterval while running → anchored at due+2h
	if got := nextOf(t, s, "self"); !got.Equal(due.Add(2 * time.Hour)) {
		t.Fatalf("next after self-set = %v, want finish+2h = %v", got, due.Add(2*time.Hour))
	}
	runStep(s, due.Add(3*time.Hour)) // due at due+2h
	if runs != 2 {
		t.Fatalf("want 2 runs, got %d", runs)
	}
}

func TestReplaceWhileRunning(t *testing.T) {
	s := New(nil)
	var runs int
	s.Every("j", time.Hour, func(context.Context) error {
		runs++
		// Replace (and live-change the interval) from within the job — the
		// accounting job's interval live-reload does exactly this.
		s.Every("j", 3*time.Hour, func(context.Context) error { runs += 100; return nil })
		return nil
	})

	due := nextOf(t, s, "j")
	runStep(s, due) // fn1 runs and replaces itself mid-run
	if got := nextOf(t, s, "j"); !got.Equal(due.Add(3 * time.Hour)) {
		t.Fatalf("next = %v, want finish+3h = %v", got, due.Add(3*time.Hour))
	}
	runStep(s, due.Add(2*time.Hour))
	if runs != 1 {
		t.Fatalf("replacement must not run early: %d", runs)
	}
	runStep(s, due.Add(4*time.Hour))
	if runs != 101 {
		t.Fatalf("replacement must take effect, runs=%d", runs)
	}
}

func TestOneShotReplacedWhileRunningKeepsTarget(t *testing.T) {
	s := New(nil)
	base := time.Now()
	var which []string
	// The self-replacement-from-inside-the-job shape is how the Phase 4
	// webhook worker will reschedule its next attempt.
	s.At("retry", base.Add(time.Hour), func(context.Context) error {
		which = append(which, "first")
		s.At("retry", base.Add(5*time.Hour), func(context.Context) error { which = append(which, "second"); return nil })
		return nil
	})

	runStep(s, base.Add(time.Hour)) // runs "first", replaces mid-run
	runStep(s, base.Add(2*time.Hour))
	if n := len(which); n != 1 {
		t.Fatalf("replacement must not run early: %v", which)
	}
	runStep(s, base.Add(6*time.Hour))
	if len(which) != 2 || which[0] != "first" || which[1] != "second" {
		t.Fatalf("one-shot replacement lost: %v", which)
	}
}

func TestStartRunsJobsAndStopCancels(t *testing.T) {
	s := New(nil)
	var runs atomic.Int64
	block := make(chan struct{})
	first := make(chan struct{}, 1)
	s.Every("fast", 10*time.Millisecond, func(ctx context.Context) error {
		if runs.Add(1) == 1 {
			select {
			case first <- struct{}{}:
			default:
			}
			<-block // block the first run; Stop must wait for it via ctx
		}
		return nil
	})
	s.Start(context.Background())
	<-first
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	close(block)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return; the running job must honor ctx")
	}
	n := runs.Load()
	if n < 1 {
		t.Fatalf("job never ran: %d", n)
	}
	// After Stop nothing further runs.
	time.Sleep(50 * time.Millisecond)
	if runs.Load() != n {
		t.Fatalf("job ran after Stop: %d → %d", n, runs.Load())
	}
}

func TestStartRunsImmediatelyDueJobs(t *testing.T) {
	s := New(nil)
	var runs atomic.Int64
	// Past-due one-shot + recurring: both eligible on the first pass.
	s.At("past", time.Now().Add(-time.Hour), func(context.Context) error { runs.Add(10); return nil })
	s.Start(context.Background())
	deadline := time.Now().Add(5 * time.Second)
	for runs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	s.Stop()
	if runs.Load() == 0 {
		t.Fatal("past-due job never ran")
	}
}

func TestConcurrentRegistrationIsSafe(t *testing.T) {
	s := New(nil)
	var wg sync.WaitGroup
	s.Start(context.Background())
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				s.Every("shared", time.Duration(i+1)*time.Second, func(context.Context) error { return nil })
				s.At("shot", time.Now().Add(time.Duration(j)*time.Second), func(context.Context) error { return nil })
				s.SetInterval("shared", time.Duration(i+1)*time.Second)
			}
		}(i)
	}
	wg.Wait()
	s.Stop()
}

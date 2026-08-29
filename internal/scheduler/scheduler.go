// Package scheduler is the single centralized scheduler for all periodic and
// deferred work: accounting cycles, expiry enforcement, housekeeping, later
// webhooks and backups. One goroutine total — no busy loops, no per-user (or
// per-anything) timers, no unbounded goroutine growth (docs/architecture/
// overview.md §Resources). Jobs run sequentially: a job never overlaps
// itself, and a slow job delays the rest (jobs are expected to be short;
// long work belongs in its own worker, not here).
//
// Catch-up semantics: after a pause (suspension, backlog) a recurring job
// that missed several intervals runs ONCE and its next run is anchored at
// finish+interval — never N back-to-back runs, which would busy-loop the
// node for work that is already idempotent (deltas collapse, expiry is
// set-based).
//
// Replacement semantics: registering a name that is currently running only
// updates the registration; the finish path schedules the fresh values. For
// recurring jobs that is finish+interval (which also makes self-rescheduling
// from inside a job — the accounting interval live-reload — safe against hot
// loops); for one-shots the stored target time is honored.
package scheduler

import (
	"container/heap"
	"context"
	"log/slog"
	"sync"
	"time"
)

// Func is one job body. It receives the scheduler's context, which is
// cancelled by Stop; long operations must honor it.
type Func func(ctx context.Context) error

type entry struct {
	name     string
	interval time.Duration // 0 = one-shot
	fn       Func
	next     time.Time
	version  int64 // bumped by every Every/At/SetInterval mutation
	running  bool  // currently executing (popped from the heap)
	inHeap   bool  // queued
	pending  bool  // replaced while running: `next` is the pending target
	idx      int
}

// Scheduler runs registered jobs on one goroutine.
type Scheduler struct {
	logger *slog.Logger

	mu      sync.Mutex
	entries map[string]*entry
	queue   entryHeap

	wake    chan struct{}
	started bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// New returns an empty scheduler. A nil logger defaults to slog.Default().
func New(logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		logger:  logger,
		entries: map[string]*entry{},
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
}

// Every registers a recurring job, replacing any job registered under the
// same name (the accounting job uses this to live-apply interval changes —
// including from within its own body). interval must be > 0.
func (s *Scheduler) Every(name string, interval time.Duration, fn Func) {
	if interval <= 0 {
		panic("scheduler: interval must be positive for " + name)
	}
	s.register(name, interval, fn, time.Now().Add(interval))
}

// At registers a one-shot job at the given time; past times run on the next
// loop pass. Replaces any job under the same name.
func (s *Scheduler) At(name string, when time.Time, fn Func) {
	s.register(name, 0, fn, when)
}

// SetInterval changes a recurring job's interval; the next run is one new
// interval from now (anchored at finish when called from within the job).
// Returns false when no job with that name exists.
func (s *Scheduler) SetInterval(name string, interval time.Duration) bool {
	if interval <= 0 {
		panic("scheduler: interval must be positive for " + name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[name]
	if !ok {
		return false
	}
	e.interval = interval
	e.version++
	if e.running {
		// The finish path re-anchors at finish+interval (pending marks a
		// mid-run mutation so the stale `next` cannot cause a hot loop).
		e.pending = true
		return true
	}
	e.next = time.Now().Add(interval)
	e.pending = false
	s.repositionLocked(e)
	s.signalLocked()
	return true
}

func (s *Scheduler) register(name string, interval time.Duration, fn Func, next time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[name]
	if !ok {
		e = &entry{name: name}
		s.entries[name] = e
	}
	e.interval = interval
	e.fn = fn
	e.version++
	if e.running {
		// The finish path schedules with the fresh values.
		e.next, e.pending = next, true
		return
	}
	e.next, e.pending = next, false
	s.repositionLocked(e)
	s.signalLocked()
}

// repositionLocked inserts or re-orders e in the run queue.
func (s *Scheduler) repositionLocked(e *entry) {
	if e.inHeap {
		heap.Fix(&s.queue, e.idx)
		return
	}
	e.inHeap = true
	heap.Push(&s.queue, e)
}

func (s *Scheduler) signalLocked() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Start launches the scheduler goroutine. Panics on double start.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		panic("scheduler: already started")
	}
	s.started = true
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()

	go func() {
		defer close(s.done)
		s.loop(runCtx)
	}()
}

// Stop cancels the context, waits for the loop (and the job it may be
// running, which is expected to honor the cancellation) to exit, and is
// idempotent.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	cancel, started := s.cancel, s.started
	s.mu.Unlock()
	if !started {
		return
	}
	cancel()
	<-s.done
}

func (s *Scheduler) loop(ctx context.Context) {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	if !timer.Stop() {
		<-timer.C
	}

	for {
		s.mu.Lock()
		e := s.queue.peek()
		if e == nil {
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			}
			continue
		}
		delay := time.Until(e.next)
		if delay < 0 {
			delay = 0
		}
		s.mu.Unlock()

		timer.Reset(delay)
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}

		if ctx.Err() != nil {
			return
		}
		// One due job per pass; loop immediately so a backlog drains at
		// one-run-per-job granularity (never N runs for N missed intervals).
		s.step(ctx, time.Now())
	}
}

// step pops and runs the earliest due job (if any), then reschedules it.
// now doubles as the finish anchor for the next run — tests pass a fixed
// time for determinism.
func (s *Scheduler) step(ctx context.Context, now time.Time) {
	s.mu.Lock()
	e := s.queue.peek()
	if e == nil || e.next.After(now) {
		s.mu.Unlock()
		return
	}
	s.queue.pop()
	e.inHeap = false
	e.running = true
	fn, v := e.fn, e.version
	s.mu.Unlock()

	s.run(ctx, e.name, fn)

	s.mu.Lock()
	defer s.mu.Unlock()
	e.running = false
	if e.interval > 0 {
		// Recurring: an unchanged registration anchors catch-up at
		// now+interval; a replaced registration re-schedules from the fresh
		// values (register set next itself, unless it was pending — then
		// also anchor here so a self-set interval cannot land in the past
		// and hot-loop).
		if e.version == v || e.pending {
			e.next = now.Add(e.interval)
			e.pending = false
		}
		s.repositionLocked(e)
		return
	}
	// One-shot: consumed, unless it was replaced while running — the
	// replacement's stored target time then applies.
	if e.version == v {
		delete(s.entries, e.name)
		return
	}
	if e.pending {
		e.pending = false
	}
	s.repositionLocked(e)
}

func (s *Scheduler) run(ctx context.Context, name string, fn Func) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("scheduler: job panicked", "job", name, "panic", r)
		}
	}()
	if err := fn(ctx); err != nil {
		s.logger.Warn("scheduler: job failed", "job", name, "err", err)
	}
}

// entryHeap orders entries by next run time (then name for determinism).
type entryHeap []*entry

func (h entryHeap) Len() int      { return len(h) }
func (h entryHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i]; h[i].idx, h[j].idx = i, j }
func (h entryHeap) Less(i, j int) bool {
	if !h[i].next.Equal(h[j].next) {
		return h[i].next.Before(h[j].next)
	}
	return h[i].name < h[j].name
}
func (h *entryHeap) Push(x any) { e := x.(*entry); e.idx = len(*h); *h = append(*h, e) }
func (h *entryHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	*h = old[:n-1]
	return e
}
func (h *entryHeap) pop() { heap.Pop(h) }
func (h entryHeap) peek() *entry {
	if len(h) == 0 {
		return nil
	}
	return h[0]
}

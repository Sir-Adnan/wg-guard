package serve

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/scheduler"
)

func TestSlowWorkDoesNotDelayCentralEnforcementAndCoalescesRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 2)
	finish := make(chan struct{})
	var calls atomic.Int32
	work := startSlowWork(ctx, quietLogger(), "fixture", time.Minute, func(ctx context.Context) error {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-finish:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	defer work.stop(context.Background())
	_ = work.request(ctx)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("slow job did not start")
	}
	for range 100 {
		_ = work.request(ctx)
	}
	enforced := make(chan struct{}, 1)
	sched := scheduler.New(quietLogger())
	sched.At("slow", time.Now(), work.request)
	sched.At("enforcement", time.Now(), func(context.Context) error { enforced <- struct{}{}; return nil })
	sched.Start(ctx)
	defer sched.Stop()
	select {
	case <-enforced:
	case <-time.After(time.Second):
		t.Fatal("stalled I/O delayed central enforcement")
	}
	if calls.Load() != 1 || len(work.wake) != 1 {
		t.Fatal("slow job overlapped itself or queued unlimited work")
	}
	close(finish)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("coalesced durable-work notification was lost")
	}
}

func TestSlowWorkShutdownIsBoundedAndRetryable(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	work := startSlowWork(context.Background(), quietLogger(), "fixture", time.Minute, func(context.Context) error {
		close(started)
		<-release // Deliberately non-cooperative operation.
		return nil
	})
	_ = work.request(context.Background())
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := work.stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("non-cooperative job bypassed shutdown deadline")
	}
	close(release)
	if err := work.stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownPreservesDatabaseAndOwnershipUntilSlowWorkDrains(t *testing.T) {
	cfg := testConfig(t, "127.0.0.1:0")
	service := &backup.Service{Cfg: cfg}
	lease, err := service.OpenData(false)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	started, release := make(chan struct{}), make(chan struct{})
	work := startSlowWork(context.Background(), quietLogger(), "fixture", time.Minute, func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	released := false
	defer func() {
		if !released {
			close(release)
		}
		_ = work.stop(context.Background())
	}()
	_ = work.request(context.Background())
	<-started
	n := &Node{db: db, dataLease: lease, slowWork: []*slowWork{work}}
	n.booted.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := n.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shutdown ignored its unfinished slow worker")
	}
	if n.booted.Load() || db.PingContext(context.Background()) != nil {
		t.Fatal("draining node stayed ready or closed its active database")
	}
	if contender, err := service.OpenData(true); err == nil {
		contender.Close()
		t.Fatal("replacement admitted before slow work drained")
	}
	close(release)
	released = true
	if err := n.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	contender, err := service.OpenData(true)
	if err != nil {
		t.Fatal("successful drain leaked data ownership", err)
	}
	contender.Close()
}

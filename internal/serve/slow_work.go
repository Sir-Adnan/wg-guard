package serve

import (
	"context"
	"log/slog"
	"time"
)

// slowWork is one fixed worker with one coalesced pending wakeup. It is not a
// per-account task/timer. Due schedules and deliveries remain durable DB rows;
// notification coalescing never removes those rows or claims exactly-once delivery.
type slowWork struct {
	wake   chan struct{}
	done   chan struct{}
	cancel context.CancelFunc
}

func startSlowWork(ctx context.Context, log *slog.Logger, name string, timeout time.Duration, run func(context.Context) error) *slowWork {
	ctx, cancel := context.WithCancel(ctx)
	work := &slowWork{wake: make(chan struct{}, 1), done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(work.done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-work.wake:
				if ctx.Err() != nil {
					return
				}
				job, finish := context.WithTimeout(ctx, timeout)
				err := run(job)
				finish()
				if err != nil && ctx.Err() == nil {
					log.Warn("background operation failed", "operation", name, "err", err)
				}
			}
		}
	}()
	return work
}

func (w *slowWork) request(context.Context) error {
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return nil
}

func (w *slowWork) stop(ctx context.Context) error {
	w.cancel()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

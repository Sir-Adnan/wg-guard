// Package runtimeapply owns bounded serialized application of desired node state.
// SQLite remains durable desired state; this package records only the latest
// process-local applied/pending observation and adds no scheduler or task queue.
package runtimeapply

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/reconcile"
)

type Runner interface {
	Run(context.Context) (*reconcile.Report, error)
}
type Status string

const (
	NotConfigured Status = "not_configured"
	Applied       Status = "applied"
	Pending       Status = "pending"
)

type Outcome struct {
	Status Status
	Report *reconcile.Report
	Err    error
}

// Attempt is the shared web/API adapter boundary. Persistence already committed
// stays durable on failure; callers decide their existing response/audit behavior.
func Attempt(ctx context.Context, runner Runner, log *slog.Logger) Outcome {
	if runner == nil {
		return Outcome{Status: NotConfigured}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	report, err := runner.Run(ctx)
	if log != nil {
		if err != nil {
			log.Warn("reconcile after mutation failed", "error", err)
		}
		if report != nil {
			for _, entry := range report.Errors {
				log.Warn("reconcile interface error", "interface", entry.Interface, "error", entry.Err)
			}
		}
	}
	if err == nil && report != nil && len(report.Errors) > 0 {
		first := report.Errors[0]
		err = fmt.Errorf("reconcile interface %s: %s", first.Interface, first.Err)
	}
	status := Applied
	if err != nil {
		status = Pending
	}
	return Outcome{Status: status, Report: report, Err: err}
}

type Observation struct {
	Status           Status
	At               time.Time
	Desired, Applied uint64
}
type Coordinator struct {
	inner Runner
	gate  chan struct{}
	ready func(bool)
	mu    sync.RWMutex
	last  Observation
}

func New(inner Runner, ready func(bool)) *Coordinator {
	return &Coordinator{inner: inner, gate: make(chan struct{}, 1), ready: ready, last: Observation{Status: NotConfigured}}
}

// Run serializes API/web and accounting/runtime-repair passes, but a canceled
// waiter need not wait for a long predecessor. There is no per-account goroutine.
func (c *Coordinator) Run(ctx context.Context) (*reconcile.Report, error) {
	if c.inner == nil {
		return nil, fmt.Errorf("runtime application is not configured")
	}
	c.mu.Lock()
	c.last.Desired++
	c.last.Status = Pending
	if c.ready != nil {
		c.ready(false)
	}
	c.mu.Unlock()
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-c.gate }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.RLock()
	revision := c.last.Desired
	c.mu.RUnlock()
	report, err := c.inner.Run(ctx)
	good := err == nil && (report == nil || len(report.Errors) == 0)
	c.mu.Lock()
	if good {
		c.last.Applied = revision
	}
	good = good && c.last.Desired == revision
	c.last.Status = Pending
	if good {
		c.last.Status = Applied
	}
	c.last.At = time.Now().UTC()
	if c.ready != nil {
		c.ready(good)
	}
	c.mu.Unlock()
	return report, err
}

func (c *Coordinator) Snapshot() Observation { c.mu.RLock(); defer c.mu.RUnlock(); return c.last }

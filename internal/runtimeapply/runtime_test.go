package runtimeapply

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/reconcile"
)

type runnerFunc func(context.Context) (*reconcile.Report, error)

func (f runnerFunc) Run(ctx context.Context) (*reconcile.Report, error) { return f(ctx) }

func TestCanceledWaiterCannotOverlapRuntimePass(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	c := New(runnerFunc(func(context.Context) (*reconcile.Report, error) {
		close(entered)
		<-release
		return &reconcile.Report{}, nil
	}), nil)
	done := make(chan struct{})
	go func() { defer close(done); c.Run(context.Background()) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("runtime waiter ignored its deadline")
	}
	close(release)
	<-done
	if c.Snapshot().Status != Pending || c.Snapshot().Desired != 2 || c.Snapshot().Applied != 1 {
		t.Fatal("newer canceled mutation was mistaken for applied state")
	}
}

func TestAttemptDistinguishesPendingFromAppliedAndUnconfigured(t *testing.T) {
	if Attempt(context.Background(), nil, nil).Status != NotConfigured {
		t.Fatal("nil runtime claimed application")
	}
	runner := runnerFunc(func(context.Context) (*reconcile.Report, error) {
		return &reconcile.Report{Errors: []reconcile.InterfaceError{{Interface: "awg0", Err: "synthetic apply failure"}}}, nil
	})
	var ready bool
	c := New(runner, func(value bool) { ready = value })
	outcome := Attempt(context.Background(), c, nil)
	if outcome.Status != Pending || outcome.Err == nil || ready || c.Snapshot().Status != Pending {
		t.Fatal("partial runtime report was treated as applied")
	}
}

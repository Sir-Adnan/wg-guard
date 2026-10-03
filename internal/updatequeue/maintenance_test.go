package updatequeue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestScheduleWindowCancellationAndClaim(t *testing.T) {
	q := readyQueue(t)
	now := time.Now().UTC()
	q.Now = func() time.Time { return now }
	input := Input{Operation: OperationPanel, Channel: "release", Ref: "v0.1.7", ExpectedCommit: strings.Repeat("a", 40)}
	s, err := q.Schedule(context.Background(), input, "operator", now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := q.PromoteDue(); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Claim(context.Background()); !errors.Is(err, ErrNoRequest) {
		t.Fatal("future schedule was executed")
	}
	if _, err := q.Enqueue(context.Background(), Input{Operation: OperationInspect}); !errors.Is(err, ErrBusy) {
		t.Fatal("scheduled maintenance was overwritten")
	}
	now = now.Add(2 * time.Hour)
	if err := q.PromoteDue(); err != nil {
		t.Fatal(err)
	}
	req, err := q.Claim(context.Background())
	if err != nil || req.ID != s.ID || req.ExpectedCommit != input.ExpectedCommit {
		t.Fatal("scheduled destination identity lost")
	}
	if err := q.Cancel(s.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("running maintenance was canceled")
	}
	if err := q.Finish(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	history, err := q.History()
	if err != nil || len(history) != 1 || history[0].Actor != "operator" || history[0].State != StateSucceeded {
		t.Fatal("schedule result not retained")
	}
}

func TestScheduleMissedWindowDoesNotRun(t *testing.T) {
	q := readyQueue(t)
	now := time.Now().UTC()
	q.Now = func() time.Time { return now }
	if _, err := q.Schedule(context.Background(), Input{Operation: OperationCore, Core: "awg-2026-09"}, "owner", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	if err := q.PromoteDue(); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Claim(context.Background()); !errors.Is(err, ErrNoRequest) {
		t.Fatal("missed window ran unexpectedly")
	}
	s, err := q.Status()
	if err != nil || s.State != StateCanceled || s.Failure != "missed_window" {
		t.Fatal("missed window not explained")
	}
}

func TestMaintenanceProgressRedactionAndHistoryBound(t *testing.T) {
	q := readyQueue(t)
	for n := 0; n < MaxHistory+1; n++ {
		s, err := q.Enqueue(context.Background(), Input{Operation: OperationInspect})
		if err != nil {
			t.Fatal(err)
		}
		req, err := q.Claim(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := q.Record(req, "host", "inspect", StateRunning); err != nil {
			t.Fatal(err)
		}
		if err := q.Record(req, "host", "inspect", StateSucceeded); err != nil {
			t.Fatal(err)
		}
		updated, _ := q.Status()
		if updated.Revision <= s.Revision {
			t.Fatal("progress not visible while state remains running")
		}
		if err := q.Record(req, "host", "secret-config", StateFailed); !errors.Is(err, ErrInvalid) {
			t.Fatal("arbitrary log text admitted")
		}
		if err := q.Finish(context.Background(), req, &OperationError{Code: "preflight_blocked", Cause: errors.New("secret-not-for-web")}); err != nil {
			t.Fatal(err)
		}
	}
	history, err := q.History()
	if err != nil || len(history) != MaxHistory {
		t.Fatal("history retention is not bounded")
	}
	raw, _ := json.Marshal(history)
	if strings.Contains(string(raw), "secret-not-for-web") {
		t.Fatal("raw errors leaked into safe history")
	}
	if history[0].Failure != "preflight_blocked" {
		t.Fatal("safe failure classification lost")
	}
}

func TestCancelOnlyExactUnclaimedOperation(t *testing.T) {
	q := readyQueue(t)
	s, err := q.Enqueue(context.Background(), Input{Operation: OperationInspect})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Cancel(strings.Repeat("f", 32)); !errors.Is(err, ErrInvalid) {
		t.Fatal("different request canceled")
	}
	if err := q.Cancel(s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Claim(context.Background()); !errors.Is(err, ErrNoRequest) {
		t.Fatal("canceled request remained runnable")
	}
	if _, err := q.Enqueue(context.Background(), Input{Operation: OperationInspect}); err != nil {
		t.Fatal(err)
	}
}

func TestOldBridgeCannotReceiveNewVerbs(t *testing.T) {
	q := readyQueue(t)
	if err := os.WriteFile(q.Paths().Marker, []byte("{\"schema\":1}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !q.Available() || q.Enhanced() {
		t.Fatal("legacy marker classification")
	}
	if _, err := q.Enqueue(context.Background(), Input{Operation: OperationInspect}); !errors.Is(err, ErrInvalid) {
		t.Fatal("old broker received new verb")
	}
	if ValidReadyMarker([]byte(`{"schema":1,"execute":"foreign"}`)) {
		t.Fatal("foreign marker classified as owned")
	}
}

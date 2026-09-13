package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

func requestQueue(t *testing.T, input updatequeue.Input) *updatequeue.Queue {
	t.Helper()
	q := updatequeue.New(t.TempDir())
	if err := os.WriteFile(q.Paths().Marker, updatequeue.ReadyMarker(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Enqueue(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	return q
}

func TestUpdateRequestArgumentsStayBounded(t *testing.T) {
	cases := []struct {
		input updatequeue.Input
		kind  string
		args  []string
	}{
		{updatequeue.Input{Operation: updatequeue.OperationPanel, Channel: "release", Ref: "v1.2.3"}, "panel", []string{"--release", "v1.2.3"}},
		{updatequeue.Input{Operation: updatequeue.OperationCore, Core: "awg-2026-09"}, "core", []string{"--bundle", "awg-2026-09", "--yes"}},
		{updatequeue.Input{Operation: updatequeue.OperationAll, Channel: "release", Ref: "v1.2.3", Core: "awg-2026-09"}, "all", []string{"--release", "v1.2.3", "--bundle", "awg-2026-09", "--yes"}},
	}
	for _, tc := range cases {
		kind, args, err := updateRequestArgs(tc.input)
		if err != nil || kind != tc.kind || !reflect.DeepEqual(args, tc.args) {
			t.Errorf("args(%#v) = %q %v, %v", tc.input, kind, args, err)
		}
	}
	if _, _, err := updateRequestArgs(updatequeue.Input{Operation: updatequeue.OperationPanel, Channel: "commit", Ref: "main"}); err == nil {
		t.Fatal("development source reached the host runner")
	}
}

func TestRunUpdateRequestFinishesPublicStatus(t *testing.T) {
	q := requestQueue(t, updatequeue.Input{Operation: updatequeue.OperationCore, Core: "awg-2026-09"})
	var gotKind string
	var gotArgs []string
	err := runUpdateRequestWith(context.Background(), q, 0, func(kind string, args []string) error {
		gotKind, gotArgs = kind, append([]string(nil), args...)
		return nil
	})
	if err != nil || gotKind != "core" || !reflect.DeepEqual(gotArgs, []string{"--bundle", "awg-2026-09", "--yes"}) {
		t.Fatalf("run = %q %v, %v", gotKind, gotArgs, err)
	}
	status, err := q.Status()
	if err != nil || status.State != updatequeue.StateSucceeded {
		t.Fatalf("status = %#v, %v", status, err)
	}
}

func TestRunUpdateRequestRecordsSafeFailure(t *testing.T) {
	q := requestQueue(t, updatequeue.Input{Operation: updatequeue.OperationPanel, Channel: "release", Ref: "v1.2.3"})
	private := errors.New("private acquisition detail")
	err := runUpdateRequestWith(context.Background(), q, 0, func(string, []string) error { return private })
	if !errors.Is(err, private) {
		t.Fatalf("run error = %v", err)
	}
	status, statusErr := q.Status()
	if statusErr != nil || status.State != updatequeue.StateFailed || status.Failure != updatequeue.FailureOperation {
		t.Fatalf("status = %#v, %v", status, statusErr)
	}
}

func TestRunUpdateRequestCapsResponseGraceForFutureTimestamp(t *testing.T) {
	q := requestQueue(t, updatequeue.Input{Operation: updatequeue.OperationCore, Core: "awg-2026-09"})
	q.Now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	// Replace the generated request with one whose timestamp is in the future.
	paths := q.Paths()
	if err := os.Remove(paths.Request); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Enqueue(context.Background(), updatequeue.Input{Operation: updatequeue.OperationCore, Core: "awg-2026-09"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	called := false
	if err := runUpdateRequestWith(ctx, q, time.Millisecond, func(string, []string) error { called = true; return nil }); err != nil || !called {
		t.Fatalf("bounded future request = called %v, %v", called, err)
	}
}

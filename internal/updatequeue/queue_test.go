package updatequeue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func readyQueue(t *testing.T) *Queue {
	t.Helper()
	dir := t.TempDir()
	q := New(dir)
	if err := os.WriteFile(q.Paths().Marker, ReadyMarker(), 0o600); err != nil {
		t.Fatal(err)
	}
	q.Now = func() time.Time { return time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC) }
	return q
}

func TestQueueRequiresBrokerAndRunsOneRequestAtATime(t *testing.T) {
	ctx := context.Background()
	unavailable := New(t.TempDir())
	if _, err := unavailable.Enqueue(ctx, Input{Operation: OperationPanel, Channel: "release", Ref: "v1.2.3"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("enqueue without broker = %v", err)
	}

	q := readyQueue(t)
	queued, err := q.Enqueue(ctx, Input{Operation: OperationPanel, Channel: "release", Ref: "v1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if queued.State != StateQueued || queued.ID == "" || queued.Ref != "v1.2.3" {
		t.Fatalf("queued status = %#v", queued)
	}
	queuedRaw, err := os.ReadFile(q.Paths().Status)
	if err != nil || strings.Contains(string(queuedRaw), "0001-01-01") {
		t.Fatalf("queued status leaked zero timestamps: %q, %v", queuedRaw, err)
	}
	if _, err := q.Enqueue(ctx, Input{Operation: OperationCore, Core: "awg-2026-09"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second enqueue = %v", err)
	}

	request, err := q.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if request.ID != queued.ID || request.Operation != OperationPanel {
		t.Fatalf("claimed request = %#v", request)
	}
	status, err := q.Status()
	if err != nil || status.State != StateRunning {
		t.Fatalf("running status = %#v, %v", status, err)
	}
	if err := q.Finish(ctx, request, nil); err != nil {
		t.Fatal(err)
	}
	status, err = q.Status()
	if err != nil || status.State != StateSucceeded || status.FinishedAt.IsZero() {
		t.Fatalf("finished status = %#v, %v", status, err)
	}
	for _, path := range []string{q.Paths().Request, q.Paths().Running} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("active file survived: %s, %v", path, err)
		}
	}
	if runtime.GOOS != "windows" {
		if stat, err := os.Stat(q.Paths().Status); err != nil || stat.Mode().Perm() != 0o600 {
			t.Fatalf("status permissions = %v, %v", stat, err)
		}
	}
}

func TestQueueRecordsSafeFailureWithoutErrorText(t *testing.T) {
	q := readyQueue(t)
	queued, err := q.Enqueue(context.Background(), Input{Operation: OperationCore, Core: "awg-2026-09"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := q.Claim(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	private := errors.New("private transport details")
	if err := q.Finish(context.Background(), request, private); err != nil {
		t.Fatal(err)
	}
	status, err := q.Status()
	if err != nil || status.ID != queued.ID || status.State != StateFailed || status.Failure != FailureOperation {
		t.Fatalf("failure status = %#v, %v", status, err)
	}
	raw, err := os.ReadFile(q.Paths().Status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), private.Error()) {
		t.Fatal("private lifecycle error persisted")
	}
}

func TestQueueRejectsUncataloguedOrUnsafeRequests(t *testing.T) {
	q := readyQueue(t)
	cases := []Input{
		{},
		{Operation: OperationPanel, Channel: "commit", Ref: "main"},
		{Operation: OperationPanel, Channel: "release", Ref: "../tag"},
		{Operation: OperationCore, Core: "../core"},
		{Operation: OperationAll, Channel: "release", Ref: "v1", Core: ""},
	}
	for _, input := range cases {
		if _, err := q.Enqueue(context.Background(), input); !errors.Is(err, ErrInvalid) {
			t.Errorf("enqueue %#v = %v", input, err)
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(q.Dir, "*.tmp-*")); len(matches) != 0 {
		t.Fatalf("temporary files survived: %v", matches)
	}
}

func TestQueueClaimRejectsForgedRequestIdentity(t *testing.T) {
	q := readyQueue(t)
	request := Request{
		Schema: Schema, ID: strings.Repeat("x", 32), CreatedAt: q.now(),
		Input: Input{Operation: OperationCore, Core: "awg-2026-09"},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(q.Paths().Request, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Claim(context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("forged request identity accepted: %v", err)
	}
	if _, err := os.Stat(q.Paths().Running); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("forged running request survived: %v", err)
	}
}

func TestQueueReclaimsOnlyExpiredGeneratedWork(t *testing.T) {
	q := readyQueue(t)
	first, err := q.Enqueue(context.Background(), Input{Operation: OperationCore, Core: "awg-2026-09"})
	if err != nil {
		t.Fatal(err)
	}
	q.Now = func() time.Time { return first.CreatedAt.Add(activeLease + time.Minute) }
	status, err := q.Status()
	if err != nil || status.State != StateFailed || status.Failure != FailureInterrupted {
		t.Fatalf("expired public status = %#v, %v", status, err)
	}
	second, err := q.Enqueue(context.Background(), Input{Operation: OperationPanel, Channel: "release", Ref: "v1.2.3"})
	if err != nil || second.ID == first.ID || second.State != StateQueued {
		t.Fatalf("replacement request = %#v, %v", second, err)
	}

	q.Now = func() time.Time { return second.CreatedAt.Add(time.Minute) }
	if _, err := q.Enqueue(context.Background(), Input{Operation: OperationCore, Core: "awg-2026-09"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("live request was replaced: %v", err)
	}
	if _, err := q.Claim(context.Background()); err != nil {
		t.Fatal(err)
	}
	q.Now = func() time.Time { return second.CreatedAt.Add(activeLease + 2*time.Minute) }
	third, err := q.Enqueue(context.Background(), Input{Operation: OperationCore, Core: "awg-2026-09"})
	if err != nil || third.State != StateQueued {
		t.Fatalf("expired running request was not recovered: %#v, %v", third, err)
	}
}

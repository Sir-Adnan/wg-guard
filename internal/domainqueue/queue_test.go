package domainqueue

import (
	"errors"
	"github.com/Sir-Adnan/wg-guard/internal/domaintls"
	"os"
	"strings"
	"sync"
	"testing"
)

func readyQueue(t *testing.T) *Queue {
	t.Helper()
	q := New(t.TempDir())
	if e := os.MkdirAll(q.Dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(q.File("broker.json"), []byte("{\"schema\":1}\n"), 0600); e != nil {
		t.Fatal(e)
	}
	return q
}

func TestIndependentQueueInstancesPublishOnlyOneCompleteRequest(t *testing.T) {
	q := readyQueue(t)
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := New(strings.TrimSuffix(q.Dir, string(os.PathSeparator)+"domain-operations"))
			_, err := other.Enqueue(Input{Operation: "inspect"}, "owner-id")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrBusy) {
			t.Fatal("unexpected enqueue failure", err)
		}
	}
	if wins != 1 {
		t.Fatal("concurrent writers overwrote request")
	}
	r, err := q.Claim()
	if err != nil {
		t.Fatal("runner saw partial request", err)
	}
	if err := q.Record(r, "failed", "validating", "authorization_revoked"); err != nil {
		t.Fatal(err)
	}
	if err := q.Finish(r, errors.New("synthetic revoked owner")); err != nil {
		t.Fatal(err)
	}
	s, err := q.Status()
	if err != nil || s.Failure != "authorization_revoked" || s.Stage != "validating" {
		t.Fatal("receipt lost failure stage")
	}
}
func TestClosedQueueHasOneRequestAndNoRawCommands(t *testing.T) {
	q := readyQueue(t)
	input := Input{Operation: "configure", Role: domaintls.Subscription, Origin: "https://sub.example.test", Method: domaintls.Automatic, ExpectedRevision: "legacy", Challenge: "http"}
	s, e := q.Enqueue(input, "owner-id")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := q.Enqueue(input, "owner-id"); !errors.Is(e, ErrBusy) {
		t.Fatal("second request admitted", e)
	}
	r, e := q.Claim()
	if e != nil || r.ID != s.ID {
		t.Fatal("claim", e)
	}
	if _, e := q.Enqueue(input, "owner-id"); !errors.Is(e, ErrBusy) {
		t.Fatal("running request overwritten")
	}
	if e := q.Finish(r, nil); e != nil {
		t.Fatal(e)
	}
	got, e := q.Status()
	if e != nil || got.State != "succeeded" {
		t.Fatal("status", e)
	}
	input.Operation = "sh -c"
	if _, e := q.Enqueue(input, "owner-id"); !errors.Is(e, ErrInvalid) {
		t.Fatal("raw command admitted")
	}
}
func TestImportedMaterialIsSeparateFromReceiptsAndTraversalIsRefused(t *testing.T) {
	q := readyQueue(t)
	id, e := q.Stage([]byte("synthetic-certificate"), []byte("synthetic-private-material"))
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e := q.ImportFiles("../escape"); e == nil {
		t.Fatal("staging traversal admitted")
	}
	input := Input{Operation: "configure", Role: domaintls.Subscription, Origin: "https://sub.example.test", Method: domaintls.Manual, ExpectedRevision: "legacy", StageID: id}
	if _, e := q.Enqueue(input, "owner-id"); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(q.File("request.json"))
	if e != nil || strings.Contains(string(raw), "synthetic-private-material") {
		t.Fatal("PEM entered request receipt")
	}
	if e := q.RemoveImport(id); e != nil {
		t.Fatal(e)
	}
}

package audit

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
)

func newAudit(t *testing.T) (*Service, *database.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "a.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	return NewService(db), db
}

func TestRecordAndRecent(t *testing.T) {
	svc, _ := newAudit(t)
	ctx := context.Background()
	err := svc.Record(ctx, Entry{
		ActorType: ActorAdmin, ActorID: "a1",
		Action: "settings.set", Target: "network.mtu",
		SourceIP: "127.0.0.1", Metadata: map[string]any{"kind": "int"},
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	err = svc.Record(ctx, Entry{ActorType: ActorSystem, Action: "reconcile.run"})
	if err != nil {
		t.Fatalf("record no-meta: %v", err)
	}
	recs, err := svc.Recent(ctx, 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}
	if recs[0].Action != "reconcile.run" || recs[0].Metadata != "{}" {
		t.Fatalf("newest first / default metadata broken: %+v", recs[0])
	}
	if recs[1].Target != "network.mtu" || recs[1].SourceIP != "127.0.0.1" {
		t.Fatalf("fields not persisted: %+v", recs[1])
	}
	if recs[1].TS.After(time.Now().Add(time.Minute)) {
		t.Fatal("timestamp not UTC now")
	}
}

func TestPrune(t *testing.T) {
	svc, _ := newAudit(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := svc.Record(ctx, Entry{ActorType: ActorSystem, Action: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := svc.Prune(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 3 {
		t.Fatalf("pruned %d, want 3", n)
	}
	recs, _ := svc.Recent(ctx, 10)
	if len(recs) != 0 {
		t.Fatal("records survived prune")
	}
}

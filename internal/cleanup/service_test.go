package cleanup

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

func fixture(t *testing.T) *Service {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	return New(db, user.NewService(db), ring)
}

func account(t *testing.T, s *Service, name string) string {
	t.Helper()
	u, err := s.Users.Create(context.Background(), user.Input{Username: name})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE users SET status='expired',expires_at='2026-01-02T00:00:00Z' WHERE id=?`, u.ID); err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func withDevice(t *testing.T, s *Service, id string) {
	t.Helper()
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO tunnel_interfaces(id,name,listen_port,ipv4_subnet,mtu,public_key,private_key_encrypted,created_at,updated_at)
	VALUES('ifc','awg0',39001,'10.8.0.0/24',1420,'fixture',X'01','test','test')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(`INSERT INTO devices(id,user_id,interface_id,name,ipv4_address,public_key,private_key_encrypted,last_rx,last_tx,rx_bytes,tx_bytes,created_at,updated_at)
	VALUES(?,?,'ifc','phone','10.8.0.2/32',?,X'01',10,20,100,200,'test','test')`, "dev-"+id, id, "public-"+id)
	if err != nil {
		t.Fatal(err)
	}
}

func TestReviewedDeletionCascadesAndRetainsRemovalIntent(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	id := account(t, s, "expired-user")
	withDevice(t, s, id)
	_, err := s.DB.Exec(`INSERT INTO sub_links(user_id,token_encrypted,token_hash,created_at) VALUES(?,X'01','hash','test')`, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(`INSERT INTO traffic_samples(device_id,ts,rx_delta,tx_delta) VALUES(?,'2026-01-01T00:00:00Z',1,2)`, "dev-"+id)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Preview(ctx, Filter{Kind: "users", Status: "expired", DateField: "expires_at", Before: "2026-02-01T00:00:00Z"}, "actor")
	if err != nil || len(p.Rows) != 1 || p.Devices != 1 {
		t.Fatalf("preview: %+v %v", p, err)
	}
	if _, err := s.Execute(ctx, p.Token, "actor"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"users", "devices", "sub_links", "traffic_samples"} {
		var n int
		if err := s.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("cascade %s: %d %v", table, n, err)
		}
	}
	var pending int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM retired_peer_keys WHERE public_key=?`, "public-"+id).Scan(&pending)
	if pending != 1 {
		t.Fatal("peer removal intent disappeared")
	}
	u, err := s.Users.Create(ctx, user.Input{Username: "expired-user"})
	if err != nil || u.ID == id {
		t.Fatal("deleted username not reusable with a new identity")
	}
	if _, err := s.Execute(ctx, p.Token, "actor"); err == nil {
		t.Fatal("deleted batch replay accepted")
	}
}

func TestPreviewRejectsChangedStateAndOtherActor(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	id := account(t, s, "before-renewal")
	p, err := s.Preview(ctx, Filter{Kind: "users", Status: "expired", DateField: "expires_at"}, "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, p.Token, "second"); err == nil {
		t.Fatal("other actor executed preview")
	}
	_, _ = s.DB.Exec(`UPDATE users SET status='active',expires_at='2027-01-01T00:00:00Z' WHERE id=?`, id)
	if _, err := s.Execute(ctx, p.Token, "first"); err == nil {
		t.Fatal("renewed account deleted")
	}
	if _, err := s.Users.Get(ctx, id); err != nil {
		t.Fatal("renewed account lost")
	}
}

func TestHistoryCleanupPreservesUsageAndDeviceBaselines(t *testing.T) {
	for _, kind := range []string{"samples", "hourly", "daily"} {
		t.Run(kind, func(t *testing.T) {
			s := fixture(t)
			ctx := context.Background()
			id := account(t, s, "historical")
			withDevice(t, s, id)
			_, _ = s.DB.Exec(`UPDATE users SET traffic_used_rx=100,traffic_used_tx=200 WHERE id=?`, id)
			_, err := s.DB.Exec(`INSERT INTO traffic_samples(device_id,ts,rx_delta,tx_delta) VALUES(?,'2026-01-01T00:00:00Z',1,2)`, "dev-"+id)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.DB.Exec(`INSERT INTO traffic_rollups(device_id,bucket_start,granularity,rx,tx) VALUES(?,'2026-01-01T00:00:00Z',?,1,2)`, "dev-"+id, kind)
			if err != nil {
				t.Fatal(err)
			}
			p, err := s.Preview(ctx, Filter{Kind: kind, Before: "2026-01-02T00:00:00Z"}, "actor")
			if err != nil || len(p.Rows) != 1 {
				t.Fatalf("history preview: %v", err)
			}
			if _, err := s.Execute(ctx, p.Token, "actor"); err != nil {
				t.Fatal(err)
			}
			var rx, tx, lastRX, lastTX int
			_ = s.DB.QueryRow(`SELECT traffic_used_rx,traffic_used_tx FROM users WHERE id=?`, id).Scan(&rx, &tx)
			_ = s.DB.QueryRow(`SELECT last_rx,last_tx FROM devices WHERE user_id=?`, id).Scan(&lastRX, &lastTX)
			if rx != 100 || tx != 200 || lastRX != 10 || lastTX != 20 {
				t.Fatal("history cleanup changed charged usage or baselines")
			}
		})
	}
}

func TestCleanupOwnerScopeAndDateBoundaries(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	id := account(t, s, "reseller-user")
	other := account(t, s, "owner-user")
	_, err := s.DB.Exec(`INSERT INTO resellers(id,slug,display_name,created_at,updated_at) VALUES('r','reseller','Reseller','test','test')`)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.Exec(`UPDATE users SET reseller_id='r' WHERE id=?`, id)
	p, err := s.Preview(ctx, Filter{Kind: "users", Status: "expired", DateField: "expires_at", After: "2026-01-02T00:00:00Z", Before: "2026-01-03T00:00:00Z"}, "actor")
	if err != nil || len(p.Rows) != 1 || p.Rows[0].ID != other {
		t.Fatal("owner or inclusive start boundary failed")
	}
	p, err = s.Preview(ctx, Filter{Kind: "users", Status: "expired", DateField: "expires_at", Owner: "r", Before: "2026-01-02T00:00:00Z"}, "actor")
	if err != nil || len(p.Rows) != 0 {
		t.Fatal("exclusive end boundary failed")
	}
	if _, err := s.Preview(ctx, Filter{Kind: "users", Status: "active", DateField: "created_at"}, "actor"); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatal("active account cleanup allowed")
	}
	if _, err := s.Preview(ctx, Filter{Kind: "sqlite_master"}, "actor"); err == nil {
		t.Fatal("arbitrary table accepted")
	}
}

func TestOptimizeCompactsWithoutChangingLogicalData(t *testing.T) {
	s := fixture(t)
	id := account(t, s, "retained")
	ctx := context.Background()
	if err := Optimize(ctx, s.DB, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Users.Get(ctx, id); err != nil {
		t.Fatal("VACUUM changed account data")
	}
	if info, err := StorageInfo(ctx, s.DB); err != nil || info.Bytes == 0 {
		t.Fatal("storage inventory unavailable")
	}
}

func TestQueuedSuccessorsAreProtectedUnlessExplicitlyIncluded(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	id := account(t, s, "prepaid-next")
	_, err := s.DB.Exec(`INSERT INTO templates(id,name,created_at,updated_at) VALUES('template','Next','test','test')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(`INSERT INTO next_plan_queue(user_id,template_id,terms_json,principal_scope,created_at,updated_at)
	VALUES(?,'template','{}','owner','test','test')`, id)
	if err != nil {
		t.Fatal(err)
	}
	f := Filter{Kind: "users", Status: "expired", DateField: "expires_at"}
	p, err := s.Preview(ctx, f, "actor")
	if err != nil || len(p.Rows) != 0 {
		t.Fatal("queued successor selected by default")
	}
	f.IncludeQueued = true
	p, err = s.Preview(ctx, f, "actor")
	if err != nil || len(p.Rows) != 1 {
		t.Fatal("explicit queued selection missing")
	}
	if _, err := s.Execute(ctx, p.Token, "actor"); err != nil {
		t.Fatal(err)
	}
	var count int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM next_plan_queue`).Scan(&count)
	if count != 0 {
		t.Fatal("deleted account retained successor")
	}
}

type failSecondDeleteRecorder struct{ calls int }

func (r *failSecondDeleteRecorder) RecordTx(*sql.Tx, string, map[string]any) error {
	r.calls++
	if r.calls == 2 {
		return fmt.Errorf("synthetic event failure")
	}
	return nil
}

func TestCleanupRollsBackTheWholeBatchOnMidwayFailure(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	first := account(t, s, "rollback-first")
	_ = account(t, s, "rollback-second")
	withDevice(t, s, first)
	p, err := s.Preview(ctx, Filter{Kind: "users", Status: "expired", DateField: "expires_at"}, "actor")
	if err != nil {
		t.Fatal(err)
	}
	recorder := &failSecondDeleteRecorder{}
	s.Users.Recorder = recorder
	if _, err := s.Execute(ctx, p.Token, "actor"); err == nil || recorder.calls != 2 {
		t.Fatal("midway failure not exercised")
	}
	for table, want := range map[string]int{"users": 2, "devices": 1, "retired_peer_keys": 0} {
		var n int
		_ = s.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
		if n != want {
			t.Fatalf("partial cleanup committed %s", table)
		}
	}
}

func TestBoundedPreviewNeverIncludesNewMatchingAccounts(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	for n := 0; n < MaxBatch+1; n++ {
		account(t, s, fmt.Sprintf("batch-%03d", n))
	}
	p, err := s.Preview(ctx, Filter{Kind: "users", Status: "expired", DateField: "expires_at"}, "actor")
	if err != nil || len(p.Rows) != MaxBatch || !p.More {
		t.Fatal("preview bound missing")
	}
	newID := account(t, s, "new-after-preview")
	if _, err := s.Execute(ctx, p.Token, "actor"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Users.Get(ctx, newID); err != nil {
		t.Fatal("new match silently added to deletion")
	}
	var count int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	if count != 2 {
		t.Fatal("wrong deletion bound")
	}
}

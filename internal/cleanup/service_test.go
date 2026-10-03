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
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM devices`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO tunnel_interfaces(id,name,listen_port,ipv4_subnet,mtu,public_key,private_key_encrypted,created_at,updated_at)
	VALUES('ifc','awg0',39001,'10.8.0.0/24',1420,'fixture',X'01','test','test')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(`INSERT INTO devices(id,user_id,interface_id,name,ipv4_address,public_key,private_key_encrypted,last_rx,last_tx,rx_bytes,tx_bytes,created_at,updated_at)
	VALUES(?,?,'ifc','phone',?, ?,X'01',10,20,100,200,'test','test')`, "dev-"+id, id, fmt.Sprintf("10.8.0.%d/32", count+2), "public-"+id)
	if err != nil {
		t.Fatal(err)
	}
}

func history(t *testing.T, s *Service, userID, kind string) {
	t.Helper()
	statement := `INSERT INTO traffic_samples(device_id,ts,rx_delta,tx_delta) VALUES(?,'2026-01-01T00:00:00Z',1,2)`
	args := []any{"dev-" + userID}
	if kind != "samples" {
		statement = `INSERT INTO traffic_rollups(device_id,bucket_start,granularity,rx,tx) VALUES(?,'2026-01-01T00:00:00Z',?,1,2)`
		args = append(args, kind)
	}
	if _, err := s.DB.Exec(statement, args...); err != nil {
		t.Fatal(err)
	}
}

func TestMultiKindStatusAndOwnerUnionExcludesCascadeDuplicates(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	expired := account(t, s, "expired-combined")
	disabled := account(t, s, "disabled-combined")
	active := account(t, s, "kept-combined")
	_, err := s.DB.Exec(`INSERT INTO resellers(id,slug,display_name,created_at,updated_at) VALUES('r','reseller','Reseller','test','test')`)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.Exec(`UPDATE users SET status='disabled',reseller_id='r' WHERE id=?`, disabled)
	_, _ = s.DB.Exec(`UPDATE users SET status='active',traffic_used_rx=100,traffic_used_tx=200 WHERE id=?`, active)
	withDevice(t, s, expired)
	withDevice(t, s, active)
	for _, kind := range []string{"samples", "hourly", "daily"} {
		history(t, s, expired, kind)
		history(t, s, active, kind)
	}
	f := Filter{Kinds: []string{"daily", "users", "samples", "hourly"}, Statuses: []string{"disabled", "expired"}, Owners: []string{"r", "node"}, DateField: "expires_at"}
	p, err := s.Preview(ctx, f, "actor")
	if err != nil || p.Users != 2 || p.Devices != 1 || p.History != 3 || len(p.Groups) != 4 {
		t.Fatalf("combined preview: users/history/devices mismatch: %v", err)
	}
	if _, err := s.Execute(ctx, p.Token, "actor"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{expired, disabled} {
		if _, err := s.Users.Get(ctx, id); err == nil {
			t.Fatal("selected status/owner union was not deleted")
		}
	}
	u, err := s.Users.Get(ctx, active)
	if err != nil || u.TrafficUsedRX != 100 || u.TrafficUsedTX != 200 {
		t.Fatal("active account or charged usage changed")
	}
	for _, table := range []string{"traffic_samples", "traffic_rollups"} {
		var n int
		_ = s.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
		if n != 0 {
			t.Fatal("selected history kind not cleaned")
		}
	}
}

func TestCombinedPreviewRefusesChangedHistoryAndRollsBackMidwayFailure(t *testing.T) {
	for _, failure := range []string{"changed", "delete_error"} {
		t.Run(failure, func(t *testing.T) {
			s := fixture(t)
			ctx := context.Background()
			removed := account(t, s, "removed-combined")
			kept := account(t, s, "kept-combined")
			_, _ = s.DB.Exec(`UPDATE users SET status='active' WHERE id=?`, kept)
			withDevice(t, s, removed)
			withDevice(t, s, kept)
			history(t, s, kept, "daily")
			p, err := s.Preview(ctx, Filter{Kinds: []string{"users", "daily"}, Statuses: []string{"expired"}, Owners: []string{"node"}, DateField: "expires_at"}, "actor")
			if err != nil {
				t.Fatal(err)
			}
			if failure == "changed" {
				_, _ = s.DB.Exec(`UPDATE traffic_rollups SET rx=99`)
			} else {
				_, err = s.DB.Exec(`CREATE TRIGGER fail_cleanup BEFORE DELETE ON traffic_rollups BEGIN SELECT RAISE(ABORT,'synthetic cleanup failure'); END`)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Execute(ctx, p.Token, "actor"); err == nil {
				t.Fatal("invalid combined cleanup succeeded")
			}
			for table, want := range map[string]int{"users": 2, "devices": 2, "retired_peer_keys": 0, "traffic_rollups": 1} {
				var n int
				_ = s.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
				if n != want {
					t.Fatalf("partial combined batch retained %s", table)
				}
			}
		})
	}
}

func TestEmptyGroupAndDuplicateChoicesCannotExpandReviewedSelection(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	id := account(t, s, "live-history")
	_, _ = s.DB.Exec(`UPDATE users SET status='active' WHERE id=?`, id)
	withDevice(t, s, id)
	history(t, s, id, "samples")
	p, err := s.Preview(ctx, Filter{Kinds: []string{"samples", "users", "samples"}, Statuses: []string{"expired"}, Owners: []string{"node"}, DateField: "expires_at"}, "actor")
	if err != nil || p.Users != 0 || p.History != 1 || len(p.Groups) != 2 {
		t.Fatal("duplicate kind or empty-group handling failed")
	}
	newID := account(t, s, "new-expired")
	if _, err := s.Execute(ctx, p.Token, "actor"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Users.Get(ctx, newID); err != nil {
		t.Fatal("new match was added to an empty reviewed group")
	}
	for _, f := range []Filter{{}, {Kinds: []string{"users"}, Statuses: []string{"active"}, Owners: []string{"node"}, DateField: "created_at"}, {Kinds: []string{"samples"}, Owners: []string{}}, {Kinds: []string{"sqlite_master"}, Owners: []string{"*"}}} {
		if _, err := s.Preview(ctx, f, "actor"); err == nil {
			t.Fatal("empty/unsafe selections accepted")
		}
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
	p, err := s.Preview(ctx, Filter{Kinds: []string{"users"}, Owners: []string{"node"}, Statuses: []string{"expired"}, DateField: "expires_at", Before: "2026-02-01T00:00:00Z"}, "actor")
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
	p, err := s.Preview(ctx, Filter{Kinds: []string{"users"}, Owners: []string{"node"}, Statuses: []string{"expired"}, DateField: "expires_at"}, "first")
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
			p, err := s.Preview(ctx, Filter{Kinds: []string{kind}, Owners: []string{"node"}, Before: "2026-01-02T00:00:00Z"}, "actor")
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
	p, err := s.Preview(ctx, Filter{Kinds: []string{"users"}, Owners: []string{"node"}, Statuses: []string{"expired"}, DateField: "expires_at", After: "2026-01-02T00:00:00Z", Before: "2026-01-03T00:00:00Z"}, "actor")
	if err != nil || len(p.Rows) != 1 || p.Rows[0].ID != other {
		t.Fatal("owner or inclusive start boundary failed")
	}
	p, err = s.Preview(ctx, Filter{Kinds: []string{"users"}, Owners: []string{"r"}, Statuses: []string{"expired"}, DateField: "expires_at", Before: "2026-01-02T00:00:00Z"}, "actor")
	if err != nil || len(p.Rows) != 0 {
		t.Fatal("exclusive end boundary failed")
	}
	if _, err := s.Preview(ctx, Filter{Kinds: []string{"users"}, Owners: []string{"node"}, Statuses: []string{"active"}, DateField: "created_at"}, "actor"); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatal("active account cleanup allowed")
	}
	if _, err := s.Preview(ctx, Filter{Kinds: []string{"sqlite_master"}, Owners: []string{"node"}}, "actor"); err == nil {
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
	f := Filter{Kinds: []string{"users"}, Owners: []string{"node"}, Statuses: []string{"expired"}, DateField: "expires_at"}
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
	p, err := s.Preview(ctx, Filter{Kinds: []string{"users"}, Owners: []string{"node"}, Statuses: []string{"expired"}, DateField: "expires_at"}, "actor")
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
	p, err := s.Preview(ctx, Filter{Kinds: []string{"users"}, Owners: []string{"node"}, Statuses: []string{"expired"}, DateField: "expires_at"}, "actor")
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

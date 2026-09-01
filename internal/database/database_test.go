package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), Options{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrateFresh(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatalf("migrate twice must be a no-op: %v", err)
	}
	versions, err := db.AppliedVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"0001_init.sql", "0002_speed_limits.sql", "0003_admin_locale.sql",
		"0004_sub_links.sql", "0005_iface_advanced.sql", "0006_backup_schedules.sql",
		"0007_awg_ranges.sql"}
	if len(versions) != len(want) {
		t.Fatalf("unexpected applied versions: %v", versions)
	}
	for i, w := range want {
		if versions[i] != w {
			t.Fatalf("unexpected applied versions: %v", versions)
		}
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO devices (id, user_id, interface_id, name, ipv4_address,
		public_key, private_key_encrypted, created_at, updated_at)
		VALUES ('d1', 'nouser', 'noiface', 'phone', '10.8.0.2/32', 'pub', x'00', 'now', 'now')`)
	if err == nil {
		t.Fatal("expected FK violation for missing user/interface")
	}
}

func TestWriteTransactionsAreImmediate(t *testing.T) {
	// With two pooled connections, interleaved WithTx calls must serialize:
	// the second writer must observe the first's committed write (BEGIN
	// IMMEDIATE), not race past it. A deferred-tx driver would deadlock on
	// upgrade or lose the update.
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('k', '0', 'now')`); err != nil {
		t.Fatal(err)
	}

	const goroutines = 16
	var wg sync.WaitGroup
	var hits atomic.Int64
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := db.WithTx(ctx, func(tx *sql.Tx) error {
				var n int
				if err := tx.QueryRow(`SELECT value FROM settings WHERE key='k'`).Scan(&n); err != nil {
					return err
				}
				_, err := tx.Exec(`UPDATE settings SET value=? WHERE key='k'`, n+1)
				return err
			})
			if err != nil {
				t.Errorf("increment tx: %v", err)
				return
			}
			hits.Add(1)
		}()
	}
	wg.Wait()
	if hits.Load() != goroutines {
		t.Fatalf("only %d/%d txs succeeded", hits.Load(), goroutines)
	}
	var n int
	if err := db.QueryRow(`SELECT value FROM settings WHERE key='k'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != goroutines {
		t.Fatalf("lost updates: got %d, want %d (transactions are not serialized)", n, goroutines)
	}
}

func TestWithTxRollbackOnError(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("nope")
	err := db.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('x', '1', 'now')`); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
	var n int
	err = db.QueryRow(`SELECT COUNT(*) FROM settings WHERE key='x'`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("insert inside failed tx was not rolled back")
	}
}

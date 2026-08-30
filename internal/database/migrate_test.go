package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Sir-Adnan/wg-guard/migrations"
)

// TestMigration0002SpeedLimits applies 0001 manually, seeds legacy
// single-direction speed limits, then runs Migrate so only 0002 applies. It
// proves the phase-3 single `speed_limit_kbps` value lands in BOTH new
// columns and the old column is gone.
func TestMigration0002SpeedLimits(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "m.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	body, err := migrations.Read("0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatalf("apply 0001: %v", err)
	}
	if err := db.ensureMigrationsTable(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO migrations (version, applied_at) VALUES ('0001_init.sql', 'test')`); err != nil {
		t.Fatal(err)
	}

	// Legacy rows: one limited user/plan, one unlimited.
	_, err = db.Exec(`INSERT INTO plans (id, name, speed_limit_kbps, created_at, updated_at)
		VALUES ('p1', 'legacy', 20480, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO users (id, username, status, speed_limit_kbps, created_at, updated_at)
		VALUES ('u1', 'legacyuser', 'active', 10240, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
		       ('u2', 'freeuser', 'active', NULL, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatalf("migrate 0002: %v", err)
	}

	// The old column must be gone (fresh prepares fail).
	if _, err := db.Prepare(`SELECT speed_limit_kbps FROM users`); err == nil {
		t.Fatal("legacy users.speed_limit_kbps still exists")
	}
	if _, err := db.Prepare(`SELECT speed_limit_kbps FROM plans`); err == nil {
		t.Fatal("legacy plans.speed_limit_kbps still exists")
	}

	// The limited user/plan carries the old value in both directions.
	var down, up *int
	if err := db.QueryRow(`SELECT speed_limit_down_kbps, speed_limit_up_kbps FROM users WHERE id='u1'`).Scan(&down, &up); err != nil {
		t.Fatal(err)
	}
	if down == nil || *down != 10240 || up == nil || *up != 10240 {
		t.Fatalf("legacy user limit not copied: %v %v", down, up)
	}
	if err := db.QueryRow(`SELECT speed_limit_down_kbps, speed_limit_up_kbps FROM users WHERE id='u2'`).Scan(&down, &up); err != nil {
		t.Fatal(err)
	}
	if down != nil || up != nil {
		t.Fatalf("unlimited user must stay NULL: %v %v", down, up)
	}
	if err := db.QueryRow(`SELECT speed_limit_down_kbps, speed_limit_up_kbps FROM plans WHERE id='p1'`).Scan(&down, &up); err != nil {
		t.Fatal(err)
	}
	if down == nil || *down != 20480 || up == nil || *up != 20480 {
		t.Fatalf("legacy plan limit not copied: %v %v", down, up)
	}
}

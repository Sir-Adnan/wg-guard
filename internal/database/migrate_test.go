package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Sir-Adnan/wg-guard/migrations"
)

func openDatabaseThrough0006(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "m.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.ensureMigrationsTable(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"0001_init.sql",
		"0002_speed_limits.sql",
		"0003_admin_locale.sql",
		"0004_sub_links.sql",
		"0005_iface_advanced.sql",
		"0006_backup_schedules.sql",
	} {
		body, err := migrations.Read(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := db.Exec(`INSERT INTO migrations (version, applied_at) VALUES (?, 'test')`, name); err != nil {
			t.Fatalf("record %s: %v", name, err)
		}
	}
	return db
}

func TestMigration0007AWGRanges(t *testing.T) {
	t.Run("copies legacy values", func(t *testing.T) {
		db := openDatabaseThrough0006(t)
		ctx := context.Background()

		const insertInterface = `INSERT INTO tunnel_interfaces
			(id, name, listen_port, ipv4_subnet, mtu, public_key, private_key_encrypted,
			 jc, jmin, jmax, s1, s2, h1, h2, h3, h4, preset_name, enabled, backend_mode,
			 endpoint_override, created_at, updated_at, s3, s4, content_padding_addition)
			VALUES (?, ?, ?, ?, 1420, ?, X'0102', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 'kernel', ?,
			        '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z', ?, ?, ?)`
		rows := []struct {
			args []any
		}{
			{args: []any{"plain", "awg0", 41000, "10.80.0.0/24", "pub-plain", nil, nil, nil, nil, nil, nil, nil, nil, nil, "plain", nil, 0, 0, ""}},
			{args: []any{"scalar", "awg1", 41001, "10.81.0.0/24", "pub-scalar", 4, 40, 70, 15, 64, int64(5), int64(7), int64(9), int64(11), "recommended", "edge.example:51820", 0, 0, ""}},
			{args: []any{"gated", "awg2", 41002, "10.82.0.0/24", "pub-gated", 8, 20, 90, 64, 80, int64(1234567), int64(2234567), int64(3234567), int64(4234567), "randomized", nil, 24, 32, "10-100"}},
		}
		for _, row := range rows {
			if _, err := db.Exec(insertInterface, row.args...); err != nil {
				t.Fatalf("seed interface: %v", err)
			}
		}
		if _, err := db.Exec(`INSERT INTO plans
			(id, name, interface_id, created_at, updated_at)
			VALUES ('plan-1', 'kept-plan', 'scalar', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
			t.Fatalf("seed foreign key: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO settings (key, value, updated_at)
			VALUES ('network.client_keepalive_seconds', '37', '2026-01-03T00:00:00Z')`); err != nil {
			t.Fatalf("seed setting: %v", err)
		}

		if err := db.Migrate(ctx, nil); err != nil {
			t.Fatalf("migrate: %v", err)
		}

		assertRanges := func(id string, want [4]string) {
			t.Helper()
			var got [4]string
			if err := db.QueryRow(`SELECT h1_range, h2_range, h3_range, h4_range
				FROM tunnel_interfaces WHERE id = ?`, id).Scan(&got[0], &got[1], &got[2], &got[3]); err != nil {
				t.Fatalf("load %s ranges: %v", id, err)
			}
			if got != want {
				t.Fatalf("%s ranges = %#v, want %#v", id, got, want)
			}
		}
		assertRanges("plain", [4]string{})
		assertRanges("scalar", [4]string{"5", "7", "9", "11"})
		assertRanges("gated", [4]string{"1234567", "2234567", "3234567", "4234567"})

		var legacyH1 sql.NullInt64
		var endpoint sql.NullString
		var updated string
		if err := db.QueryRow(`SELECT h1, endpoint_override, updated_at FROM tunnel_interfaces WHERE id='scalar'`).
			Scan(&legacyH1, &endpoint, &updated); err != nil {
			t.Fatal(err)
		}
		if !legacyH1.Valid || legacyH1.Int64 != 5 || !endpoint.Valid || endpoint.String != "edge.example:51820" || updated != "2026-01-02T00:00:00Z" {
			t.Fatalf("unrelated/legacy data changed: h1=%v endpoint=%v updated=%q", legacyH1, endpoint, updated)
		}
		var planInterface string
		if err := db.QueryRow(`SELECT interface_id FROM plans WHERE id='plan-1'`).Scan(&planInterface); err != nil || planInterface != "scalar" {
			t.Fatalf("foreign key relationship changed: %q, %v", planInterface, err)
		}
		var keepalive, oldKeepalive, keepaliveUpdated string
		if err := db.QueryRow(`SELECT value, updated_at FROM settings WHERE key='network.client_persistent_keepalive'`).
			Scan(&keepalive, &keepaliveUpdated); err != nil {
			t.Fatalf("load migrated keepalive: %v", err)
		}
		if err := db.QueryRow(`SELECT value FROM settings WHERE key='network.client_keepalive_seconds'`).Scan(&oldKeepalive); err != nil {
			t.Fatalf("load legacy keepalive: %v", err)
		}
		if keepalive != "37" || oldKeepalive != "37" || keepaliveUpdated != "2026-01-03T00:00:00Z" {
			t.Fatalf("keepalive migration = new %q old %q updated %q", keepalive, oldKeepalive, keepaliveUpdated)
		}

		if err := db.Migrate(ctx, nil); err != nil {
			t.Fatalf("second migrate: %v", err)
		}
		var applied int
		if err := db.QueryRow(`SELECT COUNT(*) FROM migrations WHERE version='0007_awg_ranges.sql'`).Scan(&applied); err != nil || applied != 1 {
			t.Fatalf("0007 applied count = %d, %v", applied, err)
		}
	})

	t.Run("preserves explicit new setting", func(t *testing.T) {
		db := openDatabaseThrough0006(t)
		if _, err := db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES
			('network.client_keepalive_seconds', '25', 'old'),
			('network.client_persistent_keepalive', '40-50', 'new')`); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		var value, updated string
		if err := db.QueryRow(`SELECT value, updated_at FROM settings WHERE key='network.client_persistent_keepalive'`).Scan(&value, &updated); err != nil {
			t.Fatal(err)
		}
		if value != "40-50" || updated != "new" {
			t.Fatalf("explicit new setting overwritten: %q %q", value, updated)
		}
	})
}

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

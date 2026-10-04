package database

import (
	"context"
	"errors"
	"testing"
)

func TestMigrationStatusRejectsUnsafeHistoryBeforeWrites(t *testing.T) {
	for _, kind := range []string{"foreign", "foreign-view", "empty-history", "unknown-version", "hole", "canceled", "closed"} {
		t.Run(kind, func(t *testing.T) {
			db := openTestDB(t)
			ctx := context.Background()
			switch kind {
			case "foreign":
				_, _ = db.Exec(`CREATE TABLE unrelated(value TEXT)`)
			case "foreign-view":
				_, _ = db.Exec(`CREATE VIEW unrelated AS SELECT 1`)
			case "empty-history":
				_ = db.ensureMigrationsTable(ctx)
				_, _ = db.Exec(`CREATE TABLE unrelated(value TEXT)`)
			case "unknown-version", "hole":
				if err := db.Migrate(ctx, nil); err != nil {
					t.Fatal(err)
				}
				if kind == "hole" {
					_, _ = db.Exec(`DELETE FROM migrations WHERE version='0002_speed_limits.sql'`)
				} else {
					_, _ = db.Exec(`INSERT INTO migrations(version,applied_at) VALUES ('9999_unknown.sql','test')`)
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "closed":
				db.Close()
			}
			if _, err := db.PendingCount(ctx); err == nil {
				t.Fatal("unsafe history was reported as a fresh or current installation")
			} else if kind == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("inspection lost cancellation")
			}
			if err := db.Migrate(ctx, nil); err == nil {
				t.Fatal("unsafe migration was admitted")
			}
		})
	}
}

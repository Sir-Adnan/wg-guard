package cleanup

import (
	"context"
	"fmt"
	"os"

	"github.com/Sir-Adnan/wg-guard/internal/database"
)

type Storage struct {
	Bytes    uint64
	Reusable uint64
	WAL      uint64
}

func StorageInfo(ctx context.Context, db *database.DB) (Storage, error) {
	var pages, free, size uint64
	for _, item := range []struct {
		query string
		value *uint64
	}{{"PRAGMA page_count", &pages}, {"PRAGMA freelist_count", &free}, {"PRAGMA page_size", &size}} {
		if err := db.QueryRowContext(ctx, item.query).Scan(item.value); err != nil {
			return Storage{}, err
		}
	}
	out := Storage{Bytes: pages * size, Reusable: free * size}
	if f, err := os.Stat(db.Path() + "-wal"); err == nil {
		out.WAL = uint64(f.Size())
	}
	return out, nil
}

// Optimize is a manual operation, never a scheduler VACUUM. PASSIVE checkpoint
// respects readers; VACUUM is cancellable and runs outside application transactions.
func Optimize(ctx context.Context, db *database.DB, compact bool) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if compact {
		if _, err := conn.ExecContext(ctx, "VACUUM"); err != nil {
			return fmt.Errorf("cleanup: compact: %w", err)
		}
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)")
	return err
}

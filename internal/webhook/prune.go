package webhook

import (
	"context"
	"fmt"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
)

// Prune deletes webhook events (and, by FK cascade, their deliveries) older
// than the retention cutoff. Part of the scheduler housekeeping pass
// (database.md §Retention). Payloads are the durable record of what was
// sent; after pruning, only audit-log traces of the events remain.
func Prune(ctx context.Context, db *database.DB, before time.Time) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM webhook_events WHERE created_at < ?`,
		before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("webhook: prune: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

package api

import (
	"context"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
)

// PruneIdempotencyKeys deletes idempotency rows whose replay window has
// passed (24 h TTL — the documented replay horizon). It is the scheduler-
// callable surface of the idempotency store; rows claimed while a request
// is in flight never pass the TTL before their release, so pruning is
// always safe in housekeeping.
func PruneIdempotency(ctx context.Context, db *database.DB, now time.Time) (int64, error) {
	st := &idempotencyStore{db: db}
	return st.Prune(ctx, now)
}

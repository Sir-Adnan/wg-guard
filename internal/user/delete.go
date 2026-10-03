package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// Delete removes the account and its dependent credentials in one transaction.
// Public peer removal intent survives the cascade and failed runtime syncs.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.db.WithTx(ctx, func(tx *sql.Tx) error { return s.DeleteTx(ctx, tx, id) })
}

// DeleteTx is also used by reviewed, bounded cleanup batches. Record the event
// before removal so the webhook recorder can resolve the persisted tenant.
func (s *Service) DeleteTx(ctx context.Context, tx *sql.Tx, id string) error {
	var username string
	err := tx.QueryRowContext(ctx, `SELECT username FROM users WHERE id = ?`, id).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.E(domain.CodeUserNotFound, "user %s not found", id)
	}
	if err != nil {
		return fmt.Errorf("user: delete lookup: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO retired_peer_keys (interface_id, public_key)
		SELECT interface_id, public_key FROM devices WHERE user_id = ?`, id); err != nil {
		return err
	}
	if s.Recorder != nil {
		if err := s.Recorder.RecordTx(tx, "user.updated", map[string]any{"user_id": id, "username": username, "deleted": true, "permanent": true}); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id); err != nil {
		return fmt.Errorf("user: delete: %w", err)
	}
	return nil
}

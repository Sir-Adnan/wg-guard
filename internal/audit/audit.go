// Package audit appends security-relevant activity to audit_log. Entries
// must never contain secrets (keys, tokens, passwords, raw configs) — the
// metadata allowlist is enforced upstream by callers passing only safe,
// bounded maps. See docs/operations/security.md.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
)

// Actor types.
const (
	ActorAdmin  = "admin"
	ActorToken  = "token"
	ActorSystem = "system"
)

// Entry is one audit record. Metadata is stored as JSON; nil means "{}".
type Entry struct {
	ActorType string
	ActorID   string
	Action    string
	Target    string
	SourceIP  string
	RequestID string
	Metadata  map[string]any
}

// Service writes audit entries.
type Service struct {
	db *database.DB
}

func NewService(db *database.DB) *Service { return &Service{db: db} }

// Record appends one entry. Audit failures are returned, not swallowed:
// callers on security-relevant paths decide whether to fail the operation.
func (s *Service) Record(ctx context.Context, e Entry) error {
	meta := []byte("{}")
	if e.Metadata != nil {
		var err error
		if meta, err = json.Marshal(e.Metadata); err != nil {
			return fmt.Errorf("audit: marshal metadata: %w", err)
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log
		(ts, actor_type, actor_id, action, target, source_ip, request_id, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano),
		e.ActorType, e.ActorID, e.Action, e.Target, e.SourceIP, e.RequestID, string(meta))
	if err != nil {
		return fmt.Errorf("audit: insert: %w", err)
	}
	return nil
}

// Recent returns up to limit entries, newest first (audit screen + CLI).
type Record struct {
	ID        int64
	TS        time.Time
	ActorType string
	ActorID   string
	Action    string
	Target    string
	SourceIP  string
	RequestID string
	Metadata  string
}

func (s *Service) Recent(ctx context.Context, limit int) ([]Record, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, ts, actor_type, actor_id, action,
		target, source_ip, request_id, metadata FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("audit: query: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var r Record
		var ts string
		if err := rows.Scan(&r.ID, &ts, &r.ActorType, &r.ActorID, &r.Action,
			&r.Target, &r.SourceIP, &r.RequestID, &r.Metadata); err != nil {
			return nil, fmt.Errorf("audit: scan: %w", err)
		}
		r.TS, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, fmt.Errorf("audit: parse ts %q: %w", ts, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Prune deletes entries older than cutoff (scheduler housekeeping).
func (s *Service) Prune(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE ts < ?`,
		before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("audit: prune: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

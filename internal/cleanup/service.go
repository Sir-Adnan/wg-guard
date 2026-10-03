// Package cleanup performs reviewed, bounded maintenance on application data.
// No caller supplies a table name or SQL. A sealed preview binds the exact
// candidates and their state to one actor for ten minutes.
package cleanup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

const MaxBatch = 200
const MaxHistoryBatch = 2000

func batchLimit(kind string) int {
	if kind == "users" {
		return MaxBatch
	}
	return MaxHistoryBatch
}

type Filter struct {
	Kind          string `json:"kind"` // users, samples, hourly, daily
	Status        string `json:"status"`
	DateField     string `json:"date_field"`
	After         string `json:"after"`  // inclusive UTC RFC3339; absent is unbounded
	Before        string `json:"before"` // exclusive UTC RFC3339
	Owner         string `json:"owner"`  // empty = node owner; * = all; otherwise reseller ID
	IncludeQueued bool   `json:"include_queued"`
}

type Candidate struct {
	ID      string
	Name    string
	Status  string
	Date    string
	Devices int
	Stamp   string
}

type Preview struct {
	Filter  Filter
	Rows    []Candidate
	Devices int
	More    bool
	Token   string
	Limit   int
}

type sealed struct {
	Filter  Filter
	IDs     []string
	Hash    string
	Actor   string
	Expires time.Time
}

type Service struct {
	DB    *database.DB
	Users *user.Service
	Ring  *secrets.KeyRing
	Now   func() time.Time
}

func New(db *database.DB, users *user.Service, ring *secrets.KeyRing) *Service {
	return &Service{DB: db, Users: users, Ring: ring, Now: time.Now}
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func validate(f Filter) error {
	if f.Kind != "users" && f.Kind != "samples" && f.Kind != "hourly" && f.Kind != "daily" {
		return domain.E(domain.CodeInvalidRequest, "invalid cleanup kind")
	}
	if f.Kind == "users" {
		switch f.Status {
		case "disabled", "suspended", "expired", "traffic_exceeded", "deleted":
		default:
			return domain.E(domain.CodeInvalidRequest, "select an inactive account status")
		}
		switch f.DateField {
		case "created_at", "expires_at", "updated_at", "last_activity_at", "deleted_at":
		default:
			return domain.E(domain.CodeInvalidRequest, "select a date basis")
		}
	}
	for _, value := range []string{f.After, f.Before} {
		if value != "" {
			if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
				return domain.E(domain.CodeInvalidRequest, "invalid UTC date boundary")
			}
		}
	}
	if f.After != "" && f.Before != "" {
		a, _ := time.Parse(time.RFC3339Nano, f.After)
		b, _ := time.Parse(time.RFC3339Nano, f.Before)
		if !a.Before(b) {
			return domain.E(domain.CodeInvalidRequest, "start date must precede end date")
		}
	}
	if len(f.Owner) > 64 {
		return domain.E(domain.CodeInvalidRequest, "invalid account owner")
	}
	return nil
}

func (s *Service) candidates(ctx context.Context, q queryer, f Filter, ids []string) (*Preview, error) {
	if err := validate(f); err != nil {
		return nil, err
	}
	var statement, dateExpr, idExpr string
	var args []any
	if f.Kind == "users" {
		dateExpr, idExpr = "u."+f.DateField, "u.id"
		statement = `SELECT u.id, u.username, u.status, COALESCE(` + dateExpr + `, ''),
		 (SELECT COUNT(*) FROM devices d WHERE d.user_id = u.id),
		 json_array(u.updated_at,u.status,u.enabled,u.deleted_at,u.expires_at,u.traffic_used_rx,u.traffic_used_tx,u.traffic_limit_bytes,
		 (SELECT group_concat(id || ':' || updated_at) FROM (SELECT id,updated_at FROM devices WHERE user_id=u.id ORDER BY id)))
		 FROM users u WHERE `
		if f.Status == "deleted" {
			statement += `u.deleted_at IS NOT NULL`
		} else {
			statement += `u.deleted_at IS NULL AND u.status = ?`
			args = append(args, f.Status)
		}
		if !f.IncludeQueued {
			statement += " AND NOT EXISTS (SELECT 1 FROM next_plan_queue nq WHERE nq.user_id = u.id)"
		}
	} else {
		dateExpr = "h.ts"
		table := "traffic_samples"
		granularity := ""
		if f.Kind != "samples" {
			table, dateExpr, granularity = "traffic_rollups", "h.bucket_start", f.Kind
		}
		idExpr = "json_array(h.device_id," + dateExpr + ")"
		statement = `SELECT ` + idExpr + `, u.username, u.status, ` + dateExpr + `, 0, json_array(h.rx_delta,h.tx_delta)
		 FROM ` + table + ` h JOIN devices d ON d.id = h.device_id JOIN users u ON u.id = d.user_id WHERE 1=1`
		if granularity != "" {
			statement = strings.Replace(statement, "h.rx_delta,h.tx_delta", "h.rx,h.tx", 1)
			statement += " AND h.granularity = ?"
			args = append(args, granularity)
		}
	}
	if f.Owner == "" {
		statement += " AND u.reseller_id IS NULL"
	} else if f.Owner != "*" {
		statement += " AND u.reseller_id = ?"
		args = append(args, f.Owner)
	}
	// julianday handles equivalent RFC3339 precision without lexical edge cases.
	if f.After != "" {
		statement += " AND julianday(" + dateExpr + ") >= julianday(?)"
		args = append(args, f.After)
	}
	if f.Before != "" {
		statement += " AND julianday(" + dateExpr + ") < julianday(?)"
		args = append(args, f.Before)
	}
	if ids != nil {
		if len(ids) == 0 || len(ids) > batchLimit(f.Kind) {
			return nil, domain.E(domain.CodeInvalidRequest, "invalid cleanup selection")
		}
		statement += " AND " + idExpr + " IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	statement += " ORDER BY " + idExpr + " LIMIT ?"
	args = append(args, batchLimit(f.Kind)+1)
	rows, err := q.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("cleanup: preview: %w", err)
	}
	defer rows.Close()
	p := &Preview{Filter: f, Limit: batchLimit(f.Kind)}
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.ID, &c.Name, &c.Status, &c.Date, &c.Devices, &c.Stamp); err != nil {
			return nil, err
		}
		if len(p.Rows) == p.Limit {
			p.More = true
			break
		}
		p.Rows = append(p.Rows, c)
		p.Devices += c.Devices
	}
	return p, rows.Err()
}

func fingerprint(rows []Candidate) string {
	raw, _ := json.Marshal(rows)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Service) Preview(ctx context.Context, f Filter, actor string) (*Preview, error) {
	p, err := s.candidates(ctx, s.DB, f, nil)
	if err != nil {
		return nil, err
	}
	if len(p.Rows) == 0 {
		return p, nil
	}
	sealed := sealed{Filter: f, Actor: actor, Hash: fingerprint(p.Rows), Expires: s.Now().UTC().Add(10 * time.Minute)}
	for _, c := range p.Rows {
		sealed.IDs = append(sealed.IDs, c.ID)
	}
	raw, _ := json.Marshal(sealed)
	enc, err := s.Ring.Encrypt(raw)
	if err != nil {
		return nil, err
	}
	p.Token = base64.RawURLEncoding.EncodeToString(enc)
	if len(p.Token) > 240<<10 {
		return nil, domain.E(domain.CodeInvalidRequest, "cleanup selection is too large; narrow the date range")
	}
	return p, nil
}

// Execute rechecks only the exact reviewed IDs, never newly matching accounts.
// A renewed, edited or consumed account invalidates the complete batch.
func (s *Service) Execute(ctx context.Context, token, actor string) (*Preview, error) {
	if token == "" || len(token) > 240<<10 {
		return nil, domain.E(domain.CodeInvalidRequest, "cleanup preview is required")
	}
	enc, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, domain.E(domain.CodeInvalidRequest, "invalid cleanup preview")
	}
	raw, err := s.Ring.Decrypt(enc)
	if err != nil {
		return nil, domain.E(domain.CodeInvalidRequest, "invalid cleanup preview")
	}
	var selected sealed
	if json.Unmarshal(raw, &selected) != nil || selected.Actor != actor || !s.Now().Before(selected.Expires) {
		return nil, domain.E(domain.CodeInvalidRequest, "cleanup preview expired or belongs to another operator")
	}
	var result *Preview
	err = s.DB.WithTx(ctx, func(tx *sql.Tx) error {
		p, err := s.candidates(ctx, tx, selected.Filter, selected.IDs)
		if err != nil {
			return err
		}
		if fingerprint(p.Rows) != selected.Hash {
			return domain.E(domain.CodeInvalidRequest, "cleanup selection changed; preview again")
		}
		for _, c := range p.Rows {
			if p.Filter.Kind == "users" {
				if err := s.Users.DeleteTx(ctx, tx, c.ID); err != nil {
					return err
				}
				continue
			}
			var key []string
			if json.Unmarshal([]byte(c.ID), &key) != nil || len(key) != 2 {
				return domain.E(domain.CodeInvalidRequest, "invalid history selection")
			}
			statement := `DELETE FROM traffic_samples WHERE device_id = ? AND ts = ?`
			args := []any{key[0], key[1]}
			if p.Filter.Kind != "samples" {
				statement = `DELETE FROM traffic_rollups WHERE device_id = ? AND bucket_start = ? AND granularity = ?`
				args = append(args, p.Filter.Kind)
			}
			if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
				return err
			}
		}
		result = p
		return nil
	})
	return result, err
}

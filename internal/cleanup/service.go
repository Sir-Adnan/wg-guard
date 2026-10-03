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
	"sort"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

const MaxBatch = 200
const MaxHistoryBatch = 2000

func batchLimit(kind string, kinds []string) int {
	if kind == "users" {
		return MaxBatch
	}
	histories := 0
	for _, selected := range kinds {
		if selected != "users" {
			histories++
		}
	}
	return MaxHistoryBatch / histories
}

type Filter struct {
	Kinds         []string `json:"kinds"`
	Statuses      []string `json:"statuses"`
	DateField     string   `json:"date_field"`
	After         string   `json:"after"`  // inclusive UTC RFC3339; absent is unbounded
	Before        string   `json:"before"` // exclusive UTC RFC3339
	Owners        []string `json:"owners"` // node, reseller IDs, or explicit * for all
	IncludeQueued bool     `json:"include_queued"`
}

type Candidate struct {
	Kind    string
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
	Groups  []Group
	Users   int
	History int
}

type Group struct {
	Kind  string
	Count int
	Limit int
	More  bool
}

type selection struct {
	Kind string
	IDs  []string
	Hash string
}

type sealed struct {
	Version int
	Filter  Filter
	Groups  []selection
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

func KindChoices() []string { return []string{"users", "samples", "hourly", "daily"} }
func StatusChoices() []string {
	return []string{"expired", "traffic_exceeded", "disabled", "suspended", "deleted"}
}

// Normalize closes and orders enum selections before they enter a sealed review.
// Empty selections are invalid; they never mean an unbounded/all-data request.
func normalize(f Filter) (Filter, error) {
	choices := func(values, allowed []string) ([]string, error) {
		if len(values) == 0 || len(values) > len(allowed) {
			return nil, domain.E(domain.CodeInvalidRequest, "choose at least one cleanup option")
		}
		seen := map[string]bool{}
		for _, v := range values {
			valid := false
			for _, a := range allowed {
				if a == v {
					valid = true
					break
				}
			}
			if !valid {
				return nil, domain.E(domain.CodeInvalidRequest, "invalid cleanup option")
			}
			seen[v] = true
		}
		out := []string{}
		for _, v := range allowed {
			if seen[v] {
				out = append(out, v)
			}
		}
		return out, nil
	}
	var err error
	f.Kinds, err = choices(f.Kinds, KindChoices())
	if err != nil {
		return f, err
	}
	users := false
	for _, kind := range f.Kinds {
		if kind == "users" {
			users = true
		}
	}
	if users {
		f.Statuses, err = choices(f.Statuses, StatusChoices())
		if err != nil {
			return f, err
		}
		switch f.DateField {
		case "created_at", "expires_at", "updated_at", "last_activity_at", "deleted_at":
		default:
			return f, domain.E(domain.CodeInvalidRequest, "select a date basis")
		}
	} else {
		f.Statuses = nil
	}
	for _, value := range []string{f.After, f.Before} {
		if value != "" {
			if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
				return f, domain.E(domain.CodeInvalidRequest, "invalid UTC date boundary")
			}
		}
	}
	if f.After != "" && f.Before != "" {
		a, _ := time.Parse(time.RFC3339Nano, f.After)
		b, _ := time.Parse(time.RFC3339Nano, f.Before)
		if !a.Before(b) {
			return f, domain.E(domain.CodeInvalidRequest, "start date must precede end date")
		}
	}
	if len(f.Owners) == 0 || len(f.Owners) > 128 {
		return f, domain.E(domain.CodeInvalidRequest, "select account owners")
	}
	owners := []string{}
	seen := map[string]bool{}
	all := false
	for _, owner := range f.Owners {
		if owner == "" || len(owner) > 64 {
			return f, domain.E(domain.CodeInvalidRequest, "invalid account owner")
		}
		if owner == "*" {
			all = true
		}
		if !seen[owner] {
			seen[owner] = true
			owners = append(owners, owner)
		}
	}
	if all {
		owners = []string{"*"}
	}
	sort.Strings(owners)
	f.Owners = owners
	return f, nil
}

func (s *Service) candidates(ctx context.Context, q queryer, f Filter, kind string, ids, excludedUsers []string) ([]Candidate, bool, error) {
	var statement, dateExpr, idExpr string
	var args []any
	if kind == "users" {
		dateExpr, idExpr = "u."+f.DateField, "u.id"
		statement = `SELECT u.id, u.username, CASE WHEN u.deleted_at IS NOT NULL THEN 'deleted' ELSE u.status END, COALESCE(` + dateExpr + `, ''),
		 (SELECT COUNT(*) FROM devices d WHERE d.user_id = u.id),
		 json_array(u.updated_at,u.status,u.enabled,u.deleted_at,u.expires_at,u.traffic_used_rx,u.traffic_used_tx,u.traffic_limit_bytes,u.reseller_id,
		 (SELECT group_concat(id || ':' || updated_at) FROM (SELECT id,updated_at FROM devices WHERE user_id=u.id ORDER BY id)))
		 FROM users u WHERE `
		clauses := []string{}
		for _, status := range f.Statuses {
			if status == "deleted" {
				clauses = append(clauses, `u.deleted_at IS NOT NULL`)
			} else {
				clauses = append(clauses, `(u.deleted_at IS NULL AND u.status = ?)`)
				args = append(args, status)
			}
		}
		statement += "(" + strings.Join(clauses, " OR ") + ")"
		if !f.IncludeQueued {
			statement += " AND NOT EXISTS (SELECT 1 FROM next_plan_queue nq WHERE nq.user_id = u.id)"
		}
	} else {
		dateExpr = "h.ts"
		table := "traffic_samples"
		granularity := ""
		if kind != "samples" {
			table, dateExpr, granularity = "traffic_rollups", "h.bucket_start", kind
		}
		idExpr = "json_array(h.device_id," + dateExpr + ")"
		statement = `SELECT ` + idExpr + `, u.username, u.status, ` + dateExpr + `, 0, json_array(h.rx_delta,h.tx_delta,u.reseller_id)
		 FROM ` + table + ` h JOIN devices d ON d.id = h.device_id JOIN users u ON u.id = d.user_id WHERE 1=1`
		if granularity != "" {
			statement = strings.Replace(statement, "h.rx_delta,h.tx_delta", "h.rx,h.tx", 1)
			statement += " AND h.granularity = ?"
			args = append(args, granularity)
		}
		if len(excludedUsers) > 0 {
			statement += " AND u.id NOT IN (" + strings.TrimSuffix(strings.Repeat("?,", len(excludedUsers)), ",") + ")"
			for _, id := range excludedUsers {
				args = append(args, id)
			}
		}
	}
	if f.Owners[0] != "*" {
		clauses := []string{}
		for _, owner := range f.Owners {
			if owner == "node" {
				clauses = append(clauses, "u.reseller_id IS NULL")
			} else {
				clauses = append(clauses, "u.reseller_id = ?")
				args = append(args, owner)
			}
		}
		statement += " AND (" + strings.Join(clauses, " OR ") + ")"
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
		if len(ids) == 0 || len(ids) > batchLimit(kind, f.Kinds) {
			return nil, false, domain.E(domain.CodeInvalidRequest, "invalid cleanup selection")
		}
		statement += " AND " + idExpr + " IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	statement += " ORDER BY " + idExpr + " LIMIT ?"
	args = append(args, batchLimit(kind, f.Kinds)+1)
	rows, err := q.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, false, fmt.Errorf("cleanup: preview: %w", err)
	}
	defer rows.Close()
	out := []Candidate{}
	more := false
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.ID, &c.Name, &c.Status, &c.Date, &c.Devices, &c.Stamp); err != nil {
			return nil, false, err
		}
		if len(out) == batchLimit(kind, f.Kinds) {
			more = true
			break
		}
		c.Kind = kind
		out = append(out, c)
	}
	return out, more, rows.Err()
}

func fingerprint(rows []Candidate) string {
	raw, _ := json.Marshal(rows)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// collect shares the history budget across selected kinds so every chosen
// category is represented. User cascades are excluded from history counts.
func (s *Service) collect(ctx context.Context, q queryer, f Filter, reviewed []selection) (*Preview, error) {
	var err error
	f, err = normalize(f)
	if err != nil {
		return nil, err
	}
	if reviewed != nil && len(reviewed) != len(f.Kinds) {
		return nil, domain.E(domain.CodeInvalidRequest, "invalid cleanup preview groups")
	}
	p := &Preview{Filter: f, Rows: []Candidate{}}
	excluded := []string{}
	for n, kind := range f.Kinds {
		var ids []string
		if reviewed != nil {
			if reviewed[n].Kind != kind {
				return nil, domain.E(domain.CodeInvalidRequest, "invalid cleanup preview groups")
			}
			ids = reviewed[n].IDs
		}
		rows := []Candidate{}
		more := false
		if reviewed == nil || len(ids) > 0 {
			rows, more, err = s.candidates(ctx, q, f, kind, ids, excluded)
			if err != nil {
				return nil, err
			}
		}
		if reviewed != nil && fingerprint(rows) != reviewed[n].Hash {
			return nil, domain.E(domain.CodeInvalidRequest, "cleanup selection changed; preview again")
		}
		p.Groups = append(p.Groups, Group{Kind: kind, Count: len(rows), Limit: batchLimit(kind, f.Kinds), More: more})
		p.More = p.More || more
		for _, row := range rows {
			p.Rows = append(p.Rows, row)
			p.Devices += row.Devices
			if kind == "users" {
				p.Users++
				excluded = append(excluded, row.ID)
			} else {
				p.History++
			}
		}
	}
	return p, nil
}

func (s *Service) Preview(ctx context.Context, f Filter, actor string) (*Preview, error) {
	p, err := s.collect(ctx, s.DB, f, nil)
	if err != nil {
		return nil, err
	}
	if len(p.Rows) == 0 {
		return p, nil
	}
	sealed := sealed{Version: 2, Filter: p.Filter, Actor: actor, Expires: s.Now().UTC().Add(10 * time.Minute)}
	for _, group := range p.Groups {
		selected := selection{Kind: group.Kind, IDs: []string{}}
		rows := []Candidate{}
		for _, row := range p.Rows {
			if row.Kind == group.Kind {
				selected.IDs = append(selected.IDs, row.ID)
				rows = append(rows, row)
			}
		}
		selected.Hash = fingerprint(rows)
		sealed.Groups = append(sealed.Groups, selected)
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
	if json.Unmarshal(raw, &selected) != nil || selected.Version != 2 || selected.Actor != actor || !s.Now().Before(selected.Expires) {
		return nil, domain.E(domain.CodeInvalidRequest, "cleanup preview expired or belongs to another operator")
	}
	var result *Preview
	err = s.DB.WithTx(ctx, func(tx *sql.Tx) error {
		p, err := s.collect(ctx, tx, selected.Filter, selected.Groups)
		if err != nil {
			return err
		}
		for _, c := range p.Rows {
			if c.Kind == "users" {
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
			if c.Kind != "samples" {
				statement = `DELETE FROM traffic_rollups WHERE device_id = ? AND bucket_start = ? AND granularity = ?`
				args = append(args, c.Kind)
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

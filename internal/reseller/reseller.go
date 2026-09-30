// Package reseller owns the node-local reseller registry. Tenant access is
// enabled only after panel, REST, webhook and customer-data paths enforce it.
package reseller

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,31}$`)

type Account struct {
	ID          string
	Slug        string
	DisplayName string
	Permissions []string
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Service struct {
	db  *database.DB
	now func() time.Time
}

func NewService(db *database.DB) *Service { return &Service{db: db, now: time.Now} }

// ValidatePermissions rejects family wildcards so a later registry addition
// cannot silently widen an existing reseller grant.
func ValidatePermissions(scopes []string) ([]string, error) {
	seen := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		if !auth.ResellerGrantable(scope) {
			return nil, domain.E(domain.CodeInvalidRequest, "reseller scope %q is not allowed", scope)
		}
		seen[scope] = true
	}
	out := make([]string, 0, len(seen))
	for scope := range seen {
		out = append(out, scope)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Service) Create(ctx context.Context, slug, displayName string, scopes []string) (*Account, error) {
	slug = strings.TrimSpace(strings.ToLower(slug))
	displayName = strings.TrimSpace(displayName)
	if !slugPattern.MatchString(slug) || len([]rune(displayName)) > 80 {
		return nil, domain.E(domain.CodeInvalidRequest, "invalid reseller slug or display name")
	}
	grants, err := ValidatePermissions(scopes)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	a := &Account{ID: domain.NewID(), Slug: slug, DisplayName: displayName,
		Permissions: grants, Enabled: true, CreatedAt: now, UpdatedAt: now}
	raw, _ := json.Marshal(grants)
	_, err = s.db.ExecContext(ctx, `INSERT INTO resellers
		(id, slug, display_name, permissions, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, ?, ?)`, a.ID, a.Slug, a.DisplayName, string(raw),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: resellers.slug") {
			return nil, domain.E(domain.CodeInvalidRequest, "reseller identifier already exists")
		}
		return nil, fmt.Errorf("reseller: create: %w", err)
	}
	return a, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Account, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, slug, display_name, permissions, enabled,
		created_at, updated_at FROM resellers WHERE id = ?`, id)
	return scanAccount(row)
}

func (s *Service) List(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, display_name, permissions, enabled,
		created_at, updated_at FROM resellers ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("reseller: list: %w", err)
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Service) SetPermissions(ctx context.Context, id string, scopes []string) error {
	grants, err := ValidatePermissions(scopes)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(grants)
	res, err := s.db.ExecContext(ctx, `UPDATE resellers SET permissions = ?, updated_at = ? WHERE id = ?`,
		string(raw), s.now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("reseller: permissions: %w", err)
	}
	return requireUpdated(res)
}

func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) error {
	n := 0
	if enabled {
		n = 1
	}
	res, err := s.db.ExecContext(ctx, `UPDATE resellers SET enabled = ?, updated_at = ? WHERE id = ?`,
		n, s.now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("reseller: enabled: %w", err)
	}
	return requireUpdated(res)
}

// SetTemplates replaces the owner's technical-template assignment atomically. An empty
// selection closes provisioning without altering existing subscriptions.
func (s *Service) SetTemplates(ctx context.Context, id string, templateIDs []string) error {
	if len(templateIDs) > 128 {
		return domain.E(domain.CodeInvalidRequest, "too many reseller templates")
	}
	seen := make(map[string]bool, len(templateIDs))
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		var found int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM resellers WHERE id = ?`, id).Scan(&found); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.E(domain.CodeNotFound, "reseller not found")
			}
			return fmt.Errorf("reseller: template owner: %w", err)
		}
		for _, templateID := range templateIDs {
			if templateID == "" || seen[templateID] {
				return domain.E(domain.CodeInvalidRequest, "invalid or duplicate template assignment")
			}
			seen[templateID] = true
			var enabled int
			err := tx.QueryRowContext(ctx, `SELECT enabled FROM templates WHERE id = ?`, templateID).Scan(&enabled)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return domain.E(domain.CodeInvalidRequest, "template is unavailable")
				}
				return fmt.Errorf("reseller: template lookup: %w", err)
			}
			if enabled != 1 {
				return domain.E(domain.CodeInvalidRequest, "template is unavailable")
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM reseller_template_access WHERE reseller_id = ?`, id); err != nil {
			return fmt.Errorf("reseller: clear templates: %w", err)
		}
		for _, templateID := range templateIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO reseller_template_access (reseller_id, template_id) VALUES (?, ?)`, id, templateID); err != nil {
				return fmt.Errorf("reseller: assign template: %w", err)
			}
		}
		return nil
	})
}

func (s *Service) Templates(ctx context.Context, id string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT template_id FROM reseller_template_access
		WHERE reseller_id = ? ORDER BY template_id`, id)
	if err != nil {
		return nil, fmt.Errorf("reseller: templates: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var planID string
		if err := rows.Scan(&planID); err != nil {
			return nil, fmt.Errorf("reseller: plan scan: %w", err)
		}
		ids = append(ids, planID)
	}
	return ids, rows.Err()
}

func requireUpdated(res sql.Result) error {
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeNotFound, "reseller not found")
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanAccount(row scanner) (*Account, error) {
	var a Account
	var raw, created, updated string
	var enabled int
	if err := row.Scan(&a.ID, &a.Slug, &a.DisplayName, &raw, &enabled, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.E(domain.CodeNotFound, "reseller not found")
		}
		return nil, fmt.Errorf("reseller: scan: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), &a.Permissions); err != nil {
		return nil, fmt.Errorf("reseller: permissions JSON: %w", err)
	}
	a.Enabled = enabled == 1
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &a, nil
}

// Package admin manages panel accounts (owner + admins) — the human layer
// above the token API. The Owner is protected: it cannot be disabled,
// demoted, or deleted, and it cannot remove itself (requirements.md).
package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/reseller"
)

// Admin is a stored panel account.
type Admin struct {
	ID          string
	ResellerID  *string // nil = node-wide operator account
	Username    string
	Role        auth.Role
	Permissions []string
	Enabled     bool
	CreatedAt   time.Time
}

// Service encapsulates account rules.
type Service struct {
	db       *database.DB
	sessions *auth.SessionStore
	now      func() time.Time
}

func NewService(db *database.DB, sessions *auth.SessionStore) *Service {
	return &Service{db: db, sessions: sessions, now: time.Now}
}

// SetLocale stores the panel language preference for one account. Values
// are the i18n catalog locales ('fa' | 'en'); anything else is rejected.
func (s *Service) SetLocale(ctx context.Context, id, locale string) error {
	if locale != "fa" && locale != "en" {
		return domain.E(domain.CodeInvalidRequest, "unsupported locale %q", locale)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE admins SET locale = ?, updated_at = ? WHERE id = ?`,
		locale, s.now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("admin: set locale: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeAdminNotFound, "admin %s not found", id)
	}
	return nil
}

// SetAppearancePreset stores a validated preset ID, or clears the override
// when preset is empty. The web layer validates IDs against its local registry.
func (s *Service) SetAppearancePreset(ctx context.Context, id, preset string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE admins SET appearance_preset = ?, updated_at = ? WHERE id = ?`,
		preset, s.now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("admin: set appearance: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeAdminNotFound, "admin %s not found", id)
	}
	return nil
}

// HasOwner reports whether an owner account exists — the onboarding gate
// (first run shows the setup wizard instead of the login form).
func (s *Service) HasOwner(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins WHERE role = 'owner'`).Scan(&count); err != nil {
		return false, fmt.Errorf("admin: count owners: %w", err)
	}
	return count > 0, nil
}

// BootstrapOwner creates the first owner account; it is a no-op (returns
// created=false) when an owner already exists — onboarding calls this
// exactly once per node.
func (s *Service) BootstrapOwner(ctx context.Context, username, password string) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins WHERE role = 'owner'`).Scan(&count); err != nil {
		return false, fmt.Errorf("admin: count owners: %w", err)
	}
	if count > 0 {
		return false, nil
	}
	if _, err := s.Create(ctx, username, password, auth.RoleOwner, nil); err != nil {
		if domain.CodeOf(err) == domain.CodeOwnerProtected {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Create adds an account. Owner creation is refused once an owner exists.
func (s *Service) Create(ctx context.Context, username, password string, role auth.Role, permissions []string) (*Admin, error) {
	return s.create(ctx, username, password, role, permissions, nil)
}

// CreateForReseller creates a tenant-bound panel identity. The owner chooses
// its initial grants, which may not exceed the reseller's current ceiling.
func (s *Service) CreateForReseller(ctx context.Context, resellerID, username, password string, permissions []string) (*Admin, error) {
	if resellerID == "" {
		return nil, domain.E(domain.CodeInvalidRequest, "reseller ID is required")
	}
	return s.create(ctx, username, password, auth.RoleAdmin, permissions, &resellerID)
}

func (s *Service) create(ctx context.Context, username, password string, role auth.Role, permissions []string, resellerID *string) (*Admin, error) {
	if !role.Valid() {
		return nil, domain.E(domain.CodeInvalidRequest, "invalid role %q", role)
	}
	if resellerID != nil {
		if err := s.validateResellerGrants(ctx, *resellerID, permissions); err != nil {
			return nil, err
		}
	}
	if role == auth.RoleOwner {
		if resellerID != nil {
			return nil, domain.E(domain.CodeInvalidRequest, "reseller cannot be owner")
		}
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins WHERE role = 'owner'`).Scan(&count); err != nil {
			return nil, fmt.Errorf("admin: count owners: %w", err)
		}
		if count > 0 {
			return nil, domain.E(domain.CodeOwnerProtected, "an owner already exists; there is exactly one owner per node")
		}
		permissions = nil
	} else if err := auth.ValidateScopes(permissions); err != nil {
		return nil, domain.E(domain.CodeInvalidRequest, "permissions: %v", err)
	}
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	a := &Admin{
		ID:          domain.NewID(),
		ResellerID:  resellerID,
		Username:    username,
		Role:        role,
		Permissions: permissions,
		Enabled:     true,
		CreatedAt:   s.now().UTC(),
	}
	permsJSON, _ := json.Marshal(a.Permissions)
	if a.Permissions == nil {
		permsJSON = []byte("[]")
	}
	// The predicate and insert execute in one SQLite write statement. Both
	// web bootstrap and direct Create therefore share the same atomic gate,
	// including across independent processes/DB connections.
	res, err := s.db.ExecContext(ctx, `INSERT INTO admins (id, username, password_hash, role, permissions, enabled, created_at, updated_at, reseller_id)
		SELECT ?, ?, ?, ?, ?, 1, ?, ?, ?
		WHERE (? != 'owner' OR NOT EXISTS (SELECT 1 FROM admins WHERE role = 'owner'))
		AND (? IS NULL OR EXISTS (SELECT 1 FROM resellers WHERE id = ? AND enabled = 1))`,
		a.ID, a.Username, hash, string(role), string(permsJSON),
		a.CreatedAt.Format(time.RFC3339Nano), a.CreatedAt.Format(time.RFC3339Nano), resellerID,
		string(role), resellerID, resellerID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.E(domain.CodeAdminExists, "username %q is taken", username)
		}
		return nil, fmt.Errorf("admin: create: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("admin: create result: %w", err)
	} else if n == 0 {
		if resellerID != nil {
			return nil, domain.E(domain.CodeForbidden, "reseller is unavailable")
		}
		return nil, domain.E(domain.CodeOwnerProtected, "an owner already exists; there is exactly one owner per node")
	}
	return a, nil
}

func (s *Service) validateResellerGrants(ctx context.Context, resellerID string, permissions []string) error {
	if _, err := reseller.ValidatePermissions(permissions); err != nil {
		return err
	}
	account, err := reseller.NewService(s.db).Get(ctx, resellerID)
	if err != nil {
		return err
	}
	if !account.Enabled {
		return domain.E(domain.CodeForbidden, "reseller is disabled")
	}
	for _, permission := range permissions {
		if !auth.Allows(account.Permissions, permission) {
			return domain.E(domain.CodeForbidden, "permission exceeds reseller ceiling")
		}
	}
	return nil
}

// Authenticate verifies credentials for login; returns the account (never
// the hash). Disabled accounts fail closed.
func (s *Service) Authenticate(ctx context.Context, username, password string) (*Admin, error) {
	var (
		a               Admin
		hash            string
		role            string
		perms           string
		enabled         int
		resellerID      sql.NullString
		resellerEnabled sql.NullInt64
		createdStr      string
	)
	err := s.db.QueryRowContext(ctx, `SELECT a.id, a.username, a.password_hash, a.role,
		a.permissions, a.enabled, a.created_at, a.reseller_id, r.enabled
		FROM admins a LEFT JOIN resellers r ON r.id = a.reseller_id
		WHERE a.username = ?`, username).
		Scan(&a.ID, &a.Username, &hash, &role, &perms, &enabled, &createdStr,
			&resellerID, &resellerEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		// Burn comparable time to blunt username probing.
		_, _ = auth.VerifyPassword(password, dummyHash)
		return nil, domain.E(domain.CodeCredentialInvalid, "invalid credentials")
	}
	if err != nil {
		return nil, fmt.Errorf("admin: lookup: %w", err)
	}
	ok, err := auth.VerifyPassword(password, hash)
	if err != nil || !ok {
		return nil, domain.E(domain.CodeCredentialInvalid, "invalid credentials")
	}
	if enabled == 0 || (resellerID.Valid && (!resellerEnabled.Valid || resellerEnabled.Int64 != 1)) {
		return nil, domain.E(domain.CodeForbidden, "account disabled")
	}
	a.Role = auth.Role(role)
	if resellerID.Valid {
		a.ResellerID = &resellerID.String
	}
	a.Enabled = true
	_ = json.Unmarshal([]byte(perms), &a.Permissions)
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	return &a, nil
}

// dummyHash has the right shape so the no-such-user path costs the same as
// a failed verification.
var dummyHash = "$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// List returns all accounts (no hashes).
func (s *Service) List(ctx context.Context) ([]Admin, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, role, permissions, enabled, created_at, reseller_id
		FROM admins ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("admin: list: %w", err)
	}
	defer rows.Close()
	var out []Admin
	for rows.Next() {
		var (
			a          Admin
			role       string
			perms      string
			enabled    int
			createdStr string
			resellerID sql.NullString
		)
		if err := rows.Scan(&a.ID, &a.Username, &role, &perms, &enabled, &createdStr, &resellerID); err != nil {
			return nil, fmt.Errorf("admin: scan: %w", err)
		}
		if resellerID.Valid {
			a.ResellerID = &resellerID.String
		}
		a.Role = auth.Role(role)
		a.Enabled = enabled == 1
		_ = json.Unmarshal([]byte(perms), &a.Permissions)
		a.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetPassword rotates an account password (revokes all its sessions).
func (s *Service) SetPassword(ctx context.Context, id, newPassword string) error {
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE admins SET password_hash = ?, updated_at = ? WHERE id = ?`,
		hash, s.now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("admin: set password: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeAdminNotFound, "admin %s not found", id)
	}
	return s.sessions.RevokeAllForAdmin(ctx, id)
}

// SetPermissions updates a non-owner admin's scope grants.
func (s *Service) SetPermissions(ctx context.Context, id string, permissions []string) error {
	if err := auth.ValidateScopes(permissions); err != nil {
		return domain.E(domain.CodeInvalidRequest, "permissions: %v", err)
	}
	a, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if a.Role == auth.RoleOwner {
		return errOwnerProtected()
	}
	if a.ResellerID != nil {
		if err := s.validateResellerGrants(ctx, *a.ResellerID, permissions); err != nil {
			return err
		}
	}
	permsJSON, _ := json.Marshal(permissions)
	_, err = s.db.ExecContext(ctx, `UPDATE admins SET permissions = ?, updated_at = ? WHERE id = ?`,
		string(permsJSON), s.now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("admin: set permissions: %w", err)
	}
	return nil
}

// SetEnabled toggles an account; the owner (and the last enabled owner) is
// protected.
func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) error {
	a, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if a.Role == auth.RoleOwner {
		if !enabled {
			return errOwnerProtected()
		}
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE admins SET enabled = ?, updated_at = ? WHERE id = ?`,
		boolInt(enabled), s.now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return fmt.Errorf("admin: set enabled: %w", err)
	}
	if !enabled {
		return s.sessions.RevokeAllForAdmin(ctx, id)
	}
	return nil
}

// Delete removes a non-owner account.
func (s *Service) Delete(ctx context.Context, id string) error {
	a, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if a.Role == auth.RoleOwner {
		return errOwnerProtected()
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM admins WHERE id = ?`, id); err != nil {
		return fmt.Errorf("admin: delete: %w", err)
	}
	return s.sessions.RevokeAllForAdmin(ctx, id)
}

func (s *Service) get(ctx context.Context, id string) (*Admin, error) {
	var (
		a          Admin
		role       string
		perms      string
		enabled    int
		createdStr string
		resellerID sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `SELECT id, username, role, permissions, enabled, created_at, reseller_id
		FROM admins WHERE id = ?`, id).
		Scan(&a.ID, &a.Username, &role, &perms, &enabled, &createdStr, &resellerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.E(domain.CodeAdminNotFound, "admin %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("admin: get: %w", err)
	}
	a.Role = auth.Role(role)
	if resellerID.Valid {
		a.ResellerID = &resellerID.String
	}
	a.Enabled = enabled == 1
	_ = json.Unmarshal([]byte(perms), &a.Permissions)
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	return &a, nil
}

// Get returns one account without its password hash. Panel target checks use
// this to keep reseller-bound identities out of node-operator workflows.
func (s *Service) Get(ctx context.Context, id string) (*Admin, error) {
	return s.get(ctx, id)
}

// ValidateUsername applies the shared administrator identity policy. The
// installer calls this before sending credentials to the bootstrap command.
func ValidateUsername(u string) error {
	if len(u) < 3 || len(u) > 32 {
		return domain.E(domain.CodeInvalidRequest, "username must be 3-32 chars: letters, digits, '_' or '-'")
	}
	for _, r := range u {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return domain.E(domain.CodeInvalidRequest, "username must be 3-32 chars: letters, digits, '_' or '-'")
		}
	}
	return nil
}

// errOwnerProtected guards the single-owner invariant.
func errOwnerProtected() error {
	return domain.E(domain.CodeOwnerProtected, "the owner account cannot be disabled, demoted, or deleted")
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

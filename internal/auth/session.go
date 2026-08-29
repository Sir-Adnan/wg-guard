package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// Admin is an authenticated panel account (row of `admins`).
type Admin struct {
	ID          string
	Username    string
	Role        Role
	Permissions []string
	Enabled     bool
}

// SessionStore manages admin_sessions: hashed tokens, absolute + idle
// expiry (security.md). Token material is 256-bit; only hashes are stored.
//
// Expiry model: expires_at is the absolute cap. The idle limit is enforced
// against last_seen_at. last_seen is refreshed at most once per minute (a
// write per request buys nothing — the idle check is correct within a
// minute) and never extends expires_at.
type SessionStore struct {
	db *database.DB
	// IdleTTL and AbsoluteTTL are read from settings at wiring time; plain
	// fields here so tests can pin them without a registry.
	IdleTTL     time.Duration
	AbsoluteTTL time.Duration
	now         func() time.Time
}

func NewSessionStore(db *database.DB, idleTTL, absoluteTTL time.Duration) *SessionStore {
	return &SessionStore{db: db, IdleTTL: idleTTL, AbsoluteTTL: absoluteTTL, now: time.Now}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create issues a session for adminID; returns the bearer token (cookie
// value) and expiry. Only the hash is persisted.
func (s *SessionStore) Create(ctx context.Context, adminID, sourceIP string) (token string, expiresAt time.Time, err error) {
	now := s.now().UTC()
	token = domain.NewRandomToken(32)
	exp := now.Add(s.AbsoluteTTL)
	_, err = s.db.ExecContext(ctx, `INSERT INTO admin_sessions
		(id, admin_id, token_hash, created_at, last_seen_at, expires_at, source_ip)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		domain.NewID(), adminID, hashToken(token),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), exp.Format(time.RFC3339Nano), sourceIP)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: create session: %w", err)
	}
	return token, exp, nil
}

// Validate resolves a token to its admin, enforcing enabled/absolute/idle
// expiry and refreshing last_seen (throttled).
func (s *SessionStore) Validate(ctx context.Context, token string) (Admin, error) {
	var a Admin
	var lastSeenStr, expiresStr string
	var enabled int
	var role, permissions string
	err := s.db.QueryRowContext(ctx, `SELECT s.last_seen_at, s.expires_at,
		a.role, a.permissions, a.enabled, a.username, a.id
		FROM admin_sessions s JOIN admins a ON a.id = s.admin_id
		WHERE s.token_hash = ?`, hashToken(token)).
		Scan(&lastSeenStr, &expiresStr, &role, &permissions, &enabled, &a.Username, &a.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return Admin{}, domain.E(domain.CodeSessionExpired, "session not found")
	}
	if err != nil {
		return Admin{}, fmt.Errorf("auth: session lookup: %w", err)
	}

	now := s.now().UTC()
	expires, err := time.Parse(time.RFC3339Nano, expiresStr)
	if err != nil {
		return Admin{}, fmt.Errorf("auth: session expiry: %w", err)
	}
	if now.After(expires) {
		_ = s.revoke(ctx, hashToken(token))
		return Admin{}, domain.E(domain.CodeSessionExpired, "session expired")
	}
	lastSeen, err := time.Parse(time.RFC3339Nano, lastSeenStr)
	if err != nil {
		return Admin{}, fmt.Errorf("auth: session last_seen: %w", err)
	}
	if now.Sub(lastSeen) > s.IdleTTL {
		_ = s.revoke(ctx, hashToken(token))
		return Admin{}, domain.E(domain.CodeSessionExpired, "session idle timeout")
	}
	if enabled == 0 {
		return Admin{}, domain.E(domain.CodeForbidden, "account disabled")
	}

	a.Role = Role(role)
	a.Enabled = true
	if err := json.Unmarshal([]byte(permissions), &a.Permissions); err != nil && permissions != "" {
		return Admin{}, fmt.Errorf("auth: permissions JSON: %w", err)
	}

	if now.Sub(lastSeen) > time.Minute {
		_, _ = s.db.ExecContext(ctx, `UPDATE admin_sessions SET last_seen_at = ?
			WHERE token_hash = ?`, now.Format(time.RFC3339Nano), hashToken(token))
	}
	return a, nil
}

// Revoke removes one session (logout).
func (s *SessionStore) Revoke(ctx context.Context, token string) error {
	return s.revoke(ctx, hashToken(token))
}

func (s *SessionStore) revoke(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE token_hash = ?`, tokenHash)
	if err != nil {
		return fmt.Errorf("auth: revoke: %w", err)
	}
	return nil
}

// RevokeAllForAdmin logs an account out everywhere (password change, disable).
func (s *SessionStore) RevokeAllForAdmin(ctx context.Context, adminID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE admin_id = ?`, adminID)
	if err != nil {
		return fmt.Errorf("auth: revoke all: %w", err)
	}
	return nil
}

// Prune deletes expired sessions (scheduler housekeeping).
func (s *SessionStore) Prune(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires_at < ?`,
		now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("auth: prune: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

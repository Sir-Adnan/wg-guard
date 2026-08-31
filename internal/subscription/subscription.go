// Package subscription manages per-user subscription links: an unguessable
// capability token that grants read-only access to one user's subscription
// page (traffic, expiry, devices, per-device QR/config). The token is a
// 256-bit crypto/rand value, stored AES-GCM encrypted for panel re-display
// and SHA-256 hashed for lookup — plaintext tokens are never persisted in
// the clear and never logged (docs/operations/security.md).
package subscription

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
)

// Link is a stored subscription link. Token is the decrypted capability
// value (empty when decryption fails — the hash lookup path still works).
type Link struct {
	UserID    string
	Token     string
	CreatedAt time.Time
	RotatedAt *time.Time
	RevokedAt *time.Time
}

// Revoked reports whether the link is currently disabled.
func (l *Link) Revoked() bool { return l.RevokedAt != nil }

// Service holds the storage rules. All writes serialize on SQLite's write
// lock; uniqueness of token_hash is enforced by the schema.
type Service struct {
	db   *database.DB
	ring *secrets.KeyRing
	now  func() time.Time
}

func NewService(db *database.DB, ring *secrets.KeyRing) *Service {
	return &Service{db: db, ring: ring, now: time.Now}
}

// NewToken mints a 256-bit URL-safe token (43 chars, RawURLEncoding).
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("subscription: mint token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken is the at-rest lookup form of a token (SHA-256 hex).
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Ensure returns the user's link, creating one if none exists yet (idempotent
// — safe to call from the creation flow and the detail page alike).
func (s *Service) Ensure(ctx context.Context, userID string) (*Link, error) {
	if l, err := s.ForUser(ctx, userID); err != nil || l != nil {
		return l, err
	}
	token, err := NewToken()
	if err != nil {
		return nil, err
	}
	enc, err := s.ring.Encrypt([]byte(token))
	if err != nil {
		return nil, fmt.Errorf("subscription: encrypt token: %w", err)
	}
	now := s.now().UTC()
	_, err = s.db.ExecContext(ctx, `INSERT INTO sub_links
		(user_id, token_encrypted, token_hash, created_at) VALUES (?, ?, ?, ?)`,
		userID, enc, HashToken(token), now.Format(time.RFC3339Nano))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			// A concurrent Ensure won; return the winning row.
			return s.ForUser(ctx, userID)
		}
		return nil, fmt.Errorf("subscription: insert: %w", err)
	}
	return &Link{UserID: userID, Token: token, CreatedAt: now}, nil
}

// ForUser returns the user's link, or (nil, nil) when none exists yet.
func (s *Service) ForUser(ctx context.Context, userID string) (*Link, error) {
	row := s.db.QueryRowContext(ctx, subColumns+` FROM sub_links WHERE user_id = ?`, userID)
	l, err := s.scanLink(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("subscription: get: %w", err)
	}
	return l, nil
}

// ForUsers batch-loads links for a page of users (list view).
func (s *Service) ForUsers(ctx context.Context, userIDs []string) (map[string]*Link, error) {
	out := make(map[string]*Link, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	q := `SELECT user_id, token_encrypted, token_hash, created_at, rotated_at, revoked_at
		FROM sub_links WHERE user_id IN (` + strings.Repeat("?,", len(userIDs))
	q = q[:len(q)-1] + `)`
	args := make([]any, len(userIDs))
	for i, id := range userIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("subscription: batch: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		l, err := s.scanLink(rows)
		if err != nil {
			return nil, fmt.Errorf("subscription: batch scan: %w", err)
		}
		out[l.UserID] = l
	}
	return out, rows.Err()
}

// Regenerate replaces the token (the old one stops working immediately) and
// clears the revoked flag — a fresh link is a working link.
func (s *Service) Regenerate(ctx context.Context, userID string) (*Link, error) {
	token, err := NewToken()
	if err != nil {
		return nil, err
	}
	enc, err := s.ring.Encrypt([]byte(token))
	if err != nil {
		return nil, fmt.Errorf("subscription: encrypt token: %w", err)
	}
	now := s.now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE sub_links SET
		token_encrypted = ?, token_hash = ?, rotated_at = ?, revoked_at = NULL
		WHERE user_id = ?`, enc, HashToken(token), now.Format(time.RFC3339Nano), userID)
	if err != nil {
		return nil, fmt.Errorf("subscription: rotate: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// No link yet: rotate implies minting the first one.
		if _, err := s.Ensure(ctx, userID); err != nil {
			return nil, err
		}
		return s.ForUser(ctx, userID)
	}
	return &Link{UserID: userID, Token: token, CreatedAt: now, RotatedAt: &now}, nil
}

// SetRevoked toggles the revoked flag. Revoked links answer "not found" on
// the public surface but keep their token — restoring re-enables the same URL.
func (s *Service) SetRevoked(ctx context.Context, userID string, revoked bool) (*Link, error) {
	var ts any
	if revoked {
		ts = s.now().UTC().Format(time.RFC3339Nano)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE sub_links SET revoked_at = ? WHERE user_id = ?`, ts, userID)
	if err != nil {
		return nil, fmt.Errorf("subscription: revoke: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, domain.E(domain.CodeInvalidRequest, "no subscription link for user %s", userID)
	}
	return s.ForUser(ctx, userID)
}

// Resolve maps a presented token onto its user. Any failure — unknown hash,
// revoked link, malformed input — is the same error (no enumeration oracle).
// The caller is responsible for loading the user and checking liveness.
func (s *Service) Resolve(ctx context.Context, token string) (string, error) {
	notFound := domain.E(domain.CodeUserNotFound, "subscription link not found")
	if token == "" || len(token) > 128 {
		return "", notFound
	}
	var userID string
	var revoked sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, revoked_at FROM sub_links WHERE token_hash = ?`, HashToken(token)).
		Scan(&userID, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return "", notFound
	}
	if err != nil {
		return "", fmt.Errorf("subscription: resolve: %w", err)
	}
	if revoked.Valid {
		return "", notFound
	}
	return userID, nil
}

const subColumns = `SELECT user_id, token_encrypted, token_hash, created_at, rotated_at, revoked_at`

type rowScanner interface{ Scan(dest ...any) error }

func (s *Service) scanLink(row rowScanner) (*Link, error) {
	var (
		l       Link
		enc     []byte
		created string
		rotated sql.NullString
		revoked sql.NullString
	)
	if err := row.Scan(&l.UserID, &enc, new(string), &created, &rotated, &revoked); err != nil {
		return nil, err
	}
	if pt, err := s.ring.Decrypt(enc); err == nil {
		l.Token = string(pt)
	}
	l.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	l.RotatedAt = parseTime(rotated)
	l.RevokedAt = parseTime(revoked)
	return &l, nil
}

func parseTime(ns sql.NullString) *time.Time {
	if !ns.Valid {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, ns.String)
	if err != nil {
		return nil
	}
	return &t
}

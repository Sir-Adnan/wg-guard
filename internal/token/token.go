// Package token manages REST API tokens: `wg_`-prefixed bearer tokens,
// stored hashed with an indexed lookup prefix, scoped, optionally expiring,
// optionally CIDR-restricted (docs/architecture/api.md, security.md).
package token

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// Prefix is the token scheme marker.
const Prefix = "wg_"

// TokenLengthBytes is the entropy of the random part (256-bit).
const TokenLengthBytes = 32

// lookupLen is the number of characters after "wg_" stored in plaintext in
// `prefix` for indexed lookup (8 chars = 48 bits — enough to find one row,
// never enough to authenticate).
const lookupLen = 8

// Token is a stored API token.
type Token struct {
	ID        string
	Name      string
	Prefix    string
	Scopes    []string
	ExpiresAt *time.Time
	Enabled   bool
	CIDR      []string
	LastUsed  *time.Time
	CreatedAt time.Time
}

// Service creates and verifies API tokens.
type Service struct {
	db  *database.DB
	now func() time.Time
}

func NewService(db *database.DB) *Service {
	return &Service{db: db, now: time.Now}
}

// Create mints a token. The plaintext is returned exactly once and never
// logged; only its SHA-256 and lookup prefix are stored.
func (s *Service) Create(ctx context.Context, name string, scopes []string, expiresAt *time.Time, cidrAllowlist string) (*Token, string, error) {
	if strings.TrimSpace(name) == "" {
		return nil, "", domain.E(domain.CodeInvalidRequest, "token name is required")
	}
	if err := auth.ValidateScopes(scopes); err != nil {
		return nil, "", domain.E(domain.CodeInvalidRequest, "scopes: %v", err)
	}
	if cidrAllowlist != "" {
		if err := ValidateCIDRList(cidrAllowlist); err != nil {
			return nil, "", domain.E(domain.CodeInvalidRequest, "cidr allowlist: %v", err)
		}
	}
	plaintext := Prefix + domain.NewRandomToken(TokenLengthBytes)
	sum := sha256.Sum256([]byte(plaintext))
	prefix := plaintext[:len(Prefix)+lookupLen]
	t := &Token{
		ID:        domain.NewID(),
		Name:      name,
		Prefix:    prefix,
		Scopes:    scopes,
		ExpiresAt: expiresAt,
		Enabled:   true,
		CIDR:      splitCIDR(cidrAllowlist),
		CreatedAt: s.now().UTC(),
	}
	var exp any
	if expiresAt != nil {
		exp = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	scopesJSON, _ := json.Marshal(scopes)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO api_tokens
		(id, name, prefix, token_hash, scopes, expires_at, enabled, cidr_allowlist, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		t.ID, t.Name, prefix, hex.EncodeToString(sum[:]), string(scopesJSON), exp, cidrAllowlist,
		t.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return nil, "", fmt.Errorf("token: create: %w", err)
	}
	return t, plaintext, nil
}

// Verified is the result of a successful verification.
type Verified struct {
	Token Token
}

// Verify resolves plaintext to a token, enforcing hash, enabled, expiry and
// the CIDR allowlist. constant-time compare is inherent: SHA-256 hex lookup
// by prefix then full-hash equality on the fetched row.
func (s *Service) Verify(ctx context.Context, plaintext, remoteIP string) (*Verified, error) {
	if !strings.HasPrefix(plaintext, Prefix) || len(plaintext) != len(Prefix)+43 {
		return nil, domain.E(domain.CodeTokenInvalid, "malformed token")
	}
	prefix := plaintext[:len(Prefix)+lookupLen]
	var (
		t         Token
		hash      string
		scopesRaw string
		cidrRaw   string
		expRaw    sql.NullString
		lastRaw   sql.NullString
		enabled   int
		createdAt string
	)
	err := s.db.QueryRowContext(ctx, `SELECT id, name, prefix, token_hash, scopes, expires_at,
		enabled, cidr_allowlist, last_used_at, created_at
		FROM api_tokens WHERE prefix = ?`, prefix).
		Scan(&t.ID, &t.Name, &t.Prefix, &hash, &scopesRaw, &expRaw, &enabled, &cidrRaw, &lastRaw, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.E(domain.CodeTokenInvalid, "token not found")
	}
	if err != nil {
		return nil, fmt.Errorf("token: verify: %w", err)
	}
	sum := sha256.Sum256([]byte(plaintext))
	if subtleXOR(hash, hex.EncodeToString(sum[:])) {
		return nil, domain.E(domain.CodeTokenInvalid, "token not found")
	}
	if enabled == 0 {
		return nil, domain.E(domain.CodeForbidden, "token revoked")
	}
	if expRaw.Valid {
		exp, err := time.Parse(time.RFC3339Nano, expRaw.String)
		if err != nil {
			return nil, fmt.Errorf("token: expiry: %w", err)
		}
		t.ExpiresAt = &exp
		if s.now().UTC().After(exp) {
			return nil, domain.E(domain.CodeForbidden, "token expired")
		}
	}
	if err := json.Unmarshal([]byte(scopesRaw), &t.Scopes); err != nil {
		return nil, fmt.Errorf("token: scopes: %w", err)
	}
	if err := auth.ValidateScopes(t.Scopes); err != nil {
		// A scope that no longer exists in the registry must not widen
		// access; the token simply fails authorization later.
		t.Scopes = nil
	}
	t.CIDR = splitCIDR(cidrRaw)
	t.Enabled = true
	if createdAt != "" {
		t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	}
	if lastRaw.Valid {
		lu, _ := time.Parse(time.RFC3339Nano, lastRaw.String)
		t.LastUsed = &lu
	}

	if len(t.CIDR) > 0 {
		ip := net.ParseIP(remoteIP)
		if ip == nil || !ipInList(ip, t.CIDR) {
			return nil, domain.E(domain.CodeForbidden, "source IP not in allowlist")
		}
	}

	// last_used refresh, throttled to one write per minute.
	if t.LastUsed == nil || s.now().UTC().Sub(*t.LastUsed) > time.Minute {
		_, _ = s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`,
			s.now().UTC().Format(time.RFC3339Nano), t.ID)
	}
	return &Verified{Token: t}, nil
}

// Authorize checks a required scope against the token's grants.
func (v *Verified) Authorize(required string) bool {
	return auth.Allows(v.Token.Scopes, required)
}

// List returns all tokens without secrets.
func (s *Service) List(ctx context.Context) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, prefix, scopes, expires_at, enabled,
		cidr_allowlist, last_used_at, created_at FROM api_tokens ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("token: list: %w", err)
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var (
			t         Token
			scopesRaw string
			cidrRaw   string
			expRaw    sql.NullString
			lastRaw   sql.NullString
			enabled   int
			createdAt string
		)
		if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &scopesRaw, &expRaw, &enabled,
			&cidrRaw, &lastRaw, &createdAt); err != nil {
			return nil, fmt.Errorf("token: scan: %w", err)
		}
		_ = json.Unmarshal([]byte(scopesRaw), &t.Scopes)
		t.Enabled = enabled == 1
		t.CIDR = splitCIDR(cidrRaw)
		if expRaw.Valid {
			if exp, err := time.Parse(time.RFC3339Nano, expRaw.String); err == nil {
				t.ExpiresAt = &exp
			}
		}
		if lastRaw.Valid {
			if lu, err := time.Parse(time.RFC3339Nano, lastRaw.String); err == nil {
				t.LastUsed = &lu
			}
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		out = append(out, t)
	}
	return out, rows.Err()
}

// Revoke disables a token (audit trail preserved; rows are never deleted —
// revocation is a state change).
func (s *Service) Revoke(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET enabled = 0 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("token: revoke: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeNotFound, "token %s not found", id)
	}
	return nil
}

func splitCIDR(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ValidateCIDRList checks a comma-separated CIDR list.
func ValidateCIDRList(raw string) error {
	list := splitCIDR(raw)
	if len(list) == 0 {
		return fmt.Errorf("empty allowlist")
	}
	for _, c := range list {
		if _, _, err := net.ParseCIDR(c); err != nil {
			return fmt.Errorf("%q is not a valid CIDR", c)
		}
	}
	return nil
}

func ipInList(ip net.IP, list []string) bool {
	for _, c := range list {
		_, network, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// subtleXOR is a constant-time equality check.
func subtleXOR(a, b string) bool {
	if len(a) != len(b) {
		return true
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v != 0
}

// Package integration implements durable, principal-scoped automation
// operations. Results contain identifiers and accounting state, never
// subscription capabilities or device private keys.
package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/accounting"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/subscription"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

const ResultRetention = 90 * 24 * time.Hour

type Service struct {
	DB         *database.DB
	Users      *user.Service
	Devices    *device.Service
	Plans      *plan.Service
	Links      *subscription.Service
	Accounting *accounting.Service
	Now        func() time.Time
}

type PurchaseInput struct {
	Key         string // caller's Idempotency-Key, never persisted as plaintext
	ResellerID  *string
	Username    string
	TemplateID  string
	Entitlement *DirectEntitlement
	DeviceName  string
	Keys        device.KeyMaterial
}

// DirectEntitlement is a complete technical subscription supplied by an
// owner-scoped integration. It is not a sales SKU or a payment instruction.
// Requiring finite quota, duration and device count prevents an omitted JSON
// field from silently creating an unlimited paid subscription.
type DirectEntitlement struct {
	TrafficLimitBytes  int64              `json:"traffic_limit_bytes"`
	DurationSeconds    int64              `json:"duration_seconds"`
	DeviceLimit        int                `json:"device_limit"`
	StartPolicy        domain.StartPolicy `json:"start_policy,omitempty"`
	SpeedLimitDownKbps *int               `json:"speed_limit_down_kbps,omitempty"`
	SpeedLimitUpKbps   *int               `json:"speed_limit_up_kbps,omitempty"`
	InterfaceID        string             `json:"interface_id,omitempty"`
}

func (t *DirectEntitlement) validate() error {
	if t == nil || t.TrafficLimitBytes <= 0 {
		return domain.E(domain.CodeInvalidRequest, "entitlement.traffic_limit_bytes must be positive")
	}
	if t.DurationSeconds <= 0 || t.DurationSeconds > 315360000 {
		return domain.E(domain.CodeInvalidRequest, "entitlement.duration_seconds must be 1-315360000")
	}
	if t.DeviceLimit <= 0 || t.DeviceLimit > 100 {
		return domain.E(domain.CodeInvalidRequest, "entitlement.device_limit must be 1-100")
	}
	if t.SpeedLimitDownKbps != nil && *t.SpeedLimitDownKbps <= 0 ||
		t.SpeedLimitUpKbps != nil && *t.SpeedLimitUpKbps <= 0 {
		return domain.E(domain.CodeInvalidRequest, "entitlement speed limits must be positive")
	}
	if t.StartPolicy != "" && !t.StartPolicy.Valid() {
		return domain.E(domain.CodeInvalidRequest, "entitlement.start_policy must be immediate or first_connection")
	}
	return nil
}

type QuotaTopUpInput struct {
	Key        string // caller's Idempotency-Key, never persisted as plaintext
	ResellerID *string
	UserID     string
	Bytes      int64
}

type Result struct {
	ID         string                    `json:"operation_id"`
	Kind       string                    `json:"kind"`
	State      string                    `json:"state"`
	UserID     string                    `json:"user_id"`
	DeviceID   string                    `json:"device_id,omitempty"`
	TemplateID string                    `json:"template_id,omitempty"`
	Before     *accounting.QuotaSnapshot `json:"before,omitempty"`
	After      *accounting.QuotaSnapshot `json:"after,omitempty"`
	CreatedAt  time.Time                 `json:"created_at"`
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func scopeKey(resellerID *string) string {
	if resellerID == nil {
		return "owner"
	}
	return "reseller:" + *resellerID
}

func hashedKey(scope, key string) string {
	sum := sha256.Sum256([]byte(scope + "\x00" + key))
	return hex.EncodeToString(sum[:])
}

func validKey(key string) bool {
	if len(key) == 0 || len(key) > 128 {
		return false
	}
	for _, c := range key {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

// replayTx is shared by durable integration operations. The key namespace
// survives token rotation but remains isolated by owner/reseller identity.
func (s *Service) replayTx(ctx context.Context, tx *sql.Tx, scope, keyHash, kind, requestHash string) (*Result, bool, error) {
	var savedHash, savedKind, savedJSON, expiry string
	err := tx.QueryRowContext(ctx, `SELECT request_hash, kind, result_json, expires_at
		FROM integration_operations WHERE key_hash = ? AND scope_key = ?`, keyHash, scope).
		Scan(&savedHash, &savedKind, &savedJSON, &expiry)
	if err == nil {
		until, parseErr := time.Parse(time.RFC3339Nano, expiry)
		if parseErr != nil {
			return nil, false, fmt.Errorf("integration: operation expiry: %w", parseErr)
		}
		if until.After(s.now()) {
			if savedKind != kind || savedHash != requestHash {
				return nil, false, domain.E(domain.CodeIdempotencyKeyReused, "key was used with a different operation")
			}
			var prior Result
			if err := json.Unmarshal([]byte(savedJSON), &prior); err != nil {
				return nil, false, fmt.Errorf("integration: operation result: %w", err)
			}
			return &prior, true, nil
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM integration_operations WHERE key_hash = ? AND scope_key = ?`, keyHash, scope); err != nil {
			return nil, false, fmt.Errorf("integration: expired operation: %w", err)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("integration: lookup: %w", err)
	}
	return nil, false, nil
}

func (s *Service) recordResultTx(ctx context.Context, tx *sql.Tx, scope, keyHash, requestHash string, result *Result) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("integration: result encode: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_operations
		(id, scope_key, key_hash, kind, request_hash, result_json, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, result.ID, scope, keyHash, result.Kind,
		requestHash, string(raw), result.CreatedAt.Format(time.RFC3339Nano),
		result.CreatedAt.Add(ResultRetention).Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("integration: record result: %w", err)
	}
	return nil
}

// Purchase inserts the user, initial device, customer link and durable
// result in one SQLite write transaction. A retry after lost HTTP response
// returns the prior result without minting another customer or credential.
func (s *Service) Purchase(ctx context.Context, in PurchaseInput) (*Result, bool, error) {
	in.TemplateID = strings.TrimSpace(in.TemplateID)
	if !validKey(in.Key) || (in.TemplateID == "") == (in.Entitlement == nil) || in.ResellerID != nil && *in.ResellerID == "" {
		return nil, false, domain.E(domain.CodeInvalidRequest, "idempotency key and exactly one of template_id or entitlement are required")
	}
	if in.Entitlement != nil {
		if in.ResellerID != nil {
			return nil, false, domain.E(domain.CodeForbidden, "direct entitlements require an owner-scoped token")
		}
		if err := in.Entitlement.validate(); err != nil {
			return nil, false, err
		}
		normalized := *in.Entitlement
		in.Entitlement = &normalized
		if in.Entitlement.StartPolicy == "" {
			in.Entitlement.StartPolicy = domain.StartImmediate
		}
		in.Entitlement.InterfaceID = strings.TrimSpace(in.Entitlement.InterfaceID)
	}
	scope := scopeKey(in.ResellerID)
	in.Username = strings.TrimSpace(in.Username)
	in.DeviceName = strings.TrimSpace(in.DeviceName)
	if in.Username == "" {
		// Stable across token rotation and retries, without exposing other
		// tenants' chosen usernames to this integration.
		in.Username = "u" + hashedKey(scope, in.Key)[:16]
	}
	if in.DeviceName == "" {
		in.DeviceName = "device-1"
	}
	var canonical []byte
	if in.Entitlement == nil {
		// A template purchase has its own canonical shape; a direct order's
		// entitlement is part of its request fingerprint.
		canonical, _ = json.Marshal(struct {
			Username, TemplateID, DeviceName string
		}{in.Username, in.TemplateID, in.DeviceName})
	} else {
		canonical, _ = json.Marshal(struct {
			Username, DeviceName string
			Entitlement          *DirectEntitlement
		}{in.Username, in.DeviceName, in.Entitlement})
	}
	hashBytes := sha256.Sum256(canonical)
	requestHash := hex.EncodeToString(hashBytes[:])
	keyHash := hashedKey(scope, in.Key)
	var out *Result
	var replayed bool
	err := s.DB.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, replayed, err = s.replayTx(ctx, tx, scope, keyHash, "purchase", requestHash)
		if err != nil || replayed {
			return err
		}
		if in.ResellerID != nil {
			var allowed int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM resellers r
				JOIN reseller_template_access a ON a.reseller_id = r.id
				WHERE r.id = ? AND r.enabled = 1 AND a.template_id = ?`, *in.ResellerID, in.TemplateID).Scan(&allowed)
			if errors.Is(err, sql.ErrNoRows) {
				return domain.E(domain.CodeForbidden, "template is unavailable to this reseller")
			}
			if err != nil {
				return fmt.Errorf("integration: product access: %w", err)
			}
		}
		input, interfaceID, resultTemplateID, err := s.purchaseUserInput(ctx, tx, in)
		if err != nil {
			return err
		}
		u, err := s.Users.CreateTx(ctx, tx, input)
		if err != nil {
			return err
		}
		d, err := s.Devices.CreateTx(ctx, tx, u.ID, in.DeviceName, in.Keys, interfaceID)
		if err != nil {
			return err
		}
		if _, err := s.Links.CreateTx(ctx, tx, u.ID); err != nil {
			return err
		}
		now := s.now()
		result := &Result{ID: domain.NewID(), Kind: "purchase", State: "committed",
			UserID: u.ID, DeviceID: d.ID, TemplateID: resultTemplateID, CreatedAt: now}
		if err := s.recordResultTx(ctx, tx, scope, keyHash, requestHash, result); err != nil {
			return err
		}
		out = result
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, replayed, nil
}

func (s *Service) purchaseUserInput(ctx context.Context, tx *sql.Tx, in PurchaseInput) (user.Input, string, string, error) {
	input := user.Input{Username: in.Username, ResellerID: in.ResellerID}
	if terms := in.Entitlement; terms != nil {
		input.TrafficLimitBytes = domain.OptInt64{Set: true, Value: terms.TrafficLimitBytes}
		input.DurationSeconds = &terms.DurationSeconds
		input.DeviceLimit = domain.OptInt{Set: true, Value: terms.DeviceLimit}
		input.StartPolicy = terms.StartPolicy
		if terms.SpeedLimitDownKbps != nil {
			input.SpeedLimitDownKbps = domain.OptInt{Set: true, Value: *terms.SpeedLimitDownKbps}
		}
		if terms.SpeedLimitUpKbps != nil {
			input.SpeedLimitUpKbps = domain.OptInt{Set: true, Value: *terms.SpeedLimitUpKbps}
		}
		if terms.InterfaceID != "" {
			input.InterfaceID = domain.OptString{Set: true, Value: terms.InterfaceID}
		}
		return input, terms.InterfaceID, "", nil
	}
	product, err := s.Plans.GetTx(ctx, tx, in.TemplateID)
	if err != nil {
		return user.Input{}, "", "", err
	}
	if !product.Enabled {
		return user.Input{}, "", "", domain.E(domain.CodeForbidden, "template is unavailable")
	}
	input = plan.ApplyToUser(product, input)
	interfaceID := ""
	if product.InterfaceID != nil {
		interfaceID = *product.InterfaceID
	}
	return input, interfaceID, product.ID, nil
}

// TopUpQuota atomically increases a finite user's allowance and records a
// non-secret before/after result under the caller's principal namespace.
// Retrying the same key cannot apply the paid entitlement twice.
func (s *Service) TopUpQuota(ctx context.Context, in QuotaTopUpInput) (*Result, bool, error) {
	if !validKey(in.Key) || in.UserID == "" || in.Bytes <= 0 || in.ResellerID != nil && *in.ResellerID == "" {
		return nil, false, domain.E(domain.CodeInvalidRequest, "idempotency key, user and positive bytes are required")
	}
	if s.Accounting == nil {
		return nil, false, fmt.Errorf("integration: accounting unavailable")
	}
	scope := scopeKey(in.ResellerID)
	keyHash := hashedKey(scope, in.Key)
	canonical, _ := json.Marshal(struct {
		UserID string
		Bytes  int64
	}{in.UserID, in.Bytes})
	hash := sha256.Sum256(canonical)
	requestHash := hex.EncodeToString(hash[:])
	var out *Result
	var replayed bool
	err := s.DB.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, replayed, err = s.replayTx(ctx, tx, scope, keyHash, "quota_top_up", requestHash)
		if err != nil || replayed {
			return err
		}
		if in.ResellerID != nil {
			var allowed int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ? AND reseller_id = ?
				AND deleted_at IS NULL`, in.UserID, *in.ResellerID).Scan(&allowed)
			if errors.Is(err, sql.ErrNoRows) {
				return domain.E(domain.CodeUserNotFound, "user not found")
			}
			if err != nil {
				return fmt.Errorf("integration: quota ownership: %w", err)
			}
		}
		change, err := s.Accounting.AddQuotaTx(ctx, tx, in.UserID, in.Bytes)
		if err != nil {
			return err
		}
		now := s.now()
		out = &Result{ID: domain.NewID(), Kind: "quota_top_up", State: "committed",
			UserID: in.UserID, Before: &change.Before, After: &change.After, CreatedAt: now}
		return s.recordResultTx(ctx, tx, scope, keyHash, requestHash, out)
	})
	if err != nil {
		return nil, false, err
	}
	return out, replayed, nil
}

// Lookup resolves a previously committed result within the same owner or
// reseller namespace. The key is never logged or stored in plaintext.
func (s *Service) Lookup(ctx context.Context, resellerID *string, key string) (*Result, error) {
	if !validKey(key) {
		return nil, domain.E(domain.CodeInvalidRequest, "invalid idempotency key")
	}
	var raw, expiry string
	err := s.DB.QueryRowContext(ctx, `SELECT result_json, expires_at FROM integration_operations
		WHERE key_hash = ? AND scope_key = ?`, hashedKey(scopeKey(resellerID), key), scopeKey(resellerID)).Scan(&raw, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.E(domain.CodeNotFound, "operation not found")
	}
	if err != nil {
		return nil, fmt.Errorf("integration: result lookup: %w", err)
	}
	until, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil {
		return nil, fmt.Errorf("integration: result expiry: %w", err)
	}
	if !until.After(s.now()) {
		return nil, domain.E(domain.CodeNotFound, "operation not found")
	}
	var result Result
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("integration: result decode: %w", err)
	}
	return &result, nil
}

// Prune removes a bounded batch so one housekeeping tick cannot monopolize
// the node's single scheduler goroutine.
func Prune(ctx context.Context, db *database.DB, now time.Time) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM integration_operations WHERE id IN
		(SELECT id FROM integration_operations WHERE expires_at <= ? LIMIT 500)`, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("integration: prune: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

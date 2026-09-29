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

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/subscription"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

const ResultRetention = 90 * 24 * time.Hour

type Service struct {
	DB      *database.DB
	Users   *user.Service
	Devices *device.Service
	Plans   *plan.Service
	Links   *subscription.Service
	Now     func() time.Time
}

type PurchaseInput struct {
	Key        string // caller's Idempotency-Key, never persisted as plaintext
	ResellerID *string
	Username   string
	PlanID     string
	DeviceName string
	Keys       device.KeyMaterial
}

type Result struct {
	ID        string    `json:"operation_id"`
	Kind      string    `json:"kind"`
	State     string    `json:"state"`
	UserID    string    `json:"user_id"`
	DeviceID  string    `json:"device_id"`
	PlanID    string    `json:"plan_id"`
	CreatedAt time.Time `json:"created_at"`
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

// Purchase inserts the user, initial device, customer link and durable
// result in one SQLite write transaction. A retry after lost HTTP response
// returns the prior result without minting another customer or credential.
func (s *Service) Purchase(ctx context.Context, in PurchaseInput) (*Result, bool, error) {
	if !validKey(in.Key) || strings.TrimSpace(in.PlanID) == "" || in.ResellerID != nil && *in.ResellerID == "" {
		return nil, false, domain.E(domain.CodeInvalidRequest, "idempotency key and plan are required")
	}
	scope := scopeKey(in.ResellerID)
	in.Username = strings.TrimSpace(in.Username)
	in.PlanID = strings.TrimSpace(in.PlanID)
	in.DeviceName = strings.TrimSpace(in.DeviceName)
	if in.Username == "" {
		// Stable across token rotation and retries, without exposing other
		// tenants' chosen usernames to this integration.
		in.Username = "u" + hashedKey(scope, in.Key)[:16]
	}
	if in.DeviceName == "" {
		in.DeviceName = "device-1"
	}
	canonical, _ := json.Marshal(struct {
		Username, PlanID, DeviceName string
	}{in.Username, in.PlanID, in.DeviceName})
	hashBytes := sha256.Sum256(canonical)
	requestHash := hex.EncodeToString(hashBytes[:])
	keyHash := hashedKey(scope, in.Key)
	var out *Result
	var replayed bool
	err := s.DB.WithTx(ctx, func(tx *sql.Tx) error {
		var savedHash, savedKind, savedJSON, expiry string
		err := tx.QueryRowContext(ctx, `SELECT request_hash, kind, result_json, expires_at
			FROM integration_operations WHERE key_hash = ?`, keyHash).
			Scan(&savedHash, &savedKind, &savedJSON, &expiry)
		if err == nil {
			until, parseErr := time.Parse(time.RFC3339Nano, expiry)
			if parseErr != nil {
				return fmt.Errorf("integration: operation expiry: %w", parseErr)
			}
			if until.After(s.now()) {
				if savedKind != "purchase" || savedHash != requestHash {
					return domain.E(domain.CodeIdempotencyKeyReused, "key was used with a different operation")
				}
				var prior Result
				if err := json.Unmarshal([]byte(savedJSON), &prior); err != nil {
					return fmt.Errorf("integration: operation result: %w", err)
				}
				out, replayed = &prior, true
				return nil
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM integration_operations WHERE key_hash = ?`, keyHash); err != nil {
				return fmt.Errorf("integration: expired operation: %w", err)
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("integration: lookup: %w", err)
		}
		if in.ResellerID != nil {
			var allowed int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM resellers r
				JOIN reseller_plan_access a ON a.reseller_id = r.id
				WHERE r.id = ? AND r.enabled = 1 AND a.plan_id = ?`, *in.ResellerID, in.PlanID).Scan(&allowed)
			if errors.Is(err, sql.ErrNoRows) {
				return domain.E(domain.CodeForbidden, "plan is unavailable to this reseller")
			}
			if err != nil {
				return fmt.Errorf("integration: product access: %w", err)
			}
		}
		product, err := s.Plans.GetTx(ctx, tx, in.PlanID)
		if err != nil {
			return err
		}
		if !product.Enabled {
			return domain.E(domain.CodeForbidden, "plan is unavailable")
		}
		input := user.Input{Username: in.Username, ResellerID: in.ResellerID,
			PlanID:      domain.OptString{Set: true, Value: product.ID},
			StartPolicy: product.StartPolicy, DurationSeconds: product.DurationSeconds}
		if product.TrafficLimitBytes != nil {
			input.TrafficLimitBytes = domain.OptInt64{Set: true, Value: *product.TrafficLimitBytes}
		}
		if product.DeviceLimit != nil {
			input.DeviceLimit = domain.OptInt{Set: true, Value: *product.DeviceLimit}
		}
		if product.SpeedLimitDownKbps != nil {
			input.SpeedLimitDownKbps = domain.OptInt{Set: true, Value: *product.SpeedLimitDownKbps}
		}
		if product.SpeedLimitUpKbps != nil {
			input.SpeedLimitUpKbps = domain.OptInt{Set: true, Value: *product.SpeedLimitUpKbps}
		}
		if product.InterfaceID != nil {
			input.InterfaceID = domain.OptString{Set: true, Value: *product.InterfaceID}
		}
		u, err := s.Users.CreateTx(ctx, tx, input)
		if err != nil {
			return err
		}
		interfaceID := ""
		if product.InterfaceID != nil {
			interfaceID = *product.InterfaceID
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
			UserID: u.ID, DeviceID: d.ID, PlanID: product.ID, CreatedAt: now}
		raw, _ := json.Marshal(result)
		if _, err := tx.ExecContext(ctx, `INSERT INTO integration_operations
			(id, scope_key, key_hash, kind, request_hash, result_json, created_at, expires_at)
			VALUES (?, ?, ?, 'purchase', ?, ?, ?, ?)`, result.ID, scope, keyHash,
			requestHash, string(raw), now.Format(time.RFC3339Nano), now.Add(ResultRetention).Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("integration: record purchase: %w", err)
		}
		out = result
		return nil
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

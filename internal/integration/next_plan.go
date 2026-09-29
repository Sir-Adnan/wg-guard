package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
)

// NextPlanTerms are fixed when the successor is queued. Catalog changes do
// not rewrite an already authorized future entitlement.
type NextPlanTerms struct {
	Name               string  `json:"name"`
	TrafficLimitBytes  *int64  `json:"traffic_limit_bytes"`
	DurationSeconds    *int64  `json:"duration_seconds"`
	DeviceLimit        *int    `json:"device_limit"`
	SpeedLimitDownKbps *int    `json:"speed_limit_down_kbps"`
	SpeedLimitUpKbps   *int    `json:"speed_limit_up_kbps"`
	InterfaceID        *string `json:"interface_id"`
}

type NextPlan struct {
	UserID             string        `json:"user_id"`
	PlanID             string        `json:"plan_id"`
	Terms              NextPlanTerms `json:"terms"`
	CarryUnusedTraffic bool          `json:"carry_unused_traffic"`
	State              string        `json:"state"`
	ReviewReason       string        `json:"review_reason,omitempty"`
	CreatedAt          time.Time     `json:"created_at"`
}

type NextPlanActivation struct {
	ID          string          `json:"activation_id"`
	UserID      string          `json:"user_id"`
	PlanID      string          `json:"plan_id"`
	Trigger     string          `json:"trigger"`
	Before      json.RawMessage `json:"before"`
	After       json.RawMessage `json:"after"`
	ActivatedAt time.Time       `json:"activated_at"`
}

type QueueNextPlanInput struct {
	UserID             string
	PlanID             string
	ResellerID         *string // nil = node owner; non-nil must own customer and plan assignment
	CarryUnusedTraffic bool
}

func termsOf(p *plan.Plan) NextPlanTerms {
	return NextPlanTerms{Name: p.Name, TrafficLimitBytes: p.TrafficLimitBytes,
		DurationSeconds: p.DurationSeconds, DeviceLimit: p.DeviceLimit,
		SpeedLimitDownKbps: p.SpeedLimitDownKbps, SpeedLimitUpKbps: p.SpeedLimitUpKbps,
		InterfaceID: p.InterfaceID}
}

func ownerOfNextPlan(ctx context.Context, tx *sql.Tx, userID string, resellerID *string) (sql.NullString, sql.NullString, sql.NullInt64, sql.NullString, error) {
	var owner, currentPlan, expiry sql.NullString
	var limit sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT reseller_id, plan_id, traffic_limit_bytes, expires_at
		FROM users WHERE id = ? AND deleted_at IS NULL`, userID).Scan(&owner, &currentPlan, &limit, &expiry)
	if errors.Is(err, sql.ErrNoRows) || err == nil && resellerID != nil && (!owner.Valid || owner.String != *resellerID) {
		return owner, currentPlan, limit, expiry, domain.E(domain.CodeUserNotFound, "user not found")
	}
	if err != nil {
		return owner, currentPlan, limit, expiry, fmt.Errorf("integration: next-plan customer: %w", err)
	}
	return owner, currentPlan, limit, expiry, nil
}

// QueueNextPlan reserves exactly one successor. It does not take payment:
// callers must authorize the entitlement before invoking this operation.
func (s *Service) QueueNextPlan(ctx context.Context, in QueueNextPlanInput) (*NextPlan, error) {
	if in.UserID == "" || in.PlanID == "" {
		return nil, domain.E(domain.CodeInvalidRequest, "user and plan are required")
	}
	var out *NextPlan
	err := s.DB.WithTx(ctx, func(tx *sql.Tx) error {
		_, currentPlan, currentLimit, expiry, err := ownerOfNextPlan(ctx, tx, in.UserID, in.ResellerID)
		if err != nil {
			return err
		}
		if !currentLimit.Valid && !expiry.Valid {
			return domain.E(domain.CodeInvalidRequest, "current entitlement has no time or usage boundary")
		}
		if in.ResellerID != nil {
			var ok int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM resellers r JOIN reseller_plan_access a ON a.reseller_id = r.id
				WHERE r.id = ? AND r.enabled = 1 AND a.plan_id = ?`, *in.ResellerID, in.PlanID).Scan(&ok)
			if errors.Is(err, sql.ErrNoRows) {
				return domain.E(domain.CodeForbidden, "plan is unavailable to this reseller")
			}
			if err != nil {
				return fmt.Errorf("integration: next-plan access: %w", err)
			}
		}
		p, err := s.Plans.GetTx(ctx, tx, in.PlanID)
		if err != nil {
			return err
		}
		if !p.Enabled {
			return domain.E(domain.CodeForbidden, "plan is unavailable")
		}
		terms := termsOf(p)
		if terms.DurationSeconds != nil && *terms.DurationSeconds > int64(math.MaxInt64/int64(time.Second)) {
			return domain.E(domain.CodeInvalidRequest, "plan duration is too long")
		}
		if in.CarryUnusedTraffic {
			if !currentLimit.Valid || terms.TrafficLimitBytes == nil || *terms.TrafficLimitBytes > math.MaxInt64-currentLimit.Int64 {
				return domain.E(domain.CodeInvalidRequest, "carry requires finite compatible traffic limits")
			}
		}
		if reason, err := nextPlanDeviceConflict(ctx, tx, in.UserID, terms); err != nil {
			return err
		} else if reason != "" {
			return domain.E(domain.CodeInvalidRequest, "next plan conflicts with current devices: %s", reason)
		}
		raw, err := json.Marshal(terms)
		if err != nil {
			return fmt.Errorf("integration: encode next-plan terms: %w", err)
		}
		now := s.now()
		if _, err := tx.ExecContext(ctx, `INSERT INTO next_plan_queue
			(user_id, plan_id, source_plan_id, terms_json, carry_unused_traffic, state, review_reason, principal_scope, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 'queued', '', ?, ?, ?)
			ON CONFLICT(user_id) DO UPDATE SET plan_id = excluded.plan_id, source_plan_id = excluded.source_plan_id,
			terms_json = excluded.terms_json, carry_unused_traffic = excluded.carry_unused_traffic,
			state = 'queued', review_reason = '', principal_scope = excluded.principal_scope,
			created_at = excluded.created_at, updated_at = excluded.updated_at`,
			in.UserID, in.PlanID, currentPlan, string(raw), boolToInt(in.CarryUnusedTraffic),
			scopeKey(in.ResellerID), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("integration: queue next plan: %w", err)
		}
		out = &NextPlan{UserID: in.UserID, PlanID: in.PlanID, Terms: terms,
			CarryUnusedTraffic: in.CarryUnusedTraffic, State: "queued", CreatedAt: now}
		return nil
	})
	return out, err
}

func nextPlanDeviceConflict(ctx context.Context, tx *sql.Tx, userID string, terms NextPlanTerms) (string, error) {
	if terms.InterfaceID != nil {
		var enabled int
		err := tx.QueryRowContext(ctx, `SELECT enabled FROM tunnel_interfaces WHERE id = ?`, *terms.InterfaceID).Scan(&enabled)
		if errors.Is(err, sql.ErrNoRows) || err == nil && enabled != 1 {
			return "interface_unavailable", nil
		}
		if err != nil {
			return "", fmt.Errorf("integration: next-plan interface: %w", err)
		}
	}
	var count, mismatch int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN ? IS NOT NULL AND interface_id <> ? THEN 1 ELSE 0 END), 0)
		FROM devices WHERE user_id = ?`, terms.InterfaceID, terms.InterfaceID, userID).Scan(&count, &mismatch)
	if err != nil {
		return "", fmt.Errorf("integration: next-plan devices: %w", err)
	}
	if terms.DeviceLimit != nil && count > *terms.DeviceLimit {
		return "device_limit", nil
	}
	if mismatch > 0 {
		return "interface", nil
	}
	return "", nil
}

func (s *Service) NextPlanForUser(ctx context.Context, userID string, resellerID *string) (*NextPlan, error) {
	var out *NextPlan
	err := s.DB.WithTx(ctx, func(tx *sql.Tx) error {
		if _, _, _, _, err := ownerOfNextPlan(ctx, tx, userID, resellerID); err != nil {
			return err
		}
		var raw, created string
		var carry int
		var p NextPlan
		err := tx.QueryRowContext(ctx, `SELECT plan_id, terms_json, carry_unused_traffic, state, review_reason, created_at
			FROM next_plan_queue WHERE user_id = ?`, userID).
			Scan(&p.PlanID, &raw, &carry, &p.State, &p.ReviewReason, &created)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("integration: next-plan read: %w", err)
		}
		if err := json.Unmarshal([]byte(raw), &p.Terms); err != nil {
			return fmt.Errorf("integration: next-plan terms: %w", err)
		}
		p.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return fmt.Errorf("integration: next-plan time: %w", err)
		}
		p.UserID, p.CarryUnusedTraffic = userID, carry == 1
		out = &p
		return nil
	})
	return out, err
}

func (s *Service) CancelNextPlan(ctx context.Context, userID string, resellerID *string) error {
	return s.DB.WithTx(ctx, func(tx *sql.Tx) error {
		if _, _, _, _, err := ownerOfNextPlan(ctx, tx, userID, resellerID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM next_plan_queue WHERE user_id = ?`, userID)
		if err != nil {
			return fmt.Errorf("integration: cancel next plan: %w", err)
		}
		return nil // DELETE is naturally idempotent
	})
}

// NextPlanActivations is a bounded recovery read for automation clients that
// missed an asynchronous activation or webhook. It contains no credentials.
func (s *Service) NextPlanActivations(ctx context.Context, userID string, resellerID *string, limit int) ([]NextPlanActivation, error) {
	if limit < 1 || limit > 100 {
		return nil, domain.E(domain.CodeInvalidRequest, "limit must be 1-100")
	}
	out := make([]NextPlanActivation, 0)
	err := s.DB.WithTx(ctx, func(tx *sql.Tx) error {
		if _, _, _, _, err := ownerOfNextPlan(ctx, tx, userID, resellerID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, plan_id, trigger_kind, previous_json, applied_json, activated_at
			FROM next_plan_activations WHERE user_id = ? ORDER BY activated_at DESC, id DESC LIMIT ?`, userID, limit)
		if err != nil {
			return fmt.Errorf("integration: next-plan activation list: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var a NextPlanActivation
			var before, after, when string
			if err := rows.Scan(&a.ID, &a.PlanID, &a.Trigger, &before, &after, &when); err != nil {
				return fmt.Errorf("integration: next-plan activation scan: %w", err)
			}
			a.UserID, a.Before, a.After = userID, json.RawMessage(before), json.RawMessage(after)
			a.ActivatedAt, err = time.Parse(time.RFC3339Nano, when)
			if err != nil {
				return fmt.Errorf("integration: next-plan activation time: %w", err)
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

// ActivateDueNextPlans runs in the node's existing scheduler after metering
// and before expiry enforcement. At most 50 successors are inspected per tick.
// Each transition, counters, event and history row commit in one transaction.
func (s *Service) ActivateDueNextPlans(ctx context.Context) (int, error) {
	now := s.now()
	rows, err := s.DB.QueryContext(ctx, `SELECT q.user_id FROM next_plan_queue q JOIN users u ON u.id = q.user_id
		WHERE q.state = 'queued' AND u.deleted_at IS NULL AND u.enabled = 1
		AND u.status IN ('active', 'waiting_first_connection', 'expired', 'traffic_exceeded')
		AND ((u.expires_at IS NOT NULL AND u.expires_at <= ?) OR
			(u.traffic_limit_bytes IS NOT NULL AND
			 (u.traffic_used_rx >= u.traffic_limit_bytes OR
			  u.traffic_used_tx >= u.traffic_limit_bytes - u.traffic_used_rx)))
		ORDER BY q.created_at, q.user_id LIMIT 50`, now.Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("integration: due next plans: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("integration: due next plan scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("integration: due next plan scan: %w", err)
	}
	rows.Close()
	activated := 0
	for _, id := range ids {
		var done bool
		if err := s.DB.WithTx(ctx, func(tx *sql.Tx) error {
			var err error
			done, err = s.activateNextPlanTx(ctx, tx, id, now)
			return err
		}); err != nil {
			return activated, err
		}
		if done {
			activated++
		}
	}
	return activated, nil
}

func (s *Service) activateNextPlanTx(ctx context.Context, tx *sql.Tx, userID string, now time.Time) (bool, error) {
	var planID, raw, state string
	var sourcePlan, currentPlan, expiry, currentIface sql.NullString
	var carry, enabled int
	var status string
	var oldLimit sql.NullInt64
	var usedRX, usedTX int64
	err := tx.QueryRowContext(ctx, `SELECT q.plan_id, q.source_plan_id, q.terms_json, q.carry_unused_traffic, q.state,
		u.plan_id, u.expires_at, u.interface_id, u.status, u.enabled, u.traffic_limit_bytes,
		u.traffic_used_rx, u.traffic_used_tx
		FROM next_plan_queue q JOIN users u ON u.id = q.user_id
		WHERE q.user_id = ? AND u.deleted_at IS NULL`, userID).
		Scan(&planID, &sourcePlan, &raw, &carry, &state, &currentPlan, &expiry, &currentIface,
			&status, &enabled, &oldLimit, &usedRX, &usedTX)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("integration: next-plan activation read: %w", err)
	}
	if state != "queued" || enabled != 1 || status == "disabled" || status == "suspended" {
		return false, nil
	}
	trigger := ""
	if oldLimit.Valid && (usedRX >= oldLimit.Int64 || usedTX >= oldLimit.Int64-usedRX) {
		trigger = "quota"
	} else if expiry.Valid {
		when, err := time.Parse(time.RFC3339Nano, expiry.String)
		if err != nil {
			return false, fmt.Errorf("integration: current expiry: %w", err)
		}
		if !when.After(now) {
			trigger = "time"
		}
	}
	if trigger == "" {
		return false, nil
	}
	if sourcePlan != currentPlan {
		return false, markNextPlanReview(ctx, tx, userID, "current_plan_changed", now)
	}
	var terms NextPlanTerms
	if err := json.Unmarshal([]byte(raw), &terms); err != nil {
		return false, fmt.Errorf("integration: decode queued terms: %w", err)
	}
	if reason, err := nextPlanDeviceConflict(ctx, tx, userID, terms); err != nil {
		return false, err
	} else if reason != "" {
		return false, markNextPlanReview(ctx, tx, userID, reason, now)
	}
	var nextLimit any
	if terms.TrafficLimitBytes != nil {
		limit := *terms.TrafficLimitBytes
		if carry == 1 && trigger == "time" && oldLimit.Valid {
			remaining := int64(0)
			if usedRX < oldLimit.Int64 && usedTX < oldLimit.Int64-usedRX {
				remaining = oldLimit.Int64 - usedRX - usedTX
			}
			if remaining > 0 {
				if limit > math.MaxInt64-remaining {
					return false, markNextPlanReview(ctx, tx, userID, "traffic_overflow", now)
				}
				limit += remaining
			}
		}
		nextLimit = limit
	}
	var nextExpiry any
	if terms.DurationSeconds != nil {
		if *terms.DurationSeconds <= 0 || *terms.DurationSeconds > int64(math.MaxInt64/int64(time.Second)) {
			return false, markNextPlanReview(ctx, tx, userID, "invalid_duration", now)
		}
		nextExpiry = now.Add(time.Duration(*terms.DurationSeconds) * time.Second).Format(time.RFC3339Nano)
	}
	var nextIface any = currentIface
	if terms.InterfaceID != nil {
		nextIface = *terms.InterfaceID
	}
	previous, _ := json.Marshal(map[string]any{"plan_id": nullStringValue(currentPlan), "expires_at": nullStringValue(expiry),
		"traffic_limit_bytes": nullInt64Value(oldLimit), "traffic_used_rx": usedRX, "traffic_used_tx": usedTX,
		"status": status})
	if _, err := tx.ExecContext(ctx, `UPDATE users SET plan_id = ?, traffic_limit_bytes = ?,
		traffic_used_rx = 0, traffic_used_tx = 0, duration_seconds = ?, start_policy = 'immediate',
		activated_at = ?, expires_at = ?, device_limit = ?, speed_limit_down_kbps = ?,
		speed_limit_up_kbps = ?, interface_id = ?, status = 'active', disable_reason = NULL,
		updated_at = ? WHERE id = ?`, planID, nextLimit, terms.DurationSeconds,
		now.Format(time.RFC3339Nano), nextExpiry, terms.DeviceLimit,
		terms.SpeedLimitDownKbps, terms.SpeedLimitUpKbps, nextIface,
		now.Format(time.RFC3339Nano), userID); err != nil {
		return false, fmt.Errorf("integration: activate next plan: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET rx_bytes = 0, tx_bytes = 0 WHERE user_id = ?`, userID); err != nil {
		return false, fmt.Errorf("integration: reset next-plan device totals: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM next_plan_queue WHERE user_id = ?`, userID); err != nil {
		return false, fmt.Errorf("integration: clear next plan: %w", err)
	}
	applied, _ := json.Marshal(map[string]any{"plan_id": planID, "expires_at": nextExpiry,
		"traffic_limit_bytes": nextLimit, "traffic_used_rx": 0, "traffic_used_tx": 0,
		"status": "active"})
	if _, err := tx.ExecContext(ctx, `INSERT INTO next_plan_activations
		(id, user_id, plan_id, trigger_kind, previous_json, applied_json, activated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, domain.NewID(), userID, planID, trigger,
		string(previous), string(applied), now.Format(time.RFC3339Nano)); err != nil {
		return false, fmt.Errorf("integration: next-plan history: %w", err)
	}
	if err := nextPlanAuditTx(ctx, tx, "user.next_plan_activated", userID,
		map[string]any{"plan_id": planID, "trigger": trigger}); err != nil {
		return false, err
	}
	if s.Users != nil && s.Users.Recorder != nil {
		if err := s.Users.Recorder.RecordTx(tx, "user.updated", map[string]any{
			"user_id": userID, "plan_id": planID, "next_plan_trigger": trigger,
		}); err != nil {
			return false, err
		}
	}
	return true, nil
}

func markNextPlanReview(ctx context.Context, tx *sql.Tx, userID, reason string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE next_plan_queue SET state = 'needs_review', review_reason = ?, updated_at = ?
		WHERE user_id = ?`, reason, now.Format(time.RFC3339Nano), userID)
	if err != nil {
		return fmt.Errorf("integration: flag next plan for review: %w", err)
	}
	return nextPlanAuditTx(ctx, tx, "user.next_plan_needs_review", userID, map[string]any{"reason": reason})
}

func nextPlanAuditTx(ctx context.Context, tx *sql.Tx, action, userID string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	raw, _ := json.Marshal(metadata)
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_log
		(ts, actor_type, actor_id, action, target, source_ip, request_id, metadata)
		VALUES (?, 'system', '', ?, ?, '', '', ?)`, time.Now().UTC().Format(time.RFC3339Nano),
		action, userID, string(raw))
	if err != nil {
		return fmt.Errorf("integration: next-plan audit: %w", err)
	}
	return nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullStringValue(v sql.NullString) any {
	if v.Valid {
		return v.String
	}
	return nil
}

func nullInt64Value(v sql.NullInt64) any {
	if v.Valid {
		return v.Int64
	}
	return nil
}

// PruneNextPlanHistory keeps one-year transition evidence without letting
// long-lived high-volume nodes accumulate unbounded activation rows.
func PruneNextPlanHistory(ctx context.Context, db *database.DB, now time.Time) (int64, error) {
	cutoff := now.UTC().AddDate(-1, 0, 0).Format(time.RFC3339Nano)
	res, err := db.ExecContext(ctx, `DELETE FROM next_plan_activations WHERE id IN
		(SELECT id FROM next_plan_activations WHERE activated_at < ? LIMIT 500)`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("integration: prune next-plan history: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

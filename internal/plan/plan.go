// Package plan manages reusable subscription presets (quota, duration,
// device limit, speed limit, profile selector). Users do not need a plan;
// API clients may pass limits directly (docs/product/requirements.md).
package plan

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// Plan is a stored preset.
type Plan struct {
	ID                string
	Name              string
	TrafficLimitBytes *int64 // nil = unlimited
	DurationSeconds   *int64
	StartPolicy       domain.StartPolicy
	DeviceLimit       *int
	SpeedLimitKbps    *int
	InterfaceID       *string // profile selector, nil = default profile
	Enabled           bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Input is a create/update request.
type Input struct {
	Name              string
	TrafficLimitBytes *int64
	DurationSeconds   *int64
	StartPolicy       domain.StartPolicy
	DeviceLimit       *int
	SpeedLimitKbps    *int
	InterfaceID       *string
	Enabled           *bool
}

type Service struct {
	db  *database.DB
	now func() time.Time
}

func NewService(db *database.DB) *Service {
	return &Service{db: db, now: time.Now}
}

// Create validates and stores a preset.
func (s *Service) Create(ctx context.Context, in Input) (*Plan, error) {
	if err := validate(in); err != nil {
		return nil, err
	}
	p := &Plan{
		ID:                domain.NewID(),
		Name:              strings.TrimSpace(in.Name),
		TrafficLimitBytes: in.TrafficLimitBytes,
		DurationSeconds:   in.DurationSeconds,
		StartPolicy:       in.StartPolicy,
		DeviceLimit:       in.DeviceLimit,
		SpeedLimitKbps:    in.SpeedLimitKbps,
		InterfaceID:       in.InterfaceID,
		Enabled:           in.Enabled == nil || *in.Enabled,
		CreatedAt:         s.now().UTC(),
		UpdatedAt:         s.now().UTC(),
	}
	if p.StartPolicy == "" {
		p.StartPolicy = domain.StartImmediate
	}
	if err := s.insert(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) insert(ctx context.Context, p *Plan) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO plans
		(id, name, traffic_limit_bytes, duration_seconds, start_policy, device_limit,
		 speed_limit_kbps, interface_id, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, nullI64(p.TrafficLimitBytes), nullI64(p.DurationSeconds),
		string(p.StartPolicy), nullInt(p.DeviceLimit), nullInt(p.SpeedLimitKbps),
		nullText(p.InterfaceID), boolInt(p.Enabled),
		p.CreatedAt.Format(time.RFC3339Nano), p.UpdatedAt.Format(time.RFC3339Nano)); err != nil {
		if isUnique(err) {
			return domain.E(domain.CodeInvalidRequest, "plan name %q is taken", p.Name)
		}
		return fmt.Errorf("plan: insert: %w", err)
	}
	return nil
}

// Update applies an input to an existing plan (partial: nil fields keep the
// current value; name checked for uniqueness).
func (s *Service) Update(ctx context.Context, id string, in Input) (*Plan, error) {
	p, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != "" {
		p.Name = strings.TrimSpace(in.Name)
	}
	if in.TrafficLimitBytes != nil {
		p.TrafficLimitBytes = in.TrafficLimitBytes
	}
	if in.DurationSeconds != nil {
		p.DurationSeconds = in.DurationSeconds
	}
	if in.StartPolicy != "" {
		if !in.StartPolicy.Valid() {
			return nil, domain.E(domain.CodeInvalidRequest, "start_policy must be immediate|first_connection")
		}
		p.StartPolicy = in.StartPolicy
	}
	if in.DeviceLimit != nil {
		p.DeviceLimit = in.DeviceLimit
	}
	if in.SpeedLimitKbps != nil {
		p.SpeedLimitKbps = in.SpeedLimitKbps
	}
	if in.InterfaceID != nil {
		p.InterfaceID = in.InterfaceID
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if err := validate(Input{
		Name: p.Name, TrafficLimitBytes: p.TrafficLimitBytes, DurationSeconds: p.DurationSeconds,
		DeviceLimit: p.DeviceLimit, SpeedLimitKbps: p.SpeedLimitKbps,
	}); err != nil {
		return nil, err
	}
	p.UpdatedAt = s.now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE plans SET name = ?, traffic_limit_bytes = ?,
		duration_seconds = ?, start_policy = ?, device_limit = ?, speed_limit_kbps = ?,
		interface_id = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		p.Name, nullI64(p.TrafficLimitBytes), nullI64(p.DurationSeconds), string(p.StartPolicy),
		nullInt(p.DeviceLimit), nullInt(p.SpeedLimitKbps), nullText(p.InterfaceID),
		boolInt(p.Enabled), p.UpdatedAt.Format(time.RFC3339Nano), p.ID); err != nil {
		if isUnique(err) {
			return nil, domain.E(domain.CodeInvalidRequest, "plan name %q is taken", p.Name)
		}
		return nil, fmt.Errorf("plan: update: %w", err)
	}
	return p, nil
}

// Get loads by ID.
func (s *Service) Get(ctx context.Context, id string) (*Plan, error) {
	row := s.db.QueryRowContext(ctx, planColumns+` FROM plans WHERE id = ?`, id)
	p, err := scanPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.E(domain.CodePlanNotFound, "plan %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("plan: get: %w", err)
	}
	return p, nil
}

// List returns all plans ordered by name.
func (s *Service) List(ctx context.Context) ([]*Plan, error) {
	rows, err := s.db.QueryContext(ctx, planColumns+` FROM plans ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("plan: list: %w", err)
	}
	defer rows.Close()
	var out []*Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, fmt.Errorf("plan: scan: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Delete removes a plan; refused while users still reference it.
func (s *Service) Delete(ctx context.Context, id string) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE plan_id = ? AND deleted_at IS NULL`, id).Scan(&count); err != nil {
		return fmt.Errorf("plan: user count: %w", err)
	}
	if count > 0 {
		return domain.E(domain.CodePlanInUse, "plan is assigned to %d users", count)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM plans WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("plan: delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodePlanNotFound, "plan %s not found", id)
	}
	return nil
}

const planColumns = `SELECT id, name, traffic_limit_bytes, duration_seconds, start_policy,
	device_limit, speed_limit_kbps, interface_id, enabled, created_at, updated_at`

func scanPlan(row rowScanner) (*Plan, error) {
	var (
		p          Plan
		traffic    sql.NullInt64
		duration   sql.NullInt64
		devices    sql.NullInt64
		speed      sql.NullInt64
		ifaceID    sql.NullString
		enabled    int
		createdStr string
		updatedStr string
	)
	if err := row.Scan(&p.ID, &p.Name, &traffic, &duration, &p.StartPolicy,
		&devices, &speed, &ifaceID, &enabled, &createdStr, &updatedStr); err != nil {
		return nil, err
	}
	if traffic.Valid {
		v := traffic.Int64
		p.TrafficLimitBytes = &v
	}
	if duration.Valid {
		v := duration.Int64
		p.DurationSeconds = &v
	}
	if devices.Valid {
		v := int(devices.Int64)
		p.DeviceLimit = &v
	}
	if speed.Valid {
		v := int(speed.Int64)
		p.SpeedLimitKbps = &v
	}
	if ifaceID.Valid {
		v := ifaceID.String
		p.InterfaceID = &v
	}
	p.Enabled = enabled == 1
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
	return &p, nil
}

func validate(in Input) error {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 64 {
		return domain.E(domain.CodeInvalidRequest, "plan name must be 1-64 characters")
	}
	if in.TrafficLimitBytes != nil && *in.TrafficLimitBytes < 0 {
		return domain.E(domain.CodeInvalidRequest, "traffic limit must be non-negative (nil = unlimited)")
	}
	if in.DurationSeconds != nil && *in.DurationSeconds <= 0 {
		return domain.E(domain.CodeInvalidRequest, "duration must be positive (nil = no expiry)")
	}
	if in.DeviceLimit != nil && *in.DeviceLimit <= 0 {
		return domain.E(domain.CodeInvalidRequest, "device limit must be positive (nil = unlimited)")
	}
	if in.SpeedLimitKbps != nil && *in.SpeedLimitKbps <= 0 {
		return domain.E(domain.CodeInvalidRequest, "speed limit must be positive (nil = unlimited)")
	}
	if in.StartPolicy != "" && !in.StartPolicy.Valid() {
		return domain.E(domain.CodeInvalidRequest, "start_policy must be immediate|first_connection")
	}
	return nil
}

type rowScanner interface{ Scan(dest ...any) error }

func nullI64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullText(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

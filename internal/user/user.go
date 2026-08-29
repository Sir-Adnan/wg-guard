// Package user implements the subscription-owner domain service: creation,
// lifecycle (enable/disable/suspend/renew), soft delete/restore, and limit
// bookkeeping. Statuses and disable reasons are the REST API enum contract;
// see docs/product/requirements.md.
package user

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// User is a stored subscription owner.
type User struct {
	ID                string
	Username          string
	DisplayName       string
	Note              string
	Tags              []string
	Status            domain.UserStatus
	DisableReason     *domain.DisableReason
	TrafficLimitBytes *int64 // nil = unlimited
	TrafficUsedRX     int64
	TrafficUsedTX     int64
	SpeedLimitKbps    *int
	DeviceLimit       *int
	PlanID            *string
	InterfaceID       *string // chosen profile; nil = system default profile
	StartPolicy       domain.StartPolicy
	DurationSeconds   *int64
	ActivatedAt       *time.Time
	ExpiresAt         *time.Time
	LastActivityAt    *time.Time
	Enabled           bool
	Metadata          map[string]any
	DeletedAt         *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,32}$`)

// Input is a create/update request. For updates, nil pointers keep values.
type Input struct {
	Username          string
	DisplayName       *string
	Note              *string
	Tags              []string
	TrafficLimitBytes *int64 // nil = unlimited
	SpeedLimitKbps    *int
	DeviceLimit       *int
	PlanID            *string
	InterfaceID       *string
	StartPolicy       domain.StartPolicy
	DurationSeconds   *int64
	Enabled           *bool
	Metadata          map[string]any
}

// Service holds the business rules.
type Service struct {
	db  *database.DB
	now func() time.Time
}

func NewService(db *database.DB) *Service {
	return &Service{db: db, now: time.Now}
}

// Create provisions a user. With start_policy=immediate and a duration the
// subscription activates now and expires now+duration; with
// first_connection it waits for the first handshake (Phase 3 marks it).
func (s *Service) Create(ctx context.Context, in Input) (*User, error) {
	if !usernameRe.MatchString(in.Username) {
		return nil, domain.E(domain.CodeInvalidRequest, "username must be 3-32 chars: letters, digits, '_' or '-'")
	}
	if err := validateLimits(in); err != nil {
		return nil, err
	}
	u := &User{
		ID:                domain.NewID(),
		Username:          in.Username,
		TrafficLimitBytes: in.TrafficLimitBytes,
		SpeedLimitKbps:    in.SpeedLimitKbps,
		DeviceLimit:       in.DeviceLimit,
		PlanID:            in.PlanID,
		InterfaceID:       in.InterfaceID,
		StartPolicy:       in.StartPolicy,
		DurationSeconds:   in.DurationSeconds,
		Enabled:           true,
		Tags:              in.Tags,
		Metadata:          in.Metadata,
		CreatedAt:         s.now().UTC(),
	}
	if u.StartPolicy == "" {
		u.StartPolicy = domain.StartImmediate
	}
	u.DisplayName = ""
	if in.DisplayName != nil {
		u.DisplayName = *in.DisplayName
	}
	if in.Note != nil {
		u.Note = *in.Note
	}

	now := s.now().UTC()
	switch {
	case u.StartPolicy == domain.StartImmediate && u.DurationSeconds != nil:
		u.Status = domain.UserActive
		u.ActivatedAt = &now
		exp := now.Add(time.Duration(*u.DurationSeconds) * time.Second)
		u.ExpiresAt = &exp
	case u.StartPolicy == domain.StartImmediate:
		u.Status = domain.UserActive
		u.ActivatedAt = &now
	default:
		u.Status = domain.UserWaitingFirstConnection
	}

	if err := s.insert(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) insert(ctx context.Context, u *User) error {
	tagsJSON := encodeTags(u.Tags)
	metaJSON := encodeMeta(u.Metadata)
	var disableReason any
	if u.DisableReason != nil {
		disableReason = string(*u.DisableReason)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO users
		(id, username, display_name, note, tags, status, disable_reason, traffic_limit_bytes,
		 speed_limit_kbps, device_limit, plan_id, interface_id, start_policy, duration_seconds,
		 activated_at, expires_at, enabled, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, u.Note, tagsJSON, string(u.Status), disableReason,
		nullI64(u.TrafficLimitBytes), nullInt(u.SpeedLimitKbps), nullInt(u.DeviceLimit),
		nullText(u.PlanID), nullText(u.InterfaceID), string(u.StartPolicy), nullI64(u.DurationSeconds),
		nullTime(u.ActivatedAt), nullTime(u.ExpiresAt), boolInt(u.Enabled), metaJSON,
		u.CreatedAt.Format(time.RFC3339Nano), u.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return domain.E(domain.CodeUsernameExists, "username %q already exists", u.Username)
		}
		return fmt.Errorf("user: insert: %w", err)
	}
	return nil
}

// Get loads a live (non-deleted) user by ID.
func (s *Service) Get(ctx context.Context, id string) (*User, error) {
	u, err := s.getRaw(ctx, id)
	if err != nil {
		return nil, err
	}
	if u.DeletedAt != nil {
		return nil, domain.E(domain.CodeUserNotFound, "user %s not found", id)
	}
	return u, nil
}

// GetByUsername loads a live user by username.
func (s *Service) GetByUsername(ctx context.Context, username string) (*User, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ? AND deleted_at IS NULL`, username).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.E(domain.CodeUserNotFound, "user %q not found", username)
	}
	if err != nil {
		return nil, fmt.Errorf("user: lookup: %w", err)
	}
	return s.Get(ctx, id)
}

// List returns live users, newest first, capped at limit (cursor pagination
// arrives with the API layer in Phase 4).
func (s *Service) List(ctx context.Context, limit int) ([]*User, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, userColumns+` FROM users WHERE deleted_at IS NULL
		ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("user: list: %w", err)
	}
	defer rows.Close()
	return scanUsers(rows)
}

// Update applies partial changes.
func (s *Service) Update(ctx context.Context, id string, in Input) (*User, error) {
	u, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.DisplayName != nil {
		u.DisplayName = *in.DisplayName
	}
	if in.Note != nil {
		u.Note = *in.Note
	}
	if in.Tags != nil {
		u.Tags = in.Tags
	}
	if in.TrafficLimitBytes != nil {
		u.TrafficLimitBytes = in.TrafficLimitBytes
	}
	if in.SpeedLimitKbps != nil {
		u.SpeedLimitKbps = in.SpeedLimitKbps
	}
	if in.DeviceLimit != nil {
		u.DeviceLimit = in.DeviceLimit
	}
	if in.PlanID != nil {
		u.PlanID = in.PlanID
	}
	if in.InterfaceID != nil {
		u.InterfaceID = in.InterfaceID
	}
	if in.DurationSeconds != nil {
		u.DurationSeconds = in.DurationSeconds
	}
	if in.Enabled != nil {
		u.Enabled = *in.Enabled
	}
	if in.Metadata != nil {
		u.Metadata = in.Metadata
	}
	if err := validateLimits(in); err != nil {
		return nil, err
	}
	if err := s.save(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// SetStatus performs a guarded lifecycle transition.
func (s *Service) SetStatus(ctx context.Context, id string, status domain.UserStatus, reason domain.DisableReason) (*User, error) {
	u, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !status.Valid() {
		return nil, domain.E(domain.CodeInvalidRequest, "invalid status %q", status)
	}
	needsReason := status == domain.UserDisabled || status == domain.UserSuspended
	if needsReason && !reason.Valid() {
		return nil, domain.E(domain.CodeInvalidRequest, "status %q requires a disable reason", status)
	}
	u.Status = status
	if needsReason {
		u.DisableReason = &reason
	} else {
		u.DisableReason = nil
	}
	// Recoverable transitions restore active/waiting state correctly.
	if status == domain.UserActive && u.ActivatedAt == nil && u.StartPolicy == domain.StartFirstConnection {
		u.Status = domain.UserWaitingFirstConnection
	}
	if err := s.save(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// Renew extends the subscription. Mode: from the current expiration (at
// least now), from now, or an exact date (only for activated users).
func (s *Service) Renew(ctx context.Context, id string, mode string, duration *int64, exact *time.Time) (*User, error) {
	u, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	dur := u.DurationSeconds
	if duration != nil {
		if *duration <= 0 {
			return nil, domain.E(domain.CodeInvalidRequest, "duration must be positive")
		}
		dur = duration
	}
	switch mode {
	case "from_expiration":
		if dur == nil {
			return nil, domain.E(domain.CodeInvalidRequest, "user has no duration; pass one or set an exact date")
		}
		base := now
		if u.ExpiresAt != nil && u.ExpiresAt.After(now) {
			base = *u.ExpiresAt
		}
		exp := base.Add(time.Duration(*dur) * time.Second)
		u.ExpiresAt = &exp
	case "from_now":
		if dur == nil {
			return nil, domain.E(domain.CodeInvalidRequest, "user has no duration; pass one or set an exact date")
		}
		exp := now.Add(time.Duration(*dur) * time.Second)
		u.ExpiresAt = &exp
	case "exact":
		if exact == nil {
			return nil, domain.E(domain.CodeInvalidRequest, "exact mode requires a date")
		}
		u.ExpiresAt = exact
	default:
		return nil, domain.E(domain.CodeInvalidRequest, "renew mode must be from_expiration|from_now|exact")
	}

	// Renewal reactivates expired/traffic-exceeded accounts (quota state is
	// reset only by an explicit traffic reset — renewal alone keeps usage).
	if u.Status == domain.UserExpired {
		u.Status = domain.UserActive
		u.DisableReason = nil
	}
	if err := s.save(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// MarkActivated stamps activated_at/expires_at for first_connection users at
// their first valid handshake (idempotent; Phase 3 calls it). Returns false
// when the user was already activated.
func (s *Service) MarkActivated(ctx context.Context, id string) (bool, error) {
	u, err := s.Get(ctx, id)
	if err != nil {
		return false, err
	}
	if u.ActivatedAt != nil {
		return false, nil
	}
	now := s.now().UTC()
	u.ActivatedAt = &now
	u.Status = domain.UserActive
	if u.DurationSeconds != nil {
		exp := now.Add(time.Duration(*u.DurationSeconds) * time.Second)
		u.ExpiresAt = &exp
	}
	if err := s.save(ctx, u); err != nil {
		return false, err
	}
	return true, nil
}

// SoftDelete marks the user deleted (devices keep existing until purge —
// restore is possible); usernames remain reserved.
func (s *Service) SoftDelete(ctx context.Context, id string) error {
	u, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	u.DeletedAt = &now
	u.Enabled = false
	return s.save(ctx, u)
}

// Restore reverses a soft delete; the account returns to active.
func (s *Service) Restore(ctx context.Context, id string) (*User, error) {
	u, err := s.getRaw(ctx, id)
	if err != nil {
		return nil, err
	}
	if u.DeletedAt == nil {
		return nil, domain.E(domain.CodeInvalidRequest, "user %s is not deleted", id)
	}
	u.DeletedAt = nil
	u.Status = domain.UserActive
	u.DisableReason = nil
	u.Enabled = true
	if err := s.save(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// GetRaw returns the row even when deleted (restore path).
func (s *Service) getRaw(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRowContext(ctx, userColumns+` FROM users WHERE id = ?`, id)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.E(domain.CodeUserNotFound, "user %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("user: get: %w", err)
	}
	return u, nil
}

func (s *Service) save(ctx context.Context, u *User) error {
	u.UpdatedAt = s.now().UTC()
	var disableReason any
	if u.DisableReason != nil {
		disableReason = string(*u.DisableReason)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET
		display_name = ?, note = ?, tags = ?, status = ?, disable_reason = ?,
		traffic_limit_bytes = ?, speed_limit_kbps = ?, device_limit = ?, plan_id = ?,
		interface_id = ?, start_policy = ?, duration_seconds = ?, activated_at = ?,
		expires_at = ?, enabled = ?, metadata = ?, deleted_at = ?, updated_at = ?
		WHERE id = ?`,
		u.DisplayName, u.Note, encodeTags(u.Tags), string(u.Status), disableReason,
		nullI64(u.TrafficLimitBytes), nullInt(u.SpeedLimitKbps), nullInt(u.DeviceLimit),
		nullText(u.PlanID), nullText(u.InterfaceID), string(u.StartPolicy), nullI64(u.DurationSeconds),
		nullTime(u.ActivatedAt), nullTime(u.ExpiresAt), boolInt(u.Enabled),
		encodeMeta(u.Metadata), nullTime(u.DeletedAt), u.UpdatedAt.Format(time.RFC3339Nano), u.ID)
	if err != nil {
		return fmt.Errorf("user: save: %w", err)
	}
	return nil
}

const userColumns = `SELECT id, username, display_name, note, tags, status, disable_reason,
	traffic_limit_bytes, traffic_used_rx, traffic_used_tx, speed_limit_kbps, device_limit,
	plan_id, interface_id, start_policy, duration_seconds, activated_at, expires_at,
	last_activity_at, enabled, metadata, deleted_at, created_at, updated_at`

func scanUser(row rowScanner) (*User, error) {
	var (
		u            User
		displayName  sql.NullString
		note         sql.NullString
		tags         string
		status       string
		reason       sql.NullString
		traffic      sql.NullInt64
		speed        sql.NullInt64
		devices      sql.NullInt64
		planID       sql.NullString
		ifaceID      sql.NullString
		policy       string
		duration     sql.NullInt64
		activated    sql.NullString
		expires      sql.NullString
		lastActivity sql.NullString
		enabled      int
		meta         string
		deleted      sql.NullString
		createdStr   string
		updatedStr   string
	)
	if err := row.Scan(&u.ID, &u.Username, &displayName, &note, &tags, &status, &reason,
		&traffic, &u.TrafficUsedRX, &u.TrafficUsedTX, &speed, &devices,
		&planID, &ifaceID, &policy, &duration, &activated, &expires,
		&lastActivity, &enabled, &meta, &deleted, &createdStr, &updatedStr); err != nil {
		return nil, err
	}
	u.DisplayName = displayName.String
	u.Note = note.String
	u.Tags = decodeTags(tags)
	u.Status = domain.UserStatus(status)
	if reason.Valid {
		r := domain.DisableReason(reason.String)
		u.DisableReason = &r
	}
	if traffic.Valid {
		v := traffic.Int64
		u.TrafficLimitBytes = &v
	}
	if speed.Valid {
		v := int(speed.Int64)
		u.SpeedLimitKbps = &v
	}
	if devices.Valid {
		v := int(devices.Int64)
		u.DeviceLimit = &v
	}
	if planID.Valid {
		v := planID.String
		u.PlanID = &v
	}
	if ifaceID.Valid {
		v := ifaceID.String
		u.InterfaceID = &v
	}
	u.StartPolicy = domain.StartPolicy(policy)
	if duration.Valid {
		v := duration.Int64
		u.DurationSeconds = &v
	}
	u.ActivatedAt = parseTimePtr(activated)
	u.ExpiresAt = parseTimePtr(expires)
	u.LastActivityAt = parseTimePtr(lastActivity)
	u.Enabled = enabled == 1
	u.Metadata = decodeMeta(meta)
	u.DeletedAt = parseTimePtr(deleted)
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	u.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
	return &u, nil
}

func scanUsers(rows *sql.Rows) ([]*User, error) {
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("user: scan: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func validateLimits(in Input) error {
	if in.TrafficLimitBytes != nil && *in.TrafficLimitBytes < 0 {
		return domain.E(domain.CodeInvalidRequest, "traffic limit must be non-negative (nil = unlimited)")
	}
	if in.SpeedLimitKbps != nil && *in.SpeedLimitKbps <= 0 {
		return domain.E(domain.CodeInvalidRequest, "speed limit must be positive (nil = unlimited)")
	}
	if in.DeviceLimit != nil && *in.DeviceLimit <= 0 {
		return domain.E(domain.CodeInvalidRequest, "device limit must be positive (nil = unlimited)")
	}
	if in.DurationSeconds != nil && *in.DurationSeconds <= 0 {
		return domain.E(domain.CodeInvalidRequest, "duration must be positive")
	}
	if in.StartPolicy != "" && !in.StartPolicy.Valid() {
		return domain.E(domain.CodeInvalidRequest, "start_policy must be immediate|first_connection")
	}
	return nil
}

func encodeTags(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(tags)
	return string(b)
}

func decodeTags(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []string
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func encodeMeta(m map[string]any) string {
	if len(m) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func decodeMeta(raw string) map[string]any {
	if raw == "" || raw == "{}" {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func parseTimePtr(ns sql.NullString) *time.Time {
	if !ns.Valid {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, ns.String)
	if err != nil {
		return nil
	}
	return &t
}

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

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type rowScanner interface{ Scan(dest ...any) error }

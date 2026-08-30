// Package user implements the subscription-owner domain service: creation
// (single + bulk), lifecycle (enable/disable/suspend/renew), soft
// delete/restore, limit bookkeeping, and cursor-paginated listing. Statuses
// and disable reasons are the REST API enum contract; see
// docs/product/requirements.md.
//
// Speed limits are independent per direction (Phase 4): download
// (server→client, tc egress) and upload (client→server, tc ingress via IFB).
// nil = unlimited; the API expresses "clear to unlimited" with JSON null
// (domain.OptInt), never with 0.
package user

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// User is a stored subscription owner.
type User struct {
	ID                 string
	Username           string
	DisplayName        string
	Note               string
	Tags               []string
	Status             domain.UserStatus
	DisableReason      *domain.DisableReason
	TrafficLimitBytes  *int64 // nil = unlimited
	TrafficUsedRX      int64
	TrafficUsedTX      int64
	SpeedLimitDownKbps *int // nil = unlimited (server→client)
	SpeedLimitUpKbps   *int // nil = unlimited (client→server)
	DeviceLimit        *int
	PlanID             *string
	InterfaceID        *string // chosen profile; nil = system default profile
	StartPolicy        domain.StartPolicy
	DurationSeconds    *int64
	ActivatedAt        *time.Time
	ExpiresAt          *time.Time
	LastActivityAt     *time.Time
	Enabled            bool
	Metadata           map[string]any
	DeletedAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,32}$`)

// prefixRe is the charset-only check for bulk prefixes — a prefix may be
// shorter than a full username ("gs" + "001" is a valid generated name).
var prefixRe = regexp.MustCompile(`^[a-zA-Z0-9_-]*$`)

// Input is a create/update request. Limits are domain.Opt values so PATCH
// can distinguish "absent" (keep) from "null" (clear to unlimited).
type Input struct {
	Username           string
	DisplayName        *string
	Note               *string
	Tags               []string
	TrafficLimitBytes  domain.OptInt64
	SpeedLimitDownKbps domain.OptInt
	SpeedLimitUpKbps   domain.OptInt
	DeviceLimit        domain.OptInt
	PlanID             domain.OptString
	InterfaceID        domain.OptString
	StartPolicy        domain.StartPolicy
	DurationSeconds    *int64
	Enabled            *bool
	Metadata           map[string]any
}

// EventRecorder is the webhook surface the service needs (satisfied by
// *webhook.Recorder). Events are recorded inside the caller's transaction so
// an event can never disappear while its state change survives (webhooks.md).
// Nil = no event emission (unit tests).
type EventRecorder interface {
	RecordTx(tx *sql.Tx, eventType string, data map[string]any) error
}

// Service holds the business rules.
type Service struct {
	db       *database.DB
	now      func() time.Time
	Recorder EventRecorder
}

func NewService(db *database.DB) *Service {
	return &Service{db: db, now: time.Now}
}

// Create provisions a user. With start_policy=immediate and a duration the
// subscription activates now and expires now+duration; with
// first_connection it waits for the first handshake (the accounting cycle
// marks it). The insert and its webhook event share one transaction.
func (s *Service) Create(ctx context.Context, in Input) (*User, error) {
	if !usernameRe.MatchString(in.Username) {
		return nil, domain.E(domain.CodeInvalidRequest, "username must be 3-32 chars: letters, digits, '_' or '-'")
	}
	if err := validateLimits(in); err != nil {
		return nil, err
	}
	u := s.buildCreate(in)
	if err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.insert(ctx, tx, u); err != nil {
			return err
		}
		if s.Recorder != nil {
			return s.Recorder.RecordTx(tx, "user.created", map[string]any{
				"user_id": u.ID, "username": u.Username, "status": string(u.Status),
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) buildCreate(in Input) *User {
	u := &User{
		ID:              domain.NewID(),
		Username:        in.Username,
		DeviceLimit:     in.DeviceLimit.Resolve(nil),
		PlanID:          resolveString(in.PlanID, nil),
		InterfaceID:     resolveString(in.InterfaceID, nil),
		StartPolicy:     in.StartPolicy,
		DurationSeconds: in.DurationSeconds,
		Enabled:         true,
		Tags:            in.Tags,
		Metadata:        in.Metadata,
		CreatedAt:       s.now().UTC(),
	}
	if u.StartPolicy == "" {
		u.StartPolicy = domain.StartImmediate
	}
	// On create, an absent limit and an explicit null are the same: unlimited.
	if in.TrafficLimitBytes.Set && !in.TrafficLimitBytes.Null {
		v := in.TrafficLimitBytes.Value
		u.TrafficLimitBytes = &v
	}
	if in.SpeedLimitDownKbps.Set && !in.SpeedLimitDownKbps.Null {
		v := in.SpeedLimitDownKbps.Value
		u.SpeedLimitDownKbps = &v
	}
	if in.SpeedLimitUpKbps.Set && !in.SpeedLimitUpKbps.Null {
		v := in.SpeedLimitUpKbps.Value
		u.SpeedLimitUpKbps = &v
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
	return u
}

func (s *Service) insert(ctx context.Context, tx *sql.Tx, u *User) error {
	tagsJSON := encodeTags(u.Tags)
	metaJSON := encodeMeta(u.Metadata)
	var disableReason any
	if u.DisableReason != nil {
		disableReason = string(*u.DisableReason)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO users
		(id, username, display_name, note, tags, status, disable_reason, traffic_limit_bytes,
		 speed_limit_down_kbps, speed_limit_up_kbps, device_limit, plan_id, interface_id, start_policy,
		 duration_seconds, activated_at, expires_at, enabled, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, u.Note, tagsJSON, string(u.Status), disableReason,
		nullI64(u.TrafficLimitBytes), nullInt(u.SpeedLimitDownKbps), nullInt(u.SpeedLimitUpKbps),
		nullInt(u.DeviceLimit),
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

// BulkCreateResult reports one bulk creation.
type BulkCreateResult struct {
	Users   []*User
	Skipped int // indexes whose generated username was already taken
}

// CreateBulk provisions `count` users sharing one configuration inside a
// single transaction: `prefix` + zero-padded sequential index (gs-001…),
// skipping indexes whose username is taken (usernames stay reserved after
// soft delete). Atomic: either every created user exists or none do.
func (s *Service) CreateBulk(ctx context.Context, prefix string, count, startIndex, width int, in Input) (*BulkCreateResult, error) {
	if count < 1 || count > 500 {
		return nil, domain.E(domain.CodeInvalidRequest, "count must be 1-500")
	}
	if startIndex < 0 {
		return nil, domain.E(domain.CodeInvalidRequest, "start_index must be >= 0")
	}
	if width < 1 {
		width = 3
	}
	if width > 10 {
		return nil, domain.E(domain.CodeInvalidRequest, "width must be <= 10")
	}
	prefix = strings.TrimSpace(prefix)
	if !prefixRe.MatchString(prefix) {
		return nil, domain.E(domain.CodeInvalidRequest, "prefix must match the username charset (letters, digits, '_' or '-')")
	}
	last := startIndex + count - 1
	if len(prefix)+len(strconv.Itoa(last))+1 > 32 || len(prefix)+width > 32 {
		return nil, domain.E(domain.CodeInvalidRequest, "generated usernames would exceed 32 characters")
	}
	if err := validateLimits(in); err != nil {
		return nil, err
	}

	res := &BulkCreateResult{}
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		for i := startIndex; i <= last; i++ {
			u := s.buildCreate(in)
			u.Username = fmt.Sprintf("%s%0*d", prefix, width, i)
			if !usernameRe.MatchString(u.Username) {
				res.Skipped++
				continue
			}
			err := s.insert(ctx, tx, u)
			if err != nil {
				if domain.CodeOf(err) == domain.CodeUsernameExists {
					res.Skipped++
					continue // reserved username (e.g. after a soft delete)
				}
				return err
			}
			res.Users = append(res.Users, u)
			if s.Recorder != nil {
				if err := s.Recorder.RecordTx(tx, "user.created", map[string]any{
					"user_id": u.ID, "username": u.Username, "status": string(u.Status),
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ListQuery is the cursor-paginated user list request (api.md: limit ≤ 500,
// opaque cursor, deterministic sort). Zero value = 50 items. The handler maps
// defaults: sort created_at/desc, username/asc, expires_at/asc, used/desc.
type ListQuery struct {
	Filter ListFilter
	Sort   ListSort // default SortCreatedAt
	Desc   bool
	Limit  int    // clamped 1..500, default 50
	Cursor string // opaque, base64url
}

// ListFilter mirrors the SPEC §22 filter set.
type ListFilter struct {
	Username        *string // substring, case-insensitive
	Status          *domain.UserStatus
	TrafficExceeded *bool // true → status=traffic_exceeded
	Enabled         *bool
	ExpiresBefore   *time.Time
	ExpiresAfter    *time.Time
	CreatedBefore   *time.Time
	CreatedAfter    *time.Time
	PlanID          *string
	InterfaceID     *string
}

// ListSort selects the keyset sort column.
type ListSort string

const (
	SortCreatedAt ListSort = "created_at"
	SortUsername  ListSort = "username"
	SortExpiresAt ListSort = "expires_at"
	SortUsed      ListSort = "used"
)

func (s ListSort) Valid() bool {
	switch s {
	case SortCreatedAt, SortUsername, SortExpiresAt, SortUsed:
		return true
	}
	return false
}

// Page is one page of the user list.
type Page struct {
	Items      []*User
	NextCursor string // "" = end
}

// cursorValue is the sort-key tuple anchor. v is a float (julianday), int
// (byte totals) or string (username) depending on the sort; id breaks ties.
type cursorValue struct {
	V  json.RawMessage `json:"v"`
	ID string          `json:"id"`
}

// ListPage returns one page of live users. The keyset predicate compares the
// sort value of the last returned row, so pages are stable under concurrent
// inserts/deletes (no OFFSET). Time sorts compare julianday() values —
// RFC3339Nano strings are NOT lexicographically ordered (variable-length
// fraction); julianday is deterministic and its ~µs precision is covered by
// the id tiebreak.
func (s *Service) ListPage(ctx context.Context, q ListQuery) (*Page, error) {
	sort := q.Sort
	if sort == "" {
		sort = SortCreatedAt
	}
	if !sort.Valid() {
		return nil, domain.E(domain.CodeInvalidRequest, "sort must be created_at|username|expires_at|used")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		return nil, domain.E(domain.CodeInvalidRequest, "limit must be <= 500")
	}
	// The zero value means "newest first" (the natural list default); an
	// explicit sort with Desc=false is a real ascending request.
	desc := q.Desc
	if q.Sort == "" && !q.Desc {
		desc = true
	}

	var (
		keyExpr string // sort key expression (non-null via sentinel)
		valExpr string // select form of the same value for the next cursor
	)
	const nullSentinel = 1e12 // > any real julianday date; NULLs sort last (desc)
	switch sort {
	case SortCreatedAt:
		keyExpr = fmt.Sprintf("julianday(created_at)")
		valExpr = fmt.Sprintf("julianday(created_at)")
	case SortUsername:
		keyExpr, valExpr = "username", "username"
	case SortExpiresAt:
		keyExpr = fmt.Sprintf("COALESCE(julianday(expires_at), %v)", nullSentinel)
		valExpr = fmt.Sprintf("COALESCE(julianday(expires_at), %v)", nullSentinel)
	case SortUsed:
		keyExpr, valExpr = "(traffic_used_rx + traffic_used_tx)", "(traffic_used_rx + traffic_used_tx)"
	}

	where := strings.Builder{}
	where.WriteString("deleted_at IS NULL")
	args := []any{}
	f := q.Filter
	if f.Username != nil && *f.Username != "" {
		where.WriteString(" AND username LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeLike(*f.Username)+"%")
	}
	if f.Status != nil {
		where.WriteString(" AND status = ?")
		args = append(args, string(*f.Status))
	}
	if f.TrafficExceeded != nil {
		if *f.TrafficExceeded {
			where.WriteString(" AND status = 'traffic_exceeded'")
		} else {
			where.WriteString(" AND status != 'traffic_exceeded'")
		}
	}
	if f.Enabled != nil {
		where.WriteString(" AND enabled = ?")
		args = append(args, boolInt(*f.Enabled))
	}
	if f.ExpiresBefore != nil {
		where.WriteString(" AND expires_at IS NOT NULL AND julianday(expires_at) < ?")
		args = append(args, julianday(*f.ExpiresBefore))
	}
	if f.ExpiresAfter != nil {
		where.WriteString(" AND expires_at IS NOT NULL AND julianday(expires_at) > ?")
		args = append(args, julianday(*f.ExpiresAfter))
	}
	if f.CreatedBefore != nil {
		where.WriteString(" AND julianday(created_at) < ?")
		args = append(args, julianday(*f.CreatedBefore))
	}
	if f.CreatedAfter != nil {
		where.WriteString(" AND julianday(created_at) > ?")
		args = append(args, julianday(*f.CreatedAfter))
	}
	if f.PlanID != nil && *f.PlanID != "" {
		where.WriteString(" AND plan_id = ?")
		args = append(args, *f.PlanID)
	}
	if f.InterfaceID != nil && *f.InterfaceID != "" {
		where.WriteString(" AND interface_id = ?")
		args = append(args, *f.InterfaceID)
	}

	order := "DESC"
	cmp := "<"
	if !desc {
		order, cmp = "ASC", ">"
	}

	var cur cursorValue
	hasCursor := false
	if q.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || json.Unmarshal(raw, &cur) != nil || cur.ID == "" {
			return nil, domain.E(domain.CodeInvalidRequest, "cursor is not valid")
		}
		hasCursor = true
	}

	query := strings.Builder{}
	query.WriteString(userColumns + `, ` + valExpr + ` AS _key FROM users WHERE ` + where.String())
	queryArgs := append([]any{}, args...)
	if hasCursor {
		query.WriteString(fmt.Sprintf(` AND (%s %s ? OR (%s = ? AND id %s ?))`, keyExpr, cmp, keyExpr, cmp))
		queryArgs = append(queryArgs, cur.vAny(), cur.vAny(), cur.ID)
	}
	query.WriteString(fmt.Sprintf(` ORDER BY %s %s, id %s LIMIT ?`, keyExpr, order, order))
	queryArgs = append(queryArgs, limit+1)

	rows, err := s.db.QueryContext(ctx, query.String(), queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("user: list: %w", err)
	}
	defer rows.Close()

	page := &Page{Items: []*User{}}
	keys := []any{}
	for rows.Next() {
		kr := &keyRow{scan: rows.Scan}
		u, err := scanUser(kr)
		if err != nil {
			return nil, fmt.Errorf("user: scan: %w", err)
		}
		page.Items = append(page.Items, u)
		keys = append(keys, kr.key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user: list: %w", err)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		b, err := json.Marshal(cursorValue{V: mustJSON(keys[limit-1]), ID: page.Items[limit-1].ID})
		if err != nil {
			return nil, fmt.Errorf("user: cursor: %w", err)
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	return page, nil
}

// keyRow forwards row.Scan to the driver while capturing the trailing `_key`
// column that ListPage appends after the standard user columns.
type keyRow struct {
	scan func(dest ...any) error
	key  any
}

func (k *keyRow) Scan(dst ...any) error {
	full := append(append([]any{}, dst...), &k.key)
	return k.scan(full...)
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

// vAny decodes the cursor's sort value for use as a SQL parameter (numbers
// arrive as float64, strings as string; SQLite's numeric comparison covers
// the INTEGER/REAL mix).
func (c cursorValue) vAny() any {
	var v any
	if err := json.Unmarshal(c.V, &v); err != nil {
		return nil
	}
	return v
}

// julianday converts a Go time to SQLite's julian day number (the same value
// julianday() computes in SQL, so filter comparisons match keyset comparisons).
func julianday(t time.Time) float64 {
	return float64(t.UnixNano())/86400e9 + 2440587.5
}

// escapeLike escapes the LIKE wildcards for a substring filter.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// Update applies partial changes. Enabled flips and status changes are
// mapped to the enabled/disabled events (catalog §webhooks.md).
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
	u.TrafficLimitBytes = in.TrafficLimitBytes.Resolve(u.TrafficLimitBytes)
	u.SpeedLimitDownKbps = in.SpeedLimitDownKbps.Resolve(u.SpeedLimitDownKbps)
	u.SpeedLimitUpKbps = in.SpeedLimitUpKbps.Resolve(u.SpeedLimitUpKbps)
	u.DeviceLimit = in.DeviceLimit.Resolve(u.DeviceLimit)
	u.PlanID = resolveString(in.PlanID, u.PlanID)
	u.InterfaceID = resolveString(in.InterfaceID, u.InterfaceID)
	if in.DurationSeconds != nil {
		u.DurationSeconds = in.DurationSeconds
	}
	wasEnabled := u.Enabled
	if in.Enabled != nil {
		u.Enabled = *in.Enabled
	}
	if err := validateLimits(in); err != nil {
		return nil, err
	}
	if err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.save(ctx, tx, u); err != nil {
			return err
		}
		if s.Recorder != nil {
			if wasEnabled != u.Enabled {
				ev := "user.disabled"
				if u.Enabled {
					ev = "user.enabled"
				}
				if err := s.Recorder.RecordTx(tx, ev, map[string]any{"user_id": u.ID, "username": u.Username}); err != nil {
					return err
				}
			}
			return s.Recorder.RecordTx(tx, "user.updated", map[string]any{"user_id": u.ID, "username": u.Username})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return u, nil
}

// SetStatus performs a guarded lifecycle transition. Enabling (active) and
// disabling (disabled/suspended) emit user.enabled / user.disabled.
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
	// The enabled flag stays consistent with the lifecycle: a disabled or
	// suspended account is not enabled (this also gates device creation),
	// and an account returned to active is enabled again.
	switch u.Status {
	case domain.UserDisabled, domain.UserSuspended, domain.UserExpired, domain.UserTrafficExceeded:
		u.Enabled = false
	case domain.UserActive, domain.UserWaitingFirstConnection:
		u.Enabled = true
	}
	// Recoverable transitions restore active/waiting state correctly.
	if status == domain.UserActive && u.ActivatedAt == nil && u.StartPolicy == domain.StartFirstConnection {
		u.Status = domain.UserWaitingFirstConnection
	}
	transition := ""
	switch u.Status {
	case domain.UserActive:
		transition = "user.enabled"
	case domain.UserDisabled, domain.UserSuspended:
		transition = "user.disabled"
	}
	if err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.save(ctx, tx, u); err != nil {
			return err
		}
		if s.Recorder != nil && transition != "" {
			data := map[string]any{"user_id": u.ID, "username": u.Username}
			if u.DisableReason != nil {
				data["reason"] = string(*u.DisableReason)
			}
			return s.Recorder.RecordTx(tx, transition, data)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return u, nil
}

// SetEnabledStatus is the enable/disable admin action in one transaction:
// enable → active (or waiting_first_connection per policy), disable → the
// given reason (manual when empty) + enabled=false.
func (s *Service) SetEnabledStatus(ctx context.Context, id string, enable bool, reason domain.DisableReason) (*User, error) {
	if enable {
		return s.SetStatus(ctx, id, domain.UserActive, "")
	}
	if !reason.Valid() {
		reason = domain.DisableManual
	}
	return s.SetStatus(ctx, id, domain.UserDisabled, reason)
}

// Renew extends the subscription. Mode: from the current expiration (at
// least now), from now, or an exact date (only for activated users). Emits
// user.updated (the change is expiration, not access).
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

	// Renewal reactivates expired accounts. A traffic_exceeded account is
	// deliberately NOT reactivated here — quota recovery is an explicit
	// admin action (traffic reset or a status change), enforced edge-wise by
	// the accounting cycle.
	if u.Status == domain.UserExpired {
		u.Status = domain.UserActive
		u.DisableReason = nil
	}
	if err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.save(ctx, tx, u); err != nil {
			return err
		}
		if s.Recorder != nil {
			return s.Recorder.RecordTx(tx, "user.updated", map[string]any{"user_id": u.ID, "username": u.Username})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return u, nil
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
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.save(ctx, tx, u); err != nil {
			return err
		}
		if s.Recorder != nil {
			return s.Recorder.RecordTx(tx, "user.updated", map[string]any{"user_id": u.ID, "username": u.Username, "deleted": true})
		}
		return nil
	})
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
	if err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.save(ctx, tx, u); err != nil {
			return err
		}
		if s.Recorder != nil {
			return s.Recorder.RecordTx(tx, "user.updated", map[string]any{"user_id": u.ID, "username": u.Username, "deleted": false})
		}
		return nil
	}); err != nil {
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

func (s *Service) save(ctx context.Context, tx *sql.Tx, u *User) error {
	u.UpdatedAt = s.now().UTC()
	var disableReason any
	if u.DisableReason != nil {
		disableReason = string(*u.DisableReason)
	}
	_, err := tx.ExecContext(ctx, `UPDATE users SET
		display_name = ?, note = ?, tags = ?, status = ?, disable_reason = ?,
		traffic_limit_bytes = ?, speed_limit_down_kbps = ?, speed_limit_up_kbps = ?,
		device_limit = ?, plan_id = ?, interface_id = ?, start_policy = ?, duration_seconds = ?,
		activated_at = ?, expires_at = ?, enabled = ?, metadata = ?, deleted_at = ?, updated_at = ?
		WHERE id = ?`,
		u.DisplayName, u.Note, encodeTags(u.Tags), string(u.Status), disableReason,
		nullI64(u.TrafficLimitBytes), nullInt(u.SpeedLimitDownKbps), nullInt(u.SpeedLimitUpKbps),
		nullInt(u.DeviceLimit),
		nullText(u.PlanID), nullText(u.InterfaceID), string(u.StartPolicy), nullI64(u.DurationSeconds),
		nullTime(u.ActivatedAt), nullTime(u.ExpiresAt), boolInt(u.Enabled),
		encodeMeta(u.Metadata), nullTime(u.DeletedAt), u.UpdatedAt.Format(time.RFC3339Nano), u.ID)
	if err != nil {
		return fmt.Errorf("user: save: %w", err)
	}
	return nil
}

const userColumns = `SELECT id, username, display_name, note, tags, status, disable_reason,
	traffic_limit_bytes, traffic_used_rx, traffic_used_tx, speed_limit_down_kbps,
	speed_limit_up_kbps, device_limit, plan_id, interface_id, start_policy, duration_seconds,
	activated_at, expires_at, last_activity_at, enabled, metadata, deleted_at, created_at, updated_at`

func scanUser(row rowScanner) (*User, error) {
	var (
		u            User
		displayName  sql.NullString
		note         sql.NullString
		tags         string
		status       string
		reason       sql.NullString
		traffic      sql.NullInt64
		down         sql.NullInt64
		up           sql.NullInt64
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
		&traffic, &u.TrafficUsedRX, &u.TrafficUsedTX, &down, &up, &devices,
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
	if down.Valid {
		v := int(down.Int64)
		u.SpeedLimitDownKbps = &v
	}
	if up.Valid {
		v := int(up.Int64)
		u.SpeedLimitUpKbps = &v
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

// List returns live users, newest first, capped at limit (small callers:
// exports, tests). The API list path is ListPage (cursor pagination).
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

// CountForPlans returns live users per plan id (batch for plan lists;
// plans deleted with users on SET NULL show their remaining references).
func (s *Service) CountForPlans(ctx context.Context, planIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(planIDs))
	if len(planIDs) == 0 {
		return counts, nil
	}
	placeholders := strings.Repeat("?,", len(planIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(planIDs))
	for i, id := range planIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT plan_id, COUNT(*) FROM users
		WHERE deleted_at IS NULL AND plan_id IN (`+placeholders+`) GROUP BY plan_id`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("user: count by plan: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("user: count by plan scan: %w", err)
		}
		counts[id] = n
	}
	return counts, rows.Err()
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
	if in.TrafficLimitBytes.Set && !in.TrafficLimitBytes.Null && in.TrafficLimitBytes.Value < 0 {
		return domain.E(domain.CodeInvalidRequest, "traffic limit must be non-negative (null = unlimited)")
	}
	for _, o := range []domain.OptInt{in.SpeedLimitDownKbps, in.SpeedLimitUpKbps, in.DeviceLimit} {
		if o.Set && !o.Null && o.Value <= 0 {
			return domain.E(domain.CodeInvalidRequest, "speed/device limits must be positive (null = unlimited)")
		}
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

func resolveString(o domain.OptString, current *string) *string {
	if !o.Set {
		return current
	}
	if o.Null {
		return nil
	}
	v := o.Value
	return &v
}

type rowScanner interface{ Scan(dest ...any) error }

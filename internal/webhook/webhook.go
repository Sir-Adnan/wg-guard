// Package webhook implements durable, restart-safe outbound webhooks
// (docs/integrations/webhooks.md): the event row is inserted in the SAME
// transaction as the state change (events cannot disappear), one pending
// delivery row per subscribed enabled endpoint is created at emit time, and a
// single worker pass (part of the central scheduler) delivers due rows with
// exponential backoff to a dead-letter state. HMAC signatures use the
// endpoint secret (encrypted at rest); secrets never reach logs.
package webhook

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
)

// Event catalog — the V1 contract (docs/integrations/webhooks.md). Additive
// only; receivers select which events they receive.
const (
	EventUserCreated         = "user.created"
	EventUserUpdated         = "user.updated"
	EventUserEnabled         = "user.enabled"
	EventUserDisabled        = "user.disabled"
	EventUserExpired         = "user.expired"
	EventUserTrafficExceeded = "user.traffic_exceeded"
	EventUserFirstConnected  = "user.first_connected"
	EventDeviceCreated       = "device.created"
	EventDeviceDeleted       = "device.deleted"
	EventNodeStarted         = "node.started"
)

// Catalog lists every event type receivers can subscribe to.
func Catalog() []string {
	return []string{
		EventUserCreated, EventUserUpdated, EventUserEnabled, EventUserDisabled,
		EventUserExpired, EventUserTrafficExceeded, EventUserFirstConnected,
		EventDeviceCreated, EventDeviceDeleted, EventNodeStarted,
	}
}

// ValidEvent reports whether an event type exists in the catalog.
func ValidEvent(e string) bool {
	for _, c := range Catalog() {
		if c == e {
			return true
		}
	}
	return false
}

// Recorder emits durable events inside state-changing transactions. It is
// injected into user/device/accounting services as the local EventRecorder
// interface; nil seams simply skip emission.
type Recorder struct{}

// NewRecorder returns the shared recorder.
func NewRecorder() *Recorder { return &Recorder{} }

// RecordTx inserts the event row plus one pending delivery per enabled
// endpoint subscribed to the event, all within the caller's transaction.
// Endpoints see events emitted while they are subscribed and enabled.
func (r *Recorder) RecordTx(tx *sql.Tx, eventType string, data map[string]any) error {
	if !ValidEvent(eventType) {
		return domain.E(domain.CodeInvalidRequest, "unknown webhook event %q", eventType)
	}
	// Classify from the persisted user in the same transaction, never from an
	// untrusted payload field claiming to be a reseller. An absent/unknown user
	// is visible only to node-wide endpoints. node.started has no tenant.
	var owner sql.NullString
	if eventType != EventNodeStarted {
		if userID, ok := data["user_id"].(string); ok && userID != "" {
			err := tx.QueryRowContext(context.Background(),
				`SELECT reseller_id FROM users WHERE id = ?`, userID).Scan(&owner)
			if err != nil && err != sql.ErrNoRows {
				return fmt.Errorf("webhook: classify event owner: %w", err)
			}
		}
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("webhook: marshal payload: %w", err)
	}
	eventID := domain.NewID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(context.Background(), `INSERT INTO webhook_events
		(id, event_type, payload, created_at, reseller_id) VALUES (?, ?, ?, ?, ?)`,
		eventID, eventType, string(payload), now, owner); err != nil {
		return fmt.Errorf("webhook: insert event: %w", err)
	}

	// Fan out to subscribed enabled endpoints. The endpoint table is small
	// (administratively bounded), so the filter runs in Go on the tx snapshot.
	rows, err := tx.QueryContext(context.Background(),
		`SELECT id, events FROM webhook_endpoints WHERE enabled = 1
		 AND ((reseller_id IS NULL AND (? IS NULL OR include_reseller_events = 1))
		      OR reseller_id = ?)`, owner, owner)
	if err != nil {
		return fmt.Errorf("webhook: endpoints: %w", err)
	}
	defer rows.Close()
	type sub struct {
		id     string
		events []string
	}
	var subs []sub
	for rows.Next() {
		var (
			s      sub
			events string
		)
		if err := rows.Scan(&s.id, &events); err != nil {
			return fmt.Errorf("webhook: scan endpoint: %w", err)
		}
		_ = json.Unmarshal([]byte(events), &s.events)
		subs = append(subs, s)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("webhook: endpoints: %w", err)
	}
	rows.Close()

	for _, s := range subs {
		subscribed := false
		for _, e := range s.events {
			if e == eventType {
				subscribed = true
				break
			}
		}
		if !subscribed {
			continue
		}
		if _, err := tx.ExecContext(context.Background(), `INSERT INTO webhook_deliveries
			(id, endpoint_id, event_id, event_type, payload, status, attempts, next_attempt_at,
			 last_error, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 'pending', 0, ?, '', ?, ?)`,
			domain.NewID(), s.id, eventID, eventType, string(payload), now, now, now); err != nil {
			return fmt.Errorf("webhook: insert delivery: %w", err)
		}
	}
	return nil
}

// Emit records an event outside a state change (node.started at serve start).
func (r *Recorder) Emit(ctx context.Context, db *database.DB, eventType string, data map[string]any) error {
	return db.WithTx(ctx, func(tx *sql.Tx) error {
		return r.RecordTx(tx, eventType, data)
	})
}

// Endpoint is a stored webhook receiver.
type Endpoint struct {
	ID                    string
	ResellerID            *string
	IncludeResellerEvents bool
	URL                   string
	Enabled               bool
	Events                []string
	CreatedAt             time.Time
}

// Stats is the delivery counter summary shown next to an endpoint.
type Stats struct {
	Pending   int `json:"pending"`
	Delivered int `json:"delivered"`
	Dead      int `json:"dead"`
}

// Service manages endpoints (API surface) and the delivery-time secrets.
type Service struct {
	db   *database.DB
	ring *secrets.KeyRing
	now  func() time.Time
}

// NewService wires the endpoint service. The key ring decrypts endpoint
// secrets at rest (they are never logged or serialized).
func NewService(db *database.DB, ring *secrets.KeyRing) *Service {
	return &Service{db: db, ring: ring, now: time.Now}
}

// Create stores an endpoint. An empty secret is generated; the plaintext is
// returned exactly once and never logged.
func (s *Service) Create(ctx context.Context, rawURL string, events []string, secret string) (*Endpoint, string, error) {
	return s.create(ctx, nil, false, rawURL, events, secret)
}

// CreateOwner is the only entry point that can opt a node-wide receiver into
// reseller events. The API/panel caller must already have owner authority.
func (s *Service) CreateOwner(ctx context.Context, includeResellerEvents bool, rawURL string, events []string, secret string) (*Endpoint, string, error) {
	return s.create(ctx, nil, includeResellerEvents, rawURL, events, secret)
}

// CreateFor binds a receiver to one reseller; nil creates a node-wide
// endpoint. Reseller subscriptions never receive node-wide lifecycle events.
func (s *Service) CreateFor(ctx context.Context, resellerID *string, rawURL string, events []string, secret string) (*Endpoint, string, error) {
	return s.create(ctx, resellerID, false, rawURL, events, secret)
}

func (s *Service) create(ctx context.Context, resellerID *string, includeResellerEvents bool, rawURL string, events []string, secret string) (*Endpoint, string, error) {
	if err := ValidateURL(rawURL); err != nil {
		return nil, "", err
	}
	if resellerID != nil {
		if err := ValidateResellerURL(rawURL); err != nil {
			return nil, "", err
		}
	}
	if err := ValidateEvents(events); err != nil {
		return nil, "", err
	}
	if len(events) == 0 {
		return nil, "", domain.E(domain.CodeInvalidRequest, "select at least one event to subscribe to")
	}
	if resellerID != nil {
		for _, event := range events {
			if event == EventNodeStarted {
				return nil, "", domain.E(domain.CodeForbidden, "node lifecycle events are not available to reseller endpoints")
			}
		}
	}
	generated := false
	if strings.TrimSpace(secret) == "" {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return nil, "", fmt.Errorf("webhook: secret: %w", err)
		}
		secret = hex.EncodeToString(buf)
		generated = true
	}
	enc, err := s.ring.EncryptString(secret)
	if err != nil {
		return nil, "", fmt.Errorf("webhook: encrypt secret: %w", err)
	}
	e := &Endpoint{
		ID:                    domain.NewID(),
		ResellerID:            resellerID,
		IncludeResellerEvents: includeResellerEvents,
		URL:                   rawURL,
		Enabled:               true,
		Events:                events,
		CreatedAt:             s.now().UTC(),
	}
	eventsJSON, _ := json.Marshal(events)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO webhook_endpoints
		(id, reseller_id, include_reseller_events, url, secret_encrypted, enabled, events, created_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		e.ID, resellerID, boolInt(includeResellerEvents), e.URL, enc, string(eventsJSON), e.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return nil, "", fmt.Errorf("webhook: create endpoint: %w", err)
	}
	if !generated {
		secret = "" // caller-supplied secrets are never echoed back
	}
	return e, secret, nil
}

// EndpointUpdate is a partial endpoint change.
type EndpointUpdate struct {
	URL                   *string
	Events                []string
	Enabled               *bool
	Secret                *string // rotate (empty string means "generate a new secret")
	IncludeResellerEvents *bool
}

// Update applies a partial change; rotating the secret returns the new
// plaintext exactly once when it was generated.
func (s *Service) Update(ctx context.Context, id string, in EndpointUpdate) (*Endpoint, string, error) {
	return s.update(ctx, id, nil, false, in)
}

func (s *Service) UpdateGlobal(ctx context.Context, id string, in EndpointUpdate) (*Endpoint, string, error) {
	return s.update(ctx, id, nil, true, in)
}

// UpdateFor changes only a receiver owned by the specified reseller.
func (s *Service) UpdateFor(ctx context.Context, resellerID string, id string, in EndpointUpdate) (*Endpoint, string, error) {
	return s.update(ctx, id, &resellerID, true, in)
}

func (s *Service) update(ctx context.Context, id string, resellerID *string, scoped bool, in EndpointUpdate) (*Endpoint, string, error) {
	if scoped && in.IncludeResellerEvents != nil {
		return nil, "", domain.E(domain.CodeForbidden, "only the owner can change reseller event fanout")
	}
	var e *Endpoint
	var err error
	if scoped {
		if resellerID != nil {
			e, err = s.GetFor(ctx, *resellerID, id)
		} else {
			e, err = s.GetGlobal(ctx, id)
		}
	} else {
		e, err = s.Get(ctx, id)
	}
	if err != nil {
		return nil, "", err
	}
	if e.ResellerID != nil && in.IncludeResellerEvents != nil {
		return nil, "", domain.E(domain.CodeInvalidRequest, "reseller endpoints cannot opt into other tenants")
	}
	if in.URL != nil {
		if err := ValidateURL(*in.URL); err != nil {
			return nil, "", err
		}
		if resellerID != nil {
			if err := ValidateResellerURL(*in.URL); err != nil {
				return nil, "", err
			}
		}
		e.URL = *in.URL
	}
	if in.Events != nil {
		if err := ValidateEvents(in.Events); err != nil {
			return nil, "", err
		}
		if len(in.Events) == 0 {
			return nil, "", domain.E(domain.CodeInvalidRequest, "select at least one event to subscribe to")
		}
		if resellerID != nil {
			for _, event := range in.Events {
				if event == EventNodeStarted {
					return nil, "", domain.E(domain.CodeForbidden, "node lifecycle events are not available to reseller endpoints")
				}
			}
		}
		e.Events = in.Events
	}
	if in.Enabled != nil {
		e.Enabled = *in.Enabled
	}
	if in.IncludeResellerEvents != nil {
		e.IncludeResellerEvents = *in.IncludeResellerEvents
	}
	var newSecret string
	generated := false
	if in.Secret != nil {
		newSecret = *in.Secret
		if strings.TrimSpace(newSecret) == "" {
			buf := make([]byte, 32)
			if _, err := rand.Read(buf); err != nil {
				return nil, "", fmt.Errorf("webhook: secret: %w", err)
			}
			newSecret = hex.EncodeToString(buf)
			generated = true
		}
	}
	if err := s.save(ctx, e, newSecret, resellerID, scoped); err != nil {
		return nil, "", err
	}
	if !generated {
		newSecret = "" // caller-supplied secrets are never echoed back
	}
	return e, newSecret, nil
}

func (s *Service) save(ctx context.Context, e *Endpoint, secret string, resellerID *string, scoped bool) error {
	eventsJSON, _ := json.Marshal(e.Events)
	where := ` WHERE id = ?`
	args := []any{e.ID}
	if scoped {
		if resellerID != nil {
			where += ` AND reseller_id = ?`
			args = append(args, *resellerID)
		} else {
			where += ` AND reseller_id IS NULL AND include_reseller_events = 0`
		}
	}
	if secret != "" {
		enc, err := s.ring.EncryptString(secret)
		if err != nil {
			return fmt.Errorf("webhook: encrypt secret: %w", err)
		}
		res, err := s.db.ExecContext(ctx, `UPDATE webhook_endpoints
			SET url = ?, secret_encrypted = ?, enabled = ?, events = ?, include_reseller_events = ?`+where,
			append([]any{e.URL, enc, boolInt(e.Enabled), string(eventsJSON), boolInt(e.IncludeResellerEvents)}, args...)...)
		if err != nil {
			return fmt.Errorf("webhook: update endpoint: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return domain.E(domain.CodeNotFound, "webhook endpoint not found")
		}
		return nil
	}
	res, err := s.db.ExecContext(ctx, `UPDATE webhook_endpoints
		SET url = ?, enabled = ?, events = ?, include_reseller_events = ?`+where,
		append([]any{e.URL, boolInt(e.Enabled), string(eventsJSON), boolInt(e.IncludeResellerEvents)}, args...)...)
	if err != nil {
		return fmt.Errorf("webhook: update endpoint: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeNotFound, "webhook endpoint not found")
	}
	return nil
}

// Delete removes an endpoint (deliveries cascade; the event log remains).
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.delete(ctx, nil, false, id)
}

func (s *Service) DeleteGlobal(ctx context.Context, id string) error {
	return s.delete(ctx, nil, true, id)
}

func (s *Service) DeleteFor(ctx context.Context, resellerID string, id string) error {
	return s.delete(ctx, &resellerID, false, id)
}

func (s *Service) delete(ctx context.Context, resellerID *string, globalOnly bool, id string) error {
	query := `DELETE FROM webhook_endpoints WHERE id = ?`
	args := []any{id}
	if resellerID != nil {
		query += ` AND reseller_id = ?`
		args = append(args, *resellerID)
	} else if globalOnly {
		query += ` AND reseller_id IS NULL AND include_reseller_events = 0`
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("webhook: delete endpoint: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeNotFound, "webhook endpoint %s not found", id)
	}
	return nil
}

// Get loads one endpoint.
func (s *Service) Get(ctx context.Context, id string) (*Endpoint, error) {
	row := s.db.QueryRowContext(ctx, endpointColumns+` FROM webhook_endpoints WHERE id = ?`, id)
	return scanEndpoint(row)
}

func (s *Service) GetGlobal(ctx context.Context, id string) (*Endpoint, error) {
	row := s.db.QueryRowContext(ctx, endpointColumns+` FROM webhook_endpoints
		WHERE id = ? AND reseller_id IS NULL AND include_reseller_events = 0`, id)
	return scanEndpoint(row)
}

func (s *Service) GetFor(ctx context.Context, resellerID string, id string) (*Endpoint, error) {
	row := s.db.QueryRowContext(ctx, endpointColumns+` FROM webhook_endpoints WHERE id = ? AND reseller_id = ?`, id, resellerID)
	return scanEndpoint(row)
}

// List returns all endpoints oldest first, with per-endpoint delivery stats.
func (s *Service) List(ctx context.Context) ([]EndpointWithStats, error) {
	return s.list(ctx, nil, false)
}

func (s *Service) ListGlobal(ctx context.Context) ([]EndpointWithStats, error) {
	return s.list(ctx, nil, true)
}

func (s *Service) ListFor(ctx context.Context, resellerID string) ([]EndpointWithStats, error) {
	return s.list(ctx, &resellerID, false)
}

func (s *Service) list(ctx context.Context, resellerID *string, globalOnly bool) ([]EndpointWithStats, error) {
	query := endpointColumns + ` FROM webhook_endpoints`
	var args []any
	if resellerID != nil {
		query += ` WHERE reseller_id = ?`
		args = append(args, *resellerID)
	} else if globalOnly {
		query += ` WHERE reseller_id IS NULL AND include_reseller_events = 0`
	}
	query += ` ORDER BY created_at`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("webhook: list endpoints: %w", err)
	}
	defer rows.Close()
	var out []EndpointWithStats
	for rows.Next() {
		e, err := scanEndpoint(rows)
		if err != nil {
			return nil, fmt.Errorf("webhook: scan endpoint: %w", err)
		}
		out = append(out, EndpointWithStats{Endpoint: e})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("webhook: list endpoints: %w", err)
	}
	// Delivery counters (tiny GROUP BY; the deliveries table is pruned).
	statQuery := `SELECT d.endpoint_id, d.status, COUNT(*) FROM webhook_deliveries d
		JOIN webhook_endpoints e ON e.id = d.endpoint_id
		JOIN webhook_events ev ON ev.id = d.event_id
		WHERE ((e.reseller_id IS NULL AND (ev.reseller_id IS NULL OR e.include_reseller_events = 1))
		       OR e.reseller_id = ev.reseller_id)`
	var statArgs []any
	if resellerID != nil {
		statQuery += ` AND e.reseller_id = ?`
		statArgs = append(statArgs, *resellerID)
	} else if globalOnly {
		statQuery += ` AND e.reseller_id IS NULL AND e.include_reseller_events = 0`
	}
	statQuery += ` GROUP BY d.endpoint_id, d.status`
	statRows, err := s.db.QueryContext(ctx, statQuery, statArgs...)
	if err != nil {
		return nil, fmt.Errorf("webhook: stats: %w", err)
	}
	defer statRows.Close()
	stats := map[string]Stats{}
	for statRows.Next() {
		var (
			id, status string
			n          int
		)
		if err := statRows.Scan(&id, &status, &n); err != nil {
			return nil, fmt.Errorf("webhook: scan stats: %w", err)
		}
		st := stats[id]
		switch status {
		case "pending":
			st.Pending = n
		case "delivered":
			st.Delivered = n
		case "dead":
			st.Dead = n
		}
		stats[id] = st
	}
	if err := statRows.Err(); err != nil {
		return nil, fmt.Errorf("webhook: stats: %w", err)
	}
	for i := range out {
		out[i].Stats = stats[out[i].ID]
	}
	return out, nil
}

// EndpointWithStats pairs an endpoint with its delivery counters.
type EndpointWithStats struct {
	*Endpoint
	Stats Stats `json:"stats"`
}

// Secret decrypts the endpoint secret (delivery signing and redeliver
// verification only — never logged, never serialized).
func (s *Service) Secret(ctx context.Context, e *Endpoint) (string, error) {
	var enc []byte
	err := s.db.QueryRowContext(ctx, `SELECT secret_encrypted FROM webhook_endpoints WHERE id = ?`, e.ID).Scan(&enc)
	if err != nil {
		return "", fmt.Errorf("webhook: secret lookup: %w", err)
	}
	pt, err := s.ring.DecryptString(string(enc))
	if err != nil {
		return "", fmt.Errorf("webhook: decrypt secret: %w", err)
	}
	return pt, nil
}

// Redeliver resets one delivery to pending (manual redeliver of a dead or
// failed delivery). The worker picks it up on the next pass.
func (s *Service) Redeliver(ctx context.Context, endpointID, deliveryID string) error {
	return s.redeliver(ctx, nil, false, endpointID, deliveryID)
}

func (s *Service) RedeliverGlobal(ctx context.Context, endpointID, deliveryID string) error {
	return s.redeliver(ctx, nil, true, endpointID, deliveryID)
}

func (s *Service) RedeliverFor(ctx context.Context, resellerID, endpointID, deliveryID string) error {
	return s.redeliver(ctx, &resellerID, false, endpointID, deliveryID)
}

func (s *Service) redeliver(ctx context.Context, resellerID *string, globalOnly bool, endpointID, deliveryID string) error {
	scope := ""
	args := []any{s.now().UTC().Format(time.RFC3339Nano), s.now().UTC().Format(time.RFC3339Nano), deliveryID, endpointID}
	if resellerID != nil {
		scope = ` AND e.reseller_id = ?`
		args = append(args, *resellerID)
	} else if globalOnly {
		scope = ` AND e.reseller_id IS NULL AND e.include_reseller_events = 0`
	}
	res, err := s.db.ExecContext(ctx, `UPDATE webhook_deliveries
		SET status = 'pending', attempts = 0, next_attempt_at = ?, last_error = '', updated_at = ?
		WHERE id = ? AND endpoint_id = ? AND EXISTS (
			SELECT 1 FROM webhook_endpoints e JOIN webhook_events ev ON ev.id = webhook_deliveries.event_id
			WHERE e.id = webhook_deliveries.endpoint_id`+scope+`
			AND ((e.reseller_id IS NULL AND (ev.reseller_id IS NULL OR e.include_reseller_events = 1))
			     OR e.reseller_id = ev.reseller_id))`, args...)
	if err != nil {
		return fmt.Errorf("webhook: redeliver: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeNotFound, "delivery %s not found for endpoint %s", deliveryID, endpointID)
	}
	return nil
}

// Receipt is a non-secret delivery result for reconciliation. The signed
// body and endpoint URL are deliberately absent.
type Receipt struct {
	ID            string  `json:"id"`
	EventID       string  `json:"event_id"`
	EventType     string  `json:"event_type"`
	Status        string  `json:"status"`
	Attempts      int     `json:"attempts"`
	NextAttemptAt *string `json:"next_attempt_at"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

// Receipts returns a bounded, newest-first cursor page for one endpoint.
// before is a delivery ID from the preceding page, scoped to this endpoint.
func (s *Service) Receipts(ctx context.Context, resellerID *string, endpointID string, limit int, before, eventID string) ([]Receipt, string, error) {
	return s.receipts(ctx, resellerID, false, endpointID, limit, before, eventID)
}

func (s *Service) receipts(ctx context.Context, resellerID *string, globalOnly bool, endpointID string, limit int, before, eventID string) ([]Receipt, string, error) {
	if limit <= 0 || limit > 100 {
		return nil, "", domain.E(domain.CodeInvalidRequest, "limit must be 1..100")
	}
	if resellerID != nil {
		if _, err := s.GetFor(ctx, *resellerID, endpointID); err != nil {
			return nil, "", err
		}
	} else if globalOnly {
		if _, err := s.GetGlobal(ctx, endpointID); err != nil {
			return nil, "", err
		}
	} else if _, err := s.Get(ctx, endpointID); err != nil {
		return nil, "", err
	}
	query := `SELECT d.id, d.event_id, d.event_type, d.status, d.attempts,
		d.next_attempt_at, d.created_at, d.updated_at FROM webhook_deliveries d
		JOIN webhook_events ev ON ev.id = d.event_id
		JOIN webhook_endpoints e ON e.id = d.endpoint_id
		WHERE d.endpoint_id = ? AND ((e.reseller_id IS NULL AND (ev.reseller_id IS NULL OR e.include_reseller_events = 1))
		     OR e.reseller_id = ev.reseller_id)`
	args := []any{endpointID}
	if resellerID != nil {
		query += ` AND e.reseller_id = ?`
		args = append(args, *resellerID)
	} else if globalOnly {
		query += ` AND e.reseller_id IS NULL AND e.include_reseller_events = 0`
	}
	if eventID != "" {
		query += ` AND d.event_id = ?`
		args = append(args, eventID)
	}
	if before != "" {
		var created string
		err := s.db.QueryRowContext(ctx, `SELECT created_at FROM webhook_deliveries
			WHERE id = ? AND endpoint_id = ?`, before, endpointID).Scan(&created)
		if err == sql.ErrNoRows {
			return nil, "", domain.E(domain.CodeNotFound, "delivery cursor not found")
		}
		if err != nil {
			return nil, "", fmt.Errorf("webhook: receipt cursor: %w", err)
		}
		query += ` AND (d.created_at < ? OR (d.created_at = ? AND d.id < ?))`
		args = append(args, created, created, before)
	}
	query += ` ORDER BY d.created_at DESC, d.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("webhook: receipts: %w", err)
	}
	defer rows.Close()
	var out []Receipt
	for rows.Next() {
		var item Receipt
		var next sql.NullString
		if err := rows.Scan(&item.ID, &item.EventID, &item.EventType, &item.Status,
			&item.Attempts, &next, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, "", fmt.Errorf("webhook: receipt scan: %w", err)
		}
		if next.Valid && item.Status == "pending" {
			item.NextAttemptAt = &next.String
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(out) > limit {
		out = out[:limit]
		return out, out[len(out)-1].ID, nil
	}
	return out, "", nil
}

func (s *Service) ReceiptsGlobal(ctx context.Context, endpointID string, limit int, before, eventID string) ([]Receipt, string, error) {
	return s.receipts(ctx, nil, true, endpointID, limit, before, eventID)
}

// ReceiptByID resolves one delivery under the same tenant boundary.
func (s *Service) ReceiptByID(ctx context.Context, resellerID *string, endpointID, deliveryID string) (*Receipt, error) {
	return s.receiptByID(ctx, resellerID, false, endpointID, deliveryID)
}

func (s *Service) receiptByID(ctx context.Context, resellerID *string, globalOnly bool, endpointID, deliveryID string) (*Receipt, error) {
	if resellerID != nil {
		if _, err := s.GetFor(ctx, *resellerID, endpointID); err != nil {
			return nil, err
		}
	} else if globalOnly {
		if _, err := s.GetGlobal(ctx, endpointID); err != nil {
			return nil, err
		}
	} else if _, err := s.Get(ctx, endpointID); err != nil {
		return nil, err
	}
	var item Receipt
	var next sql.NullString
	query := `SELECT d.id, d.event_id, d.event_type, d.status, d.attempts,
		d.next_attempt_at, d.created_at, d.updated_at FROM webhook_deliveries d
		JOIN webhook_events ev ON ev.id = d.event_id JOIN webhook_endpoints e ON e.id = d.endpoint_id
		WHERE d.id = ? AND d.endpoint_id = ? AND ((e.reseller_id IS NULL AND (ev.reseller_id IS NULL OR e.include_reseller_events = 1))
		     OR e.reseller_id = ev.reseller_id)`
	args := []any{deliveryID, endpointID}
	if resellerID != nil {
		query += ` AND e.reseller_id = ?`
		args = append(args, *resellerID)
	} else if globalOnly {
		query += ` AND e.reseller_id IS NULL AND e.include_reseller_events = 0`
	}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&item.ID, &item.EventID, &item.EventType,
		&item.Status, &item.Attempts, &next, &item.CreatedAt, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, domain.E(domain.CodeNotFound, "delivery not found")
	}
	if err != nil {
		return nil, fmt.Errorf("webhook: receipt lookup: %w", err)
	}
	if next.Valid && item.Status == "pending" {
		item.NextAttemptAt = &next.String
	}
	return &item, nil
}

func (s *Service) ReceiptByIDGlobal(ctx context.Context, endpointID, deliveryID string) (*Receipt, error) {
	return s.receiptByID(ctx, nil, true, endpointID, deliveryID)
}

const endpointColumns = `SELECT id, reseller_id, include_reseller_events, url, enabled, events, created_at`

func scanEndpoint(row rowScanner) (*Endpoint, error) {
	var (
		e               Endpoint
		enabled         int
		events          string
		createdStr      string
		reseller        sql.NullString
		includeReseller int
	)
	if err := row.Scan(&e.ID, &reseller, &includeReseller, &e.URL, &enabled, &events, &createdStr); err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.E(domain.CodeNotFound, "webhook endpoint not found")
		}
		return nil, err
	}
	if reseller.Valid {
		e.ResellerID = &reseller.String
	}
	e.IncludeResellerEvents = includeReseller == 1
	_ = json.Unmarshal([]byte(events), &e.Events)
	e.Enabled = enabled == 1
	e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	return &e, nil
}

// ValidateURL enforces the receiver contract: absolute http(s) URL with a
// host. Anything else is rejected before a secret is ever stored for it.
func ValidateURL(raw string) error {
	u := strings.TrimSpace(raw)
	if u == "" || len(u) > 2048 {
		return domain.E(domain.CodeInvalidRequest, "webhook URL must be a non-empty http(s) URL (≤ 2048 chars)")
	}
	scheme, rest, ok := strings.Cut(u, "://")
	if !ok || (scheme != "http" && scheme != "https") {
		return domain.E(domain.CodeInvalidRequest, "webhook URL scheme must be http or https")
	}
	host := rest
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if i := strings.LastIndex(host, "@"); i >= 0 {
		return domain.E(domain.CodeInvalidRequest, "webhook URL must not embed credentials")
	}
	if host == "" {
		return domain.E(domain.CodeInvalidRequest, "webhook URL host is required")
	}
	return nil
}

// ValidateResellerURL is the request-time half of the tenant egress policy.
// The worker resolves and checks every dialed address again, so DNS rebinding
// cannot turn a previously accepted public hostname into an internal target.
func ValidateResellerURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return domain.E(domain.CodeInvalidRequest, "reseller webhook URL must be a public HTTPS URL without credentials or fragments")
	}
	if addr, err := netip.ParseAddr(u.Hostname()); err == nil && !publicWebhookAddr(addr) {
		return domain.E(domain.CodeInvalidRequest, "reseller webhook URL must use a public destination")
	}
	return nil
}

// ValidateEvents checks every requested event exists in the catalog.
func ValidateEvents(events []string) error {
	for _, e := range events {
		if !ValidEvent(e) {
			return domain.E(domain.CodeInvalidRequest, "unknown event %q (see /docs or the event catalog)", e)
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type rowScanner interface{ Scan(dest ...any) error }

// Delivery is one delivery row as shown in the panel (no payload bodies —
// they can embed user identifiers; the panel links to the source records).
type Delivery struct {
	ID         string
	EndpointID string
	EventType  string
	Status     string
	Attempts   int
	LastError  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Deliveries lists the most recent deliveries of one endpoint, newest first
// (panel screen; the REST API intentionally stays out of delivery browsing).
func (s *Service) Deliveries(ctx context.Context, endpointID string, limit int) ([]Delivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, endpoint_id, event_type, status,
		attempts, last_error, created_at, updated_at
		FROM webhook_deliveries WHERE endpoint_id = ?
		ORDER BY created_at DESC, id DESC LIMIT ?`, endpointID, limit)
	if err != nil {
		return nil, fmt.Errorf("webhook: deliveries: %w", err)
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var d Delivery
		var created, updated string
		if err := rows.Scan(&d.ID, &d.EndpointID, &d.EventType, &d.Status,
			&d.Attempts, &d.LastError, &created, &updated); err != nil {
			return nil, fmt.Errorf("webhook: deliveries scan: %w", err)
		}
		d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		d.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, d)
	}
	return out, rows.Err()
}

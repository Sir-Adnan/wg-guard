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
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("webhook: marshal payload: %w", err)
	}
	eventID := domain.NewID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(context.Background(), `INSERT INTO webhook_events
		(id, event_type, payload, created_at) VALUES (?, ?, ?, ?)`,
		eventID, eventType, string(payload), now); err != nil {
		return fmt.Errorf("webhook: insert event: %w", err)
	}

	// Fan out to subscribed enabled endpoints. The endpoint table is small
	// (administratively bounded), so the filter runs in Go on the tx snapshot.
	rows, err := tx.QueryContext(context.Background(),
		`SELECT id, events FROM webhook_endpoints WHERE enabled = 1`)
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
	ID        string
	URL       string
	Enabled   bool
	Events    []string
	CreatedAt time.Time
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
	if err := ValidateURL(rawURL); err != nil {
		return nil, "", err
	}
	if err := ValidateEvents(events); err != nil {
		return nil, "", err
	}
	if len(events) == 0 {
		return nil, "", domain.E(domain.CodeInvalidRequest, "select at least one event to subscribe to")
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
		ID:        domain.NewID(),
		URL:       rawURL,
		Enabled:   true,
		Events:    events,
		CreatedAt: s.now().UTC(),
	}
	eventsJSON, _ := json.Marshal(events)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO webhook_endpoints
		(id, url, secret_encrypted, enabled, events, created_at) VALUES (?, ?, ?, 1, ?, ?)`,
		e.ID, e.URL, enc, string(eventsJSON), e.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return nil, "", fmt.Errorf("webhook: create endpoint: %w", err)
	}
	if !generated {
		secret = "" // caller-supplied secrets are never echoed back
	}
	return e, secret, nil
}

// EndpointUpdate is a partial endpoint change.
type EndpointUpdate struct {
	URL     *string
	Events  []string
	Enabled *bool
	Secret  *string // rotate (empty string means "generate a new secret")
}

// Update applies a partial change; rotating the secret returns the new
// plaintext exactly once when it was generated.
func (s *Service) Update(ctx context.Context, id string, in EndpointUpdate) (*Endpoint, string, error) {
	e, err := s.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if in.URL != nil {
		if err := ValidateURL(*in.URL); err != nil {
			return nil, "", err
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
		e.Events = in.Events
	}
	if in.Enabled != nil {
		e.Enabled = *in.Enabled
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
	if err := s.save(ctx, e, newSecret); err != nil {
		return nil, "", err
	}
	if !generated {
		newSecret = "" // caller-supplied secrets are never echoed back
	}
	return e, newSecret, nil
}

func (s *Service) save(ctx context.Context, e *Endpoint, secret string) error {
	eventsJSON, _ := json.Marshal(e.Events)
	if secret != "" {
		enc, err := s.ring.EncryptString(secret)
		if err != nil {
			return fmt.Errorf("webhook: encrypt secret: %w", err)
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE webhook_endpoints
			SET url = ?, secret_encrypted = ?, enabled = ?, events = ? WHERE id = ?`,
			e.URL, enc, boolInt(e.Enabled), string(eventsJSON), e.ID); err != nil {
			return fmt.Errorf("webhook: update endpoint: %w", err)
		}
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE webhook_endpoints
		SET url = ?, enabled = ?, events = ? WHERE id = ?`,
		e.URL, boolInt(e.Enabled), string(eventsJSON), e.ID); err != nil {
		return fmt.Errorf("webhook: update endpoint: %w", err)
	}
	return nil
}

// Delete removes an endpoint (deliveries cascade; the event log remains).
func (s *Service) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM webhook_endpoints WHERE id = ?`, id)
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

// List returns all endpoints oldest first, with per-endpoint delivery stats.
func (s *Service) List(ctx context.Context) ([]EndpointWithStats, error) {
	rows, err := s.db.QueryContext(ctx, endpointColumns+` FROM webhook_endpoints ORDER BY created_at`)
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
	statRows, err := s.db.QueryContext(ctx, `SELECT endpoint_id, status, COUNT(*)
		FROM webhook_deliveries GROUP BY endpoint_id, status`)
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
	res, err := s.db.ExecContext(ctx, `UPDATE webhook_deliveries
		SET status = 'pending', attempts = 0, next_attempt_at = ?, last_error = '', updated_at = ?
		WHERE id = ? AND endpoint_id = ?`,
		s.now().UTC().Format(time.RFC3339Nano), s.now().UTC().Format(time.RFC3339Nano),
		deliveryID, endpointID)
	if err != nil {
		return fmt.Errorf("webhook: redeliver: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeNotFound, "delivery %s not found for endpoint %s", deliveryID, endpointID)
	}
	return nil
}

const endpointColumns = `SELECT id, url, enabled, events, created_at`

func scanEndpoint(row rowScanner) (*Endpoint, error) {
	var (
		e          Endpoint
		enabled    int
		events     string
		createdStr string
	)
	if err := row.Scan(&e.ID, &e.URL, &enabled, &events, &createdStr); err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.E(domain.CodeNotFound, "webhook endpoint not found")
		}
		return nil, err
	}
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

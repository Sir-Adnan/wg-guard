package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
)

// env is a temp-DB test environment with a key ring.
type env struct {
	db   *database.DB
	ring *secrets.KeyRing
	svc  *Service
	rec  *Recorder
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "w.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	return &env{db: db, ring: ring, svc: NewService(db, ring), rec: NewRecorder()}
}

func TestSignRoundTrip(t *testing.T) {
	secret := "whsec-secret"
	body := []byte(`{"id":"evt1","type":"user.created"}`)
	ts := int64(1690000000)
	sig := Sign(secret, ts, body)
	if !Verify(secret, ts, body, sig) {
		t.Fatalf("signature does not verify: %s", sig)
	}
	if Verify("wrong", ts, body, sig) || Verify(secret, ts+1, body, sig) {
		t.Fatal("wrong secret or timestamp must not verify")
	}
}

func TestRecordTxFansOutToSubscribedEndpoints(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sub, _, err := e.svc.Create(ctx, "https://hooks.example/wg", []string{EventUserCreated, EventUserDisabled}, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = sub
	other, _, err := e.svc.Create(ctx, "https://hooks.example/other", []string{EventDeviceCreated}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE webhook_endpoints SET enabled = 0 WHERE id = ?`, other.ID); err != nil {
		t.Fatal(err)
	}

	err = e.db.WithTx(ctx, func(tx *sql.Tx) error {
		return e.rec.RecordTx(tx, EventUserCreated, map[string]any{"user_id": "u1", "username": "ann"})
	})
	if err != nil {
		t.Fatal(err)
	}
	// Second event: subscribed endpoint does not receive it.
	err = e.db.WithTx(ctx, func(tx *sql.Tx) error {
		return e.rec.RecordTx(tx, EventDeviceCreated, map[string]any{"device_id": "d1"})
	})
	if err != nil {
		t.Fatal(err)
	}

	var events, deliveries int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM webhook_events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 2 {
		t.Fatalf("want 2 events, got %d", events)
	}
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries`).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if deliveries != 1 {
		t.Fatalf("want 1 delivery (subscribed+enabled only), got %d", deliveries)
	}
	var payload string
	if err := e.db.QueryRow(`SELECT payload FROM webhook_deliveries`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != `{"user_id":"u1","username":"ann"}` {
		t.Fatalf("payload mismatch: %s", payload)
	}
}

func TestTenantFanoutAndScopedReceipts(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, statement := range []string{
		`INSERT INTO resellers (id, slug, created_at, updated_at) VALUES ('r1','r1','now','now'),('r2','r2','now','now')`,
		`INSERT INTO users (id, username, reseller_id, created_at, updated_at) VALUES
		 ('u1','tenant-one','r1','now','now'),('u2','tenant-two','r2','now','now'),('u3','operator',NULL,'now','now')`,
	} {
		if _, err := e.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	global, _, err := e.svc.Create(ctx, "https://global.example/hook", []string{EventUserCreated, EventNodeStarted}, "")
	if err != nil {
		t.Fatal(err)
	}
	ownerWide, _, err := e.svc.CreateOwner(ctx, true, "https://owner.example/hook", []string{EventUserCreated, EventNodeStarted}, "")
	if err != nil {
		t.Fatal(err)
	}
	r1, _, err := e.svc.CreateFor(ctx, strPtr("r1"), "https://r1.example/hook", []string{EventUserCreated}, "")
	if err != nil {
		t.Fatal(err)
	}
	r2, _, err := e.svc.CreateFor(ctx, strPtr("r2"), "https://r2.example/hook", []string{EventUserCreated}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.CreateFor(ctx, strPtr("r1"), "https://r1.example/node", []string{EventNodeStarted}, ""); domain.CodeOf(err) != domain.CodeForbidden {
		t.Fatalf("reseller node event accepted: %v", err)
	}
	for _, id := range []string{"u1", "u2", "u3", "unknown"} {
		if err := e.db.WithTx(ctx, func(tx *sql.Tx) error {
			return e.rec.RecordTx(tx, EventUserCreated, map[string]any{"user_id": id, "username": id, "reseller_id": "r2"})
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.rec.Emit(ctx, e.db, EventNodeStarted, map[string]any{"node_id": "node"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		want int
	}{{global.ID, 3}, {ownerWide.ID, 5}, {r1.ID, 1}, {r2.ID, 1}} {
		var got int
		if err := e.db.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries WHERE endpoint_id = ?`, tc.id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("endpoint %s received %d, want %d", tc.id, got, tc.want)
		}
	}
	if _, err := e.svc.GetFor(ctx, "r2", r1.ID); domain.CodeOf(err) != domain.CodeNotFound {
		t.Fatalf("foreign GetFor: %v", err)
	}
	disabled := false
	if _, _, err := e.svc.UpdateFor(ctx, "r2", r1.ID, EndpointUpdate{Enabled: &disabled}); domain.CodeOf(err) != domain.CodeNotFound {
		t.Fatalf("foreign UpdateFor: %v", err)
	}
	if err := e.svc.DeleteFor(ctx, "r2", r1.ID); domain.CodeOf(err) != domain.CodeNotFound {
		t.Fatalf("foreign DeleteFor: %v", err)
	}
	if got, err := e.svc.ListFor(ctx, "r1"); err != nil || len(got) != 1 || got[0].ID != r1.ID {
		t.Fatalf("ListFor = %v, %v", got, err)
	}
	page, next, err := e.svc.Receipts(ctx, strPtr("r1"), r1.ID, 1, "", "")
	if err != nil || len(page) != 1 || next != "" {
		t.Fatalf("receipt page = %v %q %v", page, next, err)
	}
	if _, _, err := e.svc.Receipts(ctx, strPtr("r2"), r1.ID, 1, "", ""); domain.CodeOf(err) != domain.CodeNotFound {
		t.Fatalf("foreign receipts: %v", err)
	}
	if _, err := e.svc.ReceiptByID(ctx, strPtr("r2"), r1.ID, page[0].ID); domain.CodeOf(err) != domain.CodeNotFound {
		t.Fatalf("foreign receipt: %v", err)
	}
	if err := e.svc.RedeliverFor(ctx, "r2", r1.ID, page[0].ID); domain.CodeOf(err) != domain.CodeNotFound {
		t.Fatalf("foreign redelivery: %v", err)
	}
}

func TestWorkerSkipsLegacyCrossTenantDelivery(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, statement := range []string{
		`INSERT INTO resellers (id, slug, created_at, updated_at) VALUES ('r1','r1','now','now'),('r2','r2','now','now')`,
		`INSERT INTO users (id, username, reseller_id, created_at, updated_at) VALUES ('u1','tenant-one','r1','now','now')`,
	} {
		if _, err := e.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	ep, _, err := e.svc.CreateFor(ctx, strPtr("r2"), "https://r2.example/hook", []string{EventUserCreated}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.WithTx(ctx, func(tx *sql.Tx) error {
		return e.rec.RecordTx(tx, EventUserCreated, map[string]any{"user_id": "u1"})
	}); err != nil {
		t.Fatal(err)
	}
	var eventID string
	if err := e.db.QueryRow(`SELECT id FROM webhook_events`).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO webhook_deliveries
		(id, endpoint_id, event_id, event_type, payload, status, attempts, next_attempt_at, last_error, created_at, updated_at)
		VALUES ('bad', ?, ?, ?, '{}', 'pending', 0, '2020-01-01T00:00:00Z', '', '2020-01-01T00:00:00Z', '2020-01-01T00:00:00Z')`, ep.ID, eventID, EventUserCreated); err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(e.db, e.ring, nil, nil)
	if report, err := worker.Pass(ctx); err != nil || report.Attempted != 0 {
		t.Fatalf("cross-tenant delivery attempted: %+v %v", report, err)
	}
	if _, err := e.svc.ReceiptByID(ctx, strPtr("r2"), ep.ID, "bad"); domain.CodeOf(err) != domain.CodeNotFound {
		t.Fatalf("cross-tenant receipt visible: %v", err)
	}
	if err := e.svc.RedeliverFor(ctx, "r2", ep.ID, "bad"); domain.CodeOf(err) != domain.CodeNotFound {
		t.Fatalf("cross-tenant redelivery: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestResellerDeliveryUsesRestrictedClient(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, statement := range []string{
		`INSERT INTO resellers (id, slug, created_at, updated_at) VALUES ('r1','r1','now','now')`,
		`INSERT INTO users (id, username, reseller_id, created_at, updated_at) VALUES ('u1','tenant-one','r1','now','now')`,
	} {
		if _, err := e.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := e.svc.CreateFor(ctx, strPtr("r1"), "https://receiver.example/hook", []string{EventUserCreated}, "test-secret"); err != nil {
		t.Fatal(err)
	}
	if err := e.rec.Emit(ctx, e.db, EventUserCreated, map[string]any{"user_id": "u1", "username": "tenant-one"}); err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(e.db, e.ring, nil, nil)
	called := 0
	worker.publicClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called++
		if r.URL.Host != "receiver.example" || r.Header.Get("X-WG-Delivery") == "" {
			t.Errorf("unexpected tenant request: %s", r.URL.Host)
		}
		body, _ := io.ReadAll(r.Body)
		var event struct {
			ID   string `json:"id"`
			Data struct {
				UserID string `json:"user_id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &event); err != nil || event.ID == "" || event.Data.UserID != "u1" {
			t.Errorf("tenant body: %v %v", event, err)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	report, err := worker.Pass(ctx)
	if err != nil || report.Delivered != 1 || called != 1 {
		t.Fatalf("restricted delivery: %+v %d %v", report, called, err)
	}
}

func TestValidateURL(t *testing.T) {
	for _, ok := range []string{"https://hooks.example/x", "http://10.0.0.5:9000/hook"} {
		if err := ValidateURL(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "ftp://x", "https://user:pass@hooks.example/", "not a url", "https://"} {
		if err := ValidateURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := ValidateEvents([]string{"user.created", "nope"}); err == nil {
		t.Error("unknown event accepted")
	}
}

func TestResellerWebhookEgressPolicy(t *testing.T) {
	for _, raw := range []string{
		"http://hooks.example/hook", "https://127.0.0.1/hook",
		"https://10.4.0.1/hook", "https://100.64.0.1/hook",
		"https://[::ffff:127.0.0.1]/hook", "https://user:pass@hooks.example/hook",
	} {
		if err := ValidateResellerURL(raw); err == nil {
			t.Errorf("unsafe reseller destination accepted: %s", raw)
		}
	}
	if err := ValidateResellerURL("https://hooks.example/hook"); err != nil {
		t.Fatalf("public hostname rejected: %v", err)
	}
	for _, address := range []string{"127.0.0.1:443", "[::1]:443", "192.168.1.10:443", "100.64.0.1:443"} {
		if conn, err := publicWebhookDial(context.Background(), "tcp", address); err == nil {
			conn.Close()
			t.Errorf("private dial accepted: %s", address)
		}
	}
	if !publicWebhookAddr(netip.MustParseAddr("1.1.1.1")) || !publicWebhookAddr(netip.MustParseAddr("2606:4700:4700::1111")) {
		t.Fatal("public IP rejected")
	}
	if publicWebhookAddr(netip.MustParseAddr("2001:db8::1")) {
		t.Fatal("documentation IPv6 accepted")
	}
	w := NewWorker(nil, nil, nil, nil)
	if err := w.publicClient.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect policy = %v", err)
	}
}

// TestWorkerDeliversSignedEnvelope runs a full pass against a local receiver:
// the envelope shape, the event/delivery headers, and the HMAC signature.
func TestWorkerDeliversSignedEnvelope(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	type received struct {
		headers http.Header
		body    []byte
	}
	var mu sync.Mutex
	var got []received
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, received{r.Header.Clone(), b})
		mu.Unlock()
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)

	secret := "whsec-test-0123456789abcdef"
	if _, _, err := e.svc.Create(ctx, srv.URL, []string{EventUserCreated}, secret); err != nil {
		t.Fatal(err)
	}
	if err := e.db.WithTx(ctx, func(tx *sql.Tx) error {
		return e.rec.RecordTx(tx, EventUserCreated, map[string]any{"user_id": "u9", "username": "zoe"})
	}); err != nil {
		t.Fatal(err)
	}

	w := NewWorker(e.db, e.ring, nil, nil)
	rep, err := w.Pass(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Attempted != 1 || rep.Delivered != 1 || rep.Failed != 0 || rep.Dead != 0 {
		t.Fatalf("report: %+v", rep)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("receiver got %d requests", len(got))
	}
	r := got[0]
	if r.headers.Get("X-WG-Event") != EventUserCreated {
		t.Fatalf("event header: %s", r.headers.Get("X-WG-Event"))
	}
	if r.headers.Get("X-WG-Delivery") == "" {
		t.Fatal("delivery header missing")
	}
	var env map[string]any
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if env["type"] != EventUserCreated || env["data"].(map[string]any)["username"] != "zoe" || env["id"] == "" {
		t.Fatalf("envelope shape: %v", env)
	}
	// Signature: t=<ts>,v1=<hmac(secret, "<ts>.<body>")> over exactly this body.
	var ts int64
	if _, err := fmt.Sscanf(r.headers.Get("X-WG-Signature"), "t=%d,v1=", &ts); err != nil {
		t.Fatalf("signature header: %s", r.headers.Get("X-WG-Signature"))
	}
	if !Verify(secret, ts, r.body, r.headers.Get("X-WG-Signature")) {
		t.Fatal("signature does not verify over the delivered body")
	}

	var status string
	if err := e.db.QueryRow(`SELECT status FROM webhook_deliveries`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "delivered" {
		t.Fatalf("delivery status: %s", status)
	}
}

func TestWorkerRetriesThenDeadLetters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)

	if _, _, err := e.svc.Create(ctx, srv.URL, []string{EventUserUpdated}, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.db.WithTx(ctx, func(tx *sql.Tx) error {
		return e.rec.RecordTx(tx, EventUserUpdated, map[string]any{"user_id": "u1"})
	}); err != nil {
		t.Fatal(err)
	}

	w := NewWorker(e.db, e.ring, nil, nil)
	rep, err := w.Pass(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failed != 1 || rep.Dead != 0 {
		t.Fatalf("first failure report: %+v", rep)
	}
	var attempts int
	var nextAt sql.NullString
	if err := e.db.QueryRow(`SELECT attempts, next_attempt_at FROM webhook_deliveries`).Scan(&attempts, &nextAt); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || !nextAt.Valid {
		t.Fatalf("attempt/backoff not recorded: %d %v", attempts, nextAt)
	}
	// Backoff must be in the future (≈30s), so a fresh pass finds nothing due.
	rep, err = w.Pass(ctx)
	if err != nil || rep.Attempted != 0 {
		t.Fatalf("not-yet-due delivery retried: %+v %v", rep, err)
	}
	// Force the row due with attempts = max-1: the next failure dead-letters.
	if _, err := e.db.Exec(`UPDATE webhook_deliveries SET attempts = 11, next_attempt_at = ?`,
		time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	rep, err = w.Pass(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Dead != 1 {
		t.Fatalf("expected dead-letter: %+v", rep)
	}
	var status string
	var attemptsNow int
	if err := e.db.QueryRow(`SELECT status, attempts FROM webhook_deliveries`).Scan(&status, &attemptsNow); err != nil {
		t.Fatal(err)
	}
	if status != "dead" || attemptsNow != 12 {
		t.Fatalf("dead-letter state: %s %d", status, attemptsNow)
	}
}

func TestRedeliverResetsDelivery(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)
	if _, _, err := e.svc.Create(ctx, srv.URL, []string{EventUserExpired}, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.db.WithTx(ctx, func(tx *sql.Tx) error {
		return e.rec.RecordTx(tx, EventUserExpired, map[string]any{"user_id": "u1"})
	}); err != nil {
		t.Fatal(err)
	}
	w := NewWorker(e.db, e.ring, nil, nil)
	if _, err := w.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE webhook_deliveries SET status='dead', attempts=12`); err != nil {
		t.Fatal(err)
	}
	var deliveryID string
	if err := e.db.QueryRow(`SELECT id FROM webhook_deliveries`).Scan(&deliveryID); err != nil {
		t.Fatal(err)
	}
	epID := ""
	if err := e.db.QueryRow(`SELECT endpoint_id FROM webhook_deliveries`).Scan(&epID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Redeliver(ctx, epID, deliveryID); err != nil {
		t.Fatal(err)
	}
	var status string
	var attempts int
	if err := e.db.QueryRow(`SELECT status, attempts FROM webhook_deliveries`).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 0 {
		t.Fatalf("redeliver must reset: %s %d", status, attempts)
	}
	// Unknown delivery is NOT_FOUND.
	if err := e.svc.Redeliver(ctx, epID, "nope"); domain.CodeOf(err) != domain.CodeNotFound {
		t.Fatalf("redeliver unknown: %v", err)
	}
}

func TestPruneRemovesEventsAndDeliveries(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, _, err := e.svc.Create(ctx, "https://hooks.example/x", []string{EventNodeStarted}, ""); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-48 * time.Hour)
	if err := e.db.WithTx(ctx, func(tx *sql.Tx) error {
		return e.rec.RecordTx(tx, EventNodeStarted, map[string]any{"v": 1})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE webhook_events SET created_at = ?`, old.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	n, err := Prune(ctx, e.db, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pruned %d events, want 1", n)
	}
	var deliveries int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries`).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if deliveries != 0 {
		t.Fatalf("deliveries must cascade: %d", deliveries)
	}
}

func TestUpdateRotatesSecret(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ep, first, err := e.svc.Create(ctx, "https://hooks.example/x", []string{EventNodeStarted}, "")
	if err != nil {
		t.Fatal(err)
	}
	if first == "" {
		t.Fatal("generated secret must be returned once")
	}
	// Rotate with an explicit secret: never echoed back.
	rotated := "my-new-secret"
	if _, out, err := e.svc.Update(ctx, ep.ID, EndpointUpdate{Secret: &rotated}); err != nil || out != "" {
		t.Fatalf("explicit secret must not echo: %q %v", out, err)
	}
	got, err := e.svc.Secret(ctx, ep)
	if err != nil {
		t.Fatal(err)
	}
	if got != rotated || got == first {
		t.Fatalf("rotation failed: %q", got)
	}
	// Generate-on-rotate returns the new plaintext once.
	if _, gen, err := e.svc.Update(ctx, ep.ID, EndpointUpdate{Secret: strPtr("")}); err != nil || gen == "" {
		t.Fatalf("generated rotation: %q %v", gen, err)
	}
}

var _ = context.Background // keep context import stable across edits

func strPtr(s string) *string { return &s }

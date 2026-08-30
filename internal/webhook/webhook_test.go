package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

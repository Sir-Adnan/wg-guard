package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/accounting"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/reseller"
)

func TestQuotaTopUpIsRecoverableAndTenantScoped(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ifc, err := e.ifaces.Create(ctx, iface.CreateInput{Name: "awg0", ListenPort: 39003, Subnet: "10.79.0.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	duration := int64(86400)
	product, err := e.plans.Create(ctx, plan.Input{Name: "Quota plan",
		InterfaceID:       domain.OptString{Set: true, Value: ifc.ID},
		TrafficLimitBytes: domain.OptInt64{Set: true, Value: 1_000_000}, DurationSeconds: &duration})
	if err != nil {
		t.Fatal(err)
	}
	send := func(token, method, path, key, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		return rec
	}
	created := send(e.plainTok, http.MethodPost, "/api/v1/purchases", "quota-purchase",
		`{"template_id":"`+product.ID+`","username":"quota-owner"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("purchase: %d %s", created.Code, created.Body.String())
	}
	uid := decodeBody(t, created)["user_id"].(string)
	if err := e.acct.SetTraffic(ctx, uid, ptrInt64(1_000_000), nil, accounting.Actor{}); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/users/" + uid + "/quota/add"
	if rec := send(e.plainTok, http.MethodPost, path, "", `{"bytes":2000000}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing key: %d", rec.Code)
	}
	if rec := send("", http.MethodPost, path, "quota-topup", `{"bytes":2000000}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous top-up: %d", rec.Code)
	}
	_, meterToken, err := e.tokens.Create(ctx, "meter-only", []string{"traffic.update"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if rec := send(meterToken, http.MethodPost, path, "quota-topup", `{"bytes":2000000}`); rec.Code != http.StatusForbidden {
		t.Fatalf("meter correction scope gained allowance control: %d", rec.Code)
	}
	if rec := send(e.plainTok, http.MethodPost, path, "quota-invalid", `{"bytes":0}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("zero top-up: %d", rec.Code)
	}
	first := send(e.plainTok, http.MethodPost, path, "quota-topup", `{"bytes":2000000}`)
	if first.Code != http.StatusOK {
		t.Fatalf("top-up: %d %s", first.Code, first.Body.String())
	}
	result := decodeBody(t, first)
	before, after := result["before"].(map[string]any), result["after"].(map[string]any)
	if result["kind"] != "quota_top_up" || before["traffic_limit_bytes"] != float64(1_000_000) ||
		after["traffic_limit_bytes"] != float64(3_000_000) ||
		before["traffic_used_rx"] != float64(1_000_000) || after["traffic_used_rx"] != float64(1_000_000) ||
		before["status"] != string(domain.UserTrafficExceeded) || after["status"] != string(domain.UserActive) {
		t.Fatalf("top-up before/after: %v", result)
	}
	lookup := send(e.plainTok, http.MethodGet, "/api/v1/operations/result", "quota-topup", "")
	if lookup.Code != http.StatusOK || decodeBody(t, lookup)["operation_id"] != result["operation_id"] {
		t.Fatalf("recover committed result: %d", lookup.Code)
	}
	_, replacementToken, err := e.tokens.Create(ctx, "replacement bot", []string{"users.update", "operations.read"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	replay := send(replacementToken, http.MethodPost, path, "quota-topup", `{"bytes":2000000}`)
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotency-Replayed") != "true" ||
		decodeBody(t, replay)["operation_id"] != result["operation_id"] {
		t.Fatalf("top-up replay: %d", replay.Code)
	}
	if rec := send(e.plainTok, http.MethodPost, path, "quota-topup", `{"bytes":3000000}`); rec.Code != http.StatusConflict ||
		errCode(t, rec) != domain.CodeIdempotencyKeyReused {
		t.Fatalf("changed payload reused key: %d", rec.Code)
	}
	if rec := send(e.plainTok, http.MethodPost, path, "quota-purchase", `{"bytes":100}`); rec.Code != http.StatusConflict {
		t.Fatalf("purchase/top-up key collision: %d", rec.Code)
	}
	var limit, used int64
	if err := e.db.QueryRowContext(ctx, `SELECT traffic_limit_bytes, traffic_used_rx FROM users WHERE id = ?`, uid).
		Scan(&limit, &used); err != nil || limit != 3_000_000 || used != 1_000_000 {
		t.Fatalf("replay changed entitlement or usage: %d/%d %v", limit, used, err)
	}

	// A journal insert failure must roll back the allowance and outbox write.
	var eventsBefore, eventsAfter int
	if err := e.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhook_events`).Scan(&eventsBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.ExecContext(ctx, `CREATE TRIGGER reject_quota_journal BEFORE INSERT ON integration_operations
		WHEN NEW.kind = 'quota_top_up' BEGIN SELECT RAISE(ABORT, 'synthetic journal failure'); END`); err != nil {
		t.Fatal(err)
	}
	if rec := send(e.plainTok, http.MethodPost, path, "quota-failed", `{"bytes":100}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("journal failure response: %d", rec.Code)
	}
	if err := e.db.QueryRowContext(ctx, `SELECT traffic_limit_bytes FROM users WHERE id = ?`, uid).Scan(&limit); err != nil || limit != 3_000_000 {
		t.Fatalf("journal failure committed allowance: %d %v", limit, err)
	}
	if err := e.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhook_events`).Scan(&eventsAfter); err != nil || eventsAfter != eventsBefore {
		t.Fatalf("journal failure committed webhook event: %d/%d %v", eventsBefore, eventsAfter, err)
	}
	if _, err := e.db.ExecContext(ctx, `DROP TRIGGER reject_quota_journal`); err != nil {
		t.Fatal(err)
	}
	if rec := send(e.plainTok, http.MethodPost, path, "quota-failed", `{"bytes":100}`); rec.Code != http.StatusOK {
		t.Fatalf("retry after rollback: %d", rec.Code)
	}

	r, err := reseller.NewService(e.db).Create(ctx, "quota-reseller", "Quota reseller",
		[]string{"purchases.create", "operations.read", "users.update"})
	if err != nil {
		t.Fatal(err)
	}
	if err := reseller.NewService(e.db).SetTemplates(ctx, r.ID, []string{product.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.ExecContext(ctx, `INSERT INTO admins
		(id, username, password_hash, role, created_at, updated_at)
		VALUES ('quota-owner-admin', 'quota-owner-admin', 'x', 'owner', 'test', 'test')`); err != nil {
		t.Fatal(err)
	}
	_, resellerToken, err := e.tokens.CreateForAdmin(ctx, "quota-owner-admin", &r.ID, "quota bot",
		[]string{"purchases.create", "operations.read", "users.update"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if rec := send(resellerToken, http.MethodPost, path, "foreign-topup", `{"bytes":100}`); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign allowance exposed: %d", rec.Code)
	}
	owned := send(resellerToken, http.MethodPost, "/api/v1/purchases", "owned-purchase",
		`{"template_id":"`+product.ID+`","username":"quota-owned"}`)
	if owned.Code != http.StatusCreated {
		t.Fatalf("reseller purchase: %d %s", owned.Code, owned.Body.String())
	}
	ownedID := decodeBody(t, owned)["user_id"].(string)
	if rec := send(resellerToken, http.MethodPost, "/api/v1/users/"+ownedID+"/quota/add", "owned-topup", `{"bytes":250}`); rec.Code != http.StatusOK {
		t.Fatalf("owned top-up: %d %s", rec.Code, rec.Body.String())
	}
	if rec := send(e.plainTok, http.MethodGet, "/api/v1/operations/result", "owned-topup", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("reseller journal exposed to owner namespace: %d", rec.Code)
	}
	if rec := send(resellerToken, http.MethodGet, "/api/v1/operations/result", "owned-topup", ""); rec.Code != http.StatusOK {
		t.Fatalf("owned journal lookup: %d", rec.Code)
	}
	if err := e.acct.SetTraffic(ctx, uid, ptrInt64(3_000_100), nil, accounting.Actor{}); err != nil {
		t.Fatal(err)
	}
	e.srv.Reconciler = failingAccessReconciler{}
	if rec := send(e.plainTok, http.MethodPost, path, "quota-runtime", `{"bytes":100}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("runtime failure must report pending reconciliation: %d", rec.Code)
	}
	if rec := send(e.plainTok, http.MethodGet, "/api/v1/operations/result", "quota-runtime", ""); rec.Code != http.StatusOK || decodeBody(t, rec)["state"] != "committed" {
		t.Fatalf("runtime failure lost committed result: %d", rec.Code)
	}
	e.srv.Reconciler = nil
	if rec := send(e.plainTok, http.MethodPost, path, "quota-runtime", `{"bytes":100}`); rec.Code != http.StatusOK || rec.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("runtime recovery replay: %d", rec.Code)
	}
	if err := e.db.QueryRowContext(ctx, `SELECT traffic_limit_bytes FROM users WHERE id = ?`, uid).Scan(&limit); err != nil || limit != 3_000_200 {
		t.Fatalf("runtime replay duplicated allowance: %d %v", limit, err)
	}
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- send(e.plainTok, http.MethodPost, path, "quota-concurrent", `{"bytes":1000}`)
		}()
	}
	wg.Wait()
	close(results)
	var concurrentID any
	for rec := range results {
		if rec.Code != http.StatusOK {
			t.Fatalf("concurrent identical top-up: %d", rec.Code)
		}
		id := decodeBody(t, rec)["operation_id"]
		if concurrentID != nil && id != concurrentID {
			t.Fatal("concurrent retries returned different operations")
		}
		concurrentID = id
	}
	if err := e.db.QueryRowContext(ctx, `SELECT traffic_limit_bytes FROM users WHERE id = ?`, uid).Scan(&limit); err != nil || limit != 3_001_200 {
		t.Fatalf("concurrent retries changed allowance more than once: %d %v", limit, err)
	}
}

func ptrInt64(v int64) *int64 { return &v }

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/reseller"
)

func TestDirectPurchaseOwnerOnlyAndRecoverable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.ifaces.Create(ctx, iface.CreateInput{Name: "awg0", ListenPort: 39001, Subnet: "10.77.0.0/24"}); err != nil {
		t.Fatal(err)
	}
	send := func(token, key, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/purchases", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		return rec
	}
	body := `{"username":"direct-bot","entitlement":{"traffic_limit_bytes":7000000000,"duration_seconds":2592000,"device_limit":2,"start_policy":"first_connection"}}`
	first := send(e.plainTok, "direct-order-1", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("owner direct purchase: %d %s", first.Code, first.Body.String())
	}
	result := decodeBody(t, first)
	if result["user_id"] == nil || result["device_id"] == nil || result["template_id"] != nil {
		t.Fatalf("direct result has wrong identifiers: %v", result)
	}
	u, err := e.users.Get(ctx, result["user_id"].(string))
	if err != nil || u.TrafficLimitBytes == nil || *u.TrafficLimitBytes != 7_000_000_000 || u.TemplateID != nil ||
		u.Status != domain.UserWaitingFirstConnection {
		t.Fatalf("direct terms not applied: %+v %v", u, err)
	}
	if replay := send(e.plainTok, "direct-order-1", body); replay.Code != http.StatusCreated ||
		replay.Header().Get("Idempotency-Replayed") != "true" ||
		decodeBody(t, replay)["operation_id"] != result["operation_id"] {
		t.Fatalf("direct replay: %d %s", replay.Code, replay.Body.String())
	}
	if bad := send(e.plainTok, "direct-order-1", strings.Replace(body, "7000000000", "8000000000", 1)); bad.Code != http.StatusConflict || errCode(t, bad) != domain.CodeIdempotencyKeyReused {
		t.Fatalf("changed direct order reused key: %d", bad.Code)
	}
	if bad := send(e.plainTok, "missing-device-count", `{"entitlement":{"traffic_limit_bytes":1000,"duration_seconds":86400}}`); bad.Code != http.StatusBadRequest {
		t.Fatalf("missing finite term accepted: %d", bad.Code)
	}
	if bad := send(e.plainTok, "both", `{"template_id":"some-template","entitlement":{"traffic_limit_bytes":1000,"duration_seconds":86400,"device_limit":1}}`); bad.Code != http.StatusBadRequest {
		t.Fatalf("ambiguous terms accepted: %d", bad.Code)
	}
	r, err := reseller.NewService(e.db).Create(ctx, "direct-north", "North", []string{"purchases.create"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.ExecContext(ctx, `INSERT INTO admins (id, username, password_hash, role, created_at, updated_at)
		VALUES ('direct-owner', 'owner-direct', 'x', 'owner', 'test', 'test')`); err != nil {
		t.Fatal(err)
	}
	_, resellerToken, err := e.tokens.CreateForAdmin(ctx, "direct-owner", &r.ID, "reseller bot",
		[]string{"purchases.create"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if denied := send(resellerToken, "reseller-direct-order", body); denied.Code != http.StatusForbidden {
		t.Fatalf("reseller bypassed assigned-template policy: %d %s", denied.Code, denied.Body.String())
	}
}

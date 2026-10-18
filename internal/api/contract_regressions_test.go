package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Optional bodies may be absent or blank; a bad body is rejected before any
// change, and the response is a single JSON document.
func TestOptionalBodiesAndDisableReason(t *testing.T) {
	e := newEnv(t)
	rec := e.do("POST", "/api/v1/users", `{"username":"opt-body"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	id := decodeBody(t, rec)["id"].(string)
	if rec := e.do("POST", "/api/v1/users/"+id+"/disable", `{"reason":"expired"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("lifecycle reason must be rejected: %d", rec.Code)
	}
	if u, _ := e.users.Get(context.Background(), id); u.Status == "disabled" {
		t.Fatal("a rejected disable must not change the user")
	}
	for _, body := range []string{"", "   "} {
		req := httptest.NewRequest("POST", "/api/v1/users/"+id+"/disable", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+e.plainTok)
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		var got map[string]any
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got["status"] != "disabled" {
			t.Fatalf("disable with body %q: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	if rec := e.do("POST", "/api/v1/users/"+id+"/disable", `{"reason":`); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: %d", rec.Code)
	}
}

func TestUserTagsAreAlwaysAnArray(t *testing.T) {
	e := newEnv(t)
	rec := e.do("POST", "/api/v1/users", `{"username":"no-tags"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"tags":[]`) {
		t.Fatalf("tags must serialize as []: %s", rec.Body.String())
	}
}

func TestSettingsPatchIsAtomic(t *testing.T) {
	e := newEnv(t)
	before, err := e.reg.GetInt(context.Background(), "network.mtu")
	if err != nil {
		t.Fatal(err)
	}
	rec := e.do("PATCH", "/api/v1/settings", `{"network.mtu": 1380, "no.such.setting": 1}`)
	if rec.Code < 400 {
		t.Fatalf("unknown key must fail: %d", rec.Code)
	}
	if after, _ := e.reg.GetInt(context.Background(), "network.mtu"); after != before {
		t.Fatalf("failed PATCH applied network.mtu: %d → %d", before, after)
	}
	rec = e.do("PATCH", "/api/v1/settings", `{"network.mtu": 1380}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid PATCH: %d %s", rec.Code, rec.Body.String())
	}
}

func TestBulkCreateStartIndexDefaultsToOne(t *testing.T) {
	e := newEnv(t)
	rec := e.do("POST", "/api/v1/users/bulk", `{"count": 2, "prefix": "dflt"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bulk: %d %s", rec.Code, rec.Body.String())
	}
	first := decodeBody(t, rec)["created"].([]any)[0].(map[string]any)
	if first["username"] != "dflt001" {
		t.Fatalf("omitted start_index must start at 1 (OpenAPI default): %v", first["username"])
	}
}

func TestWebhookGetReportsDeliveryStats(t *testing.T) {
	e := newEnv(t)
	rec := e.do("POST", "/api/v1/webhooks", `{"url": "https://example.test/hook", "events": ["user.created"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create webhook: %d %s", rec.Code, rec.Body.String())
	}
	id := decodeBody(t, rec)["id"].(string)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := e.db.Exec(`INSERT INTO webhook_events (id, event_type, payload, created_at) VALUES ('ev1', 'user.created', '{}', ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO webhook_deliveries (id, endpoint_id, event_id, event_type, payload, status, created_at, updated_at)
		VALUES ('d1', ?, 'ev1', 'user.created', '{}', 'dead', ?, ?)`, id, now, now); err != nil {
		t.Fatal(err)
	}
	stats := decodeBody(t, e.do("GET", "/api/v1/webhooks/"+id, ""))["stats"].(map[string]any)
	if stats["dead"] != float64(1) {
		t.Fatalf("GET must report the endpoint's counters like the list: %v", stats)
	}
}

func TestIdempotentReplayKeepsContentType(t *testing.T) {
	e := newEnv(t)
	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/users", strings.NewReader(`{"username":"replayed"}`))
		req.Header.Set("Authorization", "Bearer "+e.plainTok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "replay-content-type")
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		return rec
	}
	first, replay := send(), send()
	if first.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay: %d / %q", first.Code, replay.Header().Get("Idempotency-Replayed"))
	}
	if got, want := replay.Header().Get("Content-Type"), first.Header().Get("Content-Type"); got != want {
		t.Fatalf("replayed Content-Type %q, want %q", got, want)
	}
}

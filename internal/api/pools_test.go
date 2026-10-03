package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPoolCapacityOverflowAndAtomicPurchaseFailure(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodPost, "/api/v1/interfaces", `{"name":"awg0","ipv4_pools":["10.77.0.0/29","10.77.1.0/29"]}`)
	if rec.Code != 201 {
		t.Fatalf("create pools: %d %s", rec.Code, rec.Body.String())
	}
	var profile struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &profile)
	rec = e.do(http.MethodGet, "/api/v1/interfaces/"+profile.ID+"/capacity", "")
	if rec.Code != 200 {
		t.Fatal("capacity inaccessible")
	}
	var capacity struct{ Capacity, Free int }
	_ = json.Unmarshal(rec.Body.Bytes(), &capacity)
	if capacity.Capacity != 10 || capacity.Free != 10 {
		t.Fatal("wrong capacity")
	}
	// Eleven configs cannot fit ten free addresses: no account or devices commit.
	send := func(key, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/purchases", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+e.plainTok)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key)
		r := httptest.NewRecorder()
		e.handler.ServeHTTP(r, request)
		return r
	}
	rec = send("full-batch", `{"username":"failed-buy","device_count":11,"entitlement":{"traffic_limit_bytes":1000000000,"duration_seconds":86400,"device_limit":11}}`)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "DEVICE_POOL_EXHAUSTED") {
		t.Fatalf("full purchase: %d", rec.Code)
	}
	for _, table := range []string{"users", "devices", "integration_operations", "sub_links"} {
		var n int
		_ = e.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
		if n != 0 {
			t.Fatalf("partial purchase retained %s", table)
		}
	}
	rec = send("successful", `{"username":"successful","device_count":6,"entitlement":{"traffic_limit_bytes":1000000000,"duration_seconds":86400,"device_limit":6}}`)
	if rec.Code != 201 {
		t.Fatalf("overflow purchase: %d", rec.Code)
	}
	var purchase struct {
		UserID string `json:"user_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &purchase)
	rec = e.do(http.MethodDelete, "/api/v1/users/"+purchase.UserID, "")
	if rec.Code != 200 {
		t.Fatal("account delete failed")
	}
	rec = e.do(http.MethodGet, "/api/v1/interfaces/"+profile.ID+"/capacity", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &capacity)
	if capacity.Free != 10 {
		t.Fatal("API account deletion retained allocated addresses")
	}
	var pending int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM retired_peer_keys`).Scan(&pending)
	if pending != 6 {
		t.Fatal("API deletion dropped removal intent")
	}
}

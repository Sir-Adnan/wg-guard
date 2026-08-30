package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

func TestUserLifecycleViaAPI(t *testing.T) {
	e := newEnv(t)
	// Create: immediate, with independent speed limits.
	rec := e.do("POST", "/api/v1/users", `{
		"username": "alice", "duration_seconds": 86400,
		"traffic_limit_bytes": 1000000,
		"speed_limit_down_kbps": 10240, "speed_limit_up_kbps": 2048,
		"device_limit": 2, "metadata": {"source": "test"}
	}`)
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	u := decodeBody(t, rec)
	id := u["id"].(string)
	if u["status"] != "active" || u["speed_limit_down_kbps"].(float64) != 10240 || u["speed_limit_up_kbps"].(float64) != 2048 {
		t.Fatalf("create shape: %v", u)
	}
	if u["traffic_used_total"].(float64) != 0 {
		t.Fatal("new user has zero usage")
	}

	// PATCH: tri-state — clear upload (null), keep download, set new traffic limit.
	rec = e.do("PATCH", "/api/v1/users/"+id, `{
		"speed_limit_up_kbps": null, "display_name": "Alice A",
		"traffic_limit_bytes": 2000000
	}`)
	if rec.Code != 200 {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	u = decodeBody(t, rec)
	if u["speed_limit_up_kbps"] != nil {
		t.Fatalf("null must clear upload: %v", u["speed_limit_up_kbps"])
	}
	if u["speed_limit_down_kbps"].(float64) != 10240 {
		t.Fatalf("absent must keep download: %v", u["speed_limit_down_kbps"])
	}
	if u["traffic_limit_bytes"].(float64) != 2000000 {
		t.Fatalf("traffic limit update: %v", u["traffic_limit_bytes"])
	}
	// Absent field keeps display name on a second patch.
	rec = e.do("PATCH", "/api/v1/users/"+id, `{"note": "hello"}`)
	u = decodeBody(t, rec)
	if u["display_name"] != "Alice A" {
		t.Fatalf("absent field must be untouched: %v", u["display_name"])
	}

	// Renew extends the expiry.
	rec = e.do("POST", "/api/v1/users/"+id+"/renew", `{"mode": "from_expiration"}`)
	if rec.Code != 200 {
		t.Fatalf("renew: %d %s", rec.Code, rec.Body.String())
	}

	// Traffic add → total reflects; set absolute; reset.
	rec = e.do("POST", "/api/v1/users/"+id+"/traffic/add", `{"rx_bytes": 500, "tx_bytes": 250}`)
	u = decodeBody(t, rec)
	if u["traffic_used_total"].(float64) != 750 {
		t.Fatalf("traffic add: %v", u["traffic_used_total"])
	}
	// Adding past the (patched) limit trips the account (edge-triggered).
	rec = e.do("POST", "/api/v1/users/"+id+"/traffic/add", `{"rx_bytes": 2999999}`)
	u = decodeBody(t, rec)
	if u["status"] != "traffic_exceeded" || u["disable_reason"] != "traffic_limit" {
		t.Fatalf("quota trip: %v %v", u["status"], u["disable_reason"])
	}
	// Reset unblocks in one op.
	rec = e.do("POST", "/api/v1/users/"+id+"/traffic/reset", "")
	u = decodeBody(t, rec)
	if u["status"] != "active" || u["traffic_used_total"].(float64) != 0 {
		t.Fatalf("reset: %v %v", u["status"], u["traffic_used_total"])
	}
	// Set absolute below limit.
	rec = e.do("POST", "/api/v1/users/"+id+"/traffic/set", `{"rx_bytes": 10, "tx_bytes": 5}`)
	if u = decodeBody(t, rec); u["traffic_used_total"].(float64) != 15 {
		t.Fatalf("traffic set: %v", u["traffic_used_total"])
	}
	// Bad set: negative rejected.
	if rec = e.do("POST", "/api/v1/users/"+id+"/traffic/set", `{"rx_bytes": -5}`); rec.Code != 400 {
		t.Fatalf("negative set: %d", rec.Code)
	}

	// Stats endpoint.
	rec = e.do("GET", "/api/v1/users/"+id+"/stats", "")
	st := decodeBody(t, rec)
	if st["traffic_used_total"].(float64) != 15 || st["devices"].(float64) != 0 {
		t.Fatalf("user stats: %v", st)
	}

	// Disable/enable.
	rec = e.do("POST", "/api/v1/users/"+id+"/disable", `{"reason": "manual"}`)
	if u = decodeBody(t, rec); u["status"] != "disabled" || u["enabled"] != false {
		t.Fatalf("disable: %v", u)
	}
	rec = e.do("POST", "/api/v1/users/"+id+"/enable", "")
	if u = decodeBody(t, rec); u["status"] != "active" {
		t.Fatalf("enable: %v", u)
	}

	// List + filters.
	rec = e.do("GET", "/api/v1/users?limit=10&username=ali", "")
	page := decodeBody(t, rec)
	items := page["items"].([]any)
	if len(items) != 1 || page["next_cursor"] != "" {
		t.Fatalf("filtered list: %v", page)
	}

	// Delete → user gone, list empty.
	if rec = e.do("DELETE", "/api/v1/users/"+id, ""); rec.Code != 200 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec = e.do("GET", "/api/v1/users/"+id, ""); rec.Code != 404 || errCode(t, rec) != "USER_NOT_FOUND" {
		t.Fatalf("deleted user: %d %s", rec.Code, rec.Body.String())
	}
}

func TestUserListPaginationViaAPI(t *testing.T) {
	e := newEnv(t)
	for i := 1; i <= 5; i++ {
		body := `{"username": "user` + string(rune('0'+i)) + `", "start_policy": "first_connection"}`
		if rec := e.do("POST", "/api/v1/users", body); rec.Code != 201 {
			t.Fatalf("seed %d: %d", i, rec.Code)
		}
	}
	// Walk pages of 2.
	seen := 0
	cursor := ""
	for {
		path := "/api/v1/users?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		rec := e.do("GET", path, "")
		if rec.Code != 200 {
			t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
		}
		page := decodeBody(t, rec)
		items := page["items"].([]any)
		seen += len(items)
		cursor, _ = page["next_cursor"].(string)
		if cursor == "" {
			break
		}
		if len(items) != 2 {
			t.Fatalf("page must be full until end: %d", len(items))
		}
	}
	if seen != 5 {
		t.Fatalf("walked %d users, want 5", seen)
	}
	// status filter via API.
	rec := e.do("GET", "/api/v1/users?status=waiting_first_connection", "")
	page := decodeBody(t, rec)
	if len(page["items"].([]any)) != 5 {
		t.Fatalf("status filter: %v", page)
	}
}

func TestBulkViaAPI(t *testing.T) {
	e := newEnv(t)
	rec := e.do("POST", "/api/v1/users/bulk", `{
		"count": 3, "prefix": "gs", "start_index": 1, "width": 3,
		"duration_seconds": 86400, "speed_limit_down_kbps": 1024
	}`)
	if rec.Code != 201 {
		t.Fatalf("bulk: %d %s", rec.Code, rec.Body.String())
	}
	res := decodeBody(t, rec)
	created := res["created"].([]any)
	if len(created) != 3 || res["skipped"].(float64) != 0 {
		t.Fatalf("bulk result: %v", res)
	}
	first := created[0].(map[string]any)
	if first["username"] != "gs001" {
		t.Fatalf("bulk names: %v", first)
	}

	// Bulk action over the real (UUID) ids.
	rec = e.do("GET", "/api/v1/users?limit=10", "")
	items := decodeBody(t, rec)["items"].([]any)
	ids := []string{}
	for _, it := range items {
		ids = append(ids, it.(map[string]any)["id"].(string))
	}
	body, _ := json.Marshal(map[string]any{
		"action": "disable", "user_ids": ids, "params": map[string]string{"reason": "manual"},
	})
	rec = e.do("POST", "/api/v1/users/bulk-action", string(body))
	res = decodeBody(t, rec)
	results := res["results"].([]any)
	if len(results) != len(ids) {
		t.Fatalf("bulk-action results: %v", res)
	}
	for _, r := range results {
		if !r.(map[string]any)["ok"].(bool) {
			t.Fatalf("bulk-action item failed: %v", r)
		}
	}
	// Unknown action is INVALID_REQUEST on that item.
	rec = e.do("POST", "/api/v1/users/bulk-action", `{"action": "explode", "user_ids": ["x"]}`)
	res = decodeBody(t, rec)
	r0 := res["results"].([]any)[0].(map[string]any)
	if r0["ok"] != false || r0["error"] != "INVALID_REQUEST" {
		t.Fatalf("unknown action: %v", r0)
	}
}

func TestIdempotency(t *testing.T) {
	e := newEnv(t)
	body := `{"username": "idem-user", "duration_seconds": 60}`
	req := httptest.NewRequest("POST", "/api/v1/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.plainTok)
	req.Header.Set("Idempotency-Key", "bot-retry-1")
	r1 := httptest.NewRecorder()
	e.handler.ServeHTTP(r1, req)
	if r1.Code != 201 {
		t.Fatalf("first: %d %s", r1.Code, r1.Body.String())
	}
	id1 := decodeBody(t, r1)["id"].(string)

	// Same key + same body → replay, same user id, no duplicate.
	req2 := httptest.NewRequest("POST", "/api/v1/users", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+e.plainTok)
	req2.Header.Set("Idempotency-Key", "bot-retry-1")
	r2 := httptest.NewRecorder()
	e.handler.ServeHTTP(r2, req2)
	if r2.Code != 201 {
		t.Fatalf("replay: %d", r2.Code)
	}
	if r2.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("replay flag missing")
	}
	if decodeBody(t, r2)["id"].(string) != id1 {
		t.Fatal("replay must return the original user")
	}
	count := 0
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = 'idem-user'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate user created: %d %v", count, err)
	}

	// Same key, different body → 409.
	req3 := httptest.NewRequest("POST", "/api/v1/users", strings.NewReader(`{"username": "other"}`))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+e.plainTok)
	req3.Header.Set("Idempotency-Key", "bot-retry-1")
	r3 := httptest.NewRecorder()
	e.handler.ServeHTTP(r3, req3)
	if r3.Code != 409 || errCode(t, r3) != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("key reuse: %d %s", r3.Code, r3.Body.String())
	}

	// A failing request releases the key (client may retry).
	req4 := httptest.NewRequest("POST", "/api/v1/users", strings.NewReader(`{"username": "bad name!"}`))
	req4.Header.Set("Content-Type", "application/json")
	req4.Header.Set("Authorization", "Bearer "+e.plainTok)
	req4.Header.Set("Idempotency-Key", "retry-after-fail")
	r4 := httptest.NewRecorder()
	e.handler.ServeHTTP(r4, req4)
	if r4.Code != 400 {
		t.Fatalf("invalid create: %d", r4.Code)
	}
	req5 := httptest.NewRequest("POST", "/api/v1/users", strings.NewReader(`{"username": "good-name"}`))
	req5.Header.Set("Content-Type", "application/json")
	req5.Header.Set("Authorization", "Bearer "+e.plainTok)
	req5.Header.Set("Idempotency-Key", "retry-after-fail")
	r5 := httptest.NewRecorder()
	e.handler.ServeHTTP(r5, req5)
	if r5.Code != 201 {
		t.Fatalf("retry after failure: %d %s", r5.Code, r5.Body.String())
	}
}

func TestRateLimit(t *testing.T) {
	e := newEnv(t)
	if err := e.srv.Settings.Set(context.Background(), "api.rate_limit_per_minute", 3); err != nil {
		t.Fatal(err)
	}
	e.srv.limiter = newRateLimiter(3) // the server snapshots the limit at boot
	var saw429 bool
	for i := 0; i < 5; i++ {
		rec := e.do("GET", "/api/v1/node", "")
		if rec.Code == 429 {
			saw429 = true
			if errCode(t, rec) != "RATE_LIMITED" {
				t.Fatalf("429 envelope: %s", rec.Body.String())
			}
			if rec.Header().Get("Retry-After") == "" {
				t.Fatal("Retry-After missing")
			}
		}
	}
	if !saw429 {
		t.Fatal("rate limit never triggered")
	}
}

func TestDeviceLifecycleAndConfig(t *testing.T) {
	e := newEnv(t)
	// Interface + user.
	if _, err := e.ifaces.Create(context.Background(), iface.CreateInput{Name: "awg0", ListenPort: 39001, Subnet: "10.77.0.0/24"}); err != nil {
		t.Fatal(err)
	}
	uid := ""
	if rec := e.do("POST", "/api/v1/users", `{"username": "dev-user", "device_limit": 2}`); rec.Code == 201 {
		uid = decodeBody(t, rec)["id"].(string)
	} else {
		t.Fatalf("user: %d %s", rec.Code, rec.Body.String())
	}
	// node.endpoint drives the config endpoint.
	if err := e.srv.Settings.Set(context.Background(), "node.endpoint", "vpn.example.com"); err != nil {
		t.Fatal(err)
	}

	// Create device (keys generated server-side).
	rec := e.do("POST", "/api/v1/users/"+uid+"/devices", `{"name": "phone", "preshared_key": true}`)
	if rec.Code != 201 {
		t.Fatalf("device create: %d %s", rec.Code, rec.Body.String())
	}
	d := decodeBody(t, rec)
	did := d["id"].(string)
	if d["ipv4_address"] == "" || d["public_key"] == "" {
		t.Fatalf("device shape: %v", d)
	}
	// Second device fills the device limit (2); the third is refused.
	rec2 := e.do("POST", "/api/v1/users/"+uid+"/devices", `{"name": "laptop"}`)
	if rec2.Code != 201 {
		t.Fatalf("second device: %d %s", rec2.Code, rec2.Body.String())
	}
	rec3 := e.do("POST", "/api/v1/users/"+uid+"/devices", `{"name": "tablet"}`)
	if rec3.Code != 409 || errCode(t, rec3) != "DEVICE_LIMIT_REACHED" {
		t.Fatalf("device limit: %d %s", rec3.Code, rec3.Body.String())
	}

	// Config download: contains the client shape; no-store.
	rec = e.do("GET", "/api/v1/devices/"+did+"/config", "")
	if rec.Code != 200 {
		t.Fatalf("config: %d %s", rec.Code, rec.Body.String())
	}
	cfg := rec.Body.String()
	for _, want := range []string{"[Interface]", "PrivateKey = ", "Address = 10.77.0.", "[Peer]", "PublicKey = ", "Endpoint = vpn.example.com:39001", "PresharedKey = ", "PersistentKeepalive = 25", "AllowedIPs = 0.0.0.0/0"} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("config missing %q:\n%s", want, cfg)
		}
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("config must be no-store")
	}

	// QR: valid PNG.
	rec = e.do("GET", "/api/v1/devices/"+did+"/qr", "")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("qr: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	png, _ := io.ReadAll(rec.Body)
	if len(png) < 8 || png[0] != 0x89 || string(png[1:4]) != "PNG" {
		t.Fatal("qr is not a PNG")
	}

	// Rename.
	rec = e.do("PATCH", "/api/v1/devices/"+did, `{"name": "phone-renamed"}`)
	if d = decodeBody(t, rec); d["name"] != "phone-renamed" {
		t.Fatalf("rename: %v", d["name"])
	}
	// Regenerate → new public key.
	oldKey := d["public_key"].(string)
	rec = e.do("POST", "/api/v1/devices/"+did+"/regenerate", `{"preshared_key": false}`)
	if d = decodeBody(t, rec); rec.Code != 200 || d["public_key"] == oldKey {
		t.Fatalf("regenerate: %d %v", rec.Code, d)
	}
	// Disable/enable.
	if rec = e.do("POST", "/api/v1/devices/"+did+"/disable", ""); rec.Code != 200 {
		t.Fatalf("disable: %d", rec.Code)
	}
	if rec = e.do("POST", "/api/v1/devices/"+did+"/enable", ""); rec.Code != 200 {
		t.Fatalf("enable: %d", rec.Code)
	}
	// Device stats.
	rec = e.do("GET", "/api/v1/devices/"+did+"/stats", "")
	if rec.Code != 200 {
		t.Fatalf("device stats: %d", rec.Code)
	}
	// Delete.
	if rec = e.do("DELETE", "/api/v1/devices/"+did, ""); rec.Code != 200 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec = e.do("GET", "/api/v1/devices/"+did, ""); rec.Code != 404 {
		t.Fatalf("deleted device: %d", rec.Code)
	}
	// List for user.
	rec = e.do("GET", "/api/v1/users/"+uid+"/devices", "")
	if items := decodeBody(t, rec)["items"].([]any); len(items) != 1 {
		t.Fatalf("device list: %v", items)
	}
}

func TestPlansAndInterfacesViaAPI(t *testing.T) {
	e := newEnv(t)
	rec := e.do("POST", "/api/v1/plans", `{"name": "monthly", "duration_seconds": 2592000, "speed_limit_down_kbps": 5120, "speed_limit_up_kbps": null}`)
	if rec.Code != 201 {
		t.Fatalf("plan create: %d %s", rec.Code, rec.Body.String())
	}
	p := decodeBody(t, rec)
	pid := p["id"].(string)
	if p["speed_limit_down_kbps"].(float64) != 5120 || p["speed_limit_up_kbps"] != nil {
		t.Fatalf("plan shape: %v", p)
	}
	if rec = e.do("GET", "/api/v1/plans/"+pid, ""); rec.Code != 200 {
		t.Fatalf("plan get: %d", rec.Code)
	}
	rec = e.do("PATCH", "/api/v1/plans/"+pid, `{"name": "monthly-v2", "traffic_limit_bytes": null}`)
	if p = decodeBody(t, rec); p["name"] != "monthly-v2" {
		t.Fatalf("plan patch: %v", p)
	}
	if rec = e.do("DELETE", "/api/v1/plans/"+pid, ""); rec.Code != 200 {
		t.Fatalf("plan delete: %d", rec.Code)
	}

	ifc, err := e.ifaces.Create(context.Background(), iface.CreateInput{Name: "awg1", ListenPort: 39002})
	if err != nil {
		t.Fatal(err)
	}
	rec = e.do("GET", "/api/v1/interfaces", "")
	if items := decodeBody(t, rec)["items"].([]any); len(items) != 1 {
		t.Fatalf("iface list: %v", items)
	}
	rec = e.do("PATCH", "/api/v1/interfaces/"+ifc.ID, `{"mtu": 1380}`)
	if ifcDTO := decodeBody(t, rec); ifcDTO["mtu"].(float64) != 1380 {
		t.Fatalf("iface patch: %v", ifcDTO["mtu"])
	}
	rec = e.do("PATCH", "/api/v1/interfaces/"+ifc.ID, `{"obfuscation": {"enabled": true, "jc": 4, "jmin": 40, "jmax": 70, "s1": 15, "s2": 16, "h1": 1, "h2": 2, "h3": 3, "h4": 4}}`)
	if rec.Code != 200 {
		t.Fatalf("obfuscation patch: %d %s", rec.Code, rec.Body.String())
	}
	// Invalid obfuscation (duplicate H) rejected with the constraint code.
	rec = e.do("PATCH", "/api/v1/interfaces/"+ifc.ID, `{"obfuscation": {"enabled": true, "jc": 4, "jmin": 40, "jmax": 70, "s1": 15, "s2": 16, "h1": 1, "h2": 1, "h3": 3, "h4": 4}}`)
	if rec.Code != 400 || errCode(t, rec) != "PARAM_CONSTRAINT" {
		t.Fatalf("bad obfuscation: %d %s", rec.Code, rec.Body.String())
	}
	// Delete the empty profile.
	if rec = e.do("DELETE", "/api/v1/interfaces/"+ifc.ID, ""); rec.Code != 200 {
		t.Fatalf("iface delete: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSettingsViaAPI(t *testing.T) {
	e := newEnv(t)
	rec := e.do("GET", "/api/v1/settings", "")
	if rec.Code != 200 {
		t.Fatalf("settings get: %d", rec.Code)
	}
	items := decodeBody(t, rec)["items"].([]any)
	if len(items) == 0 {
		t.Fatal("settings catalog empty")
	}
	// Secrets are redacted: value null + secret_set.
	for _, it := range items {
		s := it.(map[string]any)
		if s["secret"] == true && s["value"] != nil {
			t.Fatalf("secret leaked: %v", s)
		}
	}
	// Update validates.
	rec = e.do("PATCH", "/api/v1/settings", `{"network.mtu": 1380}`)
	if rec.Code != 200 {
		t.Fatalf("settings patch: %d %s", rec.Code, rec.Body.String())
	}
	if mtu, err := e.srv.Settings.GetInt(context.Background(), "network.mtu"); err != nil || mtu != 1380 {
		t.Fatalf("setting not applied: %d %v", mtu, err)
	}
	// Unknown/invalid keys are rejected.
	rec = e.do("PATCH", "/api/v1/settings", `{"network.mtu": 1}`)
	if rec.Code != 400 || errCode(t, rec) != "SETTING_INVALID" {
		t.Fatalf("setting validation: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do("PATCH", "/api/v1/settings", `{"nope.key": 1}`)
	if rec.Code != 400 || errCode(t, rec) != "SETTING_UNKNOWN" {
		t.Fatalf("unknown setting: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWebhookEndToEnd(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	type received struct {
		event   string
		headers http.Header
		body    []byte
	}
	var mu sync.Mutex
	var got []received
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, received{event: r.Header.Get("X-WG-Event"), headers: r.Header.Clone(), body: b})
		mu.Unlock()
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)

	// Subscribe to user.created only.
	rec := e.do("POST", "/api/v1/webhooks", `{"url": "`+srv.URL+`", "events": ["user.created"]}`)
	if rec.Code != 201 {
		t.Fatalf("webhook create: %d %s", rec.Code, rec.Body.String())
	}
	wh := decodeBody(t, rec)
	secret, _ := wh["secret"].(string)
	if secret == "" {
		t.Fatal("generated secret must be returned once")
	}
	// The secret is never echoed again.
	rec = e.do("GET", "/api/v1/webhooks/"+wh["id"].(string), "")
	if wh2 := decodeBody(t, rec); wh2["secret"] != nil {
		t.Fatal("secret echoed back")
	}

	// A user creation emits user.created through the same transaction.
	if rec := e.do("POST", "/api/v1/users", `{"username": "hooked"}`); rec.Code != 201 {
		t.Fatalf("user: %d", rec.Code)
	}

	// Worker pass delivers (registry supplies node.id for the envelope).
	worker := webhook.NewWorker(e.db, e.ring, e.reg, nil)
	report, err := worker.Pass(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Delivered != 1 {
		t.Fatalf("worker report: %+v", report)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].event != "user.created" {
		t.Fatalf("receiver: %v", got)
	}
	var envelope map[string]any
	if err := json.Unmarshal(got[0].body, &envelope); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if envelope["type"] != "user.created" || envelope["node_id"] != "test-node" || envelope["data"].(map[string]any)["username"] != "hooked" {
		t.Fatalf("envelope shape: %v", envelope)
	}
	if got[0].headers.Get("X-WG-Signature") == "" {
		t.Fatal("signature missing")
	}

	// Redeliver: force the delivery dead, then redeliver it.
	if _, err := e.db.Exec(`UPDATE webhook_deliveries SET status='dead', attempts=12`); err != nil {
		t.Fatal(err)
	}
	var deliveryID string
	if err := e.db.QueryRow(`SELECT id FROM webhook_deliveries`).Scan(&deliveryID); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"delivery_id": deliveryID})
	rec = e.do("POST", "/api/v1/webhooks/"+wh["id"].(string)+"/redeliver", string(body))
	if rec.Code != 202 {
		t.Fatalf("redeliver: %d %s", rec.Code, rec.Body.String())
	}
}

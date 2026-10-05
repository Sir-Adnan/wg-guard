package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
)

func TestTemplateCreationModeRequiresSelection(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	form := url.Values{"username": {"template-missing"}, "creation_mode": {"template"}, "auto_devices": {""}}
	response := e.post("/users", form, cookie, deriveCSRF(cookie.Value))
	if response.Code < 400 || !strings.Contains(response.Body.String(), "Choose a template or switch to Standard.") {
		t.Fatal("empty template mode must retain a visible selection error")
	}
	var count int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = 'template-missing'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("empty template mode silently created an unlimited user")
	}
	form.Set("creation_mode", "custom")
	if rec := e.post("/users", form, cookie, deriveCSRF(cookie.Value)); rec.Code != http.StatusSeeOther {
		t.Fatal("Standard mode must still allow manually unlimited terms")
	}
}

// create a user through the real form flow, return its id.
func createUserViaForm(t *testing.T, e *env, cookie *http.Cookie, username string) string {
	t.Helper()
	form := url.Values{
		"_csrf":            {deriveCSRF(cookie.Value)},
		"username":         {username},
		"traffic_limit_gb": {"10"},
		"speed_down":       {"0.128"},
		"speed_up":         {"0.064"},
		"device_limit":     {"2"},
		"duration_days":    {"30"},
		"start_policy":     {"immediate"},
	}
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create user: %d %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/users/") {
		t.Fatalf("create redirect = %s", loc)
	}
	id := strings.TrimPrefix(loc, "/users/")
	if i := strings.Index(id, "?"); i >= 0 {
		id = id[:i]
	}
	return id
}

func TestUserCreateListDetail(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")

	id := createUserViaForm(t, e, cookie, "alice")

	// List shows the row with the username and status.
	rec := e.get("/users", cookie)
	body := rec.Body.String()
	if !strings.Contains(body, "alice") || !strings.Contains(body, "فعال") {
		t.Fatal("list does not show created user")
	}

	// Detail renders the overview and empty devices state.
	rec = e.get("/users/"+id, cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "alice") {
		t.Fatalf("detail: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "هنوز دستگاهی نیست") {
		t.Fatal("detail missing empty-devices state")
	}

	// Create a device through the form flow (needs a seeded interface).
	e.seedIface()
	form := url.Values{"_csrf": {deriveCSRF(cookie.Value)}, "name": {"phone"}}
	req := httptest.NewRequest(http.MethodPost, "/users/"+id+"/devices", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("device create: %d", rec.Code)
	}

	// Detail now lists the device with its VPN IP (seeded iface subnet).
	rec = e.get("/users/"+id, cookie)
	if !strings.Contains(rec.Body.String(), "phone") || !strings.Contains(rec.Body.String(), "10.77.") {
		t.Fatal("device row missing")
	}

	// Config download and QR are session-gated and no-store.
	var devID string
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if i := strings.Index(line, `href="/devices/`); i >= 0 && strings.Contains(line, "/config") {
			rest := line[i+len(`href="/devices/`):]
			devID = rest[:strings.Index(rest, `"`)]
			devID = strings.TrimSuffix(devID, "/config")
		}
	}
	if devID == "" {
		t.Fatal("device config link missing")
	}
	rec = e.get("/devices/"+devID+"/config", cookie)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") ||
		!strings.Contains(rec.Body.String(), "[Interface]") {
		t.Fatalf("config download: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "test") && strings.Contains(rec.Body.String(), "PRIVATE KEY = test") {
		t.Fatal("config contains unexpected material")
	}
	rec = e.get("/devices/"+devID+"/qr", cookie)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("qr: %d", rec.Code)
	}

	// Config without a session is redirected (leaks nothing).
	if rec := e.get("/devices/"+devID+"/config", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous config fetch: %d", rec.Code)
	}
	if rec := e.get("/devices/"+devID+"/qr", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous QR fetch: %d", rec.Code)
	}
}

func TestSelectedTemplateControlsCreatedUserAndBulkTerms(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	owner := e.loginEN("owner")
	ctx := context.Background()
	profile, err := e.ifaces.Create(ctx, iface.CreateInput{Name: "awg0", ListenPort: 39001, Subnet: "10.77.0.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	duration, devices, down, up := int64(7*86400), 2, 1024, 512
	p, err := e.srv.Plans.Create(ctx, plan.Input{Name: "7 days / 5 GB", DurationSeconds: &duration,
		StartPolicy:        domain.StartFirstConnection,
		TrafficLimitBytes:  domain.OptInt64{Set: true, Value: 5_000_000_000},
		DeviceLimit:        domain.OptInt{Set: true, Value: devices},
		SpeedLimitDownKbps: domain.OptInt{Set: true, Value: down},
		SpeedLimitUpKbps:   domain.OptInt{Set: true, Value: up},
		InterfaceID:        domain.OptString{Set: true, Value: profile.ID}})
	if err != nil {
		t.Fatal(err)
	}
	page := e.get("/users/new", owner).Body.String()
	for _, mode := range []string{`data-user-create-tab="custom"`, `data-user-create-tab="template"`, `id="user-template-choice"`} {
		if !strings.Contains(page, mode) {
			t.Fatal("creation mode control missing")
		}
	}
	form := url.Values{
		"username": {"template-user"}, "template_id": {p.ID}, "auto_devices": {"1"},
		"traffic_limit_value": {"broken"}, "duration_value": {"broken"},
		"device_limit": {"broken"}, "expires_on": {"yesterday"},
		"speed_down": {"broken"}, "start_policy": {"immediate"},
		"interface": {"forged-interface"}, "note": {"created from template"},
	}
	rec := e.postForm("/users", form, owner)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create with template: %d %s", rec.Code, rec.Body.String())
	}
	var id string
	if err := e.db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, "template-user").Scan(&id); err != nil {
		t.Fatal(err)
	}
	u, err := e.srv.Users.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if u.TemplateID == nil || *u.TemplateID != p.ID || u.TrafficLimitBytes == nil || *u.TrafficLimitBytes != 5_000_000_000 ||
		u.DurationSeconds == nil || *u.DurationSeconds != duration || u.DeviceLimit == nil || *u.DeviceLimit != devices ||
		u.SpeedLimitDownKbps == nil || *u.SpeedLimitDownKbps != down ||
		u.SpeedLimitUpKbps == nil || *u.SpeedLimitUpKbps != up ||
		u.InterfaceID == nil || *u.InterfaceID != profile.ID ||
		u.StartPolicy != domain.StartFirstConnection || u.Status != domain.UserWaitingFirstConnection || u.Note != "created from template" {
		t.Fatalf("template terms were not applied: %+v", u)
	}
	var deviceCount int
	if err := e.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE user_id = ?`, id).Scan(&deviceCount); err != nil || deviceCount != 2 {
		t.Fatalf("automatic devices ignored template limit: %d %v", deviceCount, err)
	}
	bulk := url.Values{"prefix": {"tpl-"}, "count": {"2"}, "start_index": {"1"}, "template_id": {p.ID},
		"traffic_limit_value": {"broken"}}
	if rec := e.postForm("/users/bulk", bulk, owner); rec.Code != http.StatusSeeOther {
		t.Fatalf("bulk with template: %d %s", rec.Code, rec.Body.String())
	}
	var bulkLimit int64
	if err := e.db.QueryRowContext(ctx, `SELECT traffic_limit_bytes FROM users WHERE username = ?`, "tpl-001").Scan(&bulkLimit); err != nil || bulkLimit != 5_000_000_000 {
		t.Fatalf("bulk template terms missing: %d %v", bulkLimit, err)
	}
	falseValue := false
	inactive, err := e.srv.Plans.Create(ctx, plan.Input{Name: "Inactive template", Enabled: &falseValue})
	if err != nil {
		t.Fatal(err)
	}
	if rec := e.postForm("/users", url.Values{"username": {"inactive-template"}, "template_id": {inactive.ID}}, owner); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `id="u-template"`) {
		t.Fatalf("disabled template was accepted or error hidden: %d", rec.Code)
	}
}

func TestPanelAddVolumeRaisesLimitNotUsedTraffic(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	owner := e.loginEN("owner")
	id := createUserViaForm(t, e, owner, "quota-topup")
	var beforeLimit, beforeRX, beforeTX int64
	if err := e.db.QueryRow(`SELECT traffic_limit_bytes, traffic_used_rx, traffic_used_tx FROM users WHERE id = ?`, id).
		Scan(&beforeLimit, &beforeRX, &beforeTX); err != nil {
		t.Fatal(err)
	}
	rec := e.postForm("/users/"+id+"/quota/add", url.Values{
		"traffic_value": {"2"}, "traffic_unit": {"gb"},
	}, owner)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("quota top-up: %d %s", rec.Code, rec.Body.String())
	}
	var afterLimit, afterRX, afterTX int64
	if err := e.db.QueryRow(`SELECT traffic_limit_bytes, traffic_used_rx, traffic_used_tx FROM users WHERE id = ?`, id).
		Scan(&afterLimit, &afterRX, &afterTX); err != nil {
		t.Fatal(err)
	}
	if afterLimit != beforeLimit+2_000_000_000 || afterRX != beforeRX || afterTX != beforeTX {
		t.Fatalf("panel added used traffic instead of quota: before=%d/%d/%d after=%d/%d/%d",
			beforeLimit, beforeRX, beforeTX, afterLimit, afterRX, afterTX)
	}
	trafficOnly := e.limitedLogin(t, []string{"users.read", "traffic.update"})
	if rec := e.postForm("/users/"+id+"/quota/add", url.Values{"traffic_value": {"1"}, "traffic_unit": {"gb"}}, trafficOnly); rec.Code != http.StatusSeeOther {
		t.Fatalf("permission denial did not redirect safely: %d", rec.Code)
	}
	var deniedLimit int64
	if err := e.db.QueryRow(`SELECT traffic_limit_bytes FROM users WHERE id = ?`, id).Scan(&deniedLimit); err != nil {
		t.Fatal(err)
	}
	if deniedLimit != afterLimit {
		t.Fatal("traffic-counter permission changed the user's quota")
	}
	if rec := e.postForm("/users/"+id+"/traffic/add", url.Values{
		"traffic_value": {"1"}, "traffic_unit": {"gb"},
	}, trafficOnly); rec.Code != http.StatusSeeOther {
		t.Fatalf("legacy meter correction: %d", rec.Code)
	}
	var correctedLimit, correctedRX int64
	if err := e.db.QueryRow(`SELECT traffic_limit_bytes, traffic_used_rx FROM users WHERE id = ?`, id).
		Scan(&correctedLimit, &correctedRX); err != nil {
		t.Fatal(err)
	}
	if correctedLimit != afterLimit || correctedRX != afterRX+1_000_000_000 {
		t.Fatal("legacy traffic/add must retain its charged-counter meaning")
	}
}

func TestUserLifecycleAndSearch(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	id := createUserViaForm(t, e, cookie, "bob")

	csrf := deriveCSRF(cookie.Value)
	rec := e.post("/users/"+id+"/disable", url.Values{}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("disable: %d", rec.Code)
	}
	rec = e.get("/users?status=disabled", cookie)
	if !strings.Contains(rec.Body.String(), "bob") {
		t.Fatal("disabled filter does not show bob")
	}
	rec = e.get("/users?status=active", cookie)
	if strings.Contains(rec.Body.String(), "cell-main\">bob<") {
		t.Fatal("active filter must not show disabled bob")
	}

	// Search by username.
	rec = e.get("/users?q=bob", cookie)
	if !strings.Contains(rec.Body.String(), "bob") {
		t.Fatal("search does not find bob")
	}
	rec = e.get("/users?q=zzz", cookie)
	if !strings.Contains(rec.Body.String(), "کاربری یافت نشد") {
		t.Fatal("no-match empty state missing")
	}

	// Renew extends expiry (from_now + 30d).
	rec = e.post("/users/"+id+"/renew", url.Values{"mode": {"from_now"}, "days": {"30"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("renew: %d", rec.Code)
	}

	// Traffic add + reset through the panel.
	rec = e.post("/users/"+id+"/traffic/add", url.Values{"gb": {"5"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("traffic add: %d", rec.Code)
	}
	rec = e.post("/users/"+id+"/traffic/reset", url.Values{}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("traffic reset: %d", rec.Code)
	}

	// Soft delete → filtered out of the live list.
	rec = e.post("/users/"+id+"/delete", url.Values{}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d", rec.Code)
	}
	rec = e.get("/users", cookie)
	if strings.Contains(rec.Body.String(), "cell-main\">bob<") {
		t.Fatal("deleted user still listed")
	}
}

func TestUserBulkCreate(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")

	form := url.Values{
		"prefix": {"gs-"}, "count": {"12"}, "start_index": {"1"},
		"traffic_limit_gb": {"50"}, "duration_days": {"90"}, "start_policy": {"immediate"},
	}
	rec := e.post("/users/bulk", form, cookie, deriveCSRF(cookie.Value))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("bulk create: %d", rec.Code)
	}
	rec = e.get("/users?sort=username&limit=50", cookie)
	for _, name := range []string{"gs-001", "gs-012"} {
		if !strings.Contains(rec.Body.String(), name) {
			t.Fatalf("bulk user %s missing", name)
		}
	}

	// Invalid count is rejected.
	form.Set("count", "501")
	rec = e.post("/users/bulk", form, cookie, deriveCSRF(cookie.Value))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid bulk count: %d", rec.Code)
	}
}

func TestUserEditTriState(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	id := createUserViaForm(t, e, cookie, "carol")

	// Clear the traffic limit (empty field on edit = unlimited). A real
	// browser submits every rendered field; untouched fields round-trip
	// their current values, which is what preserves them.
	form := url.Values{"_csrf": {deriveCSRF(cookie.Value)}, "display_name": {"Carol"},
		"traffic_limit_gb": {""}, "speed_down": {"0.128"}, "speed_up": {"0.064"}, "device_limit": {"2"}}
	req := httptest.NewRequest(http.MethodPost, "/users/"+id+"/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("edit: %d", rec.Code)
	}
	u, err := e.srv.Users.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if u.TrafficLimitBytes != nil {
		t.Fatalf("traffic limit not cleared: %v", *u.TrafficLimitBytes)
	}
	if u.DisplayName != "Carol" {
		t.Fatalf("display name = %q", u.DisplayName)
	}
	if u.SpeedLimitDownKbps == nil || *u.SpeedLimitDownKbps != 1024 {
		t.Fatalf("speed limit must be preserved by untouched fields")
	}
}

func TestUserCreateAutoDevices(t *testing.T) {
	e := newEnv(t)
	e.seedIface()
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)

	// Device limit 2 + auto-create → exactly two devices + a sub link.
	rec := e.post("/users", url.Values{
		"username":            {"autopilot"},
		"device_limit":        {"2"},
		"auto_devices":        {"1"},
		"traffic_limit_value": {"0.2"},
		"traffic_limit_unit":  {"gb"},
		"duration_value":      {"6"},
		"duration_unit":       {"hours"},
	}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create: %d", rec.Code)
	}
	u, err := e.srv.Users.GetByUsername(context.Background(), "autopilot")
	if err != nil {
		t.Fatal(err)
	}
	devs, err := e.srv.Devices.ListForUser(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 {
		t.Fatalf("want 2 auto devices, got %d", len(devs))
	}
	names := map[string]bool{}
	for _, d := range devs {
		names[d.Name] = true
	}
	for i := 1; i <= 2; i++ {
		if !names[fmt.Sprintf("device-%d", i)] {
			t.Fatalf("device-%d missing (got %v)", i, names)
		}
	}
	// 0.2 GB quota stored exactly (regression: small test accounts).
	if u.TrafficLimitBytes == nil || *u.TrafficLimitBytes != 200000000 {
		t.Fatalf("quota: %v", u.TrafficLimitBytes)
	}
	// 6-hour duration stored exactly.
	if u.DurationSeconds == nil || *u.DurationSeconds != 21600 {
		t.Fatalf("duration: %v", u.DurationSeconds)
	}
	if u.ExpiresAt == nil || time.Until(*u.ExpiresAt) > 6*time.Hour {
		t.Fatalf("expiry: %v", u.ExpiresAt)
	}
	// Subscription link ensured at creation.
	link, err := e.srv.Links.ForUser(context.Background(), u.ID)
	if err != nil || link == nil || link.Token == "" {
		t.Fatalf("sub link: %v %v", link, err)
	}

	// Unlimited device limit → one device.
	rec = e.post("/users", url.Values{"username": {"lonely"}, "auto_devices": {"1"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create lonely: %d", rec.Code)
	}
	u2, _ := e.srv.Users.GetByUsername(context.Background(), "lonely")
	if devs, _ := e.srv.Devices.ListForUser(context.Background(), u2.ID); len(devs) != 1 {
		t.Fatalf("unlimited + auto → 1 device, got %d", len(devs))
	}
}

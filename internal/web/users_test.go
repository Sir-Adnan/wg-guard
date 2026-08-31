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
)

// create a user through the real form flow, return its id.
func createUserViaForm(t *testing.T, e *env, cookie *http.Cookie, username string) string {
	t.Helper()
	form := url.Values{
		"_csrf":            {deriveCSRF(cookie.Value)},
		"username":         {username},
		"traffic_limit_gb": {"10"},
		"speed_down":       {"1024"},
		"speed_up":         {"512"},
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
	if rec.Code != http.StatusBadRequest {
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
		"traffic_limit_gb": {""}, "speed_down": {"1024"}, "speed_up": {"512"}, "device_limit": {"2"}}
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
	for i, d := range devs {
		if want := fmt.Sprintf("device-%d", i+1); d.Name != want {
			t.Fatalf("device %d named %q, want %q", i, d.Name, want)
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

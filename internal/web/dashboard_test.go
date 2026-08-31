package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// createDeviceViaForm runs the panel's device-create flow.
func createDeviceViaForm(t *testing.T, e *env, cookie *http.Cookie, userID, name string) {
	t.Helper()
	form := url.Values{"_csrf": {deriveCSRF(cookie.Value)}, "name": {name}}
	req := httptest.NewRequest(http.MethodPost, "/users/"+userID+"/devices", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("device create: %d", rec.Code)
	}
}

// seedRollup inserts an aggregated bucket for the user's first device.
func seedRollup(t *testing.T, e *env, userID, gran string, bucket time.Time, rx, tx int64) {
	t.Helper()
	var devID string
	if err := e.db.QueryRow(`SELECT id FROM devices WHERE user_id = ? LIMIT 1`, userID).Scan(&devID); err != nil {
		t.Fatalf("device lookup: %v", err)
	}
	_, err := e.db.Exec(`INSERT INTO traffic_rollups (device_id, granularity, bucket_start, rx, tx)
		VALUES (?, ?, ?, ?, ?)`,
		devID, gran, bucket.UTC().Format(time.RFC3339Nano), rx, tx)
	if err != nil {
		t.Fatalf("seed rollup: %v", err)
	}
}

func TestDashboardChart(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	e.seedIface()
	cookie := e.login("owner")
	uid := createUserViaForm(t, e, cookie, "chart-user")
	createDeviceViaForm(t, e, cookie, uid, "d1")

	// Two hourly buckets (current and previous hour). No daily rollups yet.
	now := time.Now().UTC().Truncate(time.Hour)
	seedRollup(t, e, uid, "hourly", now, 7_000, 3_000)
	seedRollup(t, e, uid, "hourly", now.Add(-time.Hour), 12_000, 4_000)

	// Full page: the 24 h view renders bars from the hourly buckets.
	rec := e.get("/dashboard", cookie)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `class="chart"`) || !strings.Contains(body, "chart-rx") {
		t.Fatal("24h chart missing bars")
	}

	// A range without data renders the honest empty state (no daily rows).
	rec = e.get("/dashboard/chart?range=30d", cookie)
	if !strings.Contains(rec.Body.String(), "در این بازه ترافیکی ثبت نشده است.") {
		t.Fatal("empty chart state missing")
	}

	// The daily rollup must not leak into the hourly view.
	seedRollup(t, e, uid, "daily", now.Truncate(24*time.Hour), 900_000, 100_000)
	rec = e.get("/dashboard", cookie)
	if strings.Contains(rec.Body.String(), ">1M<") || strings.Contains(rec.Body.String(), ">0.9M<") {
		t.Fatal("daily bucket leaked into hourly view")
	}

	// 7 d view aggregates the daily rollups.
	rec = e.get("/dashboard/chart?range=7d", cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "chart-rx") {
		t.Fatal("7d chart missing bars")
	}
	if !strings.Contains(rec.Body.String(), `seg is-on`) {
		t.Fatal("no active range segment")
	}
}

func TestDashboardLiveFragment(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	rec := e.get("/dashboard/live", cookie)
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("live fragment: %d", rec.Code)
	}
	for _, want := range []string{
		`id="live"`,
		`hx-trigger="every 30s"`,
		"hx-get=\"/dashboard/live\"",
		"کل کاربران",
		// Test env wires no hoststats reader → the card degrades honestly.
		"آمار میزبان فقط روی لینوکس در دسترس است.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("live fragment missing %q", want)
		}
	}
}

func TestDashboardCounters(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	rec := e.get("/dashboard", cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "کل کاربران") {
		t.Fatalf("dashboard render: %d", rec.Code)
	}
	for _, want := range []string{"آنلاین", "اتمام حجم", "وی‌گارد", "مجموع ترافیک"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
}

func TestDashboardAttention(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")

	// User whose expiry is imminent: created with an exact date inside the
	// 7-day window → expiring list.
	soon := time.Now().UTC().AddDate(0, 0, 2).Format("2006-01-02")
	form := url.Values{
		"_csrf":        {deriveCSRF(cookie.Value)},
		"username":     {"soon-gone"},
		"expires_on":   {soon},
		"start_policy": {"immediate"},
	}
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	e.handler.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusSeeOther {
		t.Fatalf("create soon-gone: %d %s", rec2.Code, rec2.Body.String())
	}

	// Quota-exhausted user via direct status flip.
	uid := createUserViaForm(t, e, cookie, "overquota")
	if _, err := e.db.Exec(`UPDATE users SET status = 'traffic_exceeded' WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}

	rec := e.get("/dashboard", cookie)
	if rec.Code != 200 {
		t.Fatalf("dashboard: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"dash-attention", "soon-gone", "overquota", "attention-col--exceeded"} {
		if !strings.Contains(body, want) {
			t.Fatalf("attention card missing %q", want)
		}
	}
}

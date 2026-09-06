package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
)

func TestBackupSafetyMessagesUseRequestLocale(t *testing.T) {
	for _, lang := range []string{"fa", "en"} {
		t.Run(lang, func(t *testing.T) {
			e := newEnv(t)
			e.seedOwner()
			cookie := e.login("owner")
			e.post("/prefs/locale", url.Values{"locale": {lang}}, cookie, deriveCSRF(cookie.Value))
			if err := e.reg.SetRaw(context.Background(), "backup.password", "synthetic-password"); err != nil {
				t.Fatal(err)
			}
			if _, err := e.db.Exec(`UPDATE settings SET value='corrupt' WHERE key='backup.password'`); err != nil {
				t.Fatal(err)
			}
			rec := e.postForm("/backups/create", url.Values{}, cookie)
			want := "stored password could not be read"
			if lang == "fa" {
				want = "گذرواژه ذخیره‌شده"
			}
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
				t.Fatalf("localized password safety message absent (%s, %d)", lang, rec.Code)
			}
			if lang == "fa" && strings.Contains(rec.Body.String(), "stored password could not be read") {
				t.Fatal("English safety body rendered in Persian")
			}
			rec = e.postForm("/backups/telegram-test", url.Values{}, cookie)
			want = "Telegram is not configured"
			if lang == "fa" {
				want = "تلگرام تنظیم نشده"
			}
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
				t.Fatal("localized Telegram configuration error absent")
			}
		})
	}
}

type backupWarningHTTP struct{}

func (backupWarningHTTP) Do(*http.Request) (*http.Response, error) {
	return nil, errors.New("private-synthetic-token in transport URL")
}

func TestBackupWarningRedirectDoesNotExposeTransportCause(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	for key, value := range map[string]string{"backup.telegram_token": "synthetic-token", "backup.telegram_chat": "-123"} {
		if err := e.reg.SetRaw(context.Background(), key, value); err != nil {
			t.Fatal(err)
		}
	}
	e.srv.Backup.HTTPClient = backupWarningHTTP{}
	rec := e.postForm("/backups/create", url.Values{}, cookie)
	if rec.Code != 303 {
		t.Fatal("warning creation did not redirect", rec.Code)
	}
	location := rec.Header().Get("Location")
	page := e.get(location, cookie)
	if strings.Contains(location, "synthetic-token") || strings.Contains(page.Body.String(), "synthetic-token") {
		t.Fatal("private transport cause exposed")
	}
	if !strings.Contains(page.Body.String(), "telegram delivery failed") {
		t.Fatal("public warning omitted")
	}
}

func (e *env) postForm(path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.post(path, form, cookie, deriveCSRF(cookie.Value))
}

// loginEN logs in and persists the English locale so text assertions are
// stable (BootstrapOwner defaults to fa).
func (e *env) loginEN(username string) *http.Cookie {
	e.t.Helper()
	cookie := e.login(username)
	if rec := e.post("/prefs/locale", url.Values{"locale": {"en"}}, cookie,
		deriveCSRF(cookie.Value)); rec.Code != http.StatusSeeOther {
		e.t.Fatalf("set locale: %d", rec.Code)
	}
	return cookie
}

func TestBackupsCreateListDelete(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")

	// Empty page renders.
	rec := e.get("/backups", cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "No archives yet") {
		t.Fatalf("empty backups page: %d", rec.Code)
	}

	// Create (no password → plain).
	rec = e.postForm("/backups/create", url.Values{}, cookie)
	if rec.Code != 200 && rec.Code != 303 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.get("/backups", cookie)
	body := rec.Body.String()
	if !strings.Contains(body, "wg-guard-") || !strings.Contains(body, "Plain") {
		t.Fatalf("archive not listed: %s", body[:min(400, len(body))])
	}

	// Create with a stored password → encrypted badge.
	if err := e.reg.Set(context.Background(), "backup.password", "stored-pass-123"); err != nil {
		t.Fatal(err)
	}
	rec = e.postForm("/backups/create", url.Values{}, cookie)
	if rec.Code != 200 && rec.Code != 303 {
		t.Fatalf("create 2: %d", rec.Code)
	}
	body = e.get("/backups", cookie).Body.String()
	if !strings.Contains(body, "Encrypted") {
		t.Fatal("stored-password archive not encrypted")
	}

	// Download serves attachment.
	name := archiveNameFrom(body)
	rec = e.get("/backups/"+name+"/download", cookie)
	if rec.Code != 200 ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), name) {
		t.Fatalf("download: %d %v", rec.Code, rec.Header())
	}

	// Delete removes it.
	rec = e.postForm("/backups/delete", url.Values{"name": {name}}, cookie)
	if rec.Code != 303 {
		t.Fatalf("delete: %d", rec.Code)
	}
	body = e.get("/backups", cookie).Body.String()
	if strings.Contains(body, name) {
		t.Fatal("archive still listed after delete")
	}
}

func TestBackupCreationWarningsRedirectAndRefreshDoesNotCreate(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	// This fixture's missing archived key is a real nonfatal archive warning.
	e.srv.Backup.ConfigPath = "/does-not-exist/private-config"
	rec := e.postForm("/backups/create", url.Values{}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("successful creation must redirect: %d", rec.Code)
	}
	location := rec.Header().Get("Location")
	for range 2 {
		page := e.get(location, cookie)
		if page.Code != 200 || !strings.Contains(page.Body.String(), "master key missing from archive") {
			t.Fatalf("warning lost across redirect: status=%d location=%s", page.Code, location)
		}
	}
	arcs, err := e.srv.Backup.List()
	if err != nil || len(arcs) != 1 {
		t.Fatalf("refresh repeated creation: %d %v", len(arcs), err)
	}
	if strings.Contains(location, "private-config") {
		t.Fatal("private cause exposed in redirect")
	}
}

// archiveNameFrom pulls the first archive name out of the rendered table
// (names appear in the restore form's hidden value="...").
func archiveNameFrom(body string) string {
	const marker = `value="wg-guard-`
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	rest := body[i+len(marker):]
	if j := strings.Index(rest, `"`); j >= 0 {
		return "wg-guard-" + rest[:j]
	}
	return ""
}

func TestBackupScheduleLifecycle(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")

	// Invalid kind → error page keeps the name.
	rec := e.postForm("/backups/schedules", url.Values{
		"name": {"nightly"}, "kind": {"monthly"},
	}, cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "must be daily") {
		t.Fatalf("invalid schedule: %d", rec.Code)
	}

	// Valid daily schedule.
	rec = e.postForm("/backups/schedules", url.Values{
		"name": {"nightly"}, "kind": {"daily"}, "time_of_day": {"03:15"},
		"enabled": {"1"},
	}, cookie)
	if rec.Code != 303 {
		t.Fatalf("create schedule: %d", rec.Code)
	}
	body := e.get("/backups", cookie).Body.String()
	if !strings.Contains(body, "nightly") || !strings.Contains(body, "03:15") {
		t.Fatal("schedule not listed")
	}

	id := e.schedID("nightly")

	// Toggle off and on.
	if rec := e.postForm("/backups/schedules/"+id+"/toggle", url.Values{}, cookie); rec.Code != 303 {
		t.Fatalf("toggle: %d", rec.Code)
	}
	body = e.get("/backups", cookie).Body.String()
	if !strings.Contains(body, "Disabled") {
		t.Fatal("toggle did not disable")
	}

	// Update to interval kind.
	rec = e.postForm("/backups/schedules/"+id+"/update", url.Values{
		"name": {"nightly"}, "kind": {"interval"}, "interval_hours": {"6"},
		"enabled": {"1"},
	}, cookie)
	if rec.Code != 303 {
		t.Fatalf("update: %d", rec.Code)
	}
	body = e.get("/backups", cookie).Body.String()
	if !strings.Contains(body, "Every") {
		t.Fatal("updated kind not shown")
	}

	// Delete.
	if rec := e.postForm("/backups/schedules/"+id+"/delete", url.Values{}, cookie); rec.Code != 303 {
		t.Fatalf("delete schedule: %d", rec.Code)
	}
	if body := e.get("/backups", cookie).Body.String(); strings.Contains(body, "nightly") {
		t.Fatal("schedule still listed")
	}
}

func (e *env) schedID(name string) string {
	e.t.Helper()
	schedules, err := e.srv.Backup.Schedules(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	for _, sc := range schedules {
		if sc.Name == name {
			return sc.ID
		}
	}
	e.t.Fatalf("schedule %q not found", name)
	return ""
}

func TestBackupRestoreFlow(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")

	// Create an archive, then restore it.
	if rec := e.postForm("/backups/create", url.Values{}, cookie); rec.Code != 303 {
		if rec.Code != 200 {
			t.Fatalf("create: %d", rec.Code)
		}
	}
	name := archiveNameFrom(e.get("/backups", cookie).Body.String())

	rec := e.postForm("/backups/restore", url.Values{"name": {name}}, cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Review this restore") {
		t.Fatalf("restore review: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), name) {
		t.Fatal("review lost the archive name")
	}

	// Confirm → pending banner appears on the page.
	if p, err := e.srv.Backup.Pending(); err != nil || p != nil {
		t.Fatal("unconfirmed review exposed to boot")
	}
	preview := regexp.MustCompile(`name="preview" value="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	if len(preview) != 2 {
		t.Fatal("review identity missing")
	}
	if rec := e.postForm("/backups/restore/confirm", url.Values{"preview": {preview[1]}}, cookie); rec.Code != 303 {
		t.Fatalf("confirm: %d", rec.Code)
	}
	body := e.get("/backups", cookie).Body.String()
	if !strings.Contains(body, "A verified restore is staged") {
		t.Fatal("pending banner missing after confirm")
	}

	// Cancel clears it.
	if rec := e.postForm("/backups/restore/cancel", url.Values{}, cookie); rec.Code != 303 {
		t.Fatalf("cancel: %d", rec.Code)
	}
	if p, _ := e.srv.Backup.Pending(); p != nil {
		t.Fatal("pending survived cancel")
	}
}

func TestBackupsRequireBackupManage(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	// A limited admin without backup.manage.
	if _, err := e.admins.Create(context.Background(), "helper", testPassword,
		auth.RoleAdmin, []string{auth.ScopeUsersRead}); err != nil {
		t.Fatal(err)
	}
	cookie := e.login("helper")
	rec := e.get("/backups", cookie)
	if rec.Code != 303 || !strings.Contains(rec.Header().Get("Location"), "/") {
		t.Fatalf("limited admin reached /backups: %d %v", rec.Code, rec.Header().Get("Location"))
	}
	if rec := e.postForm("/backups/create", url.Values{}, cookie); rec.Code != 303 {
		t.Fatalf("limited admin created a backup: %d", rec.Code)
	}

	// Owner passes.
	ownerCookie := e.loginEN("owner")
	if rec := e.get("/backups", ownerCookie); rec.Code != 200 {
		t.Fatalf("owner blocked from /backups: %d", rec.Code)
	}
}

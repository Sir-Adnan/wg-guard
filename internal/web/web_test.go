package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/accounting"
	"github.com/Sir-Adnan/wg-guard/internal/admin"
	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/subscription"
	"github.com/Sir-Adnan/wg-guard/internal/token"
	"github.com/Sir-Adnan/wg-guard/internal/user"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

// env is the panel test environment: temp DB, services, HTTP handler.
type env struct {
	t       *testing.T
	db      *database.DB
	reg     *settings.Registry
	srv     *Server
	handler http.Handler
	admins  *admin.Service
	ifaces  *iface.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "web.db"), database.Options{})
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
	reg, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	auditSvc := audit.NewService(db)
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.DatabasePath = filepath.Join(t.TempDir(), "x.db")
	cfg.MasterKeyFile = filepath.Join(t.TempDir(), "k.key")
	cfg.Complete()
	bak := &backup.Service{
		DB: db, Reg: reg, Audit: auditSvc, Cfg: cfg,
		ConfigPath: filepath.Join(t.TempDir(), "wg-guard.toml"),
		Version:    "test",
	}
	tokens := token.NewService(db)
	webhooksSvc := webhook.NewService(db, ring)
	sessions := auth.NewSessionStore(db, time.Hour, 24*time.Hour)
	admins := admin.NewService(db, sessions)
	ifaces := iface.NewService(db, reg, ring)
	srv, err := New(Deps{
		DB: db, Sessions: sessions, Admins: admins, Settings: reg, Ring: ring,
		Audit:      auditSvc,
		Users:      user.NewService(db),
		Devices:    device.NewService(db, ring),
		Plans:      plan.NewService(db),
		Ifaces:     ifaces,
		Links:      subscription.NewService(db, ring),
		Backup:     bak,
		Tokens:     tokens,
		Webhooks:   webhooksSvc,
		Accounting: accounting.NewService(db, nil, auditSvc, nil, reg),
		Version:    "test", TLSMode: config.TLSModeDev, NodeID: "node-1", ToolsVersion: "fake",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, db: db, reg: reg, srv: srv, handler: srv.Handler(), admins: admins, ifaces: ifaces}
}

const testPassword = "correct-horse-battery"

// seedOwner creates the owner account (post-onboarding state).
func (e *env) seedOwner() {
	e.t.Helper()
	if _, err := e.admins.BootstrapOwner(context.Background(), "owner", testPassword); err != nil {
		e.t.Fatal(err)
	}
}

// seedIface creates one enabled tunnel interface so device creation has a
// target (device.Create falls back to the first enabled interface).
func (e *env) seedIface() {
	e.t.Helper()
	if _, err := e.ifaces.Create(context.Background(), iface.CreateInput{
		Name: "awg0", ListenPort: 39001, Subnet: "10.77.0.0/24",
	}); err != nil {
		e.t.Fatal(err)
	}
}

// login performs the real form flow and returns the session cookie.
func (e *env) login(username string) *http.Cookie {
	e.t.Helper()
	form := url.Values{"username": {username}, "password": {testPassword}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		e.t.Fatalf("login: got %d, want 303", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	e.t.Fatal("no session cookie issued")
	return nil
}

func (e *env) get(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *env) post(path string, form url.Values, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set(csrfHeader, csrf)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func TestAnonymousRedirectsToLogin(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	rec := e.get("/", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("GET / anonymous = %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestOnboardingFirstRun(t *testing.T) {
	e := newEnv(t)

	// No owner yet: protected roots land on onboarding, not login.
	if rec := e.get("/", nil); rec.Header().Get("Location") != "/onboarding" {
		t.Fatalf("first-run redirect = %s", rec.Header().Get("Location"))
	}
	rec := e.get("/onboarding", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "به وی‌گارد خوش آمدید") {
		t.Fatalf("onboarding page: %d", rec.Code)
	}

	// Submit: creates owner, mints session, lands on dashboard.
	form := url.Values{
		"username": {"owner"}, "password": {testPassword},
		"password_confirm": {testPassword}, "endpoint": {"vpn.example.com"},
	}
	req := httptest.NewRequest(http.MethodPost, "/onboarding", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("onboarding submit: %d %s", w.Code, w.Header().Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("onboarding did not sign in")
	}

	// Endpoint was persisted from the optional field.
	if ep, _ := e.reg.GetString(context.Background(), "node.endpoint"); ep != "vpn.example.com" {
		t.Fatalf("node.endpoint = %q", ep)
	}

	// Authenticated dashboard renders (fa is the product default).
	rec = e.get("/", cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "کل کاربران") {
		t.Fatalf("dashboard after onboarding: %d", rec.Code)
	}

	// Second onboarding attempt is refused.
	rec = e.get("/onboarding", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("onboarding after owner exists: %d", rec.Code)
	}
}

func TestLoginFlow(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()

	// Wrong password: 401 with the generic error.
	form := url.Values{"username": {"owner"}, "password": {"nope-nope-nope"}}
	rec := e.post("/login", form, nil, "")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "نام کاربری یا گذرواژه نادرست") {
		t.Fatalf("bad login: %d", rec.Code)
	}

	// Success: cookie + redirect.
	cookie := e.login("owner")
	if rec := e.get("/", cookie); rec.Code != 200 {
		t.Fatalf("dashboard with cookie: %d", rec.Code)
	}

	// Logout (CSRF-protected) revokes the session.
	csrf := deriveCSRF(cookie.Value)
	if rec := e.post("/logout", url.Values{}, cookie, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("logout without csrf: %d", rec.Code)
	}
	rec = e.post("/logout", url.Values{}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logout with csrf: %d", rec.Code)
	}
	if rec := e.get("/", cookie); rec.Code != http.StatusSeeOther {
		t.Fatalf("session must be revoked after logout: %d", rec.Code)
	}
}

func TestCSRFFormFieldWorksWithoutHeader(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)

	rec := e.post("/prefs/locale", url.Values{
		"_csrf":  {csrf},
		"locale": {"en"},
	}, cookie, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("form-field CSRF = %d, want %d", rec.Code, http.StatusSeeOther)
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	form := url.Values{"username": {"owner"}, "password": {"nope-nope-nope"}}
	var last int
	for i := 0; i < 10; i++ {
		last = e.post("/login", form, nil, "").Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after 10 failures = %d, want 429", last)
	}
}

func TestLoginRedirectTarget(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	form := url.Values{"username": {"owner"}, "password": {testPassword}, "next": {"/dashboard"}}
	rec := e.post("/login", form, nil, "")
	if rec.Header().Get("Location") != "/dashboard" {
		t.Fatalf("next = %s", rec.Header().Get("Location"))
	}
	// Off-origin targets are dropped.
	form.Set("next", "https://evil.example")
	rec = e.post("/login", form, nil, "")
	if rec.Header().Get("Location") != "/" {
		t.Fatalf("open redirect not blocked: %s", rec.Header().Get("Location"))
	}
}

func TestSecurityHeadersAndAssets(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	rec := e.get("/login", nil)
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("CSP = %q", csp)
	}

	// The rendered login page must not carry inline scripts (CSP contract).
	if strings.Contains(rec.Body.String(), "<script>") {
		t.Fatal("inline script rendered; CSP would block it")
	}

	// Assets: hashed URL, immutable cache, 304 on ETag.
	url := e.srv.assetURL("/css/app.css")
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset fetch: %d", w.Code)
	}
	etag := w.Header().Get("ETag")
	req2 := httptest.NewRequest(http.MethodGet, url, nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	e.handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotModified {
		t.Fatalf("conditional asset fetch: %d", w2.Code)
	}
}

func TestLocaleAndThemePreferences(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()

	// Anonymous locale cookie drives the login page language.
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(&http.Cookie{Name: localeCookie, Value: "en"})
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `lang="en" dir="ltr"`) {
		t.Fatal("login page did not render English")
	}

	// Default is fa/RTL.
	if rec := e.get("/login", nil); !strings.Contains(rec.Body.String(), `dir="rtl"`) {
		t.Fatal("login page default must be Persian RTL")
	}

	// Theme cookie sets the data-theme attribute server-side.
	req = httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(&http.Cookie{Name: themeCookie, Value: "dark"})
	w = httptest.NewRecorder()
	e.handler.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `data-theme="dark"`) {
		t.Fatal("theme cookie not applied")
	}

	// Signed-in locale switch persists to the admin record.
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)
	rec := e.post("/prefs/locale", url.Values{"locale": {"en"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("locale switch: %d", rec.Code)
	}
	rec = e.get("/", cookie)
	if !strings.Contains(rec.Body.String(), `lang="en"`) ||
		!strings.Contains(rec.Body.String(), "Dashboard") {
		t.Fatal("stored locale not applied on next render")
	}
}

func TestSafeNext(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"/dashboard": "/dashboard",
		"//evil":     "",
		"https://x":  "",
		"/a\r\nb":    "",
		"/users?x=1": "/users?x=1",
	}
	for in, want := range cases {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

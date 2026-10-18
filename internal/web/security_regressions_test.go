package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
)

// An administrator with admins.manage must not reset the owner's password:
// that would hand over owner access and lock the owner out.
func TestOwnerPasswordIsOwnerOnly(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	admins, err := e.admins.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ownerID string
	for _, a := range admins {
		if a.Role == auth.RoleOwner {
			ownerID = a.ID
		}
	}
	operator := e.limitedLogin(t, []string{auth.ScopeAdminsManage})
	page := e.get("/admins?edit="+ownerID, operator).Body.String()
	if strings.Contains(page, "/admins/"+ownerID+"/password") {
		t.Fatal("the owner row must not offer a password reset to another administrator")
	}
	rec := e.post("/admins/"+ownerID+"/password", url.Values{"password": {"Taken-Over-Password-1"}}, operator, deriveCSRF(operator.Value))
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "ops.owner_protected") {
		t.Fatalf("owner password reset by operator: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	e.login("owner") // still the original password
	owner := e.loginEN("owner")
	if page := e.get("/admins?edit="+ownerID, owner).Body.String(); !strings.Contains(page, "/admins/"+ownerID+"/password") {
		t.Fatal("the owner keeps their own password form")
	}
}

func TestVisitorIPTrustsOnlyTheManagedProxy(t *testing.T) {
	req := func(peer string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/sub/x", nil)
		r.RemoteAddr = peer
		r.Header.Set("X-Real-IP", "203.0.113.9")
		return r
	}
	proxy := &Server{Deps: Deps{TLSMode: config.TLSModeProxy}}
	direct := &Server{Deps: Deps{TLSMode: config.TLSModeACME}}
	if got := proxy.visitorIP(req("127.0.0.1:4000")); got != "203.0.113.9" {
		t.Fatalf("proxy peer: %q", got)
	}
	if got := proxy.visitorIP(req("198.51.100.7:4000")); got != "198.51.100.7" {
		t.Fatalf("a public peer must not choose its address: %q", got)
	}
	if got := direct.visitorIP(req("127.0.0.1:4000")); got != "127.0.0.1" {
		t.Fatalf("direct TLS ignores proxy headers: %q", got)
	}
}

func TestLongPlanTimeMeterDoesNotOverflow(t *testing.T) {
	now := time.Now()
	expires := now.Add(10 * 24 * time.Hour)
	duration := int64(3650 * 24 * 3600)
	d := subPageData{U: &subUserView{ExpiresAt: &expires, DurationSeconds: &duration}}
	d.usageAndTime(now)
	if d.TimePercent != 99 {
		t.Fatalf("ten-year plan with ten days left: %d%%, want 99%%", d.TimePercent)
	}
}

// A crafted link cannot put its own words in a toast; server-issued
// redirects still can.
func TestFlashTextNeedsServerSignature(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	for _, path := range []string{
		"/?rawmsg=Panel+moved+to+evil.example",
		"/?toast=Panel+moved+to+evil.example",
		"/?toast=admins.toast.created&targ=evil.example",
	} {
		if body := e.get(path, cookie).Body.String(); strings.Contains(body, "evil.example") {
			t.Fatalf("%s rendered attacker text", path)
		}
	}
	signed := "/?" + url.Values{"rawmsg": {"Signed notice"}, "fsig": {flashSignature("raw", "Signed notice")}}.Encode()
	if body := e.get(signed, cookie).Body.String(); !strings.Contains(body, "Signed notice") {
		t.Fatal("a server-signed flash must still render")
	}
	if body := e.get("/?toast=common.error_generic", cookie).Body.String(); !strings.Contains(body, i18n.T(i18n.En, "common.error_generic")) {
		t.Fatal("a plain catalog toast must still render")
	}
}

// A storage failure while validating a session must not sign the visitor out.
func TestSessionSurvivesTransientStorageError(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	if _, err := e.db.Exec(`ALTER TABLE admin_sessions RENAME TO admin_sessions_unavailable`); err != nil {
		t.Fatal(err)
	}
	rec := e.get("/", cookie)
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.MaxAge < 0 {
			t.Fatal("a storage error must not clear the session cookie")
		}
	}
	if _, err := e.db.Exec(`ALTER TABLE admin_sessions_unavailable RENAME TO admin_sessions`); err != nil {
		t.Fatal(err)
	}
	if rec := e.get("/", cookie); rec.Code != http.StatusOK {
		t.Fatalf("session after recovery: %d", rec.Code)
	}
}

package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLoginFailureKeepsUsernameAndHTMLStatus(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	rec := e.post("/login", url.Values{"username": {"retained-name"}, "password": {"discarded-input"}, "next": {"/users"}}, nil, "")
	if rec.Code != 401 || !strings.Contains(rec.Result().Header.Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), `value="retained-name"`) || strings.Contains(rec.Body.String(), "discarded-input") {
		t.Fatal("login failure must retain only username and return a real HTML error response")
	}
}

func TestOnboardingMarksInvalidFieldAndClearsPasswords(t *testing.T) {
	e := newEnv(t)
	rec := e.post("/onboarding", url.Values{"username": {"my-owner"}, "password": {"discarded-input"}, "password_confirm": {"discarded-input"}, "endpoint": {"https://not-an-endpoint"}}, nil, "")
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), `id="o-endpoint" aria-invalid="true"`) || !strings.Contains(rec.Body.String(), `value="my-owner"`) || strings.Contains(rec.Body.String(), "discarded-input") {
		t.Fatal("setup must associate validation with its field and preserve only nonsecret inputs")
	}
}

func TestSurfaceErrorsPreserveStatusAndFragmentShape(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	rec := e.post("/users", url.Values{}, cookie, "wrong-csrf")
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "Reload the page") || !strings.Contains(rec.Result().Header.Get("Content-Type"), "text/html") {
		t.Fatal("CSRF failure needs localized recovery and status")
	}
	r := httptest.NewRequest(http.MethodGet, "/missing", nil)
	r.Header.Set("HX-Request", "true")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	if w.Code != 404 || strings.Contains(w.Body.String(), "<html") || !strings.Contains(w.Body.String(), `role="alert"`) {
		t.Fatal("fragment errors must not nest a whole document")
	}
}

func TestExpiredSessionAndLanguageKeepSafeReturn(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	r := httptest.NewRequest(http.MethodGet, "/users?status=active", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "expired-test-session"})
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	u, _ := url.Parse(w.Header().Get("Location"))
	if u.Path != "/login" || u.Query().Get("expired") != "1" || u.Query().Get("next") != "/users?status=active" {
		t.Fatal("expired session loses return context")
	}
	w = e.get("/login?lang=en&expired=1&next=%2Fusers", nil)
	u, _ = url.Parse(w.Header().Get("Location"))
	if u.Query().Get("expired") != "1" || u.Query().Get("next") != "/users" {
		t.Fatal("language switching loses login context")
	}
	body := e.get("/login?next=%2Fusers", nil).Body.String()
	if !strings.Contains(body, "next=%2Fusers") {
		t.Fatal("language toggle omits safe return target")
	}
}

func TestHTMXSessionReturnUsesFullSameOriginPage(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	for _, tc := range []struct{ current, want string }{{"http://example.com/dashboard?range=7d", "/dashboard?range=7d"}, {"https://evil.example/steal", "/dashboard"}, {"", "/dashboard"}} {
		r := httptest.NewRequest(http.MethodGet, "http://example.com/dashboard/live", nil)
		r.Header.Set("HX-Request", "true")
		r.Header.Set("HX-Current-URL", tc.current)
		w := httptest.NewRecorder()
		e.handler.ServeHTTP(w, r)
		u, _ := url.Parse(w.Header().Get("HX-Redirect"))
		if u.Query().Get("next") != tc.want {
			t.Fatal("HTMX sign-in return is not a full same-origin page")
		}
	}
}

func TestUnknownPublicRouteDoesNotUseAdminChrome(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	body := e.get("/sub/unknown/unmatched?lang=fa", cookie).Body.String()
	if strings.Contains(body, `id="sidebar"`) || !strings.Contains(body, `lang="fa"`) || !strings.Contains(body, `class="surface-error"`) {
		t.Fatal("unknown public route exposes the wrong context")
	}
}

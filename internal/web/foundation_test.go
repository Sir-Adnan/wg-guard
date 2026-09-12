package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
)

func TestShellNavigationReflectsPermissionAndRoot(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	helper := e.limitedLogin(t, []string{auth.ScopeUsersRead})
	body := e.get("/", helper).Body.String()
	if strings.Contains(body, `href="/settings"`) {
		t.Fatal("shell offers Settings to an account without node.settings")
	}
	if strings.Contains(body, `href="/dashboard"`) {
		t.Fatal("shell offers dashboard to an account without stats.read")
	}
}

func TestSharedThemeAndErrorSurfaces(t *testing.T) {
	e := newEnv(t)
	uid, _, _, _ := e.seedUserWithDevice()
	link, err := e.srv.Links.ForUser(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/login", "/sub/" + link.Token, "/not-a-page"} {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(&http.Cookie{Name: themeCookie, Value: "dark"})
		req.AddCookie(&http.Cookie{Name: localeCookie, Value: "en"})
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		body := rec.Body.String()
		if !strings.Contains(body, `data-theme="dark"`) || !strings.Contains(body, `data-theme-choice="system"`) {
			t.Fatal("public/auth/error surface lacks persistent theme controls")
		}
		if path == "/not-a-page" && (rec.Code != 404 || !strings.Contains(body, `lang="en"`)) {
			t.Fatal("unknown route must keep its status and localized HTML")
		}
	}
}

func TestThemeDefaultAndExplicitPreferences(t *testing.T) {
	for _, tc := range []struct{ cookie, want string }{{"", "light"}, {"invalid", "light"}, {"light", "light"}, {"dark", "dark"}, {"system", "system"}} {
		r := httptest.NewRequest("GET", "/", nil)
		if tc.cookie != "" {
			r.AddCookie(&http.Cookie{Name: themeCookie, Value: tc.cookie})
		}
		if got := themeFrom(r); got != tc.want {
			t.Errorf("theme %q: got %q, want %q", tc.cookie, got, tc.want)
		}
	}
}

func TestAnonymousNotFoundLanguageLinkOverridesCookie(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()

	request := func(path, locale string, session *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: localeCookie, Value: locale})
		if session != nil {
			req.AddCookie(session)
		}
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		return rec
	}

	rec := request("/not-a-page", "fa", nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `href="/not-a-page?lang=en"`) {
		t.Fatalf("Persian not-found language link: status=%d", rec.Code)
	}
	rec = request("/not-a-page?lang=en", "fa", nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `<html lang="en" dir="ltr"`) ||
		!strings.Contains(rec.Body.String(), `href="/not-a-page?lang=fa"`) {
		t.Fatalf("English not-found language link target: status=%d", rec.Code)
	}
	rec = request("/not-a-page?lang=english", "fa", nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `<html lang="fa" dir="rtl"`) {
		t.Fatalf("invalid not-found locale changed language: status=%d", rec.Code)
	}

	session := e.login("owner")
	csrf := deriveCSRF(session.Value)
	if switched := e.post("/prefs/locale", url.Values{"locale": {"en"}}, session, csrf); switched.Code != http.StatusSeeOther {
		t.Fatalf("set account locale: status=%d", switched.Code)
	}
	rec = request("/not-a-page?lang=fa", "fa", session)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `<html lang="en" dir="ltr"`) {
		t.Fatalf("not-found query overrode authenticated preference: status=%d", rec.Code)
	}
}

// Optional real-browser regression for shared behavior. It uses only the isolated
// test database and passes ephemeral session/capability data over stdin, never argv.
// No browser/runtime package is added to the production module.
func TestBrowserFoundation(t *testing.T) {
	node := os.Getenv("WG_TEST_BROWSER_NODE")
	if node == "" {
		t.Skip("set WG_TEST_BROWSER_NODE and WG_TEST_PLAYWRIGHT for local browser checks")
	}
	e := newEnv(t)
	uid, _, _, cookie := e.seedUserWithDevice()
	link, err := e.srv.Links.ForUser(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(e.handler)
	defer server.Close()
	payload, err := json.Marshal(map[string]string{"url": server.URL, "session": cookie.Value, "sub": "/sub/" + link.Token})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join("..", "..", "scripts", "test-web-ui.cjs"))
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser foundation: %v\n%s", err, output)
	}
	t.Log(string(output))
}

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
)

func TestVisualPresetRegistryAndCSS(t *testing.T) {
	e := newEnv(t)
	if len(e.srv.visualPresets.Presets) != 10 {
		t.Fatalf("expected ten reviewed source presets, got %d", len(e.srv.visualPresets.Presets))
	}
	for _, p := range e.srv.visualPresets.Presets {
		if !e.srv.visualPresets.known(p.ID) || !strings.Contains(string(e.srv.visualPresetCSS), `data-visual-preset="`+p.ID+`"`) {
			t.Fatalf("preset %s is not available in the stylesheet", p.ID)
		}
	}
	if got := e.srv.visualPresets.resolve("retired-or-invalid"); got != "wg-guard-neutral" {
		t.Fatalf("unknown preset must fall back safely, got %s", got)
	}
	if got := e.get("/assets/css/visual-presets.css", nil); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "--vp-sidebar-accent") || !strings.Contains(got.Body.String(), "--chart-first") {
		t.Fatal("generated CSS asset does not expose the source and semantic chart/sidebar tokens")
	}
}

func TestAppearancePrecedenceAndOwnerBoundary(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	owner := e.login("owner")
	csrf := deriveCSRF(owner.Value)
	if rec := e.get("/appearance", owner); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-choice="enterprise-blue"`) || !strings.Contains(rec.Body.String(), "تنظیم به‌عنوان پیش‌فرض پنل") {
		t.Fatalf("owner appearance: status=%d choice=%t action=%t", rec.Code, strings.Contains(rec.Body.String(), `data-choice="enterprise-blue"`), strings.Contains(rec.Body.String(), "تنظیم به‌عنوان پیش‌فرض پنل"))
	}
	if rec := e.post("/appearance/default", url.Values{"preset": {"claude-plus"}, "mode": {"dark"}}, owner, csrf); rec.Code != http.StatusBadRequest {
		t.Fatalf("panel default without confirmation: %d", rec.Code)
	}
	if rec := e.post("/appearance/default", url.Values{"preset": {"claude-plus"}, "mode": {"dark"}, "confirm": {"1"}}, owner, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("set panel default: %d", rec.Code)
	}
	if rec := e.get("/appearance", owner); !strings.Contains(rec.Body.String(), `data-visual-preset="claude-plus"`) || !strings.Contains(rec.Body.String(), `data-theme="dark"`) {
		t.Fatal("account without an override must inherit the panel preset and mode")
	}
	if rec := e.post("/appearance/me", url.Values{"preset": {"astrovista"}}, owner, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("personal preset: %d", rec.Code)
	}
	if rec := e.post("/appearance/default", url.Values{"preset": {"whatsapp"}, "mode": {"system"}, "confirm": {"1"}}, owner, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("change panel default: %d", rec.Code)
	}
	if rec := e.get("/appearance", owner); !strings.Contains(rec.Body.String(), `data-visual-preset="astrovista"`) || strings.Contains(rec.Body.String(), `data-theme=`) {
		t.Fatal("personal preset must survive a panel default change; system mode omits explicit theme")
	}
	request := httptest.NewRequest(http.MethodGet, "/appearance", nil)
	request.AddCookie(owner)
	request.AddCookie(&http.Cookie{Name: themeCookie, Value: "light"})
	response := httptest.NewRecorder()
	e.handler.ServeHTTP(response, request)
	if !strings.Contains(response.Body.String(), `data-theme="light"`) || !strings.Contains(response.Body.String(), `data-visual-preset="astrovista"`) {
		t.Fatal("legacy explicit mode cookie must remain independent of the visual preset")
	}
	if rec := e.post("/appearance/me", url.Values{"preset": {"unknown"}}, owner, csrf); rec.Code != http.StatusBadRequest {
		t.Fatalf("unregistered personal preset: %d", rec.Code)
	}
	if rec := e.post("/appearance/me/reset", url.Values{}, owner, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("reset personal preset: %d", rec.Code)
	}
	if rec := e.get("/appearance", owner); !strings.Contains(rec.Body.String(), `data-visual-preset="whatsapp"`) {
		t.Fatal("reset personal override must inherit the latest panel preset")
	}
	if _, err := e.admins.Create(context.Background(), "operator", testPassword, auth.RoleAdmin, nil); err != nil {
		t.Fatal(err)
	}
	other := e.login("operator")
	if rec := e.get("/appearance", other); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "تنظیم به‌عنوان پیش‌فرض پنل") {
		t.Fatal("non-owner should be able to choose personally without seeing panel-default controls")
	}
	if rec := e.post("/appearance/default", url.Values{"preset": {"tiesen"}, "mode": {"light"}, "confirm": {"1"}}, other, deriveCSRF(other.Value)); rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner changed panel default: %d", rec.Code)
	}
	if rec := e.post("/appearance/default/reset", url.Values{"confirm": {"1"}}, owner, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("reset panel default: %d", rec.Code)
	}
	if rec := e.get("/appearance", owner); strings.Contains(rec.Body.String(), `data-visual-preset=`) || !strings.Contains(rec.Body.String(), `data-theme="light"`) {
		t.Fatal("built-in reset must restore WG-Guard Neutral and Light")
	}
}

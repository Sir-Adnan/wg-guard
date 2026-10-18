package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/web/theme"
)

func TestVisualPresetRegistryAndCSS(t *testing.T) {
	e := newEnv(t)
	all := e.srv.themes.All()
	if len(all) < 20 || all[0].ID != theme.DefaultID {
		t.Fatalf("expected the reviewed preset set with the built-in first, got %d", len(all))
	}
	if got := e.srv.themes.Resolve("retired-or-invalid"); got != theme.DefaultID {
		t.Fatalf("unknown preset must fall back safely, got %s", got)
	}
	app := e.get("/assets/css/app.css", nil)
	if app.Code != http.StatusOK || !strings.Contains(app.Body.String(), "--sidebar-accent:") || !strings.Contains(app.Body.String(), "--chart-1:") || !strings.Contains(app.Body.String(), "@layer reset, tokens, base, components, layout, pages, utilities;") {
		t.Fatal("app.css must start with the layer order and carry the built-in preset tokens")
	}
	for _, p := range all[1:] {
		rec := e.get("/assets/css/themes/"+p.ID+".css", nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `:root[data-visual-preset="`+p.ID+`"]`) {
			t.Fatalf("preset %s stylesheet: status=%d", p.ID, rec.Code)
		}
	}
	if rec := e.get("/assets/css/theme-swatches.css", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `[data-swatch="linear"]`) {
		t.Fatal("swatch stylesheet must expose every preset for the gallery")
	}
}

func TestSubscriptionAppearanceDefaults(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	owner := e.login("owner")
	csrf := deriveCSRF(owner.Value)
	if rec := e.get("/appearance", owner); !strings.Contains(rec.Body.String(), `action="/appearance/subscription"`) || !strings.Contains(rec.Body.String(), `data-swatch="zen"`) {
		t.Fatal("owner must see the gallery and the subscription appearance form")
	}
	if rec := e.post("/appearance/subscription", url.Values{"sub_preset": {"unknown"}, "sub_mode": {""}}, owner, csrf); rec.Code != http.StatusBadRequest {
		t.Fatalf("unregistered subscription preset: %d", rec.Code)
	}
	if rec := e.post("/appearance/subscription", url.Values{"sub_preset": {"zen"}, "sub_mode": {"dark"}}, owner, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("set subscription appearance: %d", rec.Code)
	}
	sub := httptest.NewRequest(http.MethodGet, "/sub/anything", nil)
	if got := e.srv.appearanceFor(sub); got.Preset != "zen" || got.Mode != "dark" {
		t.Fatalf("subscription default not applied: %+v", got)
	}
	sub.AddCookie(&http.Cookie{Name: subPresetCookie, Value: "linear"})
	if got := e.srv.appearanceFor(sub); got.Preset != "linear" {
		t.Fatalf("a visitor's allowlisted preset must win: %+v", got)
	}
	bad := httptest.NewRequest(http.MethodGet, "/sub/anything", nil)
	bad.AddCookie(&http.Cookie{Name: subPresetCookie, Value: "x;y"})
	if got := e.srv.appearanceFor(bad); got.Preset != "zen" {
		t.Fatalf("an unknown visitor preset must be ignored: %+v", got)
	}
	panel := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	if got := e.srv.appearanceFor(panel); got.Preset != theme.DefaultID {
		t.Fatalf("the subscription default must not leak into the panel: %+v", got)
	}
	if _, err := e.admins.Create(context.Background(), "operator", testPassword, auth.RoleAdmin, nil); err != nil {
		t.Fatal(err)
	}
	other := e.login("operator")
	if rec := e.post("/appearance/subscription", url.Values{"sub_preset": {"tiesen"}}, other, deriveCSRF(other.Value)); rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner changed the subscription default: %d", rec.Code)
	}
	if rec := e.post("/appearance/default/reset", url.Values{"confirm": {"1"}}, owner, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("reset panel default: %d", rec.Code)
	}
	if got := e.srv.appearanceFor(httptest.NewRequest(http.MethodGet, "/sub/anything", nil)); got.Preset != theme.DefaultID || got.Mode != "light" {
		t.Fatalf("reset must clear the subscription default: %+v", got)
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
		t.Fatal("built-in reset must restore the built-in preset and Light")
	}
}

package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
)

func TestSettingsPageAndSave(t *testing.T) {
	e := newEnv(t)
	e.seedIface()
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)

	// GET renders the form prefilled from the registry defaults.
	rec := e.get("/settings", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"quota_presets", "100, 150"} {
		if !strings.Contains(body, want) {
			t.Fatalf("settings page missing %q", want)
		}
	}

	// POST persists values and creates follow through on the create form.
	rec = e.post("/settings", url.Values{
		"quota_presets":      {"10, 20, 30"},
		"dur_presets":        {"1, 2"},
		"default_quota_gb":   {"20"},
		"default_dur_months": {"3"},
		"default_device_lim": {"5"},
		"default_iface_id":   {""},
		"sub_base_url":       {"https://sub.example.com"},
	}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /settings: %d", rec.Code)
	}
	rec = e.get("/users", cookie)
	body = rec.Body.String()
	if !strings.Contains(body, "data-fill-value=\"30\"") {
		t.Fatal("users page did not pick up new quota presets")
	}
	if !strings.Contains(body, `name="device_limit" type="number" min="1" max="100" step="1"
             value="5"`) && !strings.Contains(body, "value=\"5\"") {
		t.Fatal("users page did not pick up default device limit")
	}

	// Invalid preset value → redisplay with the field marked, values kept.
	rec = e.post("/settings", url.Values{
		"quota_presets":      {"10, abc"},
		"dur_presets":        {"1"},
		"default_quota_gb":   {"20"},
		"default_dur_months": {"0"},
		"default_device_lim": {"1"},
	}, cookie, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid save: %d (want redisplay)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "abc") {
		t.Fatal("submitted values not preserved on error")
	}

	// Invalid base URL → redisplay with sub_base_url marked.
	rec = e.post("/settings", url.Values{
		"quota_presets":      {"10"},
		"dur_presets":        {"1"},
		"default_quota_gb":   {"0"},
		"default_dur_months": {"0"},
		"default_device_lim": {"1"},
		"sub_base_url":       {"ftp://nope"},
	}, cookie, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad base url: %d", rec.Code)
	}
}

func TestSettingsPersistentKeepaliveRange(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	csrf := deriveCSRF(cookie.Value)

	body := e.get("/settings", cookie).Body.String()
	for _, want := range []string{`name="keepalive" type="text"`, `dir="ltr"`, `value="25"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("keepalive field missing %q", want)
		}
	}
	rec := e.post("/settings", url.Values{"keepalive": {"25-35"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save ranged keepalive: %d %s", rec.Code, rec.Body.String())
	}
	if got, err := e.reg.GetString(context.Background(), "network.client_persistent_keepalive"); err != nil || got != "25-35" {
		t.Fatalf("stored keepalive = %q, %v", got, err)
	}
	if body = e.get("/settings", cookie).Body.String(); !strings.Contains(body, `value="25-35"`) {
		t.Fatal("ranged keepalive did not render back exactly")
	}

	rec = e.post("/settings", url.Values{"keepalive": {"35-25"}}, cookie, csrf)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `value="35-25"`) {
		t.Fatalf("invalid keepalive must redisplay exact input: %d", rec.Code)
	}
	if got, _ := e.reg.GetString(context.Background(), "network.client_persistent_keepalive"); got != "25-35" {
		t.Fatalf("invalid keepalive mutated setting: %q", got)
	}
}

func TestSettingsBackupSecretsAndGating(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	csrf := deriveCSRF(cookie.Value)

	// Set the backup password + telegram token.
	rec := e.post("/settings", url.Values{
		"backup_password": {"strong-pass-1"},
		"telegram_token":  {"12345:ABC"},
		"telegram_chat":   {"999"},
	}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("secret save: %d", rec.Code)
	}
	if pw, _ := e.reg.GetSecret(context.Background(), "backup.password"); pw != "strong-pass-1" {
		t.Fatal("backup password not stored")
	}
	body := e.get("/settings", cookie).Body.String()
	if !strings.Contains(body, "set</span>") {
		t.Fatal("secret state badges missing")
	}
	if strings.Contains(body, "strong-pass-1") {
		t.Fatal("secret value rendered back to the page")
	}

	// Empty fields keep the stored values.
	rec = e.post("/settings", url.Values{"telegram_chat": {"888"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("partial save: %d", rec.Code)
	}
	if pw, _ := e.reg.GetSecret(context.Background(), "backup.password"); pw != "strong-pass-1" {
		t.Fatal("empty field wiped the stored password")
	}
	if chat, _ := e.reg.GetString(context.Background(), "backup.telegram_chat"); chat != "888" {
		t.Fatal("chat id not updated")
	}

	// Weak password rejected by the registry validator.
	rec = e.post("/settings", url.Values{"backup_password": {"short"}}, cookie, csrf)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "8 characters") {
		t.Fatalf("weak password: %d", rec.Code)
	}

	// Clear checkboxes remove the secrets.
	rec = e.post("/settings", url.Values{
		"backup_password_clear": {"1"}, "telegram_token_clear": {"1"},
	}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("clear save: %d", rec.Code)
	}
	if pw, _ := e.reg.GetSecret(context.Background(), "backup.password"); pw != "" {
		t.Fatal("password survived clear")
	}
	if tok, _ := e.reg.GetSecret(context.Background(), "backup.telegram_token"); tok != "" {
		t.Fatal("token survived clear")
	}

	// A limited admin without node.settings cannot even read the page.
	if _, err := e.admins.Create(context.Background(), "viewer2", testPassword,
		auth.RoleAdmin, []string{auth.ScopeUsersRead}); err != nil {
		t.Fatal(err)
	}
	viewer := e.loginEN("viewer2")
	if rec := e.get("/settings", viewer); rec.Code != http.StatusSeeOther {
		t.Fatalf("limited admin read /settings: %d", rec.Code)
	}
	if rec := e.post("/settings", url.Values{"retention": {"5"}}, viewer,
		deriveCSRF(viewer.Value)); rec.Code != http.StatusSeeOther {
		t.Fatalf("limited admin wrote /settings: %d", rec.Code)
	}
}

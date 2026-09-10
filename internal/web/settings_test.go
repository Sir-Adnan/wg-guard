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

func TestSettingsSaveRejectsLateInputWithoutPartialPersistence(t *testing.T) {
	for _, tc := range []struct {
		name string
		form url.Values
	}{
		{
			name: "ordinary setting",
			form: url.Values{
				"node_id":   {"changed-before-error"},
				"retention": {"999"},
			},
		},
		{
			name: "secret setting",
			form: url.Values{
				"node_id":         {"changed-before-secret-error"},
				"backup_password": {"short"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.seedOwner()
			cookie := e.loginEN("owner")

			if err := e.reg.Set(context.Background(), "node.id", "saved-node"); err != nil {
				t.Fatal(err)
			}
			if err := e.reg.Set(context.Background(), "backup.password", "saved-password"); err != nil {
				t.Fatal(err)
			}
			rec := e.post("/settings", tc.form, cookie, deriveCSRF(cookie.Value))
			if rec.Code != http.StatusOK {
				t.Fatalf("invalid save: got %d, want 200", rec.Code)
			}
			if got, err := e.reg.GetString(context.Background(), "node.id"); err != nil || got != "saved-node" {
				t.Fatalf("earlier setting partially persisted: %q, %v", got, err)
			}
			if got, err := e.reg.GetSecret(context.Background(), "backup.password"); err != nil || got != "saved-password" {
				t.Fatalf("secret changed after rejected save: %q, %v", got, err)
			}
		})
	}
}

func TestSettingsValidationRedisplayPreservesInputAndSavedMetadata(t *testing.T) {
	e := newEnv(t)
	e.seedIface()
	e.seedOwner()
	cookie := e.loginEN("owner")
	ctx := context.Background()
	if err := e.reg.Set(ctx, "backup.password", "stored-secret"); err != nil {
		t.Fatal(err)
	}
	if body := e.get("/settings", cookie).Body.String(); !strings.Contains(body, `name="mtu" type="number" min="576" max="65535" step="1" value="1420"`) {
		t.Fatal("valid MTU did not render as a constrained number input")
	}

	rec := e.post("/settings", url.Values{
		"mtu":             {"twelve-eighty"},
		"backup_password": {"submitted-secret"},
	}, cookie, deriveCSRF(cookie.Value))
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid save: got %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`name="mtu" type="text" inputmode="numeric" value="twelve-eighty"`,
		`name="retention" type="text" inputmode="numeric" value="14"`,
		`<span class="badge ltr-data">dev</span>`,
		`<dd class="ltr-data">fake</dd>`,
		`value="remove"`,
		`>awg0</option>`,
		`badge badge--ok`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("validation redisplay missing %q", want)
		}
	}
	for _, secret := range []string{"submitted-secret", "stored-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("validation redisplay leaked secret %q", secret)
		}
	}
}

func TestSettingsValidationRedisplayPreservesEveryRawNumericInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		form url.Values
		want []string
	}{
		{
			name: "multiple malformed values",
			form: url.Values{
				"mtu":       {"twelve-eighty"},
				"retention": {"not-a-number"},
			},
			want: []string{
				`name="mtu" type="text" inputmode="numeric" value="twelve-eighty"`,
				`name="retention" type="text" inputmode="numeric" value="not-a-number"`,
			},
		},
		{
			name: "valid signed and padded values before later error",
			form: url.Values{
				"default_quota_gb": {"+1280"},
				"mtu":              {" 1280 "},
				"retention":        {"not-a-number"},
			},
			want: []string{
				`name="default_quota_gb" type="text" inputmode="numeric" value="&#43;1280"`,
				`name="mtu" type="text" inputmode="numeric" value=" 1280 "`,
				`name="retention" type="text" inputmode="numeric" value="not-a-number"`,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.seedOwner()
			cookie := e.loginEN("owner")
			rec := e.post("/settings", tc.form, cookie, deriveCSRF(cookie.Value))
			if rec.Code != http.StatusOK {
				t.Fatalf("invalid save: got %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Fatalf("validation redisplay missing %q", want)
				}
			}
		})
	}
}

func TestSettingsWriteFailureRedisplaysSafeSubmittedState(t *testing.T) {
	e := newEnv(t)
	e.seedIface()
	e.seedOwner()
	ctx := context.Background()
	for key, value := range map[string]any{
		"node.id":         "saved-node",
		"network.mtu":     1421,
		"backup.password": "saved-password",
	} {
		if err := e.reg.Set(ctx, key, value); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
	if _, err := e.db.Exec(`
		CREATE TRIGGER fail_settings_mtu_update
		BEFORE UPDATE ON settings
		WHEN NEW.key = 'network.mtu'
		BEGIN
			SELECT RAISE(FAIL, 'forced settings write failure');
		END`); err != nil {
		t.Fatal(err)
	}
	cookie := e.loginEN("owner")
	rec := e.post("/settings", url.Values{
		"node_id":         {"submitted-node"},
		"mtu":             {"1280"},
		"backup_password": {"submitted-password"},
	}, cookie, deriveCSRF(cookie.Value))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("write failure: got %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Something went wrong. Please try again.",
		`name="node_id" value="submitted-node"`,
		`name="mtu" type="text" inputmode="numeric" value="1280"`,
		`<span class="badge ltr-data">dev</span>`,
		`<dd class="ltr-data">fake</dd>`,
		`>awg0</option>`,
		`badge badge--ok`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("write-failure redisplay missing %q", want)
		}
	}
	for _, secret := range []string{"submitted-password", "saved-password", "forced settings write failure"} {
		if strings.Contains(body, secret) {
			t.Fatalf("write-failure redisplay leaked %q", secret)
		}
	}
	if got, _ := e.reg.GetString(ctx, "node.id"); got != "saved-node" {
		t.Fatalf("node id changed after rollback: %q", got)
	}
	if got, _ := e.reg.GetInt(ctx, "network.mtu"); got != 1421 {
		t.Fatalf("mtu changed after rollback: %d", got)
	}
	if got, _ := e.reg.GetSecret(ctx, "backup.password"); got != "saved-password" {
		t.Fatalf("password changed after rollback")
	}
}

func TestSettingsSaveCommitsOrdinaryAndSecretValuesTogether(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	rec := e.post("/settings", url.Values{
		"node_id":         {"atomic-node"},
		"mtu":             {"1280"},
		"backup_password": {"strong-password"},
	}, cookie, deriveCSRF(cookie.Value))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("valid save: got %d, want 303: %s", rec.Code, rec.Body.String())
	}
	ctx := context.Background()
	if got, _ := e.reg.GetString(ctx, "node.id"); got != "atomic-node" {
		t.Fatalf("node id = %q", got)
	}
	if got, _ := e.reg.GetInt(ctx, "network.mtu"); got != 1280 {
		t.Fatalf("mtu = %d", got)
	}
	if got, _ := e.reg.GetSecret(ctx, "backup.password"); got != "strong-password" {
		t.Fatalf("backup password was not committed")
	}
}

func TestSettingsSavePreservesEmptyAndClearSemantics(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	ctx := context.Background()
	for key, value := range map[string]any{
		"node.id":                   "saved-node",
		"network.mtu":               1280,
		"network.port_min":          31000,
		"backup.password":           "saved-password",
		"backup.telegram_token":     "saved-token",
		"downloads.filename_prefix": "custom-",
	} {
		if err := e.reg.Set(ctx, key, value); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
	cookie := e.loginEN("owner")
	rec := e.post("/settings", url.Values{
		"node_id":               {""},
		"mtu":                   {""},
		"backup_password":       {"replacement-password"},
		"backup_password_clear": {"1"},
	}, cookie, deriveCSRF(cookie.Value))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save: got %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if got, _ := e.reg.GetString(ctx, "node.id"); got != "" {
		t.Fatalf("empty text did not reset to default: %q", got)
	}
	if got, _ := e.reg.GetInt(ctx, "network.mtu"); got != 1280 {
		t.Fatalf("empty integer changed stored value: %d", got)
	}
	if got, _ := e.reg.GetInt(ctx, "network.port_min"); got != 31000 {
		t.Fatalf("absent integer changed stored value: %d", got)
	}
	if got, _ := e.reg.GetString(ctx, "downloads.filename_prefix"); got != "custom-" {
		t.Fatalf("absent text changed stored value: %q", got)
	}
	if got, _ := e.reg.GetSecret(ctx, "backup.password"); got != "" {
		t.Fatal("explicit clear did not win over replacement secret")
	}
	if got, _ := e.reg.GetSecret(ctx, "backup.telegram_token"); got != "saved-token" {
		t.Fatal("blank/absent secret did not keep stored value")
	}
}

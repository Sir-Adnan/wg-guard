package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
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

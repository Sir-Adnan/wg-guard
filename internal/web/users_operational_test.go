package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestUserDevicePartialStateAndSecretView(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	id := createUserViaForm(t, e, cookie, "partial")
	if _, err := e.db.Exec(`ALTER TABLE devices RENAME TO unavailable_devices`); err != nil {
		t.Fatal(err)
	}
	rec := e.get("/users/"+id, cookie)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Devices could not be loaded") || strings.Contains(rec.Body.String(), "No devices yet") {
		t.Fatal("failed device read must be unavailable, not empty")
	}
	for _, name := range []string{"PrivKeyEnc", "PSKEnc", "PrivateKey", "KeyMaterial"} {
		if _, exists := reflect.TypeFor[deviceView]().FieldByName(name); exists {
			t.Fatal("device template view carries secrets")
		}
	}
}

func TestUserBulkPartialOutcomeIsExplicit(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	id := createUserViaForm(t, e, cookie, "partial-bulk")
	rec := e.post("/users/bulk-action", url.Values{"ids": {id, "missing"}, "action": {"disable"}}, cookie, deriveCSRF(cookie.Value))
	location, _ := url.Parse(rec.Header().Get("Location"))
	if !strings.Contains(location.Query().Get("rawmsg"), "1 of 2") {
		t.Fatal("bulk result must disclose failed selections")
	}
}

func TestUserFormPersistenceFailurePreservesInput(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	if _, err := e.db.Exec(`CREATE TRIGGER reject_user BEFORE INSERT ON users BEGIN SELECT RAISE(ABORT, 'forced write failure'); END`); err != nil {
		t.Fatal(err)
	}
	rec := e.post("/users", url.Values{"username": {"preserved-user"}, "display_name": {"Preserved display"}}, cookie, deriveCSRF(cookie.Value))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `value="Preserved display"`) || strings.Contains(rec.Body.String(), "forced write failure") {
		t.Fatal("persistence failure must return safe retry form")
	}
}

func TestDeviceConfigFailureDoesNotRedirectToHTMLSuccess(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	rec := e.get("/devices/missing/config", cookie)
	if rec.Code != http.StatusNotFound || rec.Header().Get("Location") != "" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("failed config download must stay an uncached error response")
	}
}

func TestUserFormsRetainAllSubmittedValues(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	id := createUserViaForm(t, e, cookie, "existing")
	for _, path := range []string{"/users", "/users/" + id + "/edit", "/users/bulk"} {
		form := url.Values{"username": {"new-name"}, "display_name": {"Keep this name"}, "note": {"Keep this note"}, "tags": {"one,  two"}, "traffic_limit_value": {"bad-quota"}, "traffic_limit_unit": {"mb"}, "duration_value": {"bad-duration"}, "duration_unit": {"hours"}, "device_limit": {"bad-limit"}, "speed_down": {"bad-down"}, "speed_up": {"bad-up"}, "prefix": {"batch-"}, "count": {"bad-count"}, "start_index": {"bad-index"}, "start_policy": {"first_connection"}}
		rec := e.post(path, form, cookie, deriveCSRF(cookie.Value))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s status = %d; want validation form", path, rec.Code)
		}
		for _, value := range []string{"Keep this name", "Keep this note", "one,  two", "bad-quota", "bad-duration", "bad-limit", "bad-down", "bad-up"} {
			if !strings.Contains(rec.Body.String(), `value="`+value+`"`) {
				t.Errorf("%s lost %q", path, value)
			}
		}
		if !strings.Contains(rec.Body.String(), `aria-invalid="true"`) {
			t.Errorf("%s missing field feedback", path)
		}
	}
}

func TestUserActionFormsRetainInvalidInput(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	id := createUserViaForm(t, e, cookie, "actions")
	// Include noscript markup: enabling native fallback must not duplicate the
	// dialog input IDs or make an associated label address a hidden control.
	seen := map[string]bool{}
	for _, match := range regexp.MustCompile(`\sid="([^"]+)"`).FindAllStringSubmatch(e.get("/users/"+id, cookie).Body.String(), -1) {
		if seen[match[1]] {
			t.Errorf("duplicate detail ID: %s", match[1])
		}
		seen[match[1]] = true
	}
	for _, tc := range []struct {
		path     string
		values   url.Values
		retained string
	}{
		{"/renew", url.Values{"mode": {"from_now"}, "duration_value": {"invalid-duration"}, "duration_unit": {"hours"}}, "invalid-duration"},
		{"/traffic/add", url.Values{"traffic_value": {"invalid-traffic"}, "traffic_unit": {"mb"}}, "invalid-traffic"},
		{"/devices", url.Values{"name": {strings.Repeat("device", 20)}}, strings.Repeat("device", 20)},
	} {
		rec := e.post("/users/"+id+tc.path, tc.values, cookie, deriveCSRF(cookie.Value))
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `value="`+tc.retained+`"`) || !strings.Contains(rec.Body.String(), `aria-invalid="true"`) {
			t.Errorf("%s must retain invalid input in an accessible retry form; status %d", tc.path, rec.Code)
		}
	}
}

func TestUserOptionReadFailureRetainsAssociation(t *testing.T) {
	e := newEnv(t)
	id, _, csrf, cookie := e.seedUserWithDevice()
	cookie = e.loginEN("owner")
	csrf = deriveCSRF(cookie.Value)
	rec := e.post("/plans", url.Values{"name": {"Preserved plan"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatal("seed plan", rec.Code)
	}
	var pid, iid string
	if err := e.db.QueryRow(`SELECT id FROM plans LIMIT 1`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(`SELECT id FROM tunnel_interfaces LIMIT 1`).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE users SET plan_id=?, interface_id=? WHERE id=?`, pid, iid, id); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`ALTER TABLE plans RENAME TO unavailable_plans`, `ALTER TABLE tunnel_interfaces RENAME TO unavailable_interfaces`} {
		if _, err := e.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, rec := range []*httptest.ResponseRecorder{
		e.get("/users/"+id+"/edit", cookie),
		e.post("/users/"+id+"/edit", url.Values{"plan": {pid}, "interface": {iid}, "device_limit": {"bad"}}, cookie, csrf),
	} {
		for _, value := range []string{pid, iid} {
			if !strings.Contains(rec.Body.String(), `value="`+value+`" selected`) {
				t.Error("unavailable association must remain selected")
			}
		}
		if !strings.Contains(rec.Body.String(), "Options could not be loaded") {
			t.Error("option read failure must be disclosed")
		}
	}
	rec = e.post("/users/"+id+"/edit", url.Values{"plan": {""}, "interface": {""}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatal("explicit clear", rec.Code)
	}
	var planNull, ifaceNull bool
	if err := e.db.QueryRow(`SELECT plan_id IS NULL, interface_id IS NULL FROM users WHERE id=?`, id).Scan(&planNull, &ifaceNull); err != nil || !planNull || !ifaceNull {
		t.Fatal("explicit empty selection must still clear references")
	}
}

func TestUserIdentityErrorsMarkCorrectFields(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	for _, tc := range []struct {
		path, field string
		values      url.Values
	}{
		{"/users", "username", url.Values{"username": {"bad name"}}},
		{"/users/bulk", "prefix", url.Values{"prefix": {"bad prefix"}, "count": {"2"}, "start_index": {"1"}}},
		{"/users/bulk", "prefix", url.Values{"prefix": {strings.Repeat("x", 32)}, "count": {"2"}, "start_index": {"1"}}},
		{"/users/bulk", "start_index", url.Values{"prefix": {"batch"}, "count": {"2"}, "start_index": {"-1"}}},
	} {
		rec := e.post(tc.path, tc.values, cookie, deriveCSRF(cookie.Value))
		pattern := `<input[^>]*name="` + tc.field + `"[^>]*aria-invalid="true"`
		if rec.Code != http.StatusUnprocessableEntity || !regexp.MustCompile(pattern).MatchString(rec.Body.String()) {
			t.Errorf("%s must mark %s", tc.path, tc.field)
		}
	}
}

func TestUserOperationalPermissions(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	owner := e.loginEN("owner")
	id := createUserViaForm(t, e, owner, "restricted")
	cookie := e.limitedLogin(t, []string{"users.read"})
	for _, path := range []string{"/users", "/users/" + id} {
		rec := e.get(path, cookie)
		if rec.Code != http.StatusOK {
			t.Fatal(path, rec.Code)
		}
		for _, forbidden := range []string{`method="post" action="/users`, `/edit"`, `data-qr=`, `id="sub-url"`} {
			if strings.Contains(rec.Body.String(), forbidden) {
				t.Errorf("reader sees %s on %s", forbidden, path)
			}
		}
	}
	for _, path := range []string{"/users/new", "/users/" + id + "/edit", "/devices/missing/qr", "/devices/missing/config"} {
		if rec := e.get(path, cookie); !strings.Contains(rec.Header().Get("Location"), "common.denied") && rec.Code != http.StatusForbidden {
			t.Errorf("GET %s not permission denied: %d", path, rec.Code)
		}
	}
	for _, path := range []string{"/users", "/users/bulk", "/users/bulk-action", "/users/" + id + "/edit", "/users/" + id + "/delete", "/users/" + id + "/disable", "/users/" + id + "/restore", "/users/" + id + "/renew", "/users/" + id + "/traffic/add", "/users/" + id + "/traffic/reset", "/users/" + id + "/devices", "/users/" + id + "/sub/create", "/devices/missing/regenerate"} {
		rec := e.post(path, url.Values{}, cookie, deriveCSRF(cookie.Value))
		if !strings.Contains(rec.Header().Get("Location"), "common.denied") && rec.Code != http.StatusForbidden {
			t.Errorf("POST %s not permission denied: %d", path, rec.Code)
		}
	}
}

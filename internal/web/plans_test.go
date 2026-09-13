package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/iface"
)

func TestPlanCrudFlow(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)

	// Create via the form flow.
	form := url.Values{
		"_csrf": {csrf}, "name": {"basic"}, "traffic_limit_gb": {"50"},
		"duration_days": {"30"}, "device_limit": {"2"},
		"speed_down": {"1.28"}, "speed_up": {"0.64"},
		"start_policy": {"immediate"}, "enabled": {"1"},
	}
	rec := e.post("/plans", form, cookie, csrf)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/plans") {
		t.Fatalf("plan create: %d %s", rec.Code, rec.Header().Get("Location"))
	}

	// List shows the plan with its limits and user count.
	rec = e.get("/plans", cookie)
	body := rec.Body.String()
	for _, want := range []string{"basic", "50"} {
		if !strings.Contains(body, want) {
			t.Fatalf("plan list missing %q", want)
		}
	}

	id := ""
	if err := e.db.QueryRow(`SELECT id FROM plans WHERE name = 'basic'`).Scan(&id); err != nil {
		t.Fatal(err)
	}

	// Edit: clear the traffic limit (empty field = unlimited), rename.
	edit := url.Values{
		"_csrf": {csrf}, "name": {"basic-plus"}, "traffic_limit_gb": {""},
		"duration_days": {"30"}, "device_limit": {"2"},
		"speed_down": {"1.28"}, "speed_up": {"0.64"},
		"start_policy": {"immediate"}, "enabled": {"1"},
	}
	rec = e.post("/plans/"+id+"/edit", edit, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("plan edit: %d", rec.Code)
	}
	p, err := e.srv.Plans.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "basic-plus" || p.TrafficLimitBytes != nil {
		t.Fatalf("edit did not apply: name=%q traffic=%v", p.Name, p.TrafficLimitBytes)
	}
	if p.SpeedLimitDownKbps == nil || *p.SpeedLimitDownKbps != 10240 {
		t.Fatal("untouched speed limit must round-trip")
	}

	// Disable → inactive badge; delete → gone.
	rec = e.post("/plans/"+id+"/disable", url.Values{}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("plan disable: %d", rec.Code)
	}
	rec = e.get("/plans", cookie)
	if !strings.Contains(rec.Body.String(), "غیرفعال") {
		t.Fatal("disabled badge missing")
	}
	rec = e.post("/plans/"+id+"/delete", url.Values{}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("plan delete: %d", rec.Code)
	}
	rec = e.get("/plans", cookie)
	if strings.Contains(rec.Body.String(), "basic-plus") {
		t.Fatal("deleted plan still listed")
	}
}

func TestIfaceCrudFlow(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)

	// Regression: the create pages must render with no interface/plan data
	// (nil-pointer template guard).
	for _, path := range []string{"/interfaces/new", "/plans/new", "/users/new"} {
		if rec := e.get(path, cookie); rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
	}

	// Create: auto port and default subnet.
	form := url.Values{"_csrf": {csrf}, "name": {"awg0"}, "listen_port": {""}, "subnet": {""}}
	rec := e.post("/interfaces", form, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("iface create: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.get("/interfaces", cookie)
	if !strings.Contains(rec.Body.String(), "awg0") || !strings.Contains(rec.Body.String(), "10.8.0.0/24") {
		t.Fatal("iface list missing created profile")
	}

	var id string
	if err := e.db.QueryRow(`SELECT id FROM tunnel_interfaces WHERE name = 'awg0'`).Scan(&id); err != nil {
		t.Fatal(err)
	}

	// Edit with obfuscation enabled → rotation toast warns about clients.
	edit := url.Values{
		"_csrf": {csrf}, "mtu": {"1420"}, "enabled": {"1"}, "endpoint_override": {""},
		"obf_enabled": {"1"},
		"obf_jc":      {"4"}, "obf_jmin": {"40"}, "obf_jmax": {"80"},
		"obf_s1": {"15"}, "obf_s2": {"90"},
		"obf_h1": {"111"}, "obf_h2": {"222"}, "obf_h3": {"333"}, "obf_h4": {"444"},
	}
	rec = e.post("/interfaces/"+id+"/edit", edit, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("iface edit: %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Location"), "toast=ifaces.toast.rotation") {
		t.Fatalf("rotation warning missing: %s", rec.Header().Get("Location"))
	}
	i, err := e.srv.Ifaces.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !i.Obfuscation.Enabled || i.Obfuscation.Jc != 4 || i.Obfuscation.H4.String() != "444" {
		t.Fatalf("obfuscation not applied: %+v", i.Obfuscation)
	}

	// Delete is refused while devices reference the profile.
	uid := createUserViaForm(t, e, cookie, "iface-user")
	createDeviceViaForm(t, e, cookie, uid, "phone")
	rec = e.post("/interfaces/"+id+"/delete", url.Values{}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("blocked delete: %d", rec.Code)
	}
	if _, err := e.srv.Ifaces.Get(context.Background(), id); err != nil {
		t.Fatal("interface with devices must not be deletable")
	}

	// Disable toggles cleanly.
	rec = e.post("/interfaces/"+id+"/disable", url.Values{}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("iface disable: %d", rec.Code)
	}
	rec = e.get("/interfaces", cookie)
	if !strings.Contains(rec.Body.String(), "غیرفعال") {
		t.Fatal("disabled badge missing")
	}
}

func TestInterfaceObfuscationRangeForm(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner") // owner locale starts in Persian/RTL
	csrf := deriveCSRF(cookie.Value)
	created, err := e.ifaces.Create(context.Background(), iface.CreateInput{Name: "awg0", ListenPort: 39001})
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{
		"_csrf": {csrf}, "mtu": {"1420"}, "enabled": {"1"}, "endpoint_override": {""},
		"obf_enabled": {"1"}, "obf_jc": {"5"}, "obf_jmin": {"40"}, "obf_jmax": {"70"},
		"obf_s1": {"86"}, "obf_s2": {"61"}, "obf_s3": {"40"}, "obf_s4": {"48"},
		"obf_h1": {"100-110"}, "obf_h2": {"200"}, "obf_h3": {"300-310"}, "obf_h4": {"400"},
		"obf_i1": {"<r 90>"}, "obf_i2": {"aabb"}, "obf_i3": {""}, "obf_i4": {"ccdd"}, "obf_i5": {""},
		"obf_padding": {"10-20"}, "obf_rekey_after": {"120-180"},
		"obf_rekey_timeout": {"15-25"}, "obf_reject_after": {"90"},
		"obf_keepalive": {"30-45"}, "obf_max_handshake": {"4-8"},
	}
	rec := e.post("/interfaces/"+created.ID+"/edit", form, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("ranged form update: %d %s", rec.Code, rec.Body.String())
	}
	stored, err := e.ifaces.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Obfuscation.H1.String() != "100-110" || stored.Obfuscation.H3.String() != "300-310" ||
		stored.Obfuscation.ContentPaddingAddition.String() != "10-20" ||
		stored.Obfuscation.MaxHandshakeAttempts.String() != "4-8" ||
		stored.Obfuscation.I1 != "<r 90>" || stored.Obfuscation.I2 != "aabb" || stored.Obfuscation.I4 != "ccdd" {
		t.Fatalf("form ranges not preserved: %+v", stored.Obfuscation)
	}

	// Technical values stay LTR inside the Persian page and render the exact
	// interval. The page direction changes to English without changing the
	// input direction or value.
	editPath := "/interfaces/" + created.ID + "/edit"
	body := e.get(editPath, cookie).Body.String()
	for _, want := range []string{`<html lang="fa" dir="rtl" data-theme="light">`, `name="obf_h1" type="text" dir="ltr"`, `value="100-110"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("Persian range form missing %q", want)
		}
	}
	if rec = e.post("/prefs/locale", url.Values{"locale": {"en"}}, cookie, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("set English locale: %d", rec.Code)
	}
	body = e.get(editPath, cookie).Body.String()
	for _, want := range []string{`<html lang="en" dir="ltr" data-theme="light">`, `name="obf_h1" type="text" dir="ltr"`, `value="100-110"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("English range form missing %q", want)
		}
	}

	// Parser and relationship failures must not mutate the stored profile.
	badNumber := cloneValues(form)
	badNumber.Set("obf_jc", "five")
	rec = e.post(editPath, badNumber, cookie, csrf)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("malformed number response: %d", rec.Code)
	}
	after, _ := e.ifaces.Get(context.Background(), created.ID)
	if after.Obfuscation != stored.Obfuscation {
		t.Fatal("malformed numeric input mutated the interface")
	}
	badOverlap := cloneValues(form)
	badOverlap.Set("obf_h2", "105-120")
	rec = e.post(editPath, badOverlap, cookie, csrf)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("overlap response: %d", rec.Code)
	}
	after, _ = e.ifaces.Get(context.Background(), created.ID)
	if after.Obfuscation != stored.Obfuscation {
		t.Fatal("overlapping H ranges mutated the interface")
	}
}

func cloneValues(in url.Values) url.Values {
	out := make(url.Values, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func TestOperationalFormsPreserveInvalidInput(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)
	for _, tc := range []struct {
		path  string
		form  url.Values
		wants []string
	}{
		{"/plans", url.Values{"name": {"Retry plan"}, "traffic_limit_value": {"not-a-quota"}, "traffic_limit_unit": {"mb"}, "duration_value": {"3.5"}, "duration_unit": {"hours"}, "speed_down": {"oops"}, "enabled": {"0"}}, []string{`value="Retry plan"`, `value="not-a-quota"`, `value="3.5"`, `value="oops"`, `id="form-errors"`, `aria-invalid="true"`, `value="mb" selected`, `value="hours" selected`}},
		{"/interfaces", url.Values{"name": {"awg9"}, "listen_port": {"bad-port"}, "mtu": {"bad-mtu"}, "obf_enabled": {"1"}, "obf_h1": {"100-110"}, "obf_hpk": {"secret-do-not-redisplay"}, "obf_padding": {"not-a-range"}}, []string{`value="awg9"`, `value="bad-port"`, `value="bad-mtu"`, `value="100-110"`, `value="not-a-range"`, `id="form-errors"`, `aria-invalid="true"`, `id="awg-advanced" open`}},
	} {
		rec := e.post(tc.path, tc.form, cookie, csrf)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s failed form = %d", tc.path, rec.Code)
		}
		body := rec.Body.String()
		for _, want := range tc.wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %s", tc.path, want)
			}
		}
		if strings.Contains(body, "secret-do-not-redisplay") {
			t.Fatal("secret redisplayed")
		}
	}
	var plans, ifaces int
	_ = e.db.QueryRow(`SELECT count(*) FROM plans`).Scan(&plans)
	_ = e.db.QueryRow(`SELECT count(*) FROM tunnel_interfaces`).Scan(&ifaces)
	if plans != 0 || ifaces != 0 {
		t.Fatal("failed forms changed storage")
	}
}

func TestOperationalEnabledFormCanDisable(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)
	rec := e.post("/plans", url.Values{"name": {"Disabled plan"}, "enabled": {"0"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatal(rec.Code)
	}
	var id string
	if err := e.db.QueryRow(`SELECT id FROM plans WHERE name = 'Disabled plan'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	body := e.get("/plans/"+id+"/edit", cookie).Body.String()
	if !strings.Contains(body, `name="enabled"`) || !strings.Contains(body, `value="0" selected`) {
		t.Fatal("disabled plan form must submit an explicit zero")
	}
}

func TestOperationalFormPersistenceFailureAndPreviewRetry(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)
	if _, err := e.db.Exec(`CREATE TRIGGER fail_plan_save BEFORE INSERT ON plans BEGIN SELECT RAISE(ABORT, 'simulated storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	rec := e.post("/plans", url.Values{"name": {"Keep my work"}, "device_limit": {"3"}}, cookie, csrf)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `value="Keep my work"`) || strings.Contains(rec.Body.String(), "simulated storage") {
		t.Fatal("storage failure must preserve safe input and hide driver details")
	}
	for _, policy := range []string{"recommended", "randomized"} {
		preview := decodeProfilePreview(t, e.post("/interfaces/profile-preview", url.Values{"policy": {policy}}, cookie, csrf).Body.String())
		form := previewForm(preview.Fields, csrf, "awg0", policy, preview.Token)
		form.Set("listen_port", "bad")
		rec := e.post("/interfaces", form, cookie, csrf)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatal(rec.Code)
		}
		body := rec.Body.String()
		if policy == "recommended" {
			if !strings.Contains(body, preview.Token) {
				t.Fatal("nonsecret recommended preview must survive unrelated field error")
			}
			form.Set("listen_port", "39888")
			if retry := e.post("/interfaces", form, cookie, csrf); retry.Code != http.StatusSeeOther {
				t.Fatal("recommended preview retry failed")
			}
		} else {
			if strings.Contains(body, preview.Fields["obf_hpk"]) || strings.Contains(body, preview.Token) {
				t.Fatal("new secret or its unusable preview redisplayed")
			}
			if !strings.Contains(body, `name="profile_policy" value="randomized"`) {
				t.Fatal("failed preview must retain its policy instead of silently downgrading")
			}
		}
	}
}

func TestOperationalListSecondaryDataUnavailable(t *testing.T) {
	for _, table := range []string{"users", "devices", "tunnel_interfaces"} {
		t.Run(table, func(t *testing.T) {
			e := newEnv(t)
			e.seedOwner()
			cookie := e.login("owner")
			csrf := deriveCSRF(cookie.Value)
			i, err := e.ifaces.Create(t.Context(), iface.CreateInput{Name: "awg1", ListenPort: 39001})
			if err != nil {
				t.Fatal(err)
			}
			if rec := e.post("/plans", url.Values{"name": {"Referenced plan"}, "interface": {i.ID}}, cookie, csrf); rec.Code != http.StatusSeeOther {
				t.Fatal(rec.Code)
			}
			if _, err := e.db.Exec(`ALTER TABLE ` + table + ` RENAME TO unavailable_data`); err != nil {
				t.Fatal(err)
			}
			path := "/plans"
			if table == "devices" {
				path = "/interfaces"
			}
			rec := e.get(path, cookie)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ناموجود") {
				t.Fatalf("%s secondary failure must be distinct from zero", table)
			}
		})
	}
}

func TestPlanFormTechnicalValuesAreLossless(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)
	rec := e.post("/plans", url.Values{"name": {"Exact rates"}, "device_limit": {"3"}, "speed_down": {"1.28"}, "speed_up": {"0.64"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatal(rec.Code)
	}
	var id string
	if err := e.db.QueryRow(`SELECT id FROM plans WHERE name='Exact rates'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	body := e.get("/plans/"+id+"/edit", cookie).Body.String()
	for _, value := range []string{`value="1.28"`, `value="0.64"`} {
		if !strings.Contains(body, value) {
			t.Errorf("missing raw numeric form %s", value)
		}
	}
	body = e.get("/plans", cookie).Body.String()
	if strings.Contains(body, "3 کیلوبیت") || strings.Contains(body, "kbit/s") {
		t.Fatal("device counts or rates have misleading units")
	}
}

func TestOperationalPermissions(t *testing.T) {
	for _, scopes := range [][]string{{"users.read"}, {"plans.read", "interfaces.read"}, {"plans.write", "interfaces.write"}} {
		t.Run(strings.Join(scopes, "+"), func(t *testing.T) {
			e := newEnv(t)
			e.seedOwner()
			owner := e.login("owner")
			ownerCSRF := deriveCSRF(owner.Value)
			i, err := e.ifaces.Create(t.Context(), iface.CreateInput{Name: "awg0", ListenPort: 39001})
			if err != nil {
				t.Fatal(err)
			}
			e.post("/plans", url.Values{"name": {"Permission plan"}}, owner, ownerCSRF)
			var pid string
			if err := e.db.QueryRow(`SELECT id FROM plans WHERE name='Permission plan'`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			cookie := e.limitedLogin(t, scopes)
			csrf := deriveCSRF(cookie.Value)
			read := scopes[0] == "plans.read"
			write := scopes[0] == "plans.write"
			denied := func(rec *httptest.ResponseRecorder, path string) {
				t.Helper()
				if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "toast=common.denied") {
					t.Errorf("%s not denied: %d %s", path, rec.Code, rec.Header().Get("Location"))
				}
			}
			for _, base := range []string{"/plans", "/interfaces"} {
				rec := e.get(base, cookie)
				if read {
					if rec.Code != http.StatusOK {
						t.Fatal(rec.Code)
					}
					body := rec.Body.String()
					for _, forbidden := range []string{`href="` + base + `/new"`, `/edit"`, `/disable"`, `/delete"`, `/enable"`} {
						if strings.Contains(body, forbidden) {
							t.Errorf("reader sees write control %s", forbidden)
						}
					}
				} else {
					denied(rec, base)
				}
				id := pid
				if base == "/interfaces" {
					id = i.ID
				}
				for _, path := range []string{base + "/new", base + "/" + id + "/edit"} {
					rec := e.get(path, cookie)
					if write {
						if rec.Code != http.StatusOK {
							t.Fatal(path, rec.Code)
						}
					} else {
						denied(rec, path)
					}
				}
			}
			if !write {
				for _, path := range []string{"/plans", "/plans/" + pid + "/edit", "/plans/" + pid + "/enable", "/plans/" + pid + "/disable", "/plans/" + pid + "/delete", "/interfaces", "/interfaces/" + i.ID + "/edit", "/interfaces/" + i.ID + "/enable", "/interfaces/" + i.ID + "/disable", "/interfaces/" + i.ID + "/delete", "/interfaces/profile-preview"} {
					denied(e.post(path, url.Values{"name": {"awg1"}, "policy": {"recommended"}, "enabled": {"0"}}, cookie, csrf), path)
				}
				plan, _ := e.srv.Plans.Get(t.Context(), pid)
				after, _ := e.ifaces.Get(t.Context(), i.ID)
				if plan == nil || !plan.Enabled || plan.Name != "Permission plan" || after == nil || !after.Enabled {
					t.Fatal("denied mutation changed stored entities")
				}
				var count int
				_ = e.db.QueryRow(`SELECT count(*) FROM plans`).Scan(&count)
				if count != 1 {
					t.Fatal("denied create persisted plan")
				}
				_ = e.db.QueryRow(`SELECT count(*) FROM tunnel_interfaces`).Scan(&count)
				if count != 1 {
					t.Fatal("denied create persisted interface")
				}
			}
			body := e.get("/", cookie).Body.String()
			if !read && !write && (strings.Contains(body, `href="/plans"`) || strings.Contains(body, `href="/interfaces"`)) {
				t.Fatal("unrelated viewer sees denied navigation")
			}
			if write {
				if !strings.Contains(body, `href="/plans/new"`) || !strings.Contains(body, `href="/interfaces/new"`) {
					t.Fatal("write-only navigation must lead to authorized create page")
				}
				rec := e.post("/plans", url.Values{"name": {"Writer created"}}, cookie, csrf)
				if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/?") {
					t.Fatal("writer save must return to an authorized destination")
				}
				rec = e.post("/interfaces/profile-preview", url.Values{"policy": {"recommended"}}, cookie, csrf)
				if rec.Code != http.StatusOK {
					t.Fatal("writer preview denied")
				}
			}
		})
	}
}

func TestOperationalReadOnlyEmptyStateHasNoWriteCTA(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.limitedLogin(t, []string{"plans.read", "interfaces.read"})
	for _, path := range []string{"/plans", "/interfaces"} {
		rec := e.get(path, cookie)
		if rec.Code != http.StatusOK {
			t.Fatal(path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), `href="`+path+`/new"`) {
			t.Fatal("read-only empty state offers denied create action")
		}
	}
}

package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
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
		"speed_down": {"10240"}, "speed_up": {"5120"},
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
		"speed_down": {"10240"}, "speed_up": {"5120"},
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
	if !i.Obfuscation.Enabled || i.Obfuscation.Jc != 4 || i.Obfuscation.H4 != 444 {
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

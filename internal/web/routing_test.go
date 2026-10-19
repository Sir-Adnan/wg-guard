package web

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
)

const (
	routingTestPriv = "eLGK3fnLmMcRvJyWdMVT9ULJ2WA4xGuefpmm34dPnHI="
	routingTestPub  = "3FlgEEAdrHAeVfmj7bJyMnaKQsHajUQd/Nd0iRKzwQ8="
)

func routingConf() string {
	return "[Interface]\nPrivateKey = " + routingTestPriv + "\nAddress = 10.7.0.2/32\nJc = 4\nJmin = 40\nJmax = 70\nS1 = 0\nS2 = 0\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n" +
		"[Peer]\nPublicKey = " + routingTestPub + "\nEndpoint = 198.51.100.4:51820\n"
}

func assertNoRawKeys(t *testing.T, body string) {
	t.Helper()
	for _, prefix := range []string{"routing.", "egress.error", "nav.routing"} {
		if strings.Contains(body, ">"+prefix) || strings.Contains(body, "\""+prefix) {
			t.Fatalf("raw catalog key %q leaked into the page", prefix)
		}
	}
}

func TestRoutingWorkspaceEndToEnd(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	csrf := deriveCSRF(cookie.Value)
	ctx := context.Background()

	// Empty states guide the first step.
	body := e.get("/routing/outbounds", cookie).Body.String()
	for _, want := range []string{"No outbound yet", "/routing/outbounds/new?kind=warp", `href="/routing/outbounds" aria-current="page"`, "Routing"} {
		if !strings.Contains(body, want) {
			t.Fatalf("outbounds empty state missing %q", want)
		}
	}
	assertNoRawKeys(t, body)
	if rec := e.get("/routing/outbounds/new", cookie); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Extra server IP") {
		t.Fatalf("kind chooser: %d", rec.Code)
	}

	// A broken config is rejected in place, without echoing secrets.
	rec := e.post("/routing/outbounds", url.Values{"kind": {"wireguard"}, "name": {"NL"}, "config": {"[Interface]\nPrivateKey = " + routingTestPriv + "\n"}}, cookie, csrf)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "missing a required part") ||
		!strings.Contains(rec.Body.String(), "Paste the config again") {
		t.Fatalf("invalid config: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), routingTestPriv) {
		t.Fatal("private key echoed back into the form")
	}

	// Upload a config file (multipart) as a new outbound.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("_csrf", csrf)
	_ = mw.WriteField("kind", "wireguard")
	_ = mw.WriteField("name", "Netherlands")
	fw, _ := mw.CreateFormFile("config_file", "nl.conf")
	_, _ = fw.Write([]byte(routingConf()))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/routing/outbounds", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	up := httptest.NewRecorder()
	e.handler.ServeHTTP(up, req)
	if up.Code != http.StatusSeeOther || !strings.Contains(up.Header().Get("Location"), "routing.toast.outbound_created") {
		t.Fatalf("upload create: %d %s", up.Code, up.Header().Get("Location"))
	}
	outbounds, _ := e.egress.ListOutbounds(ctx)
	if len(outbounds) != 1 || !outbounds[0].Tunnel.Obfuscation.Enabled {
		t.Fatalf("outbounds: %+v", outbounds)
	}
	nl := outbounds[0]
	if rec := e.post("/routing/outbounds", url.Values{"kind": {"source_ip"}, "name": {"Second IP"}, "source_ip": {"203.0.113.7"}}, cookie, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("source ip: %d", rec.Code)
	}
	body = e.get("/routing/outbounds", cookie).Body.String()
	for _, want := range []string{"Netherlands", "AmneziaWG", "198.51.100.4:51820", "Second IP", "203.0.113.7", "Not used by any rule yet"} {
		if !strings.Contains(body, want) {
			t.Fatalf("outbound list missing %q", want)
		}
	}

	// HTMX test returns just the refreshed card.
	hx := httptest.NewRequest(http.MethodPost, "/routing/outbounds/"+nl.ID+"/test", strings.NewReader(url.Values{"_csrf": {csrf}}.Encode()))
	hx.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hx.Header.Set("HX-Request", "true")
	hx.AddCookie(cookie)
	hxRec := httptest.NewRecorder()
	e.handler.ServeHTTP(hxRec, hx)
	if hxRec.Code != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(hxRec.Body.String()), "<article") || strings.Contains(hxRec.Body.String(), "<html") {
		t.Fatalf("htmx card: %d %q", hxRec.Code, hxRec.Body.String())
	}

	// Balancer over both outbounds.
	src := outbounds[0]
	for _, o := range func() []*egress.Outbound { l, _ := e.egress.ListOutbounds(ctx); return l }() {
		if o.Kind == egress.KindSourceIP {
			src = o
		}
	}
	rec = e.post("/routing/balancers", url.Values{"name": {"AI exits"}, "strategy": {"sticky"}, "fallback": {"direct"}, "enabled": {"1"},
		"member": {nl.ID, src.ID}, "weight_" + nl.ID: {"3"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("balancer: %d %s", rec.Code, rec.Body.String())
	}
	balancers, _ := e.egress.ListBalancers(ctx)
	if len(balancers) != 1 || balancers[0].Members[0].Weight != 3 || balancers[0].Members[1].Weight != 1 {
		t.Fatalf("balancer members: %+v", balancers)
	}
	if body := e.get("/routing/balancers", cookie).Body.String(); !strings.Contains(body, "AI exits") || !strings.Contains(body, "Sticky per device") {
		t.Fatal("balancer list")
	}
	if rec := e.post("/routing/balancers", url.Values{"name": {"Empty"}, "strategy": {"sticky"}}, cookie, csrf); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Pick at least one member") {
		t.Fatalf("empty balancer: %d", rec.Code)
	}

	// Rule form shows the grouped presets; save a multi-source rule.
	body = e.get("/routing/rules/new?preset=gemini", cookie).Body.String()
	if !strings.Contains(body, `value="gemini" checked`) || !strings.Contains(body, "AI services") || !strings.Contains(body, "IP lookup services") {
		t.Fatal("rule form presets")
	}
	rec = e.post("/routing/rules", url.Values{"name": {"Gemini"}, "enabled": {"1"}, "target": {"balancer:" + balancers[0].ID},
		"preset": {"gemini", "ip-lookup"}, "matchers": {"geosite:google-deepmind\nipdata.co\n8.8.4.0/24"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("rule: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.post("/routing/rules", url.Values{"name": {"Bad"}, "target": {"direct"}, "matchers": {"not a domain!"}}, cookie, csrf)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "not a domain!") {
		t.Fatalf("invalid matcher feedback: %d", rec.Code)
	}
	body = e.get("/routing/rules", cookie).Body.String()
	for _, want := range []string{"Gemini", "Google Gemini", "IP lookup services", "Domains: 1", "Categories: 1", "IP ranges: 1", "AI exits", "Everything else"} {
		if !strings.Contains(body, want) {
			t.Fatalf("rule list missing %q", want)
		}
	}
	// Site check uses the same compiled plan as the kernel.
	body = e.get("/routing/rules?check=https://gemini.google.com/app", cookie).Body.String()
	if !strings.Contains(body, "Matches rule") || !strings.Contains(body, "gemini.google.com") {
		t.Fatalf("site check:\n%s", body)
	}
	if body := e.get("/routing/rules?check=8.8.4.4", cookie).Body.String(); !strings.Contains(body, "Matches rule") {
		t.Fatal("address check")
	}
	if body := e.get("/routing/rules?check=example.org", cookie).Body.String(); !strings.Contains(body, "the default route applies") {
		t.Fatal("default check")
	}

	// Default route and deletion protection.
	if rec := e.post("/routing/rules/default", url.Values{"default_target": {"outbound:" + nl.ID}, "block_dns": {"1"}}, cookie, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("default: %d", rec.Code)
	}
	if st, _ := e.egress.GetSettings(ctx); st.DefaultTarget.ID != nl.ID || !st.BlockEncryptedDNS {
		t.Fatalf("settings: %+v", st)
	}
	rec = e.post("/routing/outbounds/"+nl.ID+"/delete", nil, cookie, csrf)
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if msg := loc.Query().Get("rawmsg"); !strings.Contains(msg, "AI exits") || !strings.Contains(msg, "still used") {
		t.Fatalf("in-use deletion message: %q", msg)
	}

	// Persian rendering: no raw keys and RTL.
	if rec := e.post("/prefs/locale", url.Values{"locale": {"fa"}}, cookie, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("set locale: %d", rec.Code)
	}
	for _, path := range []string{"/routing/outbounds", "/routing/balancers", "/routing/rules", "/routing/rules/new", "/routing/balancers/new", "/routing/outbounds/new?kind=warp", "/routing/outbounds/" + nl.ID + "/edit"} {
		rec := e.get(path, cookie)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `dir="rtl"`) {
			t.Fatalf("%s is not RTL", path)
		}
		assertNoRawKeys(t, rec.Body.String())
	}
}

func TestRoutingRequiresInterfacePermissions(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	owner := e.loginEN("owner")
	csrf := deriveCSRF(owner.Value)
	if rec := e.post("/admins/create", url.Values{"username": {"viewer"}, "password": {testPassword}, "permissions": {"users.read"}}, owner, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("create admin: %d", rec.Code)
	}
	viewer := e.login("viewer")
	if rec := e.get("/routing/outbounds", viewer); rec.Code == http.StatusOK {
		t.Fatal("routing visible without interfaces.read")
	}
	if strings.Contains(e.get("/users", viewer).Body.String(), `href="/routing/outbounds"`) {
		t.Fatal("routing nav shown without permission")
	}
}

func TestOutboundKindKeyWARPPlusFollowsAccountType(t *testing.T) {
	for typ, want := range map[string]string{
		"free":      "routing.kind.warp",
		"":          "routing.kind.warp",
		"limited":   "routing.kind.warp_plus",
		"unlimited": "routing.kind.warp_plus",
	} {
		o := &egress.Outbound{Kind: egress.KindWARP, WARP: &egress.WARPAccount{AccountType: typ, Plus: true}}
		if got := outboundKindKey(o); got != want {
			t.Errorf("%q: %s, want %s", typ, got, want)
		}
	}
}

func TestOutboundEditRejectsBadConfigBeforeSaving(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	csrf := deriveCSRF(cookie.Value)
	ctx := context.Background()
	nl, err := e.egress.CreateWireGuard(ctx, "Netherlands", routingConf())
	if err != nil {
		t.Fatal(err)
	}
	rec := e.post("/routing/outbounds/"+nl.ID+"/edit", url.Values{"name": {"Renamed"}, "mtu": {"1300"},
		"config": {"[Interface]\nPrivateKey = " + routingTestPriv + "\n"}}, cookie, csrf)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("broken config on edit: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), routingTestPriv) {
		t.Fatal("private key echoed back into the form")
	}
	got, err := e.egress.GetOutbound(ctx, nl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Netherlands" || got.Tunnel.MTU != nl.Tunnel.MTU {
		t.Fatalf("a rejected edit must not save its other fields: %q mtu %d", got.Name, got.Tunnel.MTU)
	}
}

// Chaining, failover priority, the separate encrypted-DNS form, in-use
// delete blocking and error toasts.
func TestRoutingChainPriorityAndDNSForms(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	csrf := deriveCSRF(cookie.Value)
	ctx := context.Background()
	de, err := e.egress.CreateWireGuard(ctx, "Germany", routingConf())
	if err != nil {
		t.Fatal(err)
	}
	nl, err := e.egress.CreateWireGuard(ctx, "Netherlands", routingConf())
	if err != nil {
		t.Fatal(err)
	}

	// The edit form offers the other tunneled outbound as a hop.
	body := e.get("/routing/outbounds/"+nl.ID+"/edit", cookie).Body.String()
	if !strings.Contains(body, `name="via"`) || !strings.Contains(body, `value="`+de.ID+`"`) || strings.Contains(body, `value="`+nl.ID+`"`) {
		t.Fatal("chain choices")
	}
	if rec := e.post("/routing/outbounds/"+nl.ID+"/edit", url.Values{"name": {"Netherlands"}, "via": {de.ID}}, cookie, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("chain: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := e.egress.GetOutbound(ctx, nl.ID); got.Tunnel.Via != de.ID {
		t.Fatalf("via not stored: %+v", got.Tunnel)
	}
	// The hop is in use: its card shows who uses it and offers no delete.
	body = e.get("/routing/outbounds", cookie).Body.String()
	if !strings.Contains(body, "Goes through") || !strings.Contains(body, "In use. Remove it") {
		t.Fatal("chain on cards")
	}
	rec := e.post("/routing/outbounds/"+de.ID+"/delete", nil, cookie, csrf)
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "rawerr=1") {
		t.Fatalf("a refused delete must flash an error: %q", loc)
	}
	if body := e.get(loc, cookie).Body.String(); !strings.Contains(body, `alert--destructive" role="alert"><svg`) || !strings.Contains(body, "It is still used") {
		t.Fatal("a refused delete must render as an error")
	}

	// Failover priority orders members regardless of the list order.
	rec = e.post("/routing/balancers", url.Values{"name": {"Backup"}, "strategy": {"failover"}, "fallback": {"block"}, "enabled": {"1"},
		"member": {de.ID, nl.ID}, "order_" + de.ID: {"2"}, "order_" + nl.ID: {"1"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("balancer: %d %s", rec.Code, rec.Body.String())
	}
	balancers, _ := e.egress.ListBalancers(ctx)
	if len(balancers) != 1 || balancers[0].Members[0].OutboundID != nl.ID {
		t.Fatalf("failover priority: %+v", balancers[0].Members)
	}

	// The encrypted-DNS form leaves the default route alone and vice versa.
	if rec := e.post("/routing/rules/default", url.Values{"default_target": {"outbound:" + de.ID}}, cookie, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("default: %d", rec.Code)
	}
	if rec := e.post("/routing/rules/default", url.Values{"dns_form": {"1"}}, cookie, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("dns: %d", rec.Code)
	}
	st, _ := e.egress.GetSettings(ctx)
	if st.BlockEncryptedDNS || st.DefaultTarget.ID != de.ID {
		t.Fatalf("settings: %+v", st)
	}
	body = e.get("/routing/rules", cookie).Body.String()
	if !strings.Contains(body, `id="encrypted-dns"`) || !strings.Contains(body, "Private DNS provider hostname") || !strings.Contains(body, "<optgroup") {
		t.Fatal("rules page DNS card")
	}
	assertNoRawKeys(t, body)
}

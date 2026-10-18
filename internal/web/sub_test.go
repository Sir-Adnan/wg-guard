package web

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/reconcile"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
	"github.com/Sir-Adnan/wg-guard/internal/user"
	"github.com/Sir-Adnan/wg-guard/migrations"
)

// seedUserWithDevice provisions a user + device through the real handlers and
// returns (userID, deviceID, csrf, cookie).
func (e *env) seedUserWithDevice() (string, string, string, *http.Cookie) {
	e.t.Helper()
	e.seedIface()
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)
	rec := e.post("/users", url.Values{"username": {"alice"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		e.t.Fatalf("user create: %d", rec.Code)
	}
	u, err := e.srv.Users.GetByUsername(context.Background(), "alice")
	if err != nil {
		e.t.Fatal(err)
	}
	keys, err := e.srv.generateKeys(nil, false)
	if err != nil {
		e.t.Fatal(err)
	}
	d, err := e.srv.Devices.Create(context.Background(), u.ID, "phone", *keys, "")
	if err != nil {
		e.t.Fatalf("device create: %v", err)
	}
	return u.ID, d.ID, csrf, cookie
}

func TestPublicSubscriptionPresentationBoundaries(t *testing.T) {
	e := newEnv(t)
	uid, did, _, _ := e.seedUserWithDevice()
	link, err := e.srv.Links.ForUser(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	base := e.subBase(link.Token)
	rec := e.get(base+"?lang=en", nil)
	if strings.Contains(rec.Body.String(), `class="sub-qr" src=`) || !strings.Contains(rec.Body.String(), `data-qr=`) {
		t.Fatal("public QR must load only on explicit request")
	}
	if field, exists := reflect.TypeFor[subDevice]().FieldByName("D"); !exists || field.Type != reflect.TypeFor[*deviceView]() {
		t.Fatal("public templates must receive keyless device summaries")
	}
	if _, err := e.db.Exec(`UPDATE devices SET enabled=0 WHERE id=?`, did); err != nil {
		t.Fatal(err)
	}
	rec = e.get(base+"?lang=en", nil)
	if !strings.Contains(rec.Body.String(), "This device is disabled") {
		t.Fatal("disabled device must not be shown as merely offline")
	}
	if rec = e.get(base+"/devices/"+did+"/config", nil); rec.Code != http.StatusOK {
		t.Fatal("presentation must preserve existing disabled-device config access")
	}
	if _, err := e.db.Exec(`ALTER TABLE devices RENAME TO unavailable_devices`); err != nil {
		t.Fatal(err)
	}
	rec = e.get(base+"?lang=en", nil)
	if !strings.Contains(rec.Body.String(), "Devices could not be loaded") || strings.Contains(rec.Body.String(), "No devices yet") {
		t.Fatal("failed device read must not imply empty account")
	}
}

func TestPublicSubscriptionRunningLowNoticeAndCommands(t *testing.T) {
	e := newEnv(t)
	uid, _, _, _ := e.seedUserWithDevice()
	link, err := e.srv.Links.ForUser(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	base := e.subBase(link.Token)
	set := func(query string, args ...any) {
		t.Helper()
		if _, err := e.db.Exec(query, append(args, uid)...); err != nil {
			t.Fatal(err)
		}
	}
	notice := func() string {
		t.Helper()
		body := e.get(base+"?lang=en", nil).Body.String()
		if !strings.Contains(body, `<code class="tech" translate="no">sudo awg-quick up awg0</code>`) || !strings.Contains(body, `data-sub-copy hidden`) {
			t.Fatal("CLI guide must render copyable LTR commands with a JS-revealed copy button")
		}
		if i := strings.Index(body, `data-sub-attention="`); i >= 0 {
			rest := body[i+len(`data-sub-attention="`):]
			return rest[:strings.IndexByte(rest, '"')]
		}
		return ""
	}
	soon := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	later := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	set(`UPDATE users SET status='active', traffic_limit_bytes=1000, traffic_used_rx=100, traffic_used_tx=0, expires_at=? WHERE id=?`, later)
	if got := notice(); got != "" {
		t.Fatalf("healthy subscription must not warn, got %q", got)
	}
	set(`UPDATE users SET traffic_used_rx=900 WHERE id=?`)
	if got := notice(); got != "data_low" {
		t.Fatalf("90%% used: got %q", got)
	}
	set(`UPDATE users SET expires_at=? WHERE id=?`, soon)
	if got := notice(); got != "both_low" {
		t.Fatalf("low data and time: got %q", got)
	}
	set(`UPDATE users SET traffic_used_rx=0 WHERE id=?`)
	if got := notice(); got != "time_low" {
		t.Fatalf("two days left: got %q", got)
	}
	set(`UPDATE users SET status='disabled' WHERE id=?`)
	if got := notice(); got != "" {
		t.Fatalf("unusable subscription keeps its status notice, got %q", got)
	}
}

func TestPublicSubscriptionStatusAndErrorSurfaces(t *testing.T) {
	e := newEnv(t)
	uid, did, _, _ := e.seedUserWithDevice()
	link, err := e.srv.Links.ForUser(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	base := e.subBase(link.Token)
	for _, tc := range []struct{ status, text string }{
		{"active", "Ready to connect"}, {"disabled", "Access is disabled"}, {"suspended", "Access is suspended"},
		{"expired", "Your subscription has expired"}, {"traffic_exceeded", "Your data allowance is used up"}, {"waiting_first_connection", "Starts with your first connection"},
	} {
		if _, err := e.db.Exec(`UPDATE users SET status=? WHERE id=?`, tc.status, uid); err != nil {
			t.Fatal(err)
		}
		rec := e.get(base+"?lang=en", nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.text) {
			t.Errorf("missing status presentation %s", tc.status)
		}
		if wantReady := tc.status == "active" || tc.status == "waiting_first_connection"; !strings.Contains(rec.Body.String(), `data-access-ready="`+strconv.FormatBool(wantReady)+`"`) {
			t.Errorf("wrong device-first eligibility for %s", tc.status)
		}
	}
	unknown := e.get("/sub/unknown?lang=en", nil)
	if _, err := e.srv.Links.SetRevoked(context.Background(), uid, true); err != nil {
		t.Fatal(err)
	}
	revoked := e.get(base+"?lang=en", nil)
	parsedBase, _ := url.Parse(base)
	revokedMarkup := strings.ReplaceAll(revoked.Body.String(), parsedBase.Path, "/sub/unknown")
	if unknown.Code != http.StatusNotFound || revoked.Code != http.StatusNotFound || unknown.Body.String() != revokedMarkup {
		t.Fatal("unknown and revoked links must share identical error surfaces")
	}
	if revoked.Header().Get("Cache-Control") != "no-store" || !strings.Contains(revoked.Body.String(), `dir="ltr"`) || !strings.Contains(revoked.Body.String(), "This subscription link is unavailable.") || strings.Contains(revoked.Body.String(), `href="/login"`) {
		t.Fatal("public error must be localized, uncached and free of admin navigation")
	}
	if _, err := e.srv.Links.SetRevoked(context.Background(), uid, false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`ALTER TABLE tunnel_interfaces RENAME TO unavailable_interfaces`); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"config", "qr"} {
		rec := e.get(base+"/devices/"+did+"/"+suffix+"?lang=en", nil)
		if rec.Code != http.StatusNotFound || rec.Header().Get("Location") != "" || rec.Header().Get("Cache-Control") != "no-store" {
			t.Error("failed delivery must preserve uncached HTTP failure")
		}
	}
}

// subBase is the token URL used by the public endpoints (host-prefixed).
func (e *env) subBase(token string) string {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	return e.srv.subURLFor(req, token)
}

func TestSubscriptionPageLifecycle(t *testing.T) {
	e := newEnv(t)
	userID, deviceID, csrf, cookie := e.seedUserWithDevice()

	// Creation ensured a link; the detail page shows its URL.
	rec := e.get("/users/"+userID, cookie)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/sub/") {
		t.Fatalf("detail page missing sub url: %d", rec.Code)
	}
	link, err := e.srv.Links.ForUser(context.Background(), userID)
	if err != nil || link == nil || link.Token == "" {
		t.Fatalf("link not ensured: %v", err)
	}
	subURL := e.subBase(link.Token)

	// Public page renders for the token.
	rec = e.get(subURL, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "alice") {
		t.Fatalf("sub page: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), deviceID) {
		t.Fatal("sub page does not list the device")
	}

	// Per-device QR + config endpoints work through the token.
	rec = e.get(subURL+"/devices/"+deviceID+"/qr", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("sub qr: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = e.get(subURL+"/devices/"+deviceID+"/config", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sub config: %d", rec.Code)
	}

	// Another user's device is not reachable through this token.
	keys2, err := e.srv.generateKeys(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	u2, err := e.srv.Users.Create(context.Background(), user.Input{Username: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	d2, err := e.srv.Devices.Create(context.Background(), u2.ID, "tab", *keys2, "")
	if err != nil {
		t.Fatal(err)
	}
	rec = e.get(subURL+"/devices/"+d2.ID+"/config", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user config leaked: %d", rec.Code)
	}
	rec = e.get(subURL+"/devices/"+d2.ID+"/qr", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user QR leaked: %d", rec.Code)
	}

	// Unknown token = plain 404.
	rec = e.get("/sub/definitely-not-a-token", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown token: %d", rec.Code)
	}

	// Revoke → 404 everywhere; restore → back.
	if _, err := e.srv.Links.SetRevoked(context.Background(), userID, true); err != nil {
		t.Fatal(err)
	}
	rec = e.get(subURL, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("revoked page: %d", rec.Code)
	}
	if _, err := e.srv.Links.SetRevoked(context.Background(), userID, false); err != nil {
		t.Fatal(err)
	}
	rec = e.get(subURL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("restored page: %d", rec.Code)
	}

	// Regenerate via the admin action: old URL dies, new works.
	rec = e.post("/users/"+userID+"/sub/regenerate", url.Values{}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("regenerate: %d", rec.Code)
	}
	rec = e.get(subURL, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("old token alive after rotate: %d", rec.Code)
	}
	link2, err := e.srv.Links.ForUser(context.Background(), userID)
	if err != nil || link2.Token == "" || link2.Token == link.Token {
		t.Fatalf("rotate produced no new token: %v", err)
	}
	rec = e.get(e.subBase(link2.Token), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("new token page: %d", rec.Code)
	}
}

func TestSubscriptionRevokeReplacesLinkAndEveryDeviceCredential(t *testing.T) {
	e := newEnv(t)
	userID, deviceID, csrf, cookie := e.seedUserWithDevice()

	secondKeys, err := e.srv.generateKeys(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.srv.Devices.Create(context.Background(), userID, "tablet", *secondKeys, "")
	if err != nil {
		t.Fatal(err)
	}
	beforeLink, err := e.srv.Links.ForUser(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	beforeDevices, err := e.srv.Devices.ListForUser(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	beforeKeys := map[string]string{}
	beforeConfigs := map[string]string{}
	for _, d := range beforeDevices {
		beforeKeys[d.ID] = d.PublicKey
		beforeConfigs[d.ID], err = e.srv.ClientConf.Render(context.Background(), d.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	backend := fake.New()
	e.srv.Reconciler = &reconcile.Engine{DB: e.db, Backend: backend, Ring: e.srv.Ring, Policy: reconcile.PolicyReport}
	if rep, err := e.srv.Reconciler.Run(context.Background()); err != nil || len(rep.Errors) != 0 {
		t.Fatalf("initial runtime peers: %+v, %v", rep, err)
	}

	rec := e.post("/users/"+userID+"/sub/revoke", url.Values{}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("revoke: %d", rec.Code)
	}
	afterLink, err := e.srv.Links.ForUser(context.Background(), userID)
	if err != nil || afterLink == nil || afterLink.Token == "" || afterLink.Token == beforeLink.Token || afterLink.Revoked() {
		t.Fatalf("revoke did not issue active replacement access: %v", err)
	}
	if rec := e.get(e.subBase(beforeLink.Token), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("old subscription link remains valid: %d", rec.Code)
	}
	if rec := e.get(e.subBase(afterLink.Token), nil); rec.Code != http.StatusOK {
		t.Fatalf("replacement subscription link unavailable: %d", rec.Code)
	}
	afterDevices, err := e.srv.Devices.ListForUser(context.Background(), userID)
	if err != nil || len(afterDevices) != 2 {
		t.Fatalf("replacement devices: %d, %v", len(afterDevices), err)
	}
	for _, d := range afterDevices {
		if d.PublicKey == beforeKeys[d.ID] {
			t.Errorf("device %s retained its public credential", d.ID)
		}
		afterConfig, renderErr := e.srv.ClientConf.Render(context.Background(), d.ID)
		if renderErr != nil {
			t.Fatal(renderErr)
		}
		if afterConfig == beforeConfigs[d.ID] {
			t.Errorf("device %s retained its client configuration", d.ID)
		}
	}
	runtime, err := backend.Dump(context.Background(), "awg0")
	if err != nil || len(runtime.Peers) != len(afterDevices) {
		t.Fatalf("replacement runtime peer count: %d, %v", len(runtime.Peers), err)
	}
	for _, peer := range runtime.Peers {
		for _, oldKey := range beforeKeys {
			if peer.PublicKey == oldKey {
				t.Fatal("old client credential survived runtime reconciliation")
			}
		}
	}
	var pending int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM retired_peer_keys`).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("retired peer queue not acknowledged: %d, %v", pending, err)
	}
	if deviceID == second.ID {
		t.Fatal("fixture devices unexpectedly share an identity")
	}
	body := e.get("/users/"+userID, cookie).Body.String()
	if strings.Contains(body, `/sub/regenerate`) || strings.Contains(body, `/sub/restore`) {
		t.Fatal("detail page still offers overlapping regenerate/restore actions")
	}
}

func TestSubscriptionRevokeRequiresDeviceWrite(t *testing.T) {
	e := newEnv(t)
	userID, deviceID, _, _ := e.seedUserWithDevice()
	if _, err := e.admins.Create(context.Background(), "user-editor", testPassword,
		auth.RoleAdmin, []string{auth.ScopeUsersRead, auth.ScopeUsersUpdate}); err != nil {
		t.Fatal(err)
	}
	before, err := e.srv.Devices.Get(context.Background(), deviceID)
	if err != nil {
		t.Fatal(err)
	}
	link, err := e.srv.Links.ForUser(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	cookie := e.login("user-editor")
	body := e.get("/users/"+userID, cookie).Body.String()
	if strings.Contains(body, `action="/users/`+userID+`/sub/revoke"`) {
		t.Fatal("revoke control exposed without device write")
	}
	rec := e.post("/users/"+userID+"/sub/revoke", url.Values{}, cookie, deriveCSRF(cookie.Value))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("denial response: %d", rec.Code)
	}
	after, err := e.srv.Devices.Get(context.Background(), deviceID)
	if err != nil || after.PublicKey != before.PublicKey {
		t.Fatalf("limited administrator rotated a device: %v", err)
	}
	still, err := e.srv.Links.ForUser(context.Background(), userID)
	if err != nil || still.Token != link.Token {
		t.Fatalf("limited administrator rotated subscription access: %v", err)
	}
}

func TestSubscriptionRevokeRollsBackCredentialsWhenLinkWriteFails(t *testing.T) {
	e := newEnv(t)
	userID, deviceID, csrf, cookie := e.seedUserWithDevice()
	before, err := e.srv.Devices.Get(context.Background(), deviceID)
	if err != nil {
		t.Fatal(err)
	}
	link, err := e.srv.Links.ForUser(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`CREATE TRIGGER block_sub_rotation BEFORE UPDATE ON sub_links
		BEGIN SELECT RAISE(ABORT, 'forced rollback'); END`); err != nil {
		t.Fatal(err)
	}
	rec := e.post("/users/"+userID+"/sub/revoke", url.Values{}, cookie, csrf)
	if rec.Code == http.StatusSeeOther && strings.Contains(rec.Header().Get("Location"), "sub.toast.replaced") {
		t.Fatal("failed capability rotation reported success")
	}
	after, err := e.srv.Devices.Get(context.Background(), deviceID)
	if err != nil || after.PublicKey != before.PublicKey {
		t.Fatalf("device credential changed despite rolled-back capability: %v", err)
	}
	still, err := e.srv.Links.ForUser(context.Background(), userID)
	if err != nil || still.Token != link.Token {
		t.Fatalf("subscription capability changed despite rollback: %v", err)
	}
	var retired int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM retired_peer_keys`).Scan(&retired); err != nil || retired != 0 {
		t.Fatalf("retired-peer intent committed despite rollback: %d, %v", retired, err)
	}
}

func TestUserConfigArchiveContainsCanonicalConfigs(t *testing.T) {
	e := newEnv(t)
	userID, _, _, cookie := e.seedUserWithDevice()
	keys, err := e.srv.generateKeys(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.Devices.Create(context.Background(), userID, "tablet", *keys, ""); err != nil {
		t.Fatal(err)
	}

	rec := e.get("/users/"+userID+"/configs.zip", cookie)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("config archive response: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	reader, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	devices, err := e.srv.Devices.ListForUser(context.Background(), userID)
	if err != nil || len(reader.File) != len(devices) {
		t.Fatalf("archive entries: %d, devices: %d, err: %v", len(reader.File), len(devices), err)
	}
	want := map[string]string{}
	for _, d := range devices {
		config, renderErr := e.srv.ClientConf.Render(context.Background(), d.ID)
		if renderErr != nil {
			t.Fatal(renderErr)
		}
		want[e.srv.configFilename(httptest.NewRequest(http.MethodGet, "/", nil), d)] = config
	}
	for _, file := range reader.File {
		stream, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		var content bytes.Buffer
		_, copyErr := content.ReadFrom(stream)
		_ = stream.Close()
		if copyErr != nil {
			t.Fatal(copyErr)
		}
		if expected, ok := want[file.Name]; !ok || content.String() != expected {
			t.Errorf("archive entry %q is not the canonical configuration", file.Name)
		}
		delete(want, file.Name)
	}
	if len(want) != 0 {
		t.Fatalf("archive omitted %d configurations", len(want))
	}
}

func TestSubPageLocaleSwitch(t *testing.T) {
	e := newEnv(t)
	userID, _, _, _ := e.seedUserWithDevice()
	link, err := e.srv.Links.ForUser(context.Background(), userID)
	if err != nil || link == nil {
		t.Fatal(err)
	}
	rec := e.get(e.subBase(link.Token)+"?lang=en", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `dir="ltr"`) {
		t.Fatalf("en sub page: %d", rec.Code)
	}
	rec = e.get(e.subBase(link.Token), nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `dir="rtl"`) {
		t.Fatalf("fa sub page default: %d", rec.Code)
	}
}

func TestSubscriptionLayoutPickerAndPreview(t *testing.T) {
	e := newEnv(t)
	userID, _, csrf, cookie := e.seedUserWithDevice()
	link, err := e.srv.Links.ForUser(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	subURL := e.subBase(link.Token)
	page := e.get("/appearance", cookie).Body.String()
	for _, layout := range subLayouts {
		if !strings.Contains(page, `name="sub_layout" value="`+layout+`"`) {
			t.Fatalf("appearance must offer the %s design", layout)
		}
	}
	if !strings.Contains(page, `name="sub_layout" value="ticket" checked`) || strings.Contains(e.get("/settings", cookie).Body.String(), `name="sub_layout"`) {
		t.Fatal("the design is chosen once, under Appearance, and defaults to ticket")
	}
	heroes := map[string]string{"ticket": `class="ticket"`, "card": `class="member"`, "minimal": `class="lean"`, "portal": `class="portal"`}
	for _, layout := range subLayouts {
		if rec := e.post("/appearance/subscription", url.Values{"sub_layout": {layout}}, cookie, csrf); rec.Code != http.StatusSeeOther {
			t.Fatalf("save %s: HTTP %d", layout, rec.Code)
		}
		body := e.get(subURL+"?lang=en", nil).Body.String()
		if !strings.Contains(body, `data-sub-layout="`+layout+`"`) || !strings.Contains(body, heroes[layout]) || strings.Count(body, "<h1") != 1 {
			t.Fatalf("design %s must apply to the existing link with its hero and one heading", layout)
		}
		for _, section := range []string{`id="sub-tab-connect"`, `id="sub-tab-guide"`, `id="sub-tab-details"`, `<details class="sub-app"`, `name="sub-faq"`} {
			if !strings.Contains(body, section) {
				t.Fatalf("design %s lacks shared section %s", layout, section)
			}
		}
	}
	if rec := e.post("/appearance/subscription", url.Values{"sub_layout": {"unlisted"}}, cookie, csrf); rec.Code != http.StatusBadRequest {
		t.Fatalf("unlisted design: HTTP %d", rec.Code)
	}
	if got := e.srv.subLayout(context.Background()); got != "portal" {
		t.Fatalf("a rejected save changed the design to %q", got)
	}

	preview := e.get("/appearance/subscription/preview?layout=card&preset=linear&mode=dark&lang=en", cookie)
	body := preview.Body.String()
	if preview.Code != http.StatusOK || !strings.Contains(body, `data-sub-layout="card"`) || !strings.Contains(body, `data-visual-preset="linear"`) ||
		!strings.Contains(body, `data-theme="dark"`) || !strings.Contains(body, "data-sub-preview") || !strings.Contains(body, "Monthly 50 GB") {
		t.Fatalf("preview must render unsaved choices with sample data: HTTP %d", preview.Code)
	}
	if strings.Contains(body, "/sub/") || strings.Contains(body, "data-config-download") || strings.Contains(body, "alice") {
		t.Fatal("preview must not expose real links, users or live actions")
	}
	if got := e.srv.subLayout(context.Background()); got != "portal" {
		t.Fatal("preview must not save")
	}
	if _, err := e.admins.Create(context.Background(), "operator", testPassword, auth.RoleAdmin, nil); err != nil {
		t.Fatal(err)
	}
	other := e.login("operator")
	if rec := e.get("/appearance/subscription/preview", other); rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner preview: HTTP %d", rec.Code)
	}
	if rec := e.post("/appearance/subscription", url.Values{"sub_layout": {"card"}}, other, deriveCSRF(other.Value)); rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner design change: HTTP %d", rec.Code)
	}
	if rec := e.post("/appearance/default/reset", url.Values{"confirm": {"1"}}, cookie, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("reset defaults: HTTP %d", rec.Code)
	}
	if got := e.srv.subLayout(context.Background()); got != "ticket" {
		t.Fatalf("reset must restore the default design, got %q", got)
	}
}

func TestSubscriptionLayoutMigrationMapsRetiredDesigns(t *testing.T) {
	e := newEnv(t)
	migration, err := migrations.Read("0019_subscription_layouts.sql")
	if err != nil {
		t.Fatal(err)
	}
	for retired, successor := range map[string]string{"pass": "ticket", "split": "ticket", "compact": "minimal", "portal": "portal"} {
		if _, err := e.db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('subscription.layout', ?, '2026-01-01T00:00:00Z')
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, retired); err != nil {
			t.Fatal(err)
		}
		if _, err := e.db.Exec(string(migration)); err != nil {
			t.Fatal(err)
		}
		var got string
		if err := e.db.QueryRow(`SELECT value FROM settings WHERE key = 'subscription.layout'`).Scan(&got); err != nil || got != successor {
			t.Fatalf("%s migrated to %q (%v), want %s", retired, got, err, successor)
		}
	}
}

func TestSubRateLimit(t *testing.T) {
	e := newEnv(t)
	userID, _, _, _ := e.seedUserWithDevice()
	link, err := e.srv.Links.ForUser(context.Background(), userID)
	if err != nil || link == nil {
		t.Fatal(err)
	}
	path := e.subBase(link.Token)
	var last int
	for i := 0; i < 65; i++ {
		last = e.get(path, nil).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("rate limit not enforced, last=%d", last)
	}
}

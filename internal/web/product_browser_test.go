package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
)

// Product checks are opt-in and milestone-scoped; they do not replay the shell
// regression. All mutations stay in the disposable test database.
func TestBrowserPhase10(t *testing.T) {
	node := os.Getenv("WG_TEST_BROWSER_NODE")
	if node == "" {
		t.Skip("set WG_TEST_BROWSER_NODE and WG_TEST_PLAYWRIGHT for browser checks")
	}
	e := newEnv(t)
	uid, did, csrf, cookie := e.seedUserWithDevice()
	reader := e.limitedLogin(t, []string{auth.ScopePlansRead, auth.ScopeIfaceRead})
	rec := e.post("/plans", url.Values{"name": {"Monthly / ماهانه"}, "duration_days": {"30"}, "traffic_limit_gb": {"50"}, "device_limit": {"3"}}, cookie, csrf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("seed plan: status %d", rec.Code)
	}
	var pid, iid string
	if err := e.db.QueryRow(`SELECT id FROM plans LIMIT 1`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(`SELECT id FROM tunnel_interfaces LIMIT 1`).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	link, err := e.srv.Links.ForUser(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if suite := os.Getenv("WG_TEST_UI_SUITE"); suite != "" && suite != "10.2" {
		seedBrowserTelemetry(t, e, uid, true)
	}
	server := httptest.NewServer(browserQAHandler(e, false))
	defer server.Close()
	seed := map[string]string{
		"url": server.URL, "session": cookie.Value, "sub": "/sub/" + link.Token,
		"reader": reader.Value,
		"csrf":   csrf,
		"user":   uid, "device": did, "plan": pid, "iface": iid,
		"samples": "24",
	}
	if os.Getenv("WG_TEST_UI_SUITE") == "final" {
		seed["samples"] = "180"
	}
	if suite := os.Getenv("WG_TEST_UI_SUITE"); suite == "10.5" || suite == "10.6" || suite == "final" {
		setup := newEnv(t)
		setupServer := httptest.NewServer(setup.handler)
		defer setupServer.Close()
		seed["setupURL"] = setupServer.URL
		seed["loginPassword"] = rand.Text()
		if _, err := e.srv.Admins.Create(context.Background(), "browser-login", seed["loginPassword"], auth.RoleAdmin, []string{auth.ScopeStatsRead}); err != nil {
			t.Fatal("seed login account")
		}
	}
	if os.Getenv("WG_TEST_UI_SUITE") == "final" || os.Getenv("WG_TEST_UI_STATE_MODE") == "preview" {
		cases, err := json.Marshal(finalBrowserStates(t))
		if err != nil {
			t.Fatal(err)
		}
		seed["states"] = string(cases)
	}
	payload, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join("..", "..", "scripts", "test-web-product.cjs"))
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("product browser: %v", err)
	}
}

// Each composition cell represents a separate client. This test-only wrapper
// prevents a fast viewport matrix from masquerading as one abusive subscriber;
// production rate limits stay unchanged and the explicit limited case uses them.
func browserQAHandler(e *env, rateLimited bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/sub/") {
			if client := r.Header.Get("X-WG-QA-Client"); client != "" {
				r = r.Clone(r.Context())
				h := sha256.Sum256([]byte(client))
				r.RemoteAddr = "192.0.2." + strconv.Itoa(int(h[0])%254+1) + ":12345"
			}
			if rateLimited {
				for n := 0; n <= e.srv.subRL.max; n++ {
					e.srv.subRL.fail(clientIP(r), time.Now())
				}
			}
		}
		e.handler.ServeHTTP(w, r)
	})
}

type browserState struct {
	Name, Base, Session, CSRF, Path string
	Prepare                         string
	Status                          int
	Public                          bool
}

// States reuse the existing ephemeral service fixture and real panel routes.
// No screenshots, configurations or credentials are stored as test fixtures.
func finalBrowserStates(t *testing.T) []browserState {
	var cases []browserState
	serve := func(e *env, limited bool) string {
		server := httptest.NewServer(browserQAHandler(e, limited))
		t.Cleanup(server.Close)
		return server.URL
	}
	add := func(e *env, base, session, name, path string, status int, public bool) {
		cases = append(cases, browserState{Name: name, Base: base, Session: session, CSRF: deriveCSRF(session), Path: path, Status: status, Public: public})
	}
	empty := newEnv(t)
	empty.seedOwner()
	cookie := empty.login("owner")
	base := serve(empty, false)
	if _, err := empty.db.Exec(`DELETE FROM audit_log`); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/users", "/plans", "/interfaces", "/tokens", "/webhooks", "/audit", "/backups", "/dashboard"} {
		add(empty, base, cookie.Value, "empty-"+strings.TrimPrefix(path, "/"), path, 200, false)
	}
	add(empty, base, cookie.Value, "missing-user", "/users/not-present", 404, false)
	add(empty, base, cookie.Value, "filtered-users", "/users?q=not-present", 200, false)
	reader := empty.limitedLogin(t, []string{auth.ScopeUsersRead})
	add(empty, base, reader.Value, "restricted-workspace", "/", 200, false)
	add(empty, base, reader.Value, "permission-denied", "/settings", 200, false)
	if _, err := empty.db.Exec(`DELETE FROM audit_log`); err != nil {
		t.Fatal(err)
	}

	states := newEnv(t)
	uid, did, _, owner := states.seedUserWithDevice()
	stateBase := serve(states, false)
	if _, err := states.db.Exec(`UPDATE devices SET enabled=0 WHERE id=?`, did); err != nil {
		t.Fatal(err)
	}
	link, err := states.srv.Links.ForUser(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	add(states, stateBase, "", "public-disabled-device", "/sub/"+link.Token, 200, true)
	prepare := func(name, path, action string, public bool) {
		session := owner.Value
		if public {
			session = ""
		}
		cases = append(cases, browserState{Name: name, Base: stateBase, Session: session, CSRF: deriveCSRF(session), Path: path, Status: 200, Public: public, Prepare: action})
	}
	for _, entry := range [][3]string{
		{"interface-advanced", "/interfaces/new", "advanced"}, {"interface-validation", "/interfaces/new", "invalid-interface"},
		{"plan-validation", "/plans/new", "invalid-plan"}, {"user-validation", "/users/new", "invalid-user"},
		{"settings-validation", "/settings", "invalid-settings"}, {"user-create-sheet", "/users", "user-create"},
		{"calendar", "/users/new", "calendar"}, {"device-dialog", "/users/" + uid, "device-dialog"},
		{"renew-dialog", "/users/" + uid, "renew-dialog"}, {"traffic-dialog", "/users/" + uid, "traffic-dialog"},
		{"destructive-dialog", "/users/" + uid, "destructive-dialog"}, {"admin-qr", "/users/" + uid, "qr-open"},
		{"token-validation", "/tokens", "invalid-token"}, {"token-secret", "/tokens", "token-secret"},
		{"webhook-secret", "/webhooks", "webhook-secret"}, {"schedule-validation", "/backups?schedule=new", "schedule-error"},
		{"restore-review", "/backups", "restore-review"}, {"restore-pending", "/backups", "restore-pending"},
	} {
		prepare(entry[0], entry[1], entry[2], false)
	}
	for _, mode := range []string{"qr-open", "qr-loading", "qr-error", "download-error"} {
		prepare("public-"+mode, "/sub/"+link.Token, mode, true)
	}
	if _, err := states.srv.Backup.Create(context.Background(), backup.CreateOpts{}); err != nil {
		t.Fatal("state archive creation failed")
	}
	for _, entry := range [][2]string{{"empty", "active"}, {"paused", "disabled"}, {"expired", "expired"}, {"quota", "traffic_exceeded"}, {"waiting", "waiting_first_connection"}, {"suspended", "suspended"}} {
		id := createUserViaForm(t, states, owner, "case-"+entry[0])
		if _, err := states.db.Exec(`UPDATE users SET status=?, traffic_limit_bytes=2000000, traffic_used_rx=500000, traffic_used_tx=750000 WHERE id=?`, entry[1], id); err != nil {
			t.Fatal(err)
		}
		if entry[0] == "expired" {
			if _, err := states.db.Exec(`UPDATE users SET expires_at=? WHERE id=?`, time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339Nano), id); err != nil {
				t.Fatal(err)
			}
		}
		if entry[0] == "quota" {
			if _, err := states.db.Exec(`UPDATE users SET traffic_used_tx=3000000 WHERE id=?`, id); err != nil {
				t.Fatal(err)
			}
		}
		if entry[0] == "waiting" {
			if _, err := states.db.Exec(`UPDATE users SET start_policy='first_connection', activated_at=NULL, expires_at=NULL, duration_seconds=2592000 WHERE id=?`, id); err != nil {
				t.Fatal(err)
			}
		}
		link, err := states.srv.Links.ForUser(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		add(states, stateBase, "", "public-"+entry[0], "/sub/"+link.Token, 200, true)
		add(states, stateBase, owner.Value, "user-"+entry[0], "/users/"+id, 200, false)
	}
	revokedID := createUserViaForm(t, states, owner, "case-revoked")
	revokedLink, err := states.srv.Links.ForUser(context.Background(), revokedID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := states.srv.Links.SetRevoked(context.Background(), revokedID, true); err != nil {
		t.Fatal(err)
	}
	add(states, stateBase, "", "public-revoked", "/sub/"+revokedLink.Token, 404, true)
	add(states, stateBase, owner.Value, "admin-revoked-subscription", "/users/"+revokedID, 200, false)

	failed := newEnv(t)
	failedID, _, _, failedOwner := failed.seedUserWithDevice()
	failedBase := serve(failed, false)
	failedLink, err := failed.srv.Links.ForUser(context.Background(), failedID)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"devices", "plans", "tunnel_interfaces", "traffic_rollups"} {
		if _, err := failed.db.Exec(`ALTER TABLE ` + table + ` RENAME TO unavailable_` + table); err != nil {
			t.Fatal(err)
		}
	}
	failed.srv.Backup = nil
	for _, entry := range [][2]string{{"user-partial", "/users/" + failedID}, {"form-references-unavailable", "/users/" + failedID + "/edit"}, {"dashboard-unavailable", "/dashboard"}, {"backup-unavailable", "/backups"}, {"settings-references-unavailable", "/settings"}, {"plans-unavailable", "/plans"}, {"interfaces-unavailable", "/interfaces"}} {
		add(failed, failedBase, failedOwner.Value, entry[0], entry[1], 200, false)
	}
	add(failed, failedBase, "", "public-devices-unavailable", "/sub/"+failedLink.Token, 200, true)

	stale := newEnv(t)
	staleID, _, _, staleOwner := stale.seedUserWithDevice()
	seedBrowserTelemetry(t, stale, staleID)
	stale.srv.Telemetry = telemetry.New(telemetry.SourceFunc(func(_ context.Context, at time.Time) (telemetry.RawSample, error) {
		cpu := 0.0
		return telemetry.RawSample{At: at, HostAvailable: true, CPUPercent: &cpu, MemTotalBytes: 2000000, MemAvailableBytes: 1000000}, nil
	}), telemetry.DefaultCadence)
	_, _ = stale.srv.Telemetry.Sample(context.Background(), time.Now().Add(-30*time.Second))
	// Maintain the synthetic sample's relative age during a long matrix, without
	// touching a host sampler or changing the production request path.
	staleHandler := browserQAHandler(stale, false)
	staleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dashboard" || r.URL.Path == "/dashboard/live" {
			_, _ = stale.srv.Telemetry.Sample(r.Context(), time.Now().Add(-30*time.Second))
		}
		staleHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(staleServer.Close)
	add(stale, staleServer.URL, staleOwner.Value, "telemetry-stale-partial", "/dashboard", 200, false)
	limited := newEnv(t)
	limited.seedOwner()
	add(limited, serve(limited, true), "", "public-rate-limited", "/sub/unavailable", 429, true)
	return cases
}

func seedBrowserTelemetry(t *testing.T, e *env, uid string, keepFresh ...bool) {
	t.Helper()
	i := uint64(0)
	sampler := telemetry.New(telemetry.SourceFunc(func(_ context.Context, at time.Time) (telemetry.RawSample, error) {
		i++
		cpu, load := float64(18+i*7%53), 0.34
		return telemetry.RawSample{
			At: at, HostAvailable: true, CPUPercent: &cpu, Load1: &load, Uptime: 27 * time.Hour,
			MemTotalBytes: 2_000_000_000, MemAvailableBytes: 1_200_000_000 + (i%200)*2_000_000,
			DiskTotalBytes: 40_000_000_000, DiskFreeBytes: 29_000_000_000,
			ProcessMetricsAvailable: true, ProcessRSSBytes: 29_000_000, ProcessHeapBytes: 11_000_000,
			HostNetwork:       telemetry.Counter{Identity: "test-host", Available: true, RXBytes: i * i * 50000, TXBytes: i * i * 12000},
			VPNNetwork:        telemetry.Counter{Identity: "test-vpn", Available: true, RXBytes: i * i * 30000, TXBytes: i * i * 10000},
			ActivityAvailable: true, OnlineUsers: 1, ActivePeers: 1,
			InterfacesAvailable: true, EnabledInterfaces: 1, ObservedInterfaces: 1,
			ReadinessAvailable: true, Ready: true, AccountingAvailable: true,
		}, nil
	}), telemetry.DefaultCadence)
	now := time.Now().UTC()
	count := 24
	if os.Getenv("WG_TEST_UI_SUITE") == "final" {
		count = telemetry.HistoryCapacity
	}
	for n := count - 1; n >= 0; n-- {
		if _, err := sampler.Sample(context.Background(), now.Add(-time.Duration(n)*telemetry.DefaultCadence)); err != nil {
			t.Fatal(err)
		}
	}
	e.srv.Telemetry = sampler
	if len(keepFresh) > 0 && keepFresh[0] {
		// One synthetic scheduler keeps the normal state healthy through long QA;
		// page requests still read snapshots and the production sampler is untouched.
		stop, done := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			ticker := time.NewTicker(telemetry.DefaultCadence)
			defer ticker.Stop()
			for {
				select {
				case at := <-ticker.C:
					_, _ = sampler.Sample(context.Background(), at)
				case <-stop:
					return
				}
			}
		}()
		t.Cleanup(func() { close(stop); <-done })
	}
	for n := 0; n < 24; n++ {
		seedRollup(t, e, uid, "hourly", now.Truncate(time.Hour).Add(-time.Duration(n)*time.Hour), int64((n+1)*(n+3))*70000, int64(n+1)*35000)
	}
	for n := 0; n < 7; n++ {
		seedRollup(t, e, uid, "daily", now.Truncate(24*time.Hour).Add(-time.Duration(n)*24*time.Hour), int64((n+1)*3)*700000, int64(n+1)*250000)
	}
}

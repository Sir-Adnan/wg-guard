package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
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
		seedBrowserTelemetry(t, e, uid)
	}
	server := httptest.NewServer(e.handler)
	defer server.Close()
	seed := map[string]string{
		"url": server.URL, "session": cookie.Value, "sub": "/sub/" + link.Token,
		"reader": reader.Value,
		"csrf":   csrf,
		"user":   uid, "device": did, "plan": pid, "iface": iid,
	}
	if suite := os.Getenv("WG_TEST_UI_SUITE"); suite == "10.5" || suite == "final" {
		setup := newEnv(t)
		setupServer := httptest.NewServer(setup.handler)
		defer setupServer.Close()
		seed["setupURL"] = setupServer.URL
		seed["loginPassword"] = rand.Text()
		if _, err := e.srv.Admins.Create(context.Background(), "browser-login", seed["loginPassword"], auth.RoleAdmin, []string{auth.ScopeStatsRead}); err != nil {
			t.Fatal("seed login account")
		}
	}
	payload, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join("..", "..", "scripts", "test-web-product.cjs"))
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("product browser: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func seedBrowserTelemetry(t *testing.T, e *env, uid string) {
	t.Helper()
	i := uint64(0)
	sampler := telemetry.New(telemetry.SourceFunc(func(_ context.Context, at time.Time) (telemetry.RawSample, error) {
		i++
		cpu, load := float64(18+i*7%53), 0.34
		return telemetry.RawSample{
			At: at, HostAvailable: true, CPUPercent: &cpu, Load1: &load, Uptime: 27 * time.Hour,
			MemTotalBytes: 2_000_000_000, MemAvailableBytes: 1_200_000_000 + i*2_000_000,
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
	for n := 23; n >= 0; n-- {
		if _, err := sampler.Sample(context.Background(), now.Add(-time.Duration(n)*telemetry.DefaultCadence)); err != nil {
			t.Fatal(err)
		}
	}
	e.srv.Telemetry = sampler
	for n := 0; n < 24; n++ {
		seedRollup(t, e, uid, "hourly", now.Truncate(time.Hour).Add(-time.Duration(n)*time.Hour), int64((n+1)*(n+3))*70000, int64(n+1)*35000)
	}
	for n := 0; n < 7; n++ {
		seedRollup(t, e, uid, "daily", now.Truncate(24*time.Hour).Add(-time.Duration(n)*24*time.Hour), int64((n+1)*3)*700000, int64(n+1)*250000)
	}
}

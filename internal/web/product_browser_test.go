package web

import (
	"bytes"
	"context"
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
	server := httptest.NewServer(e.handler)
	defer server.Close()
	payload, err := json.Marshal(map[string]string{
		"url": server.URL, "session": cookie.Value, "sub": "/sub/" + link.Token,
		"reader": reader.Value,
		"user":   uid, "device": did, "plan": pid, "iface": iid,
	})
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

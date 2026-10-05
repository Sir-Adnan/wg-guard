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

	"github.com/Sir-Adnan/wg-guard/internal/backup"
)

func TestBrowserPresentation(t *testing.T) {
	node := os.Getenv("WG_TEST_BROWSER_NODE")
	if node == "" {
		t.Skip("opt-in browser fixture")
	}
	e := newEnv(t)
	_, _, _, cookie := e.seedUserWithDevice()
	server := httptest.NewServer(e.handler)
	defer server.Close()
	payload, _ := json.Marshal(map[string]string{"url": server.URL, "session": cookie.Value, "csrf": deriveCSRF(cookie.Value)})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join("..", "..", "scripts", "test-web-presentation.cjs"))
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("presentation browser: %v", err)
	}
}

// Reproduce the drawer/fieldset, mixed-direction table and wide-workspace
// regressions with disposable application data, not static HTML lookalikes.
func TestBrowserGuidanceLayout(t *testing.T) {
	node := os.Getenv("WG_TEST_BROWSER_NODE")
	if node == "" {
		t.Skip("opt-in browser fixture")
	}
	e := newEnv(t)
	if preset := os.Getenv("WG_TEST_VISUAL_PRESET"); preset != "" {
		if _, err := e.db.Exec(`UPDATE appearance_defaults SET preset_id = ? WHERE id = 1`, preset); err != nil {
			t.Fatal("visual preset fixture failed")
		}
	}
	uid, _, csrf, cookie := e.seedUserWithDevice()
	wireUpdateQueue(t, e)
	seedBrowserTelemetry(t, e, uid, true)
	if rec := e.post("/templates", url.Values{"name": {"Monthly / ماهانه"}, "duration_days": {"30"}, "traffic_limit_gb": {"50"}, "device_limit": {"2"}}, cookie, csrf); rec.Code != http.StatusSeeOther {
		t.Fatal("template fixture failed")
	}
	var pid string
	if err := e.db.QueryRow(`SELECT id FROM templates LIMIT 1`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.Backup.Create(context.Background(), backup.CreateOpts{}); err != nil {
		t.Fatal(err)
	}
	var iid string
	if err := e.db.QueryRow(`SELECT id FROM tunnel_interfaces LIMIT 1`).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(e.handler)
	defer server.Close()
	payload, _ := json.Marshal(map[string]string{"url": server.URL, "session": cookie.Value, "csrf": deriveCSRF(cookie.Value), "iface": iid, "template": pid})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join("..", "..", "scripts", "test-guidance-layout.cjs"))
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("guidance layout browser: %v", err)
	}
}

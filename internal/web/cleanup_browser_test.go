package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

func TestBrowserCleanup(t *testing.T) {
	node := os.Getenv("WG_TEST_BROWSER_NODE")
	if node == "" {
		t.Skip("set WG_TEST_BROWSER_NODE to run browser workflows")
	}
	e := newEnv(t)
	e.seedOwner()
	ctx := context.Background()
	i, err := e.ifaces.Create(ctx, iface.CreateInput{Name: "awg0", Pools: []string{"10.8.0.0/22", "10.8.4.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 24; n++ {
		u, err := e.srv.Users.Create(ctx, user.Input{Username: fmt.Sprintf("expired-%02d", n)})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = e.db.Exec(`UPDATE users SET status='expired',expires_at='2026-01-02T00:00:00Z' WHERE id=?`, u.ID)
	}
	cookie := e.login("owner")
	server := httptest.NewServer(e.handler)
	defer server.Close()
	payload, _ := json.Marshal(map[string]string{"url": server.URL, "session": cookie.Value, "csrf": deriveCSRF(cookie.Value), "interface": i.ID})
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join("..", "..", "scripts", "test-web-cleanup.cjs"))
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("cleanup browser: %v", err)
	}
}

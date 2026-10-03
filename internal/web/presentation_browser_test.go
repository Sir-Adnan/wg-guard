package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
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

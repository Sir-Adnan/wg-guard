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

	"github.com/Sir-Adnan/wg-guard/internal/backup"
)

func TestBrowserBackupWorkbench(t *testing.T) {
	node := os.Getenv("WG_TEST_BROWSER_NODE")
	if node == "" {
		t.Skip("set WG_TEST_BROWSER_NODE and WG_TEST_PLAYWRIGHT for local checks")
	}
	e := newEnv(t)
	_, _, _, cookie := e.seedUserWithDevice()
	var a *backup.Result
	for range 28 {
		var err error
		a, err = e.srv.Backup.Create(context.Background(), backup.CreateOpts{Retention: 40})
		if err != nil {
			t.Fatal(err)
		}
	}
	p, _, err := e.srv.Backup.StageReview(context.Background(), a.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(e.handler)
	defer server.Close()
	input, err := json.Marshal(map[string]string{"url": server.URL, "session": cookie.Value, "preview": p.PreviewID(), "archive": a.Name})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join("..", "..", "scripts", "test-backup-workbench.cjs"))
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("backup workbench browser: %v\n%s", err, output)
	}
	t.Log(string(output))
}

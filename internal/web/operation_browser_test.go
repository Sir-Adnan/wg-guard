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
	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

func TestBrowserOperationalJourney(t *testing.T) {
	node := os.Getenv("WG_TEST_BROWSER_NODE")
	if node == "" {
		t.Skip("set WG_TEST_BROWSER_NODE and WG_TEST_PLAYWRIGHT for local checks")
	}
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	wireDomainQueue(t, e)
	q := wireUpdateQueue(t, e)
	_, err := q.EnqueueAs(context.Background(), updatequeue.Input{Operation: updatequeue.OperationInspect}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	a, err := e.srv.Backup.Create(context.Background(), backup.CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := e.srv.Backup.Stage(context.Background(), a.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.Backup.Approve(p.PreviewID()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(e.handler)
	defer server.Close()
	input, err := json.Marshal(map[string]string{"url": server.URL, "session": cookie.Value})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join("..", "..", "scripts", "test-operational-journey.cjs"))
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("operational browser: %v\n%s", err, output)
	}
	t.Log(string(output))
}

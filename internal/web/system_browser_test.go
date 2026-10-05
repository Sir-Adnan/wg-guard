package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/nodestatus"
)

type systemFixtureKey struct{}

func TestBrowserSystemHealth(t *testing.T) {
	node := os.Getenv("WG_TEST_BROWSER_NODE")
	if node == "" {
		t.Skip("opt-in browser fixture")
	}
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	e.srv.NodeStatus = func(ctx context.Context, now time.Time) nodestatus.Snapshot {
		d := (nodestatus.Source{}).Read(ctx, now)
		state, _ := ctx.Value(systemFixtureKey{}).(string)
		if state == "unavailable" {
			return d
		}
		completed := now.Add(-time.Second)
		d.Readiness = nodestatus.Ready
		d.Runtime = nodestatus.Runtime{State: nodestatus.Applied, RequestedSequence: 1, AppliedSequence: 1, LastResult: nodestatus.Applied, LastCompletedAt: &completed}
		d.Accounting.State = nodestatus.Current
		d.Accounting.ObservedAt = &completed
		d.Telemetry.State = nodestatus.State("healthy")
		d.Telemetry.ObservedAt = &completed
		d.Telemetry.CadenceSeconds = 10
		if state == "pending" || state == "running" {
			d.Readiness = nodestatus.NotReady
			d.Runtime.State = nodestatus.Pending
			d.Runtime.RequestedSequence = 2
			d.Runtime.LastResult = nodestatus.Failed
			d.Runtime.InFlight = state == "running"
			d.Accounting.State = nodestatus.Failed
			d.Telemetry.State = nodestatus.State("degraded")
			d.Telemetry.Issues = []string{"accounting_recent_error"}
		}
		return d
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.handler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), systemFixtureKey{}, r.URL.Query().Get("_qa_state"))))
	}))
	defer server.Close()
	input, _ := json.Marshal(map[string]string{"url": server.URL, "session": cookie.Value})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join("..", "..", "scripts", "test-system-health.cjs"))
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("system browser: %v\n%s", err, out)
	}
	t.Log(string(out))
}

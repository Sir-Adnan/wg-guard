package serve

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/nodestatus"
)

func TestOperationalStatusSharesReadinessAndRuntimeWithoutMutation(t *testing.T) {
	n := startNode(t, testConfig(t, "127.0.0.1:0"))
	before := n.apiServer.NodeStatus(context.Background(), time.Now())
	if before.Readiness != nodestatus.Ready || before.Runtime.State != nodestatus.Unobserved {
		t.Fatal("fresh fake node claimed a completed runtime pass or lacked readiness")
	}
	for range 3 {
		_ = n.webServer.NodeStatus(context.Background(), time.Now())
	}
	after := n.apiServer.NodeStatus(context.Background(), time.Now())
	if after.Runtime.RequestedSequence != before.Runtime.RequestedSequence {
		t.Fatal("reading status ran reconciliation")
	}
	if _, err := n.reconciler.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := n.webServer.NodeStatus(context.Background(), time.Now())
	if status.Runtime.State != nodestatus.Applied || status.Runtime.LastCompletedAt == nil {
		t.Fatal("shared runtime result is not visible to the panel")
	}
	n.networkReady.Store(false)
	if s := n.apiServer.NodeStatus(context.Background(), time.Now()); s.Readiness != nodestatus.NotReady {
		t.Fatal("pending network claimed ready")
	}
	rec := httptest.NewRecorder()
	n.metrics.Readyz(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != 503 {
		t.Fatal("operational status diverged from readiness probe")
	}
}

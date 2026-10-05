package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/nodestatus"
)

func TestSystemPageRendersUnknownAndPendingWithActionableEvidence(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	for _, lang := range []string{"en", "fa"} {
		body := e.get("/system?lang="+lang, cookie).Body.String()
		if !strings.Contains(body, `data-system-state="runtime"`) || strings.Contains(body, "system.state.") || !strings.Contains(body, "sudo wg-guard doctor") {
			t.Fatal("unavailable observation page has missing or untranslated evidence")
		}
	}
	e.srv.NodeStatus = func(ctx context.Context, now time.Time) nodestatus.Snapshot {
		s := (nodestatus.Source{}).Read(ctx, now)
		s.Readiness = nodestatus.NotReady
		s.Runtime.State = nodestatus.Pending
		s.Runtime.InFlight = true
		s.Runtime.LastResult = nodestatus.Failed
		return s
	}
	body := e.get("/system?lang=en", cookie).Body.String()
	for _, want := range []string{"Pending application", "Running", "Failed", "Creating the same account again"} {
		if !strings.Contains(body, want) {
			t.Fatal("pending state or recovery guidance missing")
		}
	}
	main := body[strings.Index(body, "<main"):strings.Index(body, "</main>")]
	if strings.Contains(main, `method="post"`) {
		t.Fatal("read-only diagnostics gained a mutation form")
	}
}

func TestSystemScopeChecksPrecedeStatusRead(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	if _, err := e.admins.Create(context.Background(), "stats-only", testPassword, auth.RoleAdmin, []string{auth.ScopeStatsRead}); err != nil {
		t.Fatal(err)
	}
	cookie := e.loginEN("stats-only")
	calls := 0
	e.srv.NodeStatus = func(ctx context.Context, now time.Time) nodestatus.Snapshot {
		calls++
		return (nodestatus.Source{}).Read(ctx, now)
	}
	if rec := e.get("/system", cookie); rec.Code != http.StatusSeeOther {
		t.Fatal("node.read permission bypass")
	}
	if calls != 0 {
		t.Fatal("denied request read operational data")
	}
}

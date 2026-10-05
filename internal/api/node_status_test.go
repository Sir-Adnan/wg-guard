package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/nodestatus"
)

func TestNodeStatusRequiresScopeAndKeepsPublicLivenessSeparate(t *testing.T) {
	e := newEnv(t)
	calls := 0
	e.srv.NodeStatus = func(ctx context.Context, now time.Time) nodestatus.Snapshot {
		calls++
		s := (nodestatus.Source{}).Read(ctx, now)
		s.Readiness = nodestatus.NotReady
		s.Runtime.State = nodestatus.Pending
		s.Runtime.RequestedSequence = 2
		s.Runtime.AppliedSequence = 1
		return s
	}
	if rec := e.doAnonymous("GET", "/api/v1/node/status"); rec.Code != http.StatusUnauthorized {
		t.Fatal("anonymous operational evidence exposed")
	}
	_, limited, err := e.tokens.Create(context.Background(), "no-node-scope", []string{auth.ScopeStatsRead}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	saved := e.plainTok
	e.plainTok = limited
	if rec := e.do("GET", "/api/v1/node/status", ""); rec.Code != http.StatusForbidden {
		t.Fatal("missing scope accepted")
	}
	e.plainTok = saved
	if calls != 0 {
		t.Fatal("denied requests queried status")
	}
	if _, err := e.db.Exec(`INSERT INTO resellers (id,slug,permissions,created_at,updated_at) VALUES ('status-reseller','status-reseller','["node.read"]','test','test')`); err != nil {
		t.Fatal(err)
	}
	tenant, tenantToken, err := e.tokens.Create(context.Background(), "status-tenant", []string{auth.ScopeNodeRead}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Even a legacy token carrying a global scope must be classified/denied by
	// the route policy when it has a reseller identity.
	if _, err := e.db.Exec(`UPDATE api_tokens SET reseller_id='status-reseller' WHERE id=?`, tenant.ID); err != nil {
		t.Fatal(err)
	}
	e.plainTok = tenantToken
	if rec := e.do("GET", "/api/v1/node/status", ""); rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
		t.Fatalf("reseller gained node-wide status: %d", rec.Code)
	}
	if calls != 0 {
		t.Fatal("invalid tenant queried operational evidence")
	}
	e.plainTok = saved
	rec := e.do("GET", "/api/v1/node/status", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("snapshot response/cache contract")
	}
	var snapshot nodestatus.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Readiness != nodestatus.NotReady || snapshot.Runtime.RequestedSequence != 2 || snapshot.Runtime.State != nodestatus.Pending {
		t.Fatal("status confused responding with ready/applied")
	}
	public := decodeBody(t, e.doAnonymous("GET", "/api/v1/node/health"))
	if len(public) != 2 || public["status"] != "ok" {
		t.Fatal("public health gained private operational data")
	}
}

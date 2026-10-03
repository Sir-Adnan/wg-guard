package web

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

func TestMaintenanceReaderCannotRunOrCancel(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	q := wireUpdateQueue(t, e)
	if _, err := e.admins.Create(context.Background(), "maintenance-reader", testPassword, auth.RoleAdmin, []string{auth.ScopeUpdateRead}); err != nil {
		t.Fatal(err)
	}
	cookie := e.loginEN("maintenance-reader")
	for _, path := range []string{"/updates", "/updates?tab=versions", "/updates?tab=recovery", "/updates/status"} {
		if rec := e.get(path, cookie); rec.Code != 200 {
			t.Fatalf("reader cannot inspect %s", path)
		}
	}
	for _, path := range []string{"/updates/request", "/updates/cancel"} {
		if rec := e.post(path, url.Values{"operation": {"inspect"}}, cookie, deriveCSRF(cookie.Value)); rec.Code != 303 {
			t.Fatal("reader reached mutation")
		}
	}
	if status, _ := q.Status(); status.ID != "" {
		t.Fatal("reader created a host request")
	}
}

func TestMaintenanceSelectionPreservesDestinationAndProgressRevision(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	q := wireUpdateQueue(t, e)
	cookie := e.loginEN("owner")
	i := updatequeue.Inventory{Schema: 1, ObservedAt: time.Now().UTC(), Mode: "docker", Userspace: "unknown", Bridge: "active", ModuleIdentity: "unknown", Preflight: &updatequeue.Preflight{Input: updatequeue.Input{Operation: updatequeue.OperationPanel, Channel: "release", Ref: "v1.2.3"}, CheckedAt: time.Now().UTC(), Commit: strings.Repeat("a", 40), Ready: true}}
	if err := q.PublishInventory(i); err != nil {
		t.Fatal(err)
	}
	page := e.get("/updates?tab=versions&version=v1.2.3", cookie)
	if !strings.Contains(page.Body.String(), `name="expected_commit"`) {
		t.Fatal("reviewed source identity not attached")
	}
	if rec := e.post("/updates/request", url.Values{"operation": {"preflight"}, "target": {"panel"}, "channel": {"release"}, "ref": {"v1.2.3"}}, cookie, deriveCSRF(cookie.Value)); rec.Code != 303 {
		t.Fatal("preview not accepted")
	}
	req, err := q.Claim(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Record(req, "host", "preflight", updatequeue.StateRunning); err != nil {
		t.Fatal(err)
	}
	rec := e.get("/updates/status?id="+req.ID+"&state=running&revision=2", cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Check readiness") {
		t.Fatal("progress suppressed while state remained running")
	}
	status, _ := q.Status()
	if status.ActorID == "" {
		t.Fatal("requesting account identity not retained")
	}
}

type countingMaintenanceCatalog struct {
	calls int
	err   error
}

func (c *countingMaintenanceCatalog) Releases(context.Context) ([]distribution.Release, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return []distribution.Release{{Tag: "v1.2.3", PublishedAt: "2026-10-01T00:00:00Z"}}, nil
}

func TestMaintenanceCatalogCacheAndFailureBackoff(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	wireUpdateQueue(t, e)
	catalog := &countingMaintenanceCatalog{}
	e.srv.UpdateCatalog = catalog
	cookie := e.loginEN("owner")
	e.get("/updates?tab=versions", cookie)
	e.get("/updates?tab=versions&refresh=1", cookie)
	if catalog.calls != 1 {
		t.Fatal("refresh bypassed minimum catalog interval")
	}
	catalog.err = &distribution.HTTPStatusError{Code: 429, RetryAfter: time.Minute}
	e.srv.updateCache.mu.Lock()
	entry := e.srv.updateCache.entries["1:10"]
	entry.at = time.Now().Add(-11 * time.Minute)
	e.srv.updateCache.entries["1:10"] = entry
	e.srv.updateCache.mu.Unlock()
	page := e.get("/updates?tab=versions", cookie)
	e.get("/updates?tab=versions&refresh=1", cookie)
	if catalog.calls != 2 || !strings.Contains(page.Body.String(), "last successful result") {
		t.Fatal("stale catalog or backoff lost")
	}
}

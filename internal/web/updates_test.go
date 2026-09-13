package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

type fixedReleaseCatalog struct {
	releases []distribution.Release
	err      error
}

func (c fixedReleaseCatalog) Releases(context.Context) ([]distribution.Release, error) {
	return append([]distribution.Release(nil), c.releases...), c.err
}

func wireUpdateQueue(t *testing.T, e *env) *updatequeue.Queue {
	t.Helper()
	q := updatequeue.New(t.TempDir())
	if err := os.WriteFile(q.Paths().Marker, updatequeue.ReadyMarker(), 0o600); err != nil {
		t.Fatal(err)
	}
	e.srv.UpdateQueue = q
	e.srv.UpdateCatalog = fixedReleaseCatalog{releases: []distribution.Release{{Tag: "v1.2.3", PublishedAt: "2026-09-13T00:00:00Z"}}}
	return q
}

func TestUpdateCenterShowsVersionsAndQueuesCataloguedRelease(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	queue := wireUpdateQueue(t, e)
	cookie := e.loginEN("owner")

	dashboard := e.get("/dashboard", cookie).Body.String()
	for _, want := range []string{`href="/updates"`, `test`, `fake`} {
		if !strings.Contains(dashboard, want) {
			t.Errorf("dashboard software summary missing %q", want)
		}
	}
	page := e.get("/updates", cookie)
	if page.Code != http.StatusOK {
		t.Fatalf("updates page = %d", page.Code)
	}
	for _, want := range []string{"v1.2.3", "awg-2026-09", "awg-2026-08"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("updates page missing %q", want)
		}
	}
	rec := e.post("/updates/request", url.Values{
		"operation": {"panel"}, "channel": {"release"}, "ref": {"v1.2.3"},
	}, cookie, deriveCSRF(cookie.Value))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("queue release = %d %s", rec.Code, rec.Body.String())
	}
	status, err := queue.Status()
	if err != nil || status.State != updatequeue.StateQueued || status.Ref != "v1.2.3" {
		t.Fatalf("queued status = %#v, %v", status, err)
	}
	queuedPage := e.get("/updates", cookie)
	if !strings.Contains(queuedPage.Body.String(), `hx-get="/updates/status?`) {
		t.Fatal("queued update status does not refresh")
	}
	statusFragment := e.get("/updates/status", cookie)
	if statusFragment.Code != http.StatusOK || !strings.Contains(statusFragment.Body.String(), `id="update-status-region"`) || strings.Contains(statusFragment.Body.String(), `<html`) {
		t.Fatalf("update status fragment = %d %s", statusFragment.Code, statusFragment.Body.String())
	}
	unchanged := e.get("/updates/status?id="+url.QueryEscape(status.ID)+"&state=queued", cookie)
	if unchanged.Code != http.StatusNoContent || unchanged.Header().Get("HX-Refresh") != "" {
		t.Fatalf("unchanged update status = %d, refresh %q", unchanged.Code, unchanged.Header().Get("HX-Refresh"))
	}
	request, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Finish(context.Background(), request, nil); err != nil {
		t.Fatal(err)
	}
	completed := e.get("/updates/status?id="+url.QueryEscape(status.ID)+"&state=queued", cookie)
	if completed.Code != http.StatusNoContent || completed.Header().Get("HX-Refresh") != "true" {
		t.Fatalf("completed update status = %d, refresh %q", completed.Code, completed.Header().Get("HX-Refresh"))
	}
	auditPage := e.get("/audit", cookie).Body.String()
	if !strings.Contains(auditPage, "Software version change requested") {
		t.Fatal("update request lacks a human audit label")
	}
}

func TestUpdateCenterQueuesReviewedCoreAndRejectsConcurrentWork(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	queue := wireUpdateQueue(t, e)
	cookie := e.loginEN("owner")
	rec := e.post("/updates/request", url.Values{
		"operation": {"core"}, "core": {"awg-2026-08"},
	}, cookie, deriveCSRF(cookie.Value))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("queue core = %d %s", rec.Code, rec.Body.String())
	}
	status, err := queue.Status()
	if err != nil || status.State != updatequeue.StateQueued || status.Core != "awg-2026-08" {
		t.Fatalf("queued core status = %#v, %v", status, err)
	}
	rec = e.post("/updates/request", url.Values{
		"operation": {"panel"}, "channel": {"release"}, "ref": {"v1.2.3"},
	}, cookie, deriveCSRF(cookie.Value))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "already queued") {
		t.Fatalf("concurrent update = %d %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateCenterUnavailableAndCatalogFailureRemainUseful(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	e.srv.UpdateQueue = updatequeue.New(t.TempDir())
	e.srv.UpdateCatalog = fixedReleaseCatalog{err: errors.New("offline")}
	cookie := e.loginEN("owner")
	page := e.get("/updates", cookie)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "host update bridge is unavailable") || !strings.Contains(page.Body.String(), "Version catalog unavailable") {
		t.Fatalf("unavailable page = %d %s", page.Code, page.Body.String())
	}
	rec := e.post("/updates/request", url.Values{
		"operation": {"core"}, "core": {"awg-2026-09"},
	}, cookie, deriveCSRF(cookie.Value))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable broker accepted = %d", rec.Code)
	}
}

func TestUpdateCenterRejectsUncataloguedAndUnauthorizedRequests(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	queue := wireUpdateQueue(t, e)
	owner := e.loginEN("owner")
	rec := e.post("/updates/request", url.Values{
		"operation": {"panel"}, "channel": {"release"}, "ref": {"v9.9.9"},
	}, owner, deriveCSRF(owner.Value))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("uncatalogued release = %d", rec.Code)
	}
	if status, err := queue.Status(); err != nil || status.ID != "" {
		t.Fatalf("uncatalogued release reached queue: %#v, %v", status, err)
	}

	if _, err := e.admins.Create(context.Background(), "viewer", testPassword, auth.RoleAdmin, []string{auth.ScopeStatsRead}); err != nil {
		t.Fatal(err)
	}
	viewer := e.loginEN("viewer")
	if rec := e.get("/updates", viewer); rec.Code != http.StatusSeeOther {
		t.Fatalf("viewer reached update center: %d", rec.Code)
	}
	if rec := e.get("/updates/status", viewer); rec.Code != http.StatusSeeOther {
		t.Fatalf("viewer reached update status: %d", rec.Code)
	}
	rec = e.post("/updates/request", url.Values{
		"operation": {"core"}, "core": {"awg-2026-09"},
	}, viewer, deriveCSRF(viewer.Value))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("viewer queued core update: %d", rec.Code)
	}
	if status, err := queue.Status(); err != nil || status.ID != "" {
		t.Fatalf("viewer reached queue: %#v, %v", status, err)
	}
}

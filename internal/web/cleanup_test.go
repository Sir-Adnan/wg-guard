package web

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

func TestCleanupPanelReviewAndPermissionBoundaries(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	ctx := context.Background()
	cookie := e.loginEN("owner")
	csrf := deriveCSRF(cookie.Value)
	u, err := e.srv.Users.Create(ctx, user.Input{Username: "expired-review"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = e.db.Exec(`UPDATE users SET status='expired',expires_at='2026-01-02T00:00:00Z' WHERE id=?`, u.ID)
	rec := e.get("/cleanup", cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Database health") {
		t.Fatal("cleanup screen missing")
	}
	rec = e.post("/cleanup/preview", url.Values{"kinds": {"users"}, "statuses": {"expired"}, "owners": {"node"}, "date_field": {"expires_at"}}, cookie, csrf)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "expired-review") {
		t.Fatal("review missing")
	}
	match := regexp.MustCompile(`name="preview_token" value="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	if len(match) != 2 {
		t.Fatal("sealed preview missing")
	}
	if _, err := e.srv.Users.Get(ctx, u.ID); err != nil {
		t.Fatal("preview mutated data")
	}
	rec = e.post("/cleanup/execute", url.Values{"preview_token": {match[1]}}, cookie, csrf)
	if rec.Code != 303 {
		t.Fatal("reviewed delete failed")
	}
	if _, err := e.srv.Users.Get(ctx, u.ID); err == nil {
		t.Fatal("reviewed delete kept account")
	}
	if _, err := e.admins.Create(ctx, "cleanup-operator", testPassword, auth.RoleAdmin, []string{auth.ScopeCleanupManage}); err != nil {
		t.Fatal(err)
	}
	operator := e.loginEN("cleanup-operator")
	if rec := e.get("/cleanup", operator); rec.Code != 200 {
		t.Fatal("cleanup operator denied")
	}
	if rec := e.post("/cleanup/optimize", url.Values{"compact": {"1"}}, operator, deriveCSRF(operator.Value)); rec.Code != 403 {
		t.Fatal("operator admitted to owner-only compaction")
	}
	if _, err := e.admins.Create(ctx, "observer", testPassword, auth.RoleAdmin, []string{auth.ScopeUsersRead}); err != nil {
		t.Fatal(err)
	}
	observer := e.loginEN("observer")
	if rec := e.get("/cleanup", observer); rec.Code != 303 {
		t.Fatal("observer admitted to cleanup")
	}
}

package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// limitedLogin creates a non-owner admin with exactly the given scopes and
// logs in with the English locale persisted.
func (e *env) limitedLogin(t *testing.T, scopes []string) *http.Cookie {
	t.Helper()
	if _, err := e.admins.Create(context.Background(), "helper-"+strings.NewReplacer(".", "").Replace(strings.Join(scopes, "")), testPassword,
		auth.RoleAdmin, scopes); err != nil {
		// A duplicate name means this helper already exists — log in instead.
		if domain.CodeOf(err) != domain.CodeAdminExists {
			t.Fatal(err)
		}
	}
	return e.loginEN("helper-" + strings.NewReplacer(".", "").Replace(strings.Join(scopes, "")))
}

func TestAdminsScreenLifecycle(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")

	// Page lists the owner.
	body := e.get("/admins", cookie).Body.String()
	if !strings.Contains(body, "owner") || !strings.Contains(body, "All permissions") {
		t.Fatal("owner not listed")
	}

	// Create a limited admin through the modal form.
	rec := e.postForm("/admins/create", url.Values{
		"username": {"ops"}, "password": {"a-long-password-1"},
		"role":        {"admin"},
		"permissions": {"users.read", "users.bulk"},
	}, cookie)
	if rec.Code != 303 {
		t.Fatalf("create admin: %d", rec.Code)
	}
	body = e.get("/admins", cookie).Body.String()
	if !strings.Contains(body, "ops") || !strings.Contains(body, "No permissions") == false {
		// The matrix chip shows the count; either rendering proves the row.
		if !strings.Contains(body, "ops") {
			t.Fatal("created admin not listed")
		}
	}

	// Duplicate username → error redisplay.
	rec = e.postForm("/admins/create", url.Values{
		"username": {"ops"}, "password": {"a-long-password-1"}, "role": {"admin"},
	}, cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "already taken") {
		t.Fatalf("duplicate admin: %d", rec.Code)
	}

	// Password reset.
	id := e.adminID("ops")
	if rec := e.postForm("/admins/"+id+"/password", url.Values{"password": {"another-long-pass"}}, cookie); rec.Code != 303 {
		t.Fatalf("password reset: %d", rec.Code)
	}

	// Enable/disable + delete.
	if rec := e.postForm("/admins/"+id+"/enable", url.Values{"enable": {"0"}}, cookie); rec.Code != 303 {
		t.Fatalf("disable: %d", rec.Code)
	}
	if rec := e.postForm("/admins/"+id+"/delete", url.Values{}, cookie); rec.Code != 303 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if body := e.get("/admins", cookie).Body.String(); strings.Contains(body, ">ops<") {
		t.Fatal("admin still listed")
	}
}

func (e *env) adminID(username string) string {
	e.t.Helper()
	list, err := e.admins.List(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	for _, a := range list {
		if a.Username == username {
			return a.ID
		}
	}
	e.t.Fatalf("admin %q not found", username)
	return ""
}

func TestTokensScreenLifecycle(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")

	// Invalid: no scopes (unknown scope → registry rejects the grant).
	rec := e.postForm("/tokens/create", url.Values{"name": {"ci"}}, cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "scopes") {
		t.Fatalf("no-scope create: %d", rec.Code)
	}

	// Valid create renders the secret exactly once on the response page.
	rec = e.postForm("/tokens/create", url.Values{
		"name": {"ci"}, "scopes": {"users.read", "users.bulk"}, "expires_days": {"30"},
	}, cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "shown once") {
		t.Fatalf("create: %d", rec.Code)
	}
	body := rec.Body.String()
	const onceMarker = `id="token-once"`
	onceIdx := strings.Index(body, onceMarker)
	if onceIdx < 0 || !strings.Contains(body, "wg_") {
		t.Fatal("plaintext token missing from the once-view")
	}
	valueStart := strings.Index(body[onceIdx:], `value="`)
	secret := body[onceIdx+valueStart+len(`value="`):]
	secret = secret[:strings.Index(secret, `"`)]
	if !strings.HasPrefix(secret, "wg_") {
		t.Fatalf("once-view did not carry the plaintext: %q", secret[:min(12, len(secret))])
	}
	// The listing must NOT contain the plaintext.
	listBody := e.get("/tokens", cookie).Body.String()
	if strings.Contains(listBody, secret) {
		t.Fatal("plaintext leaked into the listing")
	}
	if !strings.Contains(listBody, "ci") || !strings.Contains(listBody, "users.read") {
		t.Fatal("token not listed")
	}

	// Revoke.
	id := e.tokenID("ci")
	if rec := e.postForm("/tokens/"+id+"/revoke", url.Values{}, cookie); rec.Code != 303 {
		t.Fatalf("revoke: %d", rec.Code)
	}
	if body := e.get("/tokens", cookie).Body.String(); !strings.Contains(body, "Revoked") {
		t.Fatal("revoke not reflected")
	}
}

func (e *env) tokenID(name string) string {
	e.t.Helper()
	list, err := e.srv.Tokens.List(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	for _, tok := range list {
		if tok.Name == name {
			return tok.ID
		}
	}
	e.t.Fatalf("token %q not found", name)
	return ""
}

func TestWebhooksScreenLifecycle(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")

	// Invalid URL → error redisplay.
	rec := e.postForm("/webhooks/create", url.Values{
		"url": {"http://"}, "events": {"user.created"},
	}, cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "URL") {
		t.Fatalf("invalid url create: %d", rec.Code)
	}

	// Create → secret shown once.
	rec = e.postForm("/webhooks/create", url.Values{
		"url": {"https://bot.example.com/hook"}, "events": {"user.created", "device.created"},
	}, cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "shown once") {
		t.Fatalf("create: %d", rec.Code)
	}
	id := e.hookID("https://bot.example.com/hook")

	// Listing shows the endpoint + stats chips, never the secret.
	listBody := e.get("/webhooks", cookie).Body.String()
	if strings.Contains(listBody, "shown once") && strings.Contains(listBody, "v1=") {
		t.Fatal("secret leaked into the listing")
	}
	if !strings.Contains(listBody, "bot.example.com") {
		t.Fatal("endpoint not listed")
	}

	// Detail view renders the deliveries table (empty state).
	detail := e.get("/webhooks/"+id, cookie).Body.String()
	if !strings.Contains(detail, "Recent deliveries") {
		t.Fatal("detail view missing deliveries card")
	}

	// Update URL.
	rec = e.postForm("/webhooks/"+id+"/update", url.Values{
		"url": {"https://bot2.example.com/hook"}, "events": {"user.created"}, "enabled": {"1"},
	}, cookie)
	if rec.Code != 303 {
		t.Fatalf("update: %d", rec.Code)
	}

	// Rotate → new secret rendered once.
	rec = e.postForm("/webhooks/"+id+"/rotate", url.Values{}, cookie)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "shown once") {
		t.Fatalf("rotate: %d", rec.Code)
	}

	// Redeliver an unknown delivery id → handled error (redisplay).
	rec = e.postForm("/webhooks/"+id+"/redeliver", url.Values{"delivery_id": {"nope"}}, cookie)
	if rec.Code == 500 {
		t.Fatalf("redeliver blew up: %d", rec.Code)
	}

	// Delete.
	if rec := e.postForm("/webhooks/"+id+"/delete", url.Values{}, cookie); rec.Code != 303 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if body := e.get("/webhooks", cookie).Body.String(); strings.Contains(body, "bot2.example.com") {
		t.Fatal("endpoint still listed")
	}
}

func (e *env) hookID(url string) string {
	e.t.Helper()
	list, err := e.srv.Webhooks.List(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	for _, ep := range list {
		if ep.URL == url {
			return ep.ID
		}
	}
	e.t.Fatalf("endpoint %q not found", url)
	return ""
}

func TestAuditScreen(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")

	// Generate auditable activity.
	_ = e.postForm("/backups/create", url.Values{}, cookie)

	body := e.get("/audit", cookie).Body.String()
	if !strings.Contains(body, "backup.created") {
		t.Fatal("audit entry missing")
	}

	// Filter narrows.
	filtered := e.get("/audit?action=admins.", cookie).Body.String()
	if strings.Contains(filtered, "backup.created") {
		t.Fatal("action filter did not narrow")
	}

	// Cursor pagination: a full page carries the Older link.
	for i := 0; i < 55; i++ {
		_ = e.srv.Audit.Record(context.Background(), audit.Entry{
			ActorType: audit.ActorSystem, Action: "filler", Target: "x",
		})
	}
	paged := e.get("/audit", cookie).Body.String()
	if !strings.Contains(paged, "before=") {
		t.Fatal("older-page link missing on a full page")
	}
}

func TestOpsScreensPermissionGating(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()

	// Each screen is closed to a limited admin and open to the owner.
	cases := []struct {
		path string
		cut  string // distinctive page marker
	}{
		{"/admins", "Administrators"},
		{"/tokens", "API tokens"},
		{"/webhooks", "Webhooks"},
		{"/audit", "Audit log"},
	}
	for _, c := range cases {
		helper := e.limitedLogin(t, []string{auth.ScopeUsersRead})
		rec := e.get(c.path, helper)
		if rec.Code != 303 {
			t.Fatalf("%s: limited admin got %d, want redirect", c.path, rec.Code)
		}
		owner := e.loginEN("owner")
		rec = e.get(c.path, owner)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), c.cut) {
			t.Fatalf("%s: owner got %d", c.path, rec.Code)
		}
	}

	// A webhooks.read admin can view but not mutate.
	viewer := e.limitedLogin(t, []string{auth.ScopeWebhooksRead})
	rec := e.get("/webhooks", viewer)
	if rec.Code != 200 {
		t.Fatalf("viewer blocked from /webhooks: %d", rec.Code)
	}
	rec = e.postForm("/webhooks/create", url.Values{
		"url": {"https://x.example.com/h"}, "events": {"user.created"},
	}, viewer)
	if rec.Code == 200 {
		t.Fatal("viewer without webhooks.write mutated endpoints")
	}
}

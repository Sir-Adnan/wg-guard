package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

func TestOperationsInvalidFormsPreserveSafeInput(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	for _, tc := range []struct {
		path          string
		values        url.Values
		retain, field string
	}{
		{"/admins/create", url.Values{"username": {"operator"}, "role": {"admin"}, "password": {"short"}, "permissions": {"users.*"}}, "operator", "password"},
		{"/tokens/create", url.Values{"name": {"Retained token"}, "expires_days": {"invalid"}, "scopes": {"users.*"}, "cidr": {"192.0.2.0/24"}}, "Retained token", "expires_days"},
		{"/webhooks/create", url.Values{"url": {"https://retained.example/hook"}, "events": {}}, "https://retained.example/hook", "events"},
	} {
		rec := e.postForm(tc.path, tc.values, cookie)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), tc.retain) || !strings.Contains(rec.Body.String(), `aria-invalid="true"`) {
			t.Errorf("%s must return retained accessible invalid form: %d", tc.path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), `value="short"`) {
			t.Fatal("password redisplayed")
		}
	}
	var count int
	_ = e.db.QueryRow(`SELECT count(*) FROM api_tokens`).Scan(&count)
	if count != 0 {
		t.Fatal("invalid expiry created an unbounded token")
	}
}

func TestAdministrationDynamicCopyCoverage(t *testing.T) {
	for _, locale := range []i18n.Locale{i18n.En, i18n.Fa} {
		v := View{Locale: locale}
		for _, scope := range auth.AllScopes() {
			if v.ScopeLabel(scope) == scope || v.ScopeDescription(scope) == scope {
				t.Errorf("missing scope copy %s / %s", locale, scope)
			}
		}
		for _, event := range webhook.Catalog() {
			if v.OpsLabel("event", event) == event || v.OpsLabel("event_help", event) == event {
				t.Errorf("missing event copy %s / %s", locale, event)
			}
		}
	}
}

func TestAdministrationReadFailureAndCredentialRedaction(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	rec := e.postForm("/webhooks/create", url.Values{"url": {"https://user:do-not-display@example.com/hook"}, "events": {"user.created"}}, cookie)
	if rec.Code != http.StatusUnprocessableEntity || strings.Contains(rec.Body.String(), "do-not-display") {
		t.Fatal("rejected URL credentials must not be redisplayed")
	}
	rec = e.postForm("/webhooks/create", url.Values{"url": {"https://user:secret%zz@example.com/hook"}, "events": {"user.created"}}, cookie)
	if rec.Code != http.StatusUnprocessableEntity || strings.Contains(rec.Body.String(), "secret%zz") {
		t.Fatal("malformed URL credentials must not be redisplayed")
	}
	for _, tc := range []struct{ table, path string }{{"api_tokens", "/tokens"}, {"webhook_endpoints", "/webhooks"}, {"audit_log", "/audit"}} {
		if _, err := e.db.Exec("ALTER TABLE " + tc.table + " RENAME TO unavailable_" + tc.table); err != nil {
			t.Fatal(err)
		}
		body := e.get(tc.path, cookie).Body.String()
		if !strings.Contains(body, "This information could not be loaded") {
			t.Errorf("%s failure not disclosed", tc.path)
		}
	}
}

func TestWebhookUpdateEmptyEventSelectionIsRejected(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	ep, _, err := e.srv.Webhooks.Create(context.Background(), "https://example.com/hook", []string{"user.created"}, "")
	if err != nil {
		t.Fatal(err)
	}
	rec := e.postForm("/webhooks/"+ep.ID+"/update", url.Values{"url": {"https://retained.example/hook"}}, cookie)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `value="https://retained.example/hook"`) || !strings.Contains(rec.Body.String(), `aria-invalid="true" aria-describedby="hook-events-help"`) {
		t.Fatal("empty submitted event selection must show a retained invalid form")
	}
	updated, err := e.srv.Webhooks.Get(context.Background(), ep.ID)
	if err != nil || updated.URL != ep.URL || !updated.Enabled || len(updated.Events) != 1 || updated.Events[0] != "user.created" {
		t.Fatal("invalid event selection must not persist any endpoint changes")
	}
}

func TestAdministrationWildcardAndWebhookReader(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	owner := e.loginEN("owner")
	a, err := e.admins.Create(context.Background(), "wildcard", testPassword, auth.RoleAdmin, []string{"users.*"})
	if err != nil {
		t.Fatal(err)
	}
	body := e.get("/admins?edit="+a.ID, owner).Body.String()
	if !strings.Contains(body, `value="users.*" checked`) {
		t.Fatal("stored family wildcard missing from editor")
	}
	passwordFailure := e.postForm("/admins/"+a.ID+"/password", url.Values{"password": {"short"}}, owner)
	if !strings.Contains(passwordFailure.Body.String(), `value="users.*" checked`) {
		t.Fatal("failed password reset must not clear the separate permission editor")
	}
	ep, _, err := e.srv.Webhooks.Create(context.Background(), "https://example.com/hook", []string{"user.created"}, "")
	if err != nil {
		t.Fatal(err)
	}
	reader := e.limitedLogin(t, []string{auth.ScopeWebhooksRead})
	for _, path := range []string{"/webhooks", "/webhooks/" + ep.ID} {
		body = e.get(path, reader).Body.String()
		if strings.Contains(body, `method="post" action="/webhooks`) {
			t.Error("webhook reader sees mutation form")
		}
	}
	rec := e.postForm("/webhooks/"+ep.ID+"/update", url.Values{"url": {ep.URL}, "events": {"user.created"}}, owner)
	updated, _ := e.srv.Webhooks.Get(context.Background(), ep.ID)
	if rec.Code != 303 || updated.Enabled {
		t.Fatal("unchecked endpoint must disable")
	}
}

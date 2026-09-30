package web

import (
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
)

func TestPermissionShortcutsClassifyEveryCurrentScope(t *testing.T) {
	for _, scope := range auth.AllScopes() {
		switch scopePresetTiers[scope] {
		case "observer", "operator", "full":
		default:
			t.Errorf("scope %s needs an explicit shortcut classification", scope)
		}
	}
	for scope := range scopePresetTiers {
		if err := auth.ValidateScopes([]string{scope}); err != nil {
			t.Errorf("obsolete shortcut scope %s: %v", scope, err)
		}
	}
	for _, secret := range []string{auth.ScopeConfigsRead, auth.ScopeSubscriptionsRead} {
		if scopePresetTiers[secret] == "observer" {
			t.Errorf("read-only shortcut must not expose %s", secret)
		}
	}
	if scopePresetTiers[auth.ScopeUsersBulk] != "full" {
		t.Fatal("bulk action includes deletion and must not be a routine shortcut")
	}
}

func TestTokenAndResellerShortcutsRenderCurrentScopes(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	owner := e.loginEN("owner")
	tokenPage := e.get("/tokens", owner).Body.String()
	for _, want := range []string{
		`value="purchases.create" data-scope-tier="operator"`,
		`value="operations.read" data-scope-tier="observer"`,
		`value="subscriptions.read" data-scope-tier="operator"`,
		`value="subscriptions.rotate" data-scope-tier="full"`,
		`value="next_plans.write" data-scope-tier="operator"`,
	} {
		if !strings.Contains(tokenPage, want) {
			t.Errorf("token shortcut missing %s", want)
		}
	}
	if !strings.Contains(tokenPage, `value="users.*" data-scope-tier=""`) {
		t.Fatal("explicit legacy family wildcard must remain editable but outside shortcuts")
	}
	for _, unwanted := range []string{`value="audit.view"`, `value="api_tokens.manage"`} {
		if strings.Contains(tokenPage, unwanted) {
			t.Errorf("REST token editor offered irrelevant or future-widening %s", unwanted)
		}
	}
	resellerPage := e.get("/resellers", owner).Body.String()
	if !strings.Contains(resellerPage, `data-scope-preset="operator"`) ||
		!strings.Contains(resellerPage, `value="purchases.create" data-scope-tier="operator"`) ||
		strings.Contains(resellerPage, `value="node.settings"`) {
		t.Fatal("reseller create form lacks safe current-scope shortcuts")
	}
}

func TestResetUsageIsVisibleForAuthorizedUser(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	owner := e.loginEN("owner")
	id := createUserViaForm(t, e, owner, "reset-visible")
	detail := e.get("/users/"+id, owner).Body.String()
	_, actions, ok := strings.Cut(detail, `class="user-action-bar"`)
	if !ok {
		t.Fatal("user detail action bar missing")
	}
	if !strings.Contains(actions, `/users/`+id+`/traffic/reset`) ||
		!strings.Contains(actions, `data-confirm-message=`) {
		t.Fatal("Reset Usage must be a visible confirmed action in user detail")
	}
	list := e.get("/users", owner).Body.String()
	if !strings.Contains(list, `/users/`+id+`/traffic/reset`) {
		t.Fatal("Reset Usage must be available in the user list actions")
	}
	for _, action := range []string{`data-user-quick-action="renew"`, `data-user-quick-action="traffic"`,
		`id="users-quick-renew"`, `id="users-quick-traffic"`} {
		if !strings.Contains(list, action) {
			t.Errorf("user list is missing %s", action)
		}
	}
	reader := e.limitedLogin(t, []string{auth.ScopeUsersRead})
	if body := e.get("/users/"+id, reader).Body.String(); strings.Contains(body, `/users/`+id+`/traffic/reset`) {
		t.Fatal("read-only account was offered Reset Usage")
	}
}

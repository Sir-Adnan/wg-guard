package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

func TestOwnerManagesResellerBoundary(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	owner := e.loginEN("owner")
	staff, err := e.admins.Create(context.Background(), "staff", testPassword, auth.RoleAdmin,
		[]string{auth.ScopeAdminsManage})
	if err != nil {
		t.Fatal(err)
	}
	_ = staff
	if rec := e.get("/resellers", e.loginEN("staff")); rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner reseller page: %d", rec.Code)
	}
	if rec := e.get("/resellers", owner); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Add reseller") {
		t.Fatalf("owner reseller page: %d", rec.Code)
	}
	rec := e.postForm("/resellers", url.Values{
		"slug": {"north"}, "display_name": {"North"},
		"permissions": {"users.read", "api_tokens.manage"},
	}, owner)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create reseller: %d %s", rec.Code, rec.Body.String())
	}
	accounts, err := e.srv.Resellers.List(context.Background())
	if err != nil || len(accounts) != 1 || accounts[0].Slug != "north" {
		t.Fatalf("reseller list: %+v %v", accounts, err)
	}
	id := accounts[0].ID
	if rec := e.postForm("/resellers", url.Values{"slug": {"north"}}, owner); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate reseller should stay in form: %d", rec.Code)
	}
	if rec := e.postForm("/resellers", url.Values{"slug": {"south"}, "permissions": {"node.settings"}}, owner); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("node-level reseller grant should be rejected: %d", rec.Code)
	}
	rec = e.postForm("/resellers/"+id+"/admins", url.Values{
		"username": {"northadmin"}, "password": {testPassword}, "permissions": {"users.read"},
	}, owner)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create reseller account: %d", rec.Code)
	}
	admins, err := e.admins.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bound := false
	for _, a := range admins {
		if a.Username == "northadmin" && a.ResellerID != nil && *a.ResellerID == id {
			bound = true
		}
	}
	if !bound {
		t.Fatal("panel account was not bound to reseller")
	}
	if rec := e.postForm("/resellers/"+id+"/enable", url.Values{"enable": {"0"}}, owner); rec.Code != http.StatusSeeOther {
		t.Fatalf("disable reseller: %d", rec.Code)
	}
	if _, err := e.admins.Authenticate(context.Background(), "northadmin", testPassword); domain.CodeOf(err) != domain.CodeForbidden {
		t.Fatalf("disabled reseller account can authenticate: %v", err)
	}
}

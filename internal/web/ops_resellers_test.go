package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
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
	product, err := e.srv.Plans.Create(context.Background(), plan.Input{Name: "Basic"})
	if err != nil {
		t.Fatal(err)
	}
	if rec := e.postForm("/resellers/"+id+"/templates", url.Values{"templates": {product.ID}}, owner); rec.Code != http.StatusSeeOther {
		t.Fatalf("assign reseller plan: %d", rec.Code)
	}
	assigned, err := e.srv.Resellers.Templates(context.Background(), id)
	if err != nil || len(assigned) != 1 || assigned[0] != product.ID {
		t.Fatalf("reseller plan access: %v, %v", assigned, err)
	}
	if rec := e.postForm("/resellers/"+id+"/permissions", url.Values{"permissions": {"users.read"}}, owner); rec.Code != http.StatusSeeOther {
		t.Fatalf("update reseller grants: %d", rec.Code)
	}
	updated, err := e.srv.Resellers.Get(context.Background(), id)
	if err != nil || len(updated.Permissions) != 1 || updated.Permissions[0] != "users.read" {
		t.Fatalf("reseller permissions did not update: %+v %v", updated, err)
	}
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
	staffCookie := e.loginEN("staff")
	if rec := e.postForm("/resellers/"+id+"/templates", url.Values{}, staffCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("node operator changed reseller products: %d", rec.Code)
	}
	if body := e.get("/admins", staffCookie).Body.String(); strings.Contains(body, "northadmin") {
		t.Fatal("node operator saw reseller-bound account in admin list")
	}
	boundID := e.adminID("northadmin")
	if rec := e.postForm("/admins/"+boundID+"/password", url.Values{"password": {"another-long-pass"}}, staffCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("node operator changed reseller-bound account: %d", rec.Code)
	}
	if rec := e.postForm("/resellers/"+id+"/enable", url.Values{"enable": {"0"}}, owner); rec.Code != http.StatusSeeOther {
		t.Fatalf("disable reseller: %d", rec.Code)
	}
	if _, err := e.admins.Authenticate(context.Background(), "northadmin", testPassword); domain.CodeOf(err) != domain.CodeForbidden {
		t.Fatalf("disabled reseller account can authenticate: %v", err)
	}
}

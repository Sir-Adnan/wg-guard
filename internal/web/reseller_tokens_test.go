package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestResellerTokenPanelStaysInTenant(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	ctx := context.Background()
	north, err := e.srv.Resellers.Create(ctx, "north", "North", []string{"users.read", "api_tokens.manage"})
	if err != nil {
		t.Fatal(err)
	}
	south, err := e.srv.Resellers.Create(ctx, "south", "South", []string{"users.read"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.admins.CreateForReseller(ctx, north.ID, "northadmin", testPassword,
		[]string{"users.read", "api_tokens.manage"}); err != nil {
		t.Fatal(err)
	}
	global, globalSecret, err := e.srv.Tokens.Create(ctx, "owner bot", []string{"users.read"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	owner := e.loginEN("owner")
	reseller := e.loginEN("northadmin")
	if rec := e.get("/reseller/tokens", reseller); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "owner bot") {
		t.Fatalf("reseller token list: %d", rec.Code)
	}
	if rec := e.postForm("/reseller/tokens", url.Values{"name": {"no scopes"}}, reseller); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty token grants: %d", rec.Code)
	}
	if rec := e.postForm("/reseller/tokens", url.Values{"name": {"north bot"}, "scopes": {"users.read"}}, reseller); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "shown once") {
		t.Fatalf("create own token: %d", rec.Code)
	}
	northTokens, err := e.srv.Tokens.ListForReseller(ctx, north.ID)
	if err != nil || len(northTokens) != 1 || northTokens[0].IssuedByAdminID == nil {
		t.Fatalf("bound token list: %+v %v", northTokens, err)
	}
	if rec := e.postForm("/reseller/tokens/"+global.ID+"/revoke", url.Values{}, reseller); rec.Code != http.StatusSeeOther {
		t.Fatalf("foreign revoke response: %d", rec.Code)
	}
	if _, err := e.srv.Tokens.Verify(ctx, globalSecret, ""); err != nil {
		t.Fatalf("global token affected by reseller: %v", err)
	}
	if _, err := e.admins.Create(ctx, "operator", testPassword, "admin", []string{"api_tokens.manage"}); err != nil {
		t.Fatal(err)
	}
	operator := e.loginEN("operator")
	if body := e.get("/tokens", operator).Body.String(); !strings.Contains(body, "owner bot") || strings.Contains(body, "north bot") {
		t.Fatal("node operator token list included a reseller token")
	}
	if rec := e.postForm("/tokens/"+northTokens[0].ID+"/revoke", url.Values{}, operator); rec.Code != http.StatusSeeOther {
		t.Fatalf("node operator foreign revoke response: %d", rec.Code)
	}
	northTokens, err = e.srv.Tokens.ListForReseller(ctx, north.ID)
	if err != nil || !northTokens[0].Enabled {
		t.Fatalf("node operator revoked reseller token: %+v %v", northTokens, err)
	}
	if rec := e.postForm("/reseller/tokens/"+northTokens[0].ID+"/revoke", url.Values{}, reseller); rec.Code != http.StatusSeeOther {
		t.Fatalf("own revoke response: %d", rec.Code)
	}
	northTokens, err = e.srv.Tokens.ListForReseller(ctx, north.ID)
	if err != nil || northTokens[0].Enabled {
		t.Fatalf("owned token remained enabled: %+v %v", northTokens, err)
	}
	if rec := e.postForm("/resellers/"+south.ID+"/tokens", url.Values{"name": {"south bot"}, "scopes": {"users.read"}}, owner); rec.Code != http.StatusOK {
		t.Fatalf("owner-issued reseller token: %d", rec.Code)
	}
	southTokens, err := e.srv.Tokens.ListForReseller(ctx, south.ID)
	if err != nil || len(southTokens) != 1 || southTokens[0].ResellerID == nil || *southTokens[0].ResellerID != south.ID {
		t.Fatalf("owner token target: %+v %v", southTokens, err)
	}
}

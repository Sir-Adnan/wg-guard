package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestResellerWebhookPanelUsesOwnedRoutes(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	ownerCookie := e.loginEN("owner")
	ctx := context.Background()
	north, err := e.srv.Resellers.Create(ctx, "north-hooks", "North", []string{"webhooks.read", "webhooks.write"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.admins.CreateForReseller(ctx, north.ID, "northhooks", testPassword,
		[]string{"webhooks.read", "webhooks.write"}); err != nil {
		t.Fatal(err)
	}
	global, _, err := e.srv.Webhooks.Create(ctx, "https://global.example/hook", []string{"user.created"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.admins.Create(ctx, "hook-operator", testPassword, "admin",
		[]string{"webhooks.read", "webhooks.write"}); err != nil {
		t.Fatal(err)
	}
	operator := e.loginEN("hook-operator")
	if rec := e.postForm("/webhooks/create", url.Values{
		"url": {"https://owner-wide.example/hook"}, "events": {"user.created"},
		"include_reseller_events": {"1"},
	}, ownerCookie); rec.Code != http.StatusOK {
		t.Fatalf("owner fanout create: %d", rec.Code)
	}
	if operatorPage := e.get("/webhooks", operator); strings.Contains(operatorPage.Body.String(), "owner-wide.example") {
		t.Fatal("operator saw owner-wide receiver")
	}
	reseller := e.loginEN("northhooks")
	page := e.get("/reseller/webhooks", reseller)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `/reseller/webhooks/create`) ||
		strings.Contains(page.Body.String(), global.URL) || strings.Contains(page.Body.String(), `value="node.started"`) {
		t.Fatalf("reseller webhook page: %d", page.Code)
	}
	if rec := e.postForm("/reseller/webhooks/create", url.Values{
		"url": {"https://127.0.0.1/unsafe"}, "events": {"user.created"},
	}, reseller); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("private receiver accepted: %d", rec.Code)
	}
	rec := e.postForm("/reseller/webhooks/create", url.Values{
		"url": {"https://north.example/hook"}, "events": {"user.created"},
	}, reseller)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "https://north.example/hook") {
		t.Fatalf("create receiver: %d", rec.Code)
	}
	items, err := e.srv.Webhooks.ListFor(ctx, north.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("owned endpoint: %v %v", items, err)
	}
	id := items[0].ID
	if operatorPage := e.get("/webhooks", operator); operatorPage.Code != http.StatusOK ||
		strings.Contains(operatorPage.Body.String(), "https://north.example/hook") {
		t.Fatalf("node operator saw reseller receiver: %d", operatorPage.Code)
	}
	if rec := e.postForm("/webhooks/"+id+"/delete", url.Values{}, operator); rec.Code != http.StatusSeeOther {
		t.Fatalf("operator foreign delete response: %d", rec.Code)
	}
	if _, err := e.srv.Webhooks.GetFor(ctx, north.ID, id); err != nil {
		t.Fatalf("operator deleted tenant endpoint: %v", err)
	}
	if rec := e.get("/reseller/webhooks/"+id, reseller); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), "/reseller/webhooks/"+id+"/update") {
		t.Fatalf("owned detail: %d", rec.Code)
	}
	if rec := e.get("/reseller/webhooks/"+global.ID, reseller); rec.Code != http.StatusSeeOther ||
		strings.Contains(rec.Body.String(), global.URL) {
		t.Fatalf("global detail leaked: %d", rec.Code)
	}
	if rec := e.postForm("/reseller/webhooks/"+id+"/update", url.Values{
		"url": {"https://north.example/updated"}, "events": {"user.updated"}, "enabled": {"1"},
	}, reseller); rec.Code != http.StatusSeeOther {
		t.Fatalf("own update: %d", rec.Code)
	}
	if rec := e.postForm("/reseller/webhooks/"+global.ID+"/delete", url.Values{}, reseller); rec.Code != http.StatusSeeOther {
		t.Fatalf("foreign delete response: %d", rec.Code)
	}
	if _, err := e.srv.Webhooks.Get(ctx, global.ID); err != nil {
		t.Fatalf("global endpoint modified: %v", err)
	}
	if rec := e.postForm("/reseller/webhooks/"+id+"/delete", url.Values{}, reseller); rec.Code != http.StatusSeeOther {
		t.Fatalf("own delete: %d", rec.Code)
	}
	items, err = e.srv.Webhooks.ListFor(ctx, north.ID)
	if err != nil || len(items) != 0 {
		t.Fatalf("delete did not clear tenant: %v %v", items, err)
	}
}

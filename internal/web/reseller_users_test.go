package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/user"
)

func TestResellerPortalReadBoundary(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	e.seedIface()
	ctx := context.Background()
	if err := e.reg.Set(ctx, "node.endpoint", "vpn.example.com"); err != nil {
		t.Fatal(err)
	}
	north, err := e.srv.Resellers.Create(ctx, "north", "North", []string{"users.read", "devices.read", "configs.read"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.admins.CreateForReseller(ctx, north.ID, "northadmin", testPassword,
		[]string{"users.read", "devices.read", "configs.read"}); err != nil {
		t.Fatal(err)
	}
	owned, err := e.srv.Users.Create(ctx, user.Input{Username: "north-customer", ResellerID: &north.ID})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := e.srv.Users.Create(ctx, user.Input{Username: "owner-customer"})
	if err != nil {
		t.Fatal(err)
	}
	makeDevice := func(userID string) string {
		t.Helper()
		keys, err := e.srv.generateKeys(httptest.NewRequest(http.MethodGet, "/", nil), false)
		if err != nil {
			t.Fatal(err)
		}
		d, err := e.srv.Devices.Create(ctx, userID, "phone", *keys, "")
		if err != nil {
			t.Fatal(err)
		}
		return d.ID
	}
	ownedDevice := makeDevice(owned.ID)
	foreignDevice := makeDevice(foreign.ID)
	cookie := e.loginEN("northadmin")
	if rec := e.get("/login", cookie); rec.Header().Get("Location") != "/reseller/users" {
		t.Fatalf("signed-in reseller login redirect: %s", rec.Header().Get("Location"))
	}
	list := e.get("/reseller/users", cookie)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "north-customer") || strings.Contains(list.Body.String(), "owner-customer") {
		t.Fatalf("reseller list leaked or omitted users: %d", list.Code)
	}
	if rec := e.get("/reseller/users/"+owned.ID, cookie); rec.Code != http.StatusOK {
		t.Fatalf("owned detail: %d", rec.Code)
	}
	if rec := e.get("/reseller/users/"+foreign.ID, cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign detail: %d", rec.Code)
	}
	for _, suffix := range []string{"/config", "/qr"} {
		if rec := e.get("/reseller/devices/"+ownedDevice+suffix, cookie); rec.Code != http.StatusOK {
			t.Fatalf("owned device %s: %d", suffix, rec.Code)
		}
		if rec := e.get("/reseller/devices/"+foreignDevice+suffix, cookie); rec.Code != http.StatusNotFound {
			t.Fatalf("foreign device %s: %d", suffix, rec.Code)
		}
	}
	for _, path := range []string{"/", "/dashboard", "/users", "/tokens", "/resellers"} {
		if rec := e.get(path, cookie); rec.Code != http.StatusForbidden {
			t.Fatalf("operator route %s: %d", path, rec.Code)
		}
	}
	if rec := e.get("/appearance", cookie); rec.Code != http.StatusOK {
		t.Fatalf("personal appearance: %d", rec.Code)
	}
	if err := e.srv.Resellers.SetEnabled(ctx, north.ID, false); err != nil {
		t.Fatal(err)
	}
	if rec := e.get("/reseller/users", cookie); rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Fatalf("disabled reseller retained session: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

package web

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
)

func TestDigitPreferencesRespectPersonalPanelAndTechnicalBoundaries(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)
	page := e.get("/appearance", cookie)
	if !strings.Contains(page.Body.String(), `data-digits="latin"`) {
		t.Fatal("default is not Latin")
	}
	if rec := e.post("/appearance/digits/me", url.Values{"digits": {"persian"}}, cookie, csrf); rec.Code != 303 {
		t.Fatal("personal preference rejected")
	}
	if page := e.get("/appearance", cookie); !strings.Contains(page.Body.String(), `data-digits="persian"`) {
		t.Fatal("personal preference not persisted")
	}
	var mode, locale, digits string
	_ = e.db.QueryRow(`SELECT mode,digits FROM appearance_defaults`).Scan(&mode, &digits)
	_ = e.db.QueryRow(`SELECT locale FROM admins WHERE username='owner'`).Scan(&locale)
	if mode != "light" || digits != "latin" || locale != "fa" {
		t.Fatal("personal digits changed unrelated defaults")
	}
	if _, err := e.admins.Create(context.Background(), "digits-operator", testPassword, auth.RoleAdmin, nil); err != nil {
		t.Fatal(err)
	}
	operator := e.login("digits-operator")
	if rec := e.post("/appearance/digits/default", url.Values{"digits": {"persian"}}, operator, deriveCSRF(operator.Value)); rec.Code != 403 {
		t.Fatal("operator changed global digits")
	}
	if rec := e.post("/appearance/digits/default", url.Values{"digits": {"persian"}}, cookie, csrf); rec.Code != 303 {
		t.Fatal("owner default rejected")
	}
	if rec := e.post("/appearance/digits/me", url.Values{"digits": {""}}, cookie, csrf); rec.Code != 303 {
		t.Fatal("inherit choice rejected")
	}
	if page := e.get("/appearance", cookie); !strings.Contains(page.Body.String(), `data-digits="persian"`) {
		t.Fatal("panel inheritance failed")
	}
	if rec := e.post("/appearance/digits/me", url.Values{"digits": {"arabic"}}, cookie, csrf); rec.Code != 303 {
		t.Fatal("unexpected invalid-value response")
	}
	var stored string
	_ = e.db.QueryRow(`SELECT appearance_digits FROM admins WHERE username='owner'`).Scan(&stored)
	if stored != "" {
		t.Fatal("unregistered numeral style stored")
	}
}

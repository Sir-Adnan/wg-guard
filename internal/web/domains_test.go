package web

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/domainqueue"
	"github.com/Sir-Adnan/wg-guard/internal/domaintls"
	"github.com/Sir-Adnan/wg-guard/internal/install"
)

func wireDomainQueue(t *testing.T, e *env) *domainqueue.Queue {
	t.Helper()
	q := domainqueue.New(t.TempDir())
	if err := os.MkdirAll(q.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(q.File("broker.json"), []byte("{\"schema\":1}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	i := install.DomainInventory{Schema: 1, Revision: "legacy", Available: true, PanelOrigin: "https://panel.example.test", SubscriptionOrigin: "https://panel.example.test", Exposure: install.ExposureDirect}
	if err := q.WriteInventory(i); err != nil {
		t.Fatal(err)
	}
	e.srv.DomainQueue = q
	return q
}

func TestDomainsPageOwnerReaderAndCSRFBoundaries(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	owner := e.login("owner")
	q := wireDomainQueue(t, e)
	for _, lang := range []string{"en", "fa"} {
		r := e.get("/settings/domains?lang="+lang, owner)
		if r.Code != 200 || !strings.Contains(r.Body.String(), "id=\"domain-origin\"") {
			t.Fatal("domain form unavailable", lang, r.Code)
		}
	}
	reader := e.limitedLogin(t, []string{auth.ScopeServerView})
	r := e.get("/settings/domains", reader)
	if r.Code != 200 || strings.Contains(r.Body.String(), "class=\"form-stack domain-configure\"") {
		t.Fatal("reader gained owner controls")
	}
	form := url.Values{"operation": {"configure"}, "role": {"subscription"}, "origin": {"https://sub.example.test"}, "method": {"automatic"}, "expected_revision": {"legacy"}, "challenge": {"http"}}
	if e.post("/settings/domains", form, reader, deriveCSRF(reader.Value)).Code != http.StatusForbidden {
		t.Fatal("non-owner gained root domain authority")
	}
	if e.post("/settings/domains", form, owner, "").Code != http.StatusForbidden {
		t.Fatal("CSRF request admitted")
	}
	if q.HasRequest() {
		t.Fatal("rejected request entered host mailbox")
	}
	if e.post("/settings/domains", form, owner, deriveCSRF(owner.Value)).Code != http.StatusSeeOther {
		t.Fatal("approved owner intent not queued")
	}
	if s, err := q.Status(); err != nil || s.State != "queued" {
		t.Fatal("queue status", err)
	}
	r = e.get("/settings/domains", owner)
	if !strings.Contains(r.Body.String(), "hx-trigger=\"every 3s\"") || !strings.Contains(r.Body.String(), "<fieldset disabled class=\"form-stack domain-fieldset\"") {
		t.Fatal("active operation did not gate controls/polling")
	}
}

func TestDomainUploadIsPrivateBoundedAndAbsentFromAudit(t *testing.T) {
	for _, kind := range []string{"valid", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			e.seedOwner()
			cookie := e.login("owner")
			q := wireDomainQueue(t, e)
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			for key, value := range map[string]string{"_csrf": deriveCSRF(cookie.Value), "operation": "configure", "role": "subscription", "origin": "https://sub.example.test", "method": "manual", "expected_revision": "legacy"} {
				if err := writer.WriteField(key, value); err != nil {
					t.Fatal(err)
				}
			}
			cert, _ := writer.CreateFormFile("certificate", "cert.pem")
			_, _ = cert.Write([]byte("synthetic public certificate"))
			key, _ := writer.CreateFormFile("private_key", "key.pem")
			private := []byte("synthetic private upload fixture")
			if kind == "oversized" {
				private = bytes.Repeat([]byte("x"), domaintls.MaxMaterialBytes+1)
			}
			_, _ = key.Write(private)
			writer.Close()
			r := httptest.NewRequest(http.MethodPost, "/settings/domains", &body)
			r.Header.Set("Content-Type", writer.FormDataContentType())
			r.AddCookie(cookie)
			rec := httptest.NewRecorder()
			e.handler.ServeHTTP(rec, r)
			if kind == "oversized" {
				if rec.Code != 400 || q.HasRequest() {
					t.Fatal("oversized material admitted")
				}
				return
			}
			if rec.Code != http.StatusSeeOther {
				t.Fatal("bounded import failed", rec.Code)
			}
			request, err := q.Claim()
			if err != nil || request.StageID == "" {
				t.Fatal("private staging missing", err)
			}
			c, k, err := q.ImportFiles(request.StageID)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range []string{c, k} {
				info, err := os.Stat(file)
				if err != nil || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
					t.Fatal("staged file was not private")
				}
			}
			for _, file := range []string{q.File("running.json"), q.File("status.json")} {
				raw, err := os.ReadFile(file)
				if err != nil || bytes.Contains(raw, private) {
					t.Fatal("private upload entered receipt")
				}
			}
			var raw string
			if err := e.db.QueryRow(`SELECT coalesce(group_concat(metadata),'') FROM audit_log`).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(raw, string(private)) {
				t.Fatal("private upload entered audit")
			}
		})
	}
}

func TestManagedDomainLinksOverrideURLSettingAndRetainToken(t *testing.T) {
	e := newEnv(t)
	uid, _, _, cookie := e.seedUserWithDevice()
	link, err := e.srv.Links.ForUser(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	p := domaintls.Policy{Schema: domaintls.Schema, Revision: strings.Repeat("a", 32), Sites: []domaintls.Site{{Role: domaintls.Panel, Origin: "https://panel.example.test", Method: domaintls.External}, {Role: domaintls.Subscription, Origin: "https://sub.example.test", Method: domaintls.External}}}
	e.srv.DomainPolicy = func() (domaintls.Policy, error) { return p, nil }
	if err := e.reg.Set(context.Background(), "subscription.base_url", "https://unapproved.example.test"); err != nil {
		t.Fatal(err)
	}
	r := e.get("/users/"+uid, cookie)
	if !strings.Contains(r.Body.String(), "https://sub.example.test/sub/"+link.Token) || strings.Contains(r.Body.String(), "https://unapproved.example.test/sub/") {
		t.Fatal("setting bypassed active domain policy")
	}
	r = e.get("/settings", cookie)
	if strings.Contains(r.Body.String(), "name=\"sub_base_url\"") || !strings.Contains(r.Body.String(), "href=\"/settings/domains\"") {
		t.Fatal("managed URL still exposed conflicting setting")
	}
}

func TestDomainStatusCompletionRefreshesRevisionAndShowsNewPanelAddress(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	q := wireDomainQueue(t, e)
	status, err := q.Enqueue(domainqueue.Input{Operation: "configure", Role: domaintls.Panel, Origin: "https://new-panel.example.test", Method: domaintls.External, ExpectedRevision: "legacy"}, "owner-id")
	if err != nil {
		t.Fatal(err)
	}
	r := e.get("/settings/domains", cookie)
	if !strings.Contains(r.Body.String(), "https://new-panel.example.test/settings/domains") {
		t.Fatal("owner was not given the new origin before losing old private routes")
	}
	request, err := q.Claim()
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Finish(request, nil); err != nil {
		t.Fatal(err)
	}
	r = e.get("/settings/domains/status?id="+status.ID+"&state=running&stage=activating", cookie)
	if r.Code != http.StatusNoContent || r.Header().Get("HX-Refresh") != "true" || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("completed operation left stale controls/revision")
	}
}

func TestBrowserDomains(t *testing.T) {
	node := os.Getenv("WG_TEST_BROWSER_NODE")
	if node == "" {
		t.Skip("set WG_TEST_BROWSER_NODE and WG_TEST_PLAYWRIGHT for browser checks")
	}
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	wireDomainQueue(t, e)
	server := httptest.NewServer(e.handler)
	defer server.Close()
	payload, err := json.Marshal(map[string]string{"url": server.URL, "session": cookie.Value})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join("..", "..", "scripts", "test-domains-ui.cjs"))
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("domain browser: %v\n%s", err, output)
	}
	t.Log(string(output))
}

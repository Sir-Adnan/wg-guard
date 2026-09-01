package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/accounting"
	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/metrics"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/token"
	"github.com/Sir-Adnan/wg-guard/internal/user"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

// env is the full-stack test environment: temp DB, all services, an HTTP
// server. The reconcile seam is nil (structural tests assert status changes,
// not backend application — covered in reconcile's own tests).
type env struct {
	t        *testing.T
	db       *database.DB
	ring     *secrets.KeyRing
	reg      *settings.Registry
	srv      *Server
	handler  http.Handler
	tokens   *token.Service
	users    *user.Service
	ifaces   *iface.Service
	devices  *device.Service
	plans    *plan.Service
	webhooks *webhook.Service
	acct     *accounting.Service
	rec      *webhook.Recorder
	plainTok string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "api.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	tokens := token.NewService(db)
	users := user.NewService(db)
	ifaces := iface.NewService(db, reg, ring)
	devices := device.NewService(db, ring)
	plans := plan.NewService(db)
	auditSvc := audit.NewService(db)
	rec := webhook.NewRecorder()
	users.Recorder = rec
	devices.Recorder = rec
	acct := accounting.NewService(db, nil, auditSvc, nil, reg)
	acct.Recorder = rec
	webhookSvc := webhook.NewService(db, ring)
	coll := metrics.New()

	tok, plaintext, err := tokens.Create(context.Background(), "test-token",
		auth.AllScopes(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = tok
	if err := reg.Set(context.Background(), "node.id", "test-node"); err != nil {
		t.Fatal(err)
	}

	srv := New(Deps{
		DB: db, Tokens: tokens, Users: users, Devices: devices, Plans: plans,
		Ifaces: ifaces, Settings: reg, Ring: ring, Audit: auditSvc,
		Accounting: acct, Webhooks: webhookSvc, Metrics: coll,
		NodeID: "test-node", ToolsVersion: "pinned-test",
	})
	return &env{
		t: t, db: db, ring: ring, reg: reg, srv: srv, handler: srv.Handler(),
		tokens: tokens, users: users, ifaces: ifaces, devices: devices,
		plans: plans, webhooks: webhookSvc, acct: acct, rec: rec, plainTok: plaintext,
	}
}

func (e *env) do(method, path, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+e.plainTok)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *env) doAnonymous(method, path string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, rec.Body.String())
	}
	return m
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	m := decodeBody(t, rec)
	inner, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope: %s", rec.Body.String())
	}
	code, _ := inner["code"].(string)
	if code == "" {
		t.Fatalf("error code missing: %s", rec.Body.String())
	}
	return code
}

func TestPublicEndpoints(t *testing.T) {
	e := newEnv(t)
	if rec := e.doAnonymous("GET", "/healthz"); rec.Code != 200 {
		t.Fatalf("healthz: %d", rec.Code)
	}
	if rec := e.doAnonymous("GET", "/readyz"); rec.Code != 200 {
		t.Fatalf("readyz: %d", rec.Code)
	}
	if rec := e.doAnonymous("GET", "/api/v1/node/health"); rec.Code != 200 {
		t.Fatalf("node/health: %d", rec.Code)
	}
	if rec := e.doAnonymous("GET", "/openapi.json"); rec.Code != 200 {
		t.Fatalf("openapi: %d", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(e.doAnonymous("GET", "/openapi.json").Body.Bytes(), &doc); err != nil {
		t.Fatalf("openapi is not valid JSON: %v", err)
	}
	if doc["openapi"] == nil || doc["paths"] == nil {
		t.Fatal("openapi document incomplete")
	}
	if rec := e.doAnonymous("GET", "/docs"); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), "WG-Guard API") {
		t.Fatalf("docs page: %d", rec.Code)
	}
	// Unmatched route → envelope, not a bare 404.
	rec := e.doAnonymous("GET", "/api/v1/definitely-not-here")
	if rec.Code != 404 || errCode(t, rec) != "NOT_FOUND" {
		t.Fatalf("unmatched route: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAuthEnforced(t *testing.T) {
	e := newEnv(t)
	// Missing token.
	rec := e.doAnonymous("GET", "/api/v1/users")
	if rec.Code != 401 || errCode(t, rec) != "UNAUTHORIZED" {
		t.Fatalf("missing token: %d %s", rec.Code, rec.Body.String())
	}
	// Malformed token.
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	req.Header.Set("Authorization", "Bearer nope")
	r1 := httptest.NewRecorder()
	e.handler.ServeHTTP(r1, req)
	if r1.Code != 401 {
		t.Fatalf("bad token: %d", r1.Code)
	}
	// Insufficient scope.
	limited, plaintext, err := e.tokens.Create(context.Background(), "limited",
		[]string{"plans.read"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = limited
	req = httptest.NewRequest("GET", "/api/v1/users", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	r2 := httptest.NewRecorder()
	e.handler.ServeHTTP(r2, req)
	if r2.Code != 403 || errCode(t, r2) != "FORBIDDEN" {
		t.Fatalf("scope enforcement: %d %s", r2.Code, r2.Body.String())
	}
}

func TestNodeEndpoint(t *testing.T) {
	e := newEnv(t)
	rec := e.do("GET", "/api/v1/node", "")
	m := decodeBody(t, rec)
	if m["node_id"] != "test-node" || m["version"] == nil || m["interfaces"] == nil {
		t.Fatalf("node: %v", m)
	}
	if rec := e.do("GET", "/api/v1/node/stats", ""); rec.Code != 200 {
		t.Fatalf("node stats: %d", rec.Code)
	}
}

// TestRouteCoverageAndMuxSync is the load-bearing documentation test: every
// registered route must appear in openapi.json with the correct scope, and
// the document must not describe routes that do not exist. A second pass
// proves every route actually answers on the mux (a request to each route
// never yields the NOT_FOUND route-miss envelope).
func TestRouteCoverageAndMuxSync(t *testing.T) {
	e := newEnv(t)
	var doc struct {
		Paths map[string]map[string]struct {
			Security []map[string][]string `json:"security"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(openapiJSON, &doc); err != nil {
		t.Fatalf("openapi.json invalid: %v", err)
	}

	for _, r := range e.srv.routes {
		ops, ok := doc.Paths[r.Path]
		if !ok {
			t.Errorf("route %s %s missing from openapi.json", r.Method, r.Path)
			continue
		}
		op, ok := ops[strings.ToLower(r.Method)]
		if !ok {
			t.Errorf("method %s %s missing from openapi.json", r.Method, r.Path)
			continue
		}
		var scopes []string
		for _, sec := range op.Security {
			for _, v := range sec {
				scopes = append(scopes, v...)
			}
		}
		if r.Scope == "" {
			if len(op.Security) != 0 {
				t.Errorf("%s %s must be public in openapi.json", r.Method, r.Path)
			}
		} else if len(scopes) != 1 || scopes[0] != r.Scope {
			t.Errorf("%s %s scope mismatch: doc=%v code=%q", r.Method, r.Path, scopes, r.Scope)
		}
		// Mux sync: the route must exist. Unauthenticated requests hit the
		// auth middleware (401 UNAUTHORIZED) — never the 404 route-miss.
		rec := e.doAnonymous(r.Method, strings.ReplaceAll(strings.ReplaceAll(r.Path, "{id}", "nope"), "/api/v1/users/{id}", "/api/v1/users/nope"))
		if rec.Code == 404 && errCode(t, rec) == "NOT_FOUND" {
			t.Errorf("route %s %s not registered on the mux", r.Method, r.Path)
		}
	}

	// The other direction: no stale paths in the document.
	registered := map[string]bool{}
	for _, r := range e.srv.routes {
		registered[r.Path] = true
	}
	registered["/docs"] = true
	for path := range doc.Paths {
		if !registered[path] {
			t.Errorf("openapi.json documents a nonexistent route: %s", path)
		}
	}
}

func TestOpenAPIObfuscationRangeContract(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(openapiJSON, &doc); err != nil {
		t.Fatal(err)
	}
	components := doc["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	obfuscation := schemas["Obfuscation"].(map[string]any)
	properties := obfuscation["properties"].(map[string]any)

	for _, key := range []string{
		"enabled", "jc", "jmin", "jmax", "s1", "s2", "s3", "s4",
		"h1", "h2", "h3", "h4", "i1", "i2", "i3", "i4", "i5",
		"header_protection_key", "header_protection_key_set", "content_padding_addition",
		"rekey_after_time", "rekey_timeout", "reject_after_time", "keepalive_timeout",
		"max_handshake_attempts", "random_trailers", "disable_cookies",
	} {
		if _, exists := properties[key]; !exists {
			t.Errorf("OpenAPI Obfuscation missing %q", key)
		}
	}
	if _, exists := properties["advanced_security"]; exists {
		t.Fatal("unsupported AdvancedSecurity must not be advertised")
	}
	assertRange := func(key string, max float64) {
		t.Helper()
		raw, exists := properties[key]
		if !exists {
			return
		}
		property, ok := raw.(map[string]any)
		if !ok {
			t.Errorf("%s property has unexpected shape: %T", key, raw)
			return
		}
		oneOf, ok := property["oneOf"].([]any)
		if !ok || len(oneOf) != 2 {
			t.Errorf("%s must be an integer-or-string union: %v", key, property)
			return
		}
		integer := oneOf[0].(map[string]any)
		text := oneOf[1].(map[string]any)
		if integer["type"] != "integer" || integer["minimum"] != float64(0) || integer["maximum"] != max {
			t.Errorf("%s integer bounds wrong: %v", key, integer)
		}
		if text["type"] != "string" || text["pattern"] == nil {
			t.Errorf("%s range string contract wrong: %v", key, text)
		}
		if property["description"] == nil || property["example"] == nil {
			t.Errorf("%s needs description and example: %v", key, property)
		}
	}
	for _, key := range []string{"h1", "h2", "h3", "h4"} {
		assertRange(key, float64(4294967295))
	}
	for _, key := range []string{"content_padding_addition", "rekey_after_time", "rekey_timeout", "reject_after_time", "keepalive_timeout", "max_handshake_attempts"} {
		assertRange(key, float64(65535))
	}
	hpk, hpkOK := properties["header_protection_key"].(map[string]any)
	hpkSet, hpkSetOK := properties["header_protection_key_set"].(map[string]any)
	if !hpkOK || !hpkSetOK || hpk["writeOnly"] != true || hpkSet["readOnly"] != true {
		t.Fatal("HPK must be write-only with a read-only presence indicator")
	}
}

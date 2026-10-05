package domaintls

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testPolicy() Policy {
	return Policy{Schema: Schema, Revision: strings.Repeat("a", 32), Sites: []Site{{Role: Panel, Origin: "https://panel.example.test", Method: Manual, CertificateID: strings.Repeat("b", 32)}, {Role: Subscription, Origin: "https://sub.example.test", Method: Manual, CertificateID: strings.Repeat("c", 32)}}}
}

func TestOriginNormalizationAndAdmission(t *testing.T) {
	for _, raw := range []string{"http://panel.example.test", "https://name:secret@panel.example.test", "https://panel.example.test/path", "https://panel.example.test?token=example", "https://panel.example.test#part", "https://panel.example.test:", "https://panel.example.test:65536", "https://panel..example.test", "https://panel.example.test%2fother"} {
		if _, err := ParseOrigin(raw); err == nil {
			t.Fatal("unsafe origin admitted")
		}
	}
	for raw, want := range map[string]string{" https://Panel.Example.Test:443/ ": "https://panel.example.test", "https://[::1]:8443": "https://[::1]:8443"} {
		got, err := ParseOrigin(raw)
		if err != nil || got.URL != want {
			t.Fatal("normalization", err, got)
		}
	}
}

func TestSubscriptionHostnameCannotReachPrivateRoutesOrTraversal(t *testing.T) {
	p := testPolicy()
	handler := Router(func() (Policy, error) { return p, nil }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, route := range []string{"/login", "/", "/users", "/api/v1/users", "/openapi.json", "/docs", "/metrics", "/healthz", "/readyz", "/assets/../login", "/assets/%2e%2e/login", "/sub/token/devices/id/../config", "/assets//x", "/sub/token/extra"} {
		r := httptest.NewRequest("GET", "https://sub.example.test"+route, nil)
		r.RemoteAddr = "192.0.2.1:9000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 404 || w.Header().Get("Location") != "" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("public role escaped", route, w.Code)
		}
	}
	for _, route := range []string{"/sub/capability", "/sub/capability/devices/device/config", "/sub/capability/devices/device/qr", "/assets/app.css"} {
		r := httptest.NewRequest("GET", "https://sub.example.test"+route, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 204 {
			t.Fatal("public route denied", route)
		}
		r = httptest.NewRequest("POST", "https://sub.example.test"+route, nil)
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatal("public write admitted")
		}
	}
}

func TestRouterRechecksRetirementAndRejectsSNIHostMismatch(t *testing.T) {
	p := testPolicy()
	handler := Router(func() (Policy, error) { return p, nil }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	r := httptest.NewRequest("GET", "https://sub.example.test/sub/capability", nil)
	r.TLS = &tls.ConnectionState{ServerName: "panel.example.test"}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("SNI/Host mismatch admitted")
	}
	r.TLS.ServerName = "sub.example.test"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("matching public host denied")
	}
	p.Sites = p.Sites[:1]
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("retired host retained on existing connection")
	}
	r = httptest.NewRequest("GET", "https://unknown.example.test/login", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("unknown host admitted")
	}
}

func TestReadPolicyHasStrictSchemaBoundsAndRoles(t *testing.T) {
	for _, raw := range []string{`{"schema":1,"revision":"` + strings.Repeat("a", 32) + `","sites":[],"command":"ignored"}`, strings.Repeat("x", MaxPolicyBytes+1), `{} {}`} {
		if _, err := ReadPolicy([]byte(raw)); err == nil {
			t.Fatal("invalid policy accepted")
		}
	}
	p := testPolicy()
	p.Sites[1].Role = Panel
	if p.Validate() == nil {
		t.Fatal("duplicate roles accepted")
	}
}

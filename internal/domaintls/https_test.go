package domaintls

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRealHTTPSSelectsSNIIsolatesRolesAndRetiresExistingConnection(t *testing.T) {
	p := testPolicy()
	policyFile := filepath.Join(t.TempDir(), "active.json")
	cert, key, roots := testMaterial(t, []string{"panel.example.test", "sub.example.test"})
	for _, site := range p.Sites {
		c, k, err := PairFiles(policyFile, site.CertificateID)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(c), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(c, cert, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(k, key, 0600); err != nil {
			t.Fatal(err)
		}
	}
	loader := &Loader{PolicyFile: policyFile, Roots: roots}
	server := httptest.NewUnstartedServer(Router(loader.Snapshot, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: loader.GetCertificate, GetConfigForClient: loader.GetConfigForClient}
	server.StartTLS()
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	for i := range p.Sites {
		p.Sites[i].Origin += ":" + port
	}
	write := func() {
		raw, _ := json.Marshal(p)
		tmp := policyFile + ".candidate"
		if err := os.WriteFile(tmp, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, policyFile); err != nil {
			t.Fatal(err)
		}
	}
	write()
	var connections atomic.Int32
	cache := tls.NewLRUClientSessionCache(4)
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ClientSessionCache: cache}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		connections.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resumed := false
	request := func(origin, route, host string) int {
		t.Helper()
		r, err := http.NewRequest(http.MethodGet, origin+route, nil)
		if err != nil {
			t.Fatal(err)
		}
		if host != "" {
			r.Host = host
		}
		res, err := client.Do(r)
		if err != nil {
			t.Fatal("HTTPS request failed", err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		resumed = res.TLS.DidResume
		return res.StatusCode
	}
	panel := p.PanelOrigin()
	sub := p.SubscriptionOrigin()
	if request(panel, "/login", "") != 204 || request(sub, "/sub/synthetic", "") != 204 {
		t.Fatal("approved hostname did not work")
	}
	for _, route := range []string{"/login", "/api/v1/users", "/readyz", "/assets/%2e%2e/login"} {
		if request(sub, route, "") != 404 {
			t.Fatal("public hostname reached private route")
		}
	}
	if request(sub, "/sub/synthetic", "panel.example.test:"+port) != 404 {
		t.Fatal("SNI/Host mismatch admitted")
	}
	transport.CloseIdleConnections()
	if request(sub, "/sub/synthetic", "") != 204 || !resumed {
		t.Fatal("fixture did not establish a resumable TLS session")
	}
	count := connections.Load()
	p.Sites = p.Sites[:1]
	p.Revision = strings.Repeat("e", 32)
	write()
	if request(sub, "/sub/synthetic", "") != 404 || connections.Load() != count {
		t.Fatal("retirement did not deny existing keep-alive connection")
	}
	conn, err := tls.Dial("tcp", server.Listener.Addr().String(), &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "sub.example.test", RootCAs: roots, ClientSessionCache: cache})
	if err == nil {
		conn.Close()
		t.Fatal("retired SNI created new TLS connection")
	}
	conn, err = tls.Dial("tcp", server.Listener.Addr().String(), &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "unknown.example.test", RootCAs: roots})
	if err == nil {
		conn.Close()
		t.Fatal("unknown SNI enrolled a certificate")
	}
}

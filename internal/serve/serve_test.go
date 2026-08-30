package serve

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/token"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
)

// testLog keeps stderr clean; failures surface through assertions.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig(t *testing.T, listen string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.HTTPListen = listen
	cfg.Complete()
	return cfg
}

// startNode brings up a node on an ephemeral loopback port with the fake
// backend and fails the test on error.
func startNode(t *testing.T, cfg *config.Config) *Node {
	t.Helper()
	n, err := Start(context.Background(), Options{Config: cfg, Backend: fake.New(), Log: quietLogger()})
	if err != nil {
		t.Fatalf("serve start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = n.Shutdown(ctx)
	})
	return n
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url) //nolint:gosec // test URL is local
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestServeLifecycle(t *testing.T) {
	cfg := testConfig(t, "127.0.0.1:0")
	n := startNode(t, cfg)
	if n.Addr() == "" {
		t.Fatal("no listener address")
	}
	base := "http://" + n.Addr()

	// Public ops endpoints answer on the fresh node.
	if resp := get(t, base+"/healthz"); resp.StatusCode != 200 {
		t.Fatalf("healthz: %d", resp.StatusCode)
	}
	if resp := get(t, base+"/readyz"); resp.StatusCode != 200 {
		t.Fatalf("readyz: %d", resp.StatusCode)
	}
	if resp := get(t, base+"/openapi.json"); resp.StatusCode != 200 {
		t.Fatalf("openapi: %d", resp.StatusCode)
	}

	// The API enforces auth end-to-end through the full stack.
	if resp := get(t, base+"/api/v1/users"); resp.StatusCode != 401 {
		t.Fatalf("anonymous users list: %d, want 401", resp.StatusCode)
	}

	// node.started is recorded durably (delivery is the worker's business).
	var events int
	if err := n.db.QueryRow(`SELECT COUNT(*) FROM webhook_events WHERE event_type = 'node.started'`).
		Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("node.started events: %d, want 1", events)
	}

	// node.id was initialized from the hostname on first serve.
	v, err := n.reg.Get(context.Background(), "node.id")
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := v.(string); id == "" {
		t.Fatal("node.id not initialized")
	}
}

func TestServeRestart(t *testing.T) {
	cfg := testConfig(t, "127.0.0.1:0")
	n := startNode(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := n.Shutdown(ctx); err != nil {
		t.Fatalf("first shutdown: %v", err)
	}
	// Double shutdown is safe.
	if err := n.Shutdown(ctx); err != nil {
		t.Fatalf("second shutdown: %v", err)
	}
	// A fresh node over the same data dir must come up (migrations are
	// forward-only and idempotent; the master key is reused).
	n2, err := Start(context.Background(), Options{Config: cfg, Backend: fake.New(), Log: quietLogger()})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := n2.Shutdown(ctx2); err != nil {
		t.Fatalf("restart shutdown: %v", err)
	}
}

func TestServeManualTLS(t *testing.T) {
	certPEM, keyPEM := selfSignedCert(t, "127.0.0.1")
	cfg := testConfig(t, "127.0.0.1:0")
	certFile := filepath.Join(cfg.DataDir, "cert.pem")
	keyFile := filepath.Join(cfg.DataDir, "key.pem")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.TLS.Mode = config.TLSModeManual
	cfg.TLS.CertFile = certFile
	cfg.TLS.KeyFile = keyFile

	n := startNode(t, cfg)
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed test cert
	}}
	resp, err := client.Get("https://" + n.Addr() + "/healthz")
	if err != nil {
		t.Fatalf("https healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("https healthz: %d", resp.StatusCode)
	}
	// TLS 1.1 and below must be refused (MinVersion 1.2).
	old := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MaxVersion: tls.VersionTLS11}, //nolint:gosec
	}}
	if _, err := old.Get("https://" + n.Addr() + "/healthz"); err == nil {
		t.Fatal("TLS 1.1 connection accepted; want refusal")
	}
}

func TestServeACMEDeferred(t *testing.T) {
	cfg := testConfig(t, "127.0.0.1:0")
	cfg.TLS.Mode = config.TLSModeACME
	cfg.TLS.Domain = "panel.example.com"
	_, err := Start(context.Background(), Options{Config: cfg, Backend: fake.New(), Log: quietLogger()})
	if err == nil || !strings.Contains(err.Error(), "installer") {
		t.Fatalf("acme: want clear deferral error, got %v", err)
	}
}

// TestServeDevBackendServesAPI proves the full management surface works
// end-to-end over the running node: mint a token through the DB, create a
// user with independent speed limits, list it back through the API.
func TestServeDevBackendServesAPI(t *testing.T) {
	cfg := testConfig(t, "127.0.0.1:0")
	n := startNode(t, cfg)
	base := "http://" + n.Addr()

	_, plaintext, err := token.NewService(n.db).Create(context.Background(),
		"test", []string{"users.read", "users.create", "users.update", "devices.read", "devices.write"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	req := func(method, path, body string) (*http.Response, []byte) {
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, base+path, rd)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+plaintext)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp, b
	}

	resp, body := req("POST", "/api/v1/users",
		`{"username":"alice","expires_in_days":30,"speed_limit_down_kbps":5000,"speed_limit_up_kbps":1000}`)
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Fatalf("create user: %d %s", resp.StatusCode, body)
	}
	var created struct {
		ID             string `json:"id"`
		SpeedLimitDown *int64 `json:"speed_limit_down_kbps"`
		SpeedLimitUp   *int64 `json:"speed_limit_up_kbps"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	if created.SpeedLimitDown == nil || *created.SpeedLimitDown != 5000 ||
		created.SpeedLimitUp == nil || *created.SpeedLimitUp != 1000 {
		t.Fatalf("independent limits not stored: %s", body)
	}

	resp, body = req("GET", "/api/v1/users/"+created.ID, "")
	if resp.StatusCode != 200 {
		t.Fatalf("get user: %d %s", resp.StatusCode, body)
	}
	// Setting one direction to null clears only that direction.
	resp, body = req("PATCH", "/api/v1/users/"+created.ID, `{"speed_limit_up_kbps": null}`)
	if resp.StatusCode != 200 {
		t.Fatalf("patch user: %d %s", resp.StatusCode, body)
	}
	var patched struct {
		SpeedLimitDown *int64 `json:"speed_limit_down_kbps"`
		SpeedLimitUp   *int64 `json:"speed_limit_up_kbps"`
	}
	if err := json.Unmarshal(body, &patched); err != nil {
		t.Fatal(err)
	}
	if patched.SpeedLimitDown == nil || *patched.SpeedLimitDown != 5000 || patched.SpeedLimitUp != nil {
		t.Fatalf("tri-state clear semantics: %s", body)
	}
}

// selfSignedCert generates a throwaway TLS pair for the manual-mode test.
func selfSignedCert(t *testing.T, host string) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{host},
		IPAddresses:           []net.IP{net.ParseIP(host)},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

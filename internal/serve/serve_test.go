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
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
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

func TestServeOwnsDataUntilShutdown(t *testing.T) {
	cfg := testConfig(t, "127.0.0.1:0")
	n := startNode(t, cfg)
	s := &backup.Service{Cfg: cfg}
	if lease, err := s.OpenData(true); err == nil {
		lease.Close()
		t.Fatal("running server admitted rotation")
	}
	shared, err := s.OpenData(false)
	if err != nil {
		t.Fatal("shared CLI admission", err)
	}
	shared.Close()
	if err := n.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	exclusive, err := s.OpenData(true)
	if err != nil {
		t.Fatal("shutdown leaked data ownership", err)
	}
	exclusive.Close()
}

func TestShutdownKeepsDataOwnershipUntilHandlersDrain(t *testing.T) {
	cfg := testConfig(t, "127.0.0.1:0")
	s := &backup.Service{Cfg: cfg}
	lease, err := s.OpenData(false)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-release }))
	defer ts.Close()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := http.Get(ts.URL)
		if err == nil {
			resp.Body.Close()
		}
	}()
	<-entered
	n := &Node{db: db, dataLease: lease, httpServer: ts.Config}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := n.Shutdown(ctx); err == nil {
		t.Fatal("shutdown ignored undrained handler")
	}
	if l, err := s.OpenData(true); err == nil {
		l.Close()
		t.Fatal("shutdown released data while handler still held keys")
	}
	close(release)
	released = true
	<-done
	if err := n.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	l, err := s.OpenData(true)
	if err != nil {
		t.Fatal("successful retry leaked ownership", err)
	}
	l.Close()
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

func TestServeConsumesPendingAWGRangeRestore(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t, "127.0.0.1:0")
	n := startNode(t, cfg)
	parseH := func(text string) awgparam.U32Range {
		value, err := awgparam.ParseU32Range(text)
		if err != nil {
			t.Fatalf("parse H range %q: %v", text, err)
		}
		return value
	}
	ifaces := iface.NewService(n.db, n.reg, n.ring)
	created, err := ifaces.Create(ctx, iface.CreateInput{
		Name: "awg0", ListenPort: 39001, Subnet: "10.77.0.0/24",
		Obfuscation: iface.Obfuscation{
			Enabled: true, Jc: 4, Jmin: 40, Jmax: 70, S1: 15, S2: 64,
			H1: parseH("100-110"), H2: parseH("200-220"),
			H3: parseH("300-330"), H4: parseH("400-440"),
		},
		Preset: "custom",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.reg.Set(ctx, "network.client_persistent_keepalive", "40-50"); err != nil {
		t.Fatal(err)
	}
	archive, err := n.backup.Create(ctx, backup.CreateOpts{Reason: "phase8-boot-restore"})
	if err != nil {
		t.Fatal(err)
	}
	preview, _, err := n.backup.Stage(ctx, archive.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.backup.Approve(preview.PreviewID()); err != nil {
		t.Fatal(err)
	}

	// Prove the next boot swaps in the staged snapshot rather than retaining
	// subsequent live mutations.
	if _, err := n.db.ExecContext(ctx, `UPDATE tunnel_interfaces SET h1 = 999, h1_range = '999' WHERE id = ?`, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := n.db.ExecContext(ctx, `UPDATE settings SET value = '99' WHERE key = 'network.client_persistent_keepalive'`); err != nil {
		t.Fatal(err)
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := n.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}

	n2, err := Start(ctx, Options{Config: cfg, Backend: fake.New(), Log: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = n2.Shutdown(stopCtx)
	})
	var h1Range string
	var legacyH1 int64
	if err := n2.db.QueryRowContext(ctx, `SELECT h1_range, h1 FROM tunnel_interfaces WHERE id = ?`, created.ID).
		Scan(&h1Range, &legacyH1); err != nil {
		t.Fatal(err)
	}
	if h1Range != "100-110" || legacyH1 != 100 {
		t.Fatalf("boot-restored H1 = %q / %d", h1Range, legacyH1)
	}
	var keepalive string
	if err := n2.db.QueryRowContext(ctx,
		`SELECT value FROM settings WHERE key='network.client_persistent_keepalive'`).Scan(&keepalive); err != nil {
		t.Fatal(err)
	}
	if keepalive != "40-50" {
		t.Fatalf("boot-restored keepalive = %q", keepalive)
	}
	var restoredEvents int
	if err := n2.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE action='backup.restored'`).Scan(&restoredEvents); err != nil {
		t.Fatal(err)
	}
	if restoredEvents != 1 {
		t.Fatalf("backup.restored audit events = %d, want 1", restoredEvents)
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

// TestServeACMEWiring brings a node up in ACME mode on an ephemeral loopback
// TLS listener and a free challenge port, then verifies the port-80 sidecar
// wiring: challenge paths are host-policy-gated and everything else redirects
// to the configured domain with the real TLS port. No ACME traffic reaches
// the network: the TLS assertion uses an SNI outside the whitelist, which
// HostWhitelist rejects before any directory lookup.
func TestServeACMEWiring(t *testing.T) {
	// A free high port for the challenge sidecar (unprivileged test run).
	freePort := func() int {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	challengePort := freePort()
	// The TLS listener port must be concrete: the sidecar redirect embeds the
	// configured port, and an ephemeral :0 cannot be redirected to.
	tlsPort := freePort()

	cfg := testConfig(t, fmt.Sprintf("127.0.0.1:%d", tlsPort))
	cfg.TLS.Mode = config.TLSModeACME
	cfg.TLS.Domain = "panel.example.com"
	cfg.TLS.ACMEHTTPPort = challengePort

	n := startNode(t, cfg)

	// A client that does NOT follow the redirect — we assert the Location.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// The sidecar redirects plain-HTTP visitors to the configured domain,
	// keeping the actual TLS port (not autocert's hardcoded :443).
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/users?x=1", challengePort), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("sidecar redirect: %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if want := "https://panel.example.com:" + strconv.Itoa(tlsPort) + "/users?x=1"; loc != want {
		t.Fatalf("redirect = %q, want %q", loc, want)
	}

	// Challenge paths on a non-whitelisted Host are refused (403), not
	// echoed: the sidecar is public, the whitelist is the gate.
	resp2 := get(t, fmt.Sprintf("http://127.0.0.1:%d/.well-known/acme-challenge/tok", challengePort))
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("challenge on foreign host: %d, want 403", resp2.StatusCode)
	}

	// TLS listener: an SNI outside the whitelist fails fast with the
	// host-policy error before any ACME network contact.
	d := &tls.Dialer{Config: &tls.Config{ServerName: "other.example.com", InsecureSkipVerify: true}} //nolint:gosec // wiring probe
	if _, err := d.DialContext(context.Background(), "tcp", n.Addr()); err == nil {
		t.Fatal("TLS handshake for non-whitelisted SNI succeeded; want host-policy refusal")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := n.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	// The challenge sidecar is closed with the node.
	if c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", challengePort)); err == nil {
		_ = c.Close()
		t.Fatal("challenge sidecar still accepting after shutdown")
	}
}

// TestServeACMEChallengePortBusy fails boot loudly when the challenge port is
// taken — a silent port-80 failure would only surface as inexplicable
// issuance errors days later.
func TestServeACMEChallengePortBusy(t *testing.T) {
	// Bind the SAME wildcard shape the node will request: on Windows a
	// loopback-only bind does not conflict with a wildcard bind.
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	cfg := testConfig(t, "127.0.0.1:0")
	cfg.TLS.Mode = config.TLSModeACME
	cfg.TLS.Domain = "panel.example.com"
	cfg.TLS.ACMEHTTPPort = port
	_, err = Start(context.Background(), Options{Config: cfg, Backend: fake.New(), Log: quietLogger()})
	if err == nil || !strings.Contains(err.Error(), "challenge listener") {
		t.Fatalf("busy challenge port: want clear boot error, got %v", err)
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
		`{"username":"alice","duration_seconds":2592000,"speed_limit_down_kbps":5000,"speed_limit_up_kbps":1000}`)
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

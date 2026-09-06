package install

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/config"
)

func TestWaitCertificatePreservesLastHandshakeCause(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	p := Defaults()
	p.TLSMode = config.TLSModeManual
	p.Domain = "wrong.invalid"
	p.PanelPort = portOf(srv.Listener.Addr().String())
	err := WaitCertificate(context.Background(), p, 100*time.Millisecond)
	var certErr *tls.CertificateVerificationError
	if !errors.As(err, &certErr) {
		t.Fatalf("lost classified certificate error: %v", err)
	}
}

func TestCertificateProbeVerifiesNameAndTrust(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	port, _ := strconv.Atoi(strings.Split(server.Listener.Addr().String(), ":")[1])
	p := Defaults()
	p.PanelPort = port
	p.Domain = "example.com"
	p.TLSMode = config.TLSModeACME
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	if err := probeCertificate(context.Background(), p, roots); err != nil {
		t.Fatal(err)
	}
	p.Domain = "wrong.invalid"
	if err := probeCertificate(context.Background(), p, roots); err == nil {
		t.Fatal("wrong hostname certificate accepted")
	}
	p.Domain = "example.com"
	if err := probeCertificate(context.Background(), p, nil); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
}

func TestCertificateFailurePersistsRecoverablePendingState(t *testing.T) {
	h := newMemHost()
	p := Defaults()
	p.Domain = "panel.example.com"
	p.TLSMode = config.TLSModeACME
	p.PanelPort = 54443
	p.ACMEHTTPPort = healthServer(t, 302)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	st, err := Install(ctx, h, InstallOptions{Plan: p, Yes: true, Stdout: io.Discard})
	if err == nil {
		t.Fatal("process health incorrectly certified TLS")
	}
	saved, loadErr := LoadState(h)
	if loadErr != nil || saved == nil || st == nil || saved.TLSReadiness != "pending" {
		t.Fatalf("lost recoverable pending state: %v %v", saved, loadErr)
	}
}

func TestCertificateCancellationIsBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := Defaults()
	p.Domain = "panel.example.com"
	p.TLSMode = config.TLSModeACME
	start := time.Now()
	if err := WaitCertificate(ctx, p, time.Minute); err == nil {
		t.Fatal("cancellation accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancelled TLS probe exceeded bound")
	}
}

func TestManualCertificatePendingDoesNotRequireHTTP01(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := Defaults()
	p.TLSMode = config.TLSModeManual
	p.PublicIP = "203.0.113.7"
	err := WaitCertificate(ctx, p, time.Second)
	if err == nil || strings.Contains(err.Error(), "TCP 80") || !strings.Contains(err.Error(), "trusted") {
		t.Fatalf("incorrect manual certificate guidance: %v", err)
	}
}

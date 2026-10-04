package install

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/config"
)

func TestLifecycleRefusesLiveButUnreadyNode(t *testing.T) {
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		probes.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	p := Defaults()
	p.PanelPort = portOf(server.Listener.Addr().String())
	err := waitHealthy(context.Background(), newMemHost(), p, 30*time.Millisecond)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || probes.Load() == 0 {
		t.Fatal("liveness alone completed the lifecycle readiness gate")
	}
}

func TestReadinessProofRejectsRedirectInvalidAndOversizedBodies(t *testing.T) {
	for _, body := range []string{`{"status":"unavailable"}`, `{"status":"ready"} {}`, `{"status":"ready","secret":"synthetic"}`, strings.Repeat(" ", 1025)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
		if err := probeReadinessURL(context.Background(), server.URL, false, ""); err == nil {
			server.Close()
			t.Fatal("invalid readiness response was accepted")
		}
		server.Close()
	}
	var external atomic.Int32
	edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		external.Add(1)
		_, _ = io.WriteString(w, `{"status":"ready"}`)
	}))
	defer edge.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, edge.URL, http.StatusFound) }))
	defer redirect.Close()
	if err := probeReadinessURL(context.Background(), redirect.URL, false, ""); !errors.Is(err, errReadinessRedirect) || external.Load() != 0 {
		t.Fatal("readiness followed an untrusted redirect")
	}
}

func TestRetainedACMEArtifactReadinessUsesRecordedLocalTLSName(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS.ServerName != "panel.example.com" || r.Host != "panel.example.com" {
			w.WriteHeader(http.StatusMisdirectedRequest)
			return
		}
		_, _ = io.WriteString(w, `{"status":"ready"}`)
	}))
	defer tlsServer.Close()
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://untrusted.invalid/", http.StatusFound)
	}))
	defer sidecar.Close()
	p := Defaults()
	p.TLSMode = config.TLSModeACME
	p.Domain = "panel.example.com"
	p.PanelPort = portOf(tlsServer.Listener.Addr().String())
	p.ACMEHTTPPort = portOf(sidecar.Listener.Addr().String())
	if err := ProbeReadiness(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

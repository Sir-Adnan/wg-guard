package web

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	restapi "github.com/Sir-Adnan/wg-guard/internal/api"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/metrics"
	"github.com/Sir-Adnan/wg-guard/internal/testutil/qrdecode"
)

func TestConfigDownloadsUseCanonicalBytesAndHeaders(t *testing.T) {
	e := newEnv(t)
	userID, deviceID, _, cookie := e.seedUserWithDevice()
	ctx := context.Background()
	for key, value := range map[string]any{
		"node.endpoint":             "vpn.example.com",
		"downloads.filename_prefix": "wg",
		"downloads.filename_suffix": "v2",
	} {
		if err := e.reg.Set(ctx, key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	canonical, err := e.srv.ClientConf.Render(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	link, err := e.srv.Links.ForUser(ctx, userID)
	if err != nil || link == nil {
		t.Fatalf("load subscription link: %v", err)
	}

	_, plaintext, err := e.srv.Tokens.Create(ctx, "config-surface-test", auth.AllScopes(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	apiServer := restapi.New(restapi.Deps{
		DB: e.db, Tokens: e.srv.Tokens, Users: e.srv.Users, Devices: e.srv.Devices,
		Plans: e.srv.Plans, Ifaces: e.srv.Ifaces, Settings: e.srv.Settings,
		Ring: e.srv.Ring, Audit: e.srv.Audit, Accounting: e.srv.Accounting,
		Webhooks: e.srv.Webhooks, Metrics: metrics.New(), ClientConf: e.srv.ClientConf,
		NodeID: e.srv.NodeID, ToolsVersion: e.srv.ToolsVersion,
	})
	apiHandler := apiServer.Handler()
	apiGet := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+plaintext)
		rec := httptest.NewRecorder()
		apiHandler.ServeHTTP(rec, req)
		return rec
	}

	responses := map[string]*httptest.ResponseRecorder{
		"REST API":            apiGet("/api/v1/devices/" + deviceID + "/config"),
		"admin panel":         e.get("/devices/"+deviceID+"/config", cookie),
		"public subscription": e.get(e.subBase(link.Token)+"/devices/"+deviceID+"/config", nil),
	}
	wantDisposition := `attachment; filename="wg-alice-phone-v2.conf"`
	for name, response := range responses {
		if response.Code != http.StatusOK {
			t.Errorf("%s config status = %d, want 200", name, response.Code)
			continue
		}
		assertCanonicalConfigBytes(t, name, []byte(canonical), response.Body.Bytes())
		if got := response.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Errorf("%s Content-Type = %q", name, got)
		}
		if got := response.Header().Get("Content-Disposition"); got != wantDisposition {
			t.Errorf("%s Content-Disposition = %q", name, got)
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s Cache-Control = %q", name, got)
		}
		if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s X-Content-Type-Options = %q", name, got)
		}
	}
	if !strings.HasSuffix(canonical, "\n") || strings.HasSuffix(canonical, "\n\n") {
		t.Fatal("canonical config must end with exactly one newline")
	}

	qrResponses := map[string]*httptest.ResponseRecorder{
		"REST API":            apiGet("/api/v1/devices/" + deviceID + "/qr"),
		"admin panel":         e.get("/devices/"+deviceID+"/qr", cookie),
		"public subscription": e.get(e.subBase(link.Token)+"/devices/"+deviceID+"/qr", nil),
	}
	wantQRDisposition := `inline; filename="wg-alice-phone-v2.png"`
	for name, response := range qrResponses {
		if response.Code != http.StatusOK {
			t.Errorf("%s QR status = %d, want 200", name, response.Code)
			continue
		}
		decoded, err := qrdecode.PNG(response.Body.Bytes())
		if err != nil {
			t.Errorf("%s independent QR decode failed (PNG len=%d): %v", name, response.Body.Len(), err)
			continue
		}
		assertCanonicalConfigBytes(t, name+" decoded QR", []byte(canonical), []byte(decoded))
		for header, want := range map[string]string{
			"Content-Type":           "image/png",
			"Content-Disposition":    wantQRDisposition,
			"Cache-Control":          "no-store",
			"X-Content-Type-Options": "nosniff",
		} {
			if got := response.Header().Get(header); got != want {
				t.Errorf("%s QR %s = %q, want %q", name, header, got, want)
			}
		}
	}

	// A corrupted legacy row can exceed the application QR bound even though
	// current writes are validated. Every delivery surface must fail closed
	// with a client error and must never echo the secret-bearing config.
	if _, err := e.db.ExecContext(ctx, `UPDATE tunnel_interfaces SET jc = 4, i1 = ?
		WHERE id = (SELECT interface_id FROM devices WHERE id = ?)`, strings.Repeat("x", 2601), deviceID); err != nil {
		t.Fatal(err)
	}
	oversizedResponses := map[string]*httptest.ResponseRecorder{
		"REST API":            apiGet("/api/v1/devices/" + deviceID + "/qr"),
		"admin panel":         e.get("/devices/"+deviceID+"/qr", cookie),
		"public subscription": e.get(e.subBase(link.Token)+"/devices/"+deviceID+"/qr", nil),
	}
	for name, response := range oversizedResponses {
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s oversized QR status = %d, want 400", name, response.Code)
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s oversized QR Cache-Control = %q", name, got)
		}
		if response.Body.Len() > 1024 {
			t.Errorf("%s oversized QR error body is unexpectedly large: %d bytes", name, response.Body.Len())
		}
	}
}

// assertCanonicalConfigBytes deliberately reports only length, digest, and
// first-difference metadata. A config contains private key material and must
// never be copied into test or CI output.
func assertCanonicalConfigBytes(t *testing.T, surface string, want, got []byte) {
	t.Helper()
	if string(got) == string(want) {
		return
	}
	wantSum := sha256.Sum256(want)
	gotSum := sha256.Sum256(got)
	t.Fatalf("%s config differs: got len=%d sha256=%x, want len=%d sha256=%x, first difference=%d",
		surface, len(got), gotSum[:8], len(want), wantSum[:8], firstConfigDifference(got, want))
}

func firstConfigDifference(left, right []byte) int {
	limit := min(len(left), len(right))
	for i := 0; i < limit; i++ {
		if left[i] != right[i] {
			return i
		}
	}
	return limit
}

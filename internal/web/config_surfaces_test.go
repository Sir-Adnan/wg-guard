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
	apiReq := httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID+"/config", nil)
	apiReq.Header.Set("Authorization", "Bearer "+plaintext)
	apiRec := httptest.NewRecorder()
	apiServer.Handler().ServeHTTP(apiRec, apiReq)

	responses := map[string]*httptest.ResponseRecorder{
		"REST API":            apiRec,
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

package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
)

func TestPurchaseDeviceCountValidationAndDelivery(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.ifaces.Create(ctx, iface.CreateInput{Name: "awg0", ListenPort: 39001, Subnet: "10.77.0.0/24"}); err != nil {
		t.Fatal(err)
	}
	send := func(method, path, key, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+e.plainTok)
		req.Header.Set("Idempotency-Key", key)
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		return rec
	}
	terms := `"entitlement":{"traffic_limit_bytes":100000000000,"duration_seconds":2592000,"device_limit":100}`
	for _, count := range []string{"0", "-1", "101", "1000000", "1.5", `"3"`} {
		rec := send("POST", "/api/v1/purchases", "bad-count-"+count, "{"+terms+`,"device_count":`+count+"}")
		if rec.Code != http.StatusBadRequest || errCode(t, rec) != domain.CodeInvalidRequest {
			t.Fatalf("invalid count %s: %d %s", count, rec.Code, rec.Body.String())
		}
	}
	body := "{" + terms + `,"device_count":3}`
	first := send("POST", "/api/v1/purchases", "all-devices", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("purchase: %d %s", first.Code, first.Body.String())
	}
	result := decodeBody(t, first)
	ids := result["device_ids"].([]any)
	if len(ids) != 3 || ids[0] != result["device_id"] {
		t.Fatalf("device identifiers: %v", result)
	}
	for i, id := range ids {
		d, err := e.devices.Get(ctx, id.(string))
		if err != nil || d.Name != fmt.Sprintf("device-%d", i+1) {
			t.Fatalf("numbered device %d: %+v %v", i, d, err)
		}
		if conf := send("GET", "/api/v1/devices/"+id.(string)+"/config", "", ""); conf.Code != http.StatusOK ||
			!strings.Contains(conf.Body.String(), "[Interface]") || conf.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("device %d configuration delivery: %d", i, conf.Code)
		}
	}
	replay := send("POST", "/api/v1/purchases", "all-devices", body)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" || !reflect.DeepEqual(decodeBody(t, replay), result) {
		t.Fatalf("API replay changed result: %d %s", replay.Code, replay.Body.String())
	}
	lookup := send("GET", "/api/v1/operations/result", "all-devices", "")
	if lookup.Code != http.StatusOK || !reflect.DeepEqual(decodeBody(t, lookup), result) {
		t.Fatalf("API result lookup: %d %s", lookup.Code, lookup.Body.String())
	}
	if rec := send("POST", "/api/v1/purchases", "all-devices", strings.Replace(body, `"device_count":3`, `"device_count":2`, 1)); rec.Code != http.StatusConflict {
		t.Fatalf("count change did not conflict: %d", rec.Code)
	}
	if rec := send("POST", "/api/v1/purchases", "over-cap", strings.Replace(body, `"device_limit":100`, `"device_limit":2`, 1)); rec.Code != http.StatusConflict || errCode(t, rec) != domain.CodeDeviceLimitReached {
		t.Fatalf("cap not enforced: %d %s", rec.Code, rec.Body.String())
	}
	// Omitted count and explicit count=1 are the same operation.
	one := send("POST", "/api/v1/purchases", "default-count", "{"+terms+"}")
	explicit := send("POST", "/api/v1/purchases", "default-count", "{"+terms+`,"device_count":1}`)
	if one.Code != http.StatusCreated || explicit.Code != http.StatusCreated || explicit.Header().Get("Idempotency-Replayed") != "true" || !reflect.DeepEqual(decodeBody(t, one), decodeBody(t, explicit)) {
		t.Fatalf("default count replay mismatch: %d %d", one.Code, explicit.Code)
	}
}

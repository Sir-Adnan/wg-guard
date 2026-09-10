package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
)

func apiTelemetrySampler(t *testing.T, count int) *telemetry.Sampler {
	t.Helper()
	base := time.Now().UTC().Add(-time.Duration(count) * telemetry.DefaultCadence)
	n := 0
	sampler := telemetry.New(telemetry.SourceFunc(func(_ context.Context, at time.Time) (telemetry.RawSample, error) {
		n++
		cpu := float64(n)
		raw := telemetry.RawSample{
			At:                      at,
			HostAvailable:           true,
			CPUPercent:              &cpu,
			MemTotalBytes:           1_000,
			MemAvailableBytes:       250,
			ProcessHeapBytes:        50,
			ProcessMetricsAvailable: true,
			Goroutines:              4,
			HostNetwork: telemetry.Counter{
				Identity: "private-host-name", RXBytes: uint64(n * 1_000), TXBytes: uint64(n * 2_000), Available: true,
			},
			VPNNetwork: telemetry.Counter{
				Identity: "private-awg-name", RXBytes: uint64(n * 3_000), TXBytes: uint64(n * 4_000), Available: true,
			},
			OnlineUsers: 2, ActivePeers: 3, ActivityAvailable: true,
			Ready: true, ReadinessAvailable: true,
		}
		if n == count {
			raw.CPUPercent = nil
		}
		return raw, nil
	}), telemetry.DefaultCadence)
	for i := 0; i < count; i++ {
		if _, err := sampler.Sample(context.Background(), base.Add(time.Duration(i)*telemetry.DefaultCadence)); err != nil {
			t.Fatal(err)
		}
	}
	return sampler
}

func TestTelemetryEndpointBoundsOrderAndNullableMetrics(t *testing.T) {
	e := newEnv(t)
	e.srv.Telemetry = apiTelemetrySampler(t, telemetry.HistoryCapacity+5)

	rec := e.do(http.MethodGet, "/api/v1/node/telemetry", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("default telemetry: %d %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	points := body["points"].([]any)
	if len(points) != 60 {
		t.Fatalf("default points = %d", len(points))
	}
	latest := body["latest"].(map[string]any)
	if latest["cpu_percent"] != nil {
		t.Fatalf("unavailable CPU must be null: %v", latest["cpu_percent"])
	}
	if latest["health"] != "healthy" || body["cadence_seconds"] != float64(10) {
		t.Fatalf("telemetry shape = %v", body)
	}
	if latest["online_users"] != float64(2) || latest["active_peers"] != float64(3) {
		t.Fatalf("activity shape = %v", latest)
	}
	first := points[0].(map[string]any)["timestamp"].(string)
	last := points[len(points)-1].(map[string]any)["timestamp"].(string)
	if first >= last {
		t.Fatalf("points not chronological: %q >= %q", first, last)
	}
	for _, secretTopology := range []string{"private-host-name", "private-awg-name"} {
		if strings.Contains(rec.Body.String(), secretTopology) {
			t.Fatalf("response leaked %q", secretTopology)
		}
	}

	if rec = e.do(http.MethodGet, "/api/v1/node/telemetry?points=1", ""); rec.Code != http.StatusOK {
		t.Fatalf("one point: %d", rec.Code)
	} else if got := len(decodeBody(t, rec)["points"].([]any)); got != 1 {
		t.Fatalf("one-point length = %d", got)
	}
	if rec = e.do(http.MethodGet, "/api/v1/node/telemetry?points=999", ""); rec.Code != http.StatusOK {
		t.Fatalf("capped points: %d", rec.Code)
	} else if got := len(decodeBody(t, rec)["points"].([]any)); got != telemetry.HistoryCapacity {
		t.Fatalf("capped length = %d", got)
	}
	for _, value := range []string{"0", "-1", "invalid", ""} {
		rec = e.do(http.MethodGet, "/api/v1/node/telemetry?points="+value, "")
		if rec.Code != http.StatusBadRequest || errCode(t, rec) != "INVALID_REQUEST" {
			t.Fatalf("points=%s: %d %s", value, rec.Code, rec.Body.String())
		}
	}
	rec = e.do(http.MethodGet, "/api/v1/node/telemetry?points=1&points=2", "")
	if rec.Code != http.StatusBadRequest || errCode(t, rec) != "INVALID_REQUEST" {
		t.Fatalf("duplicate points: %d %s", rec.Code, rec.Body.String())
	}
}

func TestTelemetryEndpointRequiresStatsRead(t *testing.T) {
	e := newEnv(t)
	if rec := e.doAnonymous(http.MethodGet, "/api/v1/node/telemetry"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous telemetry = %d", rec.Code)
	}
	_, token, err := e.tokens.Create(context.Background(), "telemetry-wrong-scope", []string{"node.read"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/node/telemetry", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong scope telemetry = %d %s", rec.Code, rec.Body.String())
	}
}

func TestOpenAPITelemetryContract(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(openapiJSON, &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	path, ok := paths["/api/v1/node/telemetry"].(map[string]any)
	if !ok {
		t.Fatal("OpenAPI missing telemetry path")
	}
	get := path["get"].(map[string]any)
	responses := get["responses"].(map[string]any)
	schema := responses["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if schema["$ref"] != "#/components/schemas/TelemetryResponse" {
		t.Fatalf("telemetry response schema = %v", schema)
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	point := schemas["TelemetryPoint"].(map[string]any)
	properties := point["properties"].(map[string]any)
	for _, name := range []string{
		"timestamp", "health", "issues", "cpu_percent", "memory_used_bytes", "memory_total_bytes",
		"host_rx_bytes_per_second", "host_tx_bytes_per_second", "vpn_rx_bytes_per_second",
		"vpn_tx_bytes_per_second", "online_users", "active_peers",
	} {
		if _, ok := properties[name]; !ok {
			t.Errorf("TelemetryPoint missing %q", name)
		}
	}
	for _, name := range []string{"cpu_percent", "host_rx_bytes_per_second", "online_users"} {
		if properties[name].(map[string]any)["nullable"] != true {
			t.Errorf("%s must be nullable", name)
		}
	}
}

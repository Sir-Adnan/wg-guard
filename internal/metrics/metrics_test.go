package metrics

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
)

func TestAccountingStatusTracksRecentFailureAndRecovery(t *testing.T) {
	c := New()
	t0 := time.Unix(1_700_000_000, 0)
	if failed, available := c.AccountingStatus(t0, 5*time.Minute); failed || available {
		t.Fatalf("initial status = failed %v available %v", failed, available)
	}
	c.SetAccountingError(t0)
	if failed, available := c.AccountingStatus(t0.Add(time.Minute), 5*time.Minute); !failed || !available {
		t.Fatalf("recent failure = failed %v available %v", failed, available)
	}
	if failed, available := c.AccountingStatus(t0.Add(10*time.Minute), 5*time.Minute); failed || !available {
		t.Fatalf("expired failure = failed %v available %v", failed, available)
	}
	c.SetLastCycle(time.Second, t0.Add(11*time.Minute), 0)
	if failed, available := c.AccountingStatus(t0.Add(11*time.Minute), 5*time.Minute); failed || !available {
		t.Fatalf("recovered status = failed %v available %v", failed, available)
	}
}

func TestHandlerRendersBoundedTelemetryWithoutTopology(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	n := 0
	sampler := telemetry.New(telemetry.SourceFunc(func(_ context.Context, at time.Time) (telemetry.RawSample, error) {
		n++
		return telemetry.RawSample{
			At:                  at,
			HostAvailable:       true,
			HostNetwork:         telemetry.Counter{Identity: "private-host-interface", RXBytes: uint64(n * 1_000), TXBytes: uint64(n * 2_000), Available: true},
			VPNNetwork:          telemetry.Counter{Identity: "private-awg-interface", RXBytes: uint64(n * 3_000), TXBytes: uint64(n * 4_000), Available: true},
			Ready:               true,
			ReadinessAvailable:  true,
			AccountingAvailable: true,
		}, nil
	}), 10*time.Second)
	if _, err := sampler.Sample(context.Background(), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := sampler.Sample(context.Background(), t0.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}

	c := New()
	c.SetTelemetry(sampler, func() time.Time { return t0.Add(10 * time.Second) })
	rec := httptest.NewRecorder()
	c.Handler(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"wgguard_telemetry_cadence_seconds 10",
		"wgguard_node_health{state=\"healthy\"} 1",
		"wgguard_host_network_receive_bytes_per_second 100",
		"wgguard_host_network_transmit_bytes_per_second 200",
		"wgguard_vpn_receive_bytes_per_second 300",
		"wgguard_vpn_transmit_bytes_per_second 400",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{"private-host-interface", "private-awg-interface"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("metrics leaked topology %q", forbidden)
		}
	}
}

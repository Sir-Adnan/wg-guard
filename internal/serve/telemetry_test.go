package serve

import (
	"context"
	"log/slog"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/hoststats"
	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
)

type fixtureTelemetryHost struct {
	snapshot  hoststats.Snapshot
	counters  hoststats.NetworkCounters
	requested []string
}

func (h *fixtureTelemetryHost) Snapshot(time.Time) hoststats.Snapshot { return h.snapshot }
func (h *fixtureTelemetryHost) InterfaceCounters(names []string) hoststats.NetworkCounters {
	h.requested = append([]string(nil), names...)
	return h.counters
}

func telemetryDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open(t.TempDir()+"/telemetry.db", database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background(), slog.Default()); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedTelemetryActivity(t *testing.T, db *database.DB, now time.Time) {
	t.Helper()
	stamp := now.Format(time.RFC3339Nano)
	for _, row := range []struct {
		id, name string
		enabled  int
		port     int
	}{
		{"if-0", "awg0", 1, 40001},
		{"if-1", "awg1", 0, 40002},
	} {
		if _, err := db.Exec(`INSERT INTO tunnel_interfaces
			(id, name, listen_port, ipv4_subnet, mtu, public_key, private_key_encrypted,
			 preset_name, enabled, backend_mode, created_at, updated_at)
			VALUES (?, ?, ?, ?, 1420, ?, x'00', 'plain', ?, 'kernel', ?, ?)`,
			row.id, row.name, row.port, "10.20."+row.id[len(row.id)-1:]+".0/24", "pub-"+row.id,
			row.enabled, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		id, name, status string
		enabled          int
	}{
		{"user-1", "alpha", "active", 1},
		{"user-2", "disabled", "active", 0},
		{"user-3", "stale", "active", 1},
	} {
		if _, err := db.Exec(`INSERT INTO users
			(id, username, status, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			row.id, row.name, row.status, row.enabled, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	devices := []struct {
		id, user string
		enabled  int
		seen     time.Time
	}{
		{"device-1", "user-1", 1, now.Add(-30 * time.Second)},
		{"device-2", "user-1", 1, now.Add(-60 * time.Second)},
		{"device-3", "user-2", 1, now.Add(-10 * time.Second)},
		{"device-4", "user-3", 1, now.Add(-10 * time.Minute)},
		{"device-5", "user-3", 0, now.Add(-10 * time.Second)},
	}
	for i, row := range devices {
		if _, err := db.Exec(`INSERT INTO devices
			(id, user_id, interface_id, name, ipv4_address, public_key, private_key_encrypted,
			 enabled, last_handshake_at, created_at, updated_at)
			VALUES (?, ?, 'if-0', ?, ?, ?, x'00', ?, ?, ?, ?)`,
			row.id, row.user, row.id, "10.20.0."+string(rune('2'+i)), "pub-"+row.id,
			row.enabled, row.seen.Format(time.RFC3339Nano), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNodeTelemetrySourceCollectsOneBoundedAggregate(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	db := telemetryDB(t)
	seedTelemetryActivity(t, db, now)
	cpu, load := 20.0, 0.4
	host := &fixtureTelemetryHost{
		snapshot: hoststats.Snapshot{
			At: now, OK: true, CPUPercent: &cpu, MemTotal: 1_000, MemAvail: 400,
			DiskTotal: 2_000, DiskFree: 500, Load1: &load, Uptime: time.Hour,
			ProcessRSSBytes: 100,
			HostNetwork:     hoststats.NetworkCounters{Identity: "eth0", RXBytes: 10, TXBytes: 20, Interfaces: 1, Available: true},
		},
		counters: hoststats.NetworkCounters{Identity: "awg0", RXBytes: 30, TXBytes: 40, Interfaces: 1, Available: true},
	}
	source := &nodeTelemetrySource{
		db: db, host: host,
		onlineWindow: func(context.Context) time.Duration { return 3 * time.Minute },
		ready:        func() bool { return true },
		accounting:   func(time.Time) (bool, bool) { return false, true },
		process:      func() (uint64, uint64) { return 50, 7 },
	}
	raw, err := source.Read(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(host.requested, []string{"awg0"}) {
		t.Fatalf("requested interfaces = %v", host.requested)
	}
	if raw.OnlineUsers != 1 || raw.ActivePeers != 2 || !raw.ActivityAvailable {
		t.Fatalf("activity = users %d peers %d available %v", raw.OnlineUsers, raw.ActivePeers, raw.ActivityAvailable)
	}
	if raw.EnabledInterfaces != 1 || raw.ObservedInterfaces != 1 || !raw.InterfacesAvailable {
		t.Fatalf("interfaces = enabled %d observed %d available %v", raw.EnabledInterfaces, raw.ObservedInterfaces, raw.InterfacesAvailable)
	}
	if raw.ProcessHeapBytes != 50 || raw.Goroutines != 7 || !raw.ProcessMetricsAvailable {
		t.Fatalf("process = heap %d goroutines %d", raw.ProcessHeapBytes, raw.Goroutines)
	}
	if raw.HostNetwork.Identity != "eth0" || raw.VPNNetwork.Identity != "awg0" || !raw.Ready {
		t.Fatalf("runtime state = %+v", raw)
	}
}

func TestStartTakesInitialTelemetrySample(t *testing.T) {
	cfg := testConfig(t, "127.0.0.1:0")
	var calls atomic.Int32
	source := telemetry.SourceFunc(func(_ context.Context, at time.Time) (telemetry.RawSample, error) {
		calls.Add(1)
		return telemetry.RawSample{At: at, HostAvailable: true, Ready: true, ReadinessAvailable: true}, nil
	})
	n, err := Start(context.Background(), Options{
		Config: cfg, Backend: fake.New(), Log: quietLogger(),
		TelemetrySource: source, TelemetryCadence: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Shutdown(context.Background()) })
	if calls.Load() != 1 {
		t.Fatalf("source calls before Start returns = %d", calls.Load())
	}
	history := n.telemetry.Snapshot(time.Now(), 1)
	if !history.Available || len(history.Points) != 1 {
		t.Fatalf("initial history = %+v", history)
	}
}

func TestTelemetryReflectsRecentAccountingFailure(t *testing.T) {
	cfg := testConfig(t, "127.0.0.1:0")
	n := startNode(t, cfg)
	n.metrics.SetAccountingError(time.Now())
	if err := n.jobTelemetry(context.Background()); err != nil {
		t.Fatal(err)
	}
	history := n.telemetry.Snapshot(time.Now(), 1)
	if !history.Latest.Issues.Has(telemetry.IssueAccountingError) {
		t.Fatalf("health issues = %v", history.Latest.Issues.Codes())
	}
}

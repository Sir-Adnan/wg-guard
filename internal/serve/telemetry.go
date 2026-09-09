package serve

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/hoststats"
	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
)

const (
	defaultOnlineWindow    = 3 * time.Minute
	recentAccountingWindow = 5 * time.Minute
)

type telemetryHost interface {
	Snapshot(time.Time) hoststats.Snapshot
	InterfaceCounters([]string) hoststats.NetworkCounters
}

// nodeTelemetrySource performs one bounded host read and one aggregate SQL
// statement. It never invokes AWG subprocesses; owned link presence and byte
// counters come from /proc/net/dev.
type nodeTelemetrySource struct {
	db           *database.DB
	host         telemetryHost
	onlineWindow func(context.Context) time.Duration
	ready        func() bool
	accounting   func(time.Time) (failed, available bool)
	process      func() (heapBytes, goroutines uint64)
}

func (s *nodeTelemetrySource) Read(ctx context.Context, now time.Time) (telemetry.RawSample, error) {
	if s.db == nil {
		return telemetry.RawSample{}, fmt.Errorf("telemetry: database is required")
	}
	window := defaultOnlineWindow
	if s.onlineWindow != nil {
		if configured := s.onlineWindow(ctx); configured > 0 {
			window = configured
		}
	}
	names, onlineUsers, activePeers, err := readTelemetryActivity(ctx, s.db, now.Add(-window))
	if err != nil {
		return telemetry.RawSample{}, err
	}

	raw := telemetry.RawSample{
		At:                  now,
		OnlineUsers:         onlineUsers,
		ActivePeers:         activePeers,
		ActivityAvailable:   true,
		EnabledInterfaces:   uint64(len(names)),
		InterfacesAvailable: true,
	}
	if s.host != nil {
		host := s.host.Snapshot(now)
		raw.HostAvailable = host.OK
		raw.CPUPercent = host.CPUPercent
		raw.MemTotalBytes = host.MemTotal
		raw.MemAvailableBytes = host.MemAvail
		raw.DiskTotalBytes = host.DiskTotal
		raw.DiskFreeBytes = host.DiskFree
		raw.Load1 = host.Load1
		raw.Uptime = host.Uptime
		raw.ProcessRSSBytes = host.ProcessRSSBytes
		raw.HostNetwork = telemetryCounter(host.HostNetwork)
		vpn := s.host.InterfaceCounters(names)
		raw.VPNNetwork = telemetryCounter(vpn)
		raw.ObservedInterfaces = uint64(vpn.Interfaces)
	}
	process := s.process
	if process == nil {
		process = readProcessRuntime
	}
	raw.ProcessHeapBytes, raw.Goroutines = process()
	raw.ProcessMetricsAvailable = true
	if s.ready != nil {
		raw.ReadinessAvailable = true
		raw.Ready = s.ready()
	}
	if s.accounting != nil {
		raw.RecentAccountingError, raw.AccountingAvailable = s.accounting(now)
	}
	return raw, nil
}

func readTelemetryActivity(ctx context.Context, db *database.DB, cutoff time.Time) ([]string, uint64, uint64, error) {
	const separator = "\x1f"
	var joined string
	var onlineUsers, activePeers uint64
	err := db.QueryRowContext(ctx, `SELECT
		COALESCE((SELECT group_concat(name, char(31)) FROM
			(SELECT name FROM tunnel_interfaces WHERE enabled = 1 ORDER BY name)), ''),
		(SELECT COUNT(DISTINCT d.user_id)
		 FROM devices d JOIN users u ON u.id = d.user_id
		 WHERE u.deleted_at IS NULL AND u.enabled = 1 AND d.enabled = 1
		   AND d.last_handshake_at IS NOT NULL AND d.last_handshake_at >= ?),
		(SELECT COUNT(*)
		 FROM devices d JOIN users u ON u.id = d.user_id
		 WHERE u.deleted_at IS NULL AND u.enabled = 1 AND d.enabled = 1
		   AND d.last_handshake_at IS NOT NULL AND d.last_handshake_at >= ?)`,
		cutoff.UTC().Format(time.RFC3339Nano), cutoff.UTC().Format(time.RFC3339Nano)).
		Scan(&joined, &onlineUsers, &activePeers)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("telemetry: activity aggregate: %w", err)
	}
	if joined == "" {
		return nil, onlineUsers, activePeers, nil
	}
	names := strings.Split(joined, separator)
	if len(names) > 8 {
		return nil, 0, 0, fmt.Errorf("telemetry: enabled interface count exceeds limit")
	}
	return names, onlineUsers, activePeers, nil
}

func telemetryCounter(value hoststats.NetworkCounters) telemetry.Counter {
	return telemetry.Counter{
		Identity: value.Identity, RXBytes: value.RXBytes, TXBytes: value.TXBytes,
		Available: value.Available,
	}
}

func readProcessRuntime() (uint64, uint64) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return memory.HeapAlloc, uint64(runtime.NumGoroutine())
}

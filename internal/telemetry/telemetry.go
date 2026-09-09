package telemetry

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	DefaultCadence  = 10 * time.Second
	HistoryCapacity = 180
	defaultPoints   = 60
)

var errNoSource = errors.New("telemetry source is not configured")

// Metric is a floating-point measurement whose zero can be meaningful.
type Metric struct {
	Value     float64
	Available bool
}

// UintMetric is an exact integer measurement whose zero can be meaningful.
type UintMetric struct {
	Value     uint64
	Available bool
}

// Health is the aggregate operational state of one point.
type Health string

const (
	HealthHealthy     Health = "healthy"
	HealthDegraded    Health = "degraded"
	HealthUnavailable Health = "unavailable"
)

// Issues is a bounded bitset of stable, non-secret health reasons.
type Issues uint32

const (
	IssueSourceUnavailable Issues = 1 << iota
	IssueHostMetricsUnavailable
	IssueHostNetworkUnavailable
	IssueVPNMetricsUnavailable
	IssueAWGInterfaceMissing
	IssueReadinessFailed
	IssueAccountingError
	IssueSampleStale
)

// Has reports whether an issue is present.
func (i Issues) Has(issue Issues) bool { return i&issue != 0 }

// Codes returns stable API-safe issue codes in deterministic order.
func (i Issues) Codes() []string {
	known := [...]struct {
		issue Issues
		code  string
	}{
		{IssueSourceUnavailable, "source_unavailable"},
		{IssueHostMetricsUnavailable, "host_metrics_unavailable"},
		{IssueHostNetworkUnavailable, "host_network_unavailable"},
		{IssueVPNMetricsUnavailable, "vpn_metrics_unavailable"},
		{IssueAWGInterfaceMissing, "awg_interface_missing"},
		{IssueReadinessFailed, "readiness_failed"},
		{IssueAccountingError, "accounting_recent_error"},
		{IssueSampleStale, "sample_stale"},
	}
	result := make([]string, 0, len(known))
	for _, item := range known {
		if i.Has(item.issue) {
			result = append(result, item.code)
		}
	}
	return result
}

// Point is a value-only public telemetry point. It deliberately contains no
// interface names, subprocess errors, database errors or other topology.
type Point struct {
	At time.Time

	CPUPercent       Metric
	MemoryUsedBytes  UintMetric
	MemoryTotalBytes UintMetric
	MemoryPercent    Metric
	DiskUsedBytes    UintMetric
	DiskTotalBytes   UintMetric
	DiskPercent      Metric
	Load1            Metric
	UptimeSeconds    UintMetric

	ProcessRSSBytes  UintMetric
	ProcessHeapBytes UintMetric
	Goroutines       UintMetric

	HostRXBytes UintMetric
	HostTXBytes UintMetric
	VPNRXBytes  UintMetric
	VPNTXBytes  UintMetric

	HostRXRate Metric
	HostTXRate Metric
	VPNRXRate  Metric
	VPNTXRate  Metric

	OnlineUsers        UintMetric
	ActivePeers        UintMetric
	EnabledInterfaces  UintMetric
	ObservedInterfaces UintMetric

	Health Health
	Issues Issues
}

// History is an immutable caller-owned view of the ring.
type History struct {
	Cadence   time.Duration
	Latest    Point
	Points    []Point
	Available bool
}

// Sampler derives rates/health and owns the fixed-size history ring.
type Sampler struct {
	source  Source
	cadence time.Duration

	sampleMu sync.Mutex
	prev     RawSample
	hasPrev  bool

	mu     sync.RWMutex
	points [HistoryCapacity]Point
	next   int
	count  int
}

// New constructs a sampler without starting background work.
func New(source Source, cadence time.Duration) *Sampler {
	if cadence <= 0 {
		cadence = DefaultCadence
	}
	if source == nil {
		source = SourceFunc(func(context.Context, time.Time) (RawSample, error) {
			return RawSample{}, errNoSource
		})
	}
	return &Sampler{source: source, cadence: cadence}
}

// Sample reads and appends exactly one point. Calls are serialized, while
// Snapshot readers remain unblocked during source I/O.
func (s *Sampler) Sample(ctx context.Context, now time.Time) (Point, error) {
	s.sampleMu.Lock()
	defer s.sampleMu.Unlock()

	raw, err := s.source.Read(ctx, now)
	raw.At = now
	point := s.point(raw, err)
	if err != nil {
		s.hasPrev = false
	} else {
		s.prev, s.hasPrev = raw, true
	}

	s.mu.Lock()
	s.points[s.next] = point
	s.next = (s.next + 1) % HistoryCapacity
	if s.count < HistoryCapacity {
		s.count++
	}
	s.mu.Unlock()
	return point, err
}

func (s *Sampler) point(raw RawSample, sourceErr error) Point {
	p := Point{At: raw.At, Health: HealthHealthy}
	if sourceErr != nil {
		p.Health = HealthUnavailable
		p.Issues = IssueSourceUnavailable
		return p
	}
	if raw.CPUPercent != nil {
		p.CPUPercent = Metric{Value: *raw.CPUPercent, Available: true}
	}
	if raw.MemTotalBytes > 0 && raw.MemAvailableBytes <= raw.MemTotalBytes {
		used := raw.MemTotalBytes - raw.MemAvailableBytes
		p.MemoryUsedBytes = UintMetric{Value: used, Available: true}
		p.MemoryTotalBytes = UintMetric{Value: raw.MemTotalBytes, Available: true}
		p.MemoryPercent = Metric{Value: percent(used, raw.MemTotalBytes), Available: true}
	}
	if raw.DiskTotalBytes > 0 && raw.DiskFreeBytes <= raw.DiskTotalBytes {
		used := raw.DiskTotalBytes - raw.DiskFreeBytes
		p.DiskUsedBytes = UintMetric{Value: used, Available: true}
		p.DiskTotalBytes = UintMetric{Value: raw.DiskTotalBytes, Available: true}
		p.DiskPercent = Metric{Value: percent(used, raw.DiskTotalBytes), Available: true}
	}
	if raw.Load1 != nil {
		p.Load1 = Metric{Value: *raw.Load1, Available: true}
	}
	if raw.Uptime > 0 {
		p.UptimeSeconds = UintMetric{Value: uint64(raw.Uptime / time.Second), Available: true}
	}
	if raw.ProcessRSSBytes > 0 {
		p.ProcessRSSBytes = UintMetric{Value: raw.ProcessRSSBytes, Available: true}
	}
	if raw.ProcessMetricsAvailable {
		p.ProcessHeapBytes = UintMetric{Value: raw.ProcessHeapBytes, Available: true}
		p.Goroutines = UintMetric{Value: raw.Goroutines, Available: true}
	}
	copyCounterMetrics(&p, raw)
	if raw.ActivityAvailable {
		p.OnlineUsers = UintMetric{Value: raw.OnlineUsers, Available: true}
		p.ActivePeers = UintMetric{Value: raw.ActivePeers, Available: true}
	}
	if raw.InterfacesAvailable {
		p.EnabledInterfaces = UintMetric{Value: raw.EnabledInterfaces, Available: true}
		p.ObservedInterfaces = UintMetric{Value: raw.ObservedInterfaces, Available: true}
	}
	p.Issues = healthIssues(raw)
	if p.Issues != 0 {
		p.Health = HealthDegraded
	}
	if s.hasPrev {
		elapsed := raw.At.Sub(s.prev.At)
		minElapsed, maxElapsed := s.cadence/2, s.cadence*3
		if elapsed >= minElapsed && elapsed <= maxElapsed {
			p.HostRXRate, p.HostTXRate = counterRates(s.prev.HostNetwork, raw.HostNetwork, elapsed)
			p.VPNRXRate, p.VPNTXRate = counterRates(s.prev.VPNNetwork, raw.VPNNetwork, elapsed)
		}
	}
	return p
}

func copyCounterMetrics(p *Point, raw RawSample) {
	if raw.HostNetwork.Available {
		p.HostRXBytes = UintMetric{Value: raw.HostNetwork.RXBytes, Available: true}
		p.HostTXBytes = UintMetric{Value: raw.HostNetwork.TXBytes, Available: true}
	}
	if raw.VPNNetwork.Available {
		p.VPNRXBytes = UintMetric{Value: raw.VPNNetwork.RXBytes, Available: true}
		p.VPNTXBytes = UintMetric{Value: raw.VPNNetwork.TXBytes, Available: true}
	}
}

func healthIssues(raw RawSample) Issues {
	var issues Issues
	if !raw.HostAvailable {
		issues |= IssueHostMetricsUnavailable
	}
	if !raw.HostNetwork.Available {
		issues |= IssueHostNetworkUnavailable
	}
	if raw.InterfacesAvailable && raw.EnabledInterfaces > 0 && !raw.VPNNetwork.Available {
		issues |= IssueVPNMetricsUnavailable
	}
	if raw.InterfacesAvailable && raw.ObservedInterfaces < raw.EnabledInterfaces {
		issues |= IssueAWGInterfaceMissing
	}
	if raw.ReadinessAvailable && !raw.Ready {
		issues |= IssueReadinessFailed
	}
	if raw.AccountingAvailable && raw.RecentAccountingError {
		issues |= IssueAccountingError
	}
	return issues
}

func counterRates(previous, current Counter, elapsed time.Duration) (Metric, Metric) {
	if !previous.Available || !current.Available || previous.Identity == "" || previous.Identity != current.Identity ||
		current.RXBytes < previous.RXBytes || current.TXBytes < previous.TXBytes {
		return Metric{}, Metric{}
	}
	seconds := elapsed.Seconds()
	if seconds <= 0 {
		return Metric{}, Metric{}
	}
	return Metric{Value: float64(current.RXBytes-previous.RXBytes) / seconds, Available: true},
		Metric{Value: float64(current.TXBytes-previous.TXBytes) / seconds, Available: true}
}

func percent(used, total uint64) float64 {
	return 100 * float64(used) / float64(total)
}

// Snapshot returns the newest limit points in chronological order. A stale
// latest view is degraded without mutating the stored historical point.
func (s *Sampler) Snapshot(now time.Time, limit int) History {
	if limit <= 0 {
		limit = defaultPoints
	}
	if limit > HistoryCapacity {
		limit = HistoryCapacity
	}
	s.mu.RLock()
	count := s.count
	if count > limit {
		count = limit
	}
	result := History{Cadence: s.cadence, Points: make([]Point, count), Available: count > 0}
	start := (s.next - count + HistoryCapacity) % HistoryCapacity
	for i := 0; i < count; i++ {
		result.Points[i] = s.points[(start+i)%HistoryCapacity]
	}
	s.mu.RUnlock()
	if count == 0 {
		result.Latest.Health = HealthUnavailable
		return result
	}
	result.Latest = result.Points[count-1]
	if now.Sub(result.Latest.At) > 2*s.cadence && result.Latest.Health != HealthUnavailable {
		result.Latest.Health = HealthDegraded
		result.Latest.Issues |= IssueSampleStale
	}
	return result
}

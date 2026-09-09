// Package telemetry owns the bounded live operational snapshot shared by
// dashboard, API and metrics readers. It starts no goroutine; the node's
// central scheduler calls Sampler.Sample.
package telemetry

import (
	"context"
	"time"
)

// Source collects one raw point. Implementations may return partial metrics;
// an error means the sample as a whole is unavailable and its details must be
// sent only to protected logs.
type Source interface {
	Read(context.Context, time.Time) (RawSample, error)
}

// SourceFunc adapts a function to Source.
type SourceFunc func(context.Context, time.Time) (RawSample, error)

// Read implements Source.
func (f SourceFunc) Read(ctx context.Context, now time.Time) (RawSample, error) {
	return f(ctx, now)
}

// Counter carries internal continuity identity and cumulative byte counters.
// Identity is never copied into Point, so topology cannot leak to readers.
type Counter struct {
	Identity  string
	RXBytes   uint64
	TXBytes   uint64
	Available bool
}

// RawSample is the collector-to-sampler contract. Availability flags make a
// real zero distinct from a source that could not be read.
type RawSample struct {
	At time.Time

	HostAvailable     bool
	CPUPercent        *float64
	MemTotalBytes     uint64
	MemAvailableBytes uint64
	DiskTotalBytes    uint64
	DiskFreeBytes     uint64
	Load1             *float64
	Uptime            time.Duration

	ProcessRSSBytes         uint64
	ProcessHeapBytes        uint64
	ProcessMetricsAvailable bool
	Goroutines              uint64

	HostNetwork Counter
	VPNNetwork  Counter

	OnlineUsers       uint64
	ActivePeers       uint64
	ActivityAvailable bool

	EnabledInterfaces   uint64
	ObservedInterfaces  uint64
	InterfacesAvailable bool

	Ready              bool
	ReadinessAvailable bool

	RecentAccountingError bool
	AccountingAvailable   bool
}

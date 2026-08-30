// Package hoststats reads host-level metrics (CPU, memory, disk, load,
// uptime) for the dashboard. Snapshots are taken on demand at render time
// (at most once per auto-refresh tick per admin) — no background polling,
// no goroutines, no idle cost. On platforms without /proc (e.g. Windows
// development hosts) reads return OK=false and the dashboard hides the
// host card instead of showing empty numbers.
package hoststats

import (
	"sync"
	"time"
)

// Snapshot is one point-in-time reading. A nil pointer or zero value means
// "unavailable on this platform"; callers must not render those as 0.
type Snapshot struct {
	At time.Time

	CPUPercent *float64 // 0–100, measured against the previous sample
	MemTotal   uint64   // bytes, 0 = unavailable
	MemAvail   uint64   // bytes, 0 = unavailable
	DiskTotal  uint64   // bytes, 0 = unavailable
	DiskFree   uint64   // bytes, 0 = unavailable
	Load1      *float64
	Load5      *float64
	Load15     *float64
	Uptime     time.Duration // 0 = unavailable

	OK bool // host metrics readable at all (Linux)
}

// MemUsed is the memory in use (0 when memory metrics are unavailable).
func (s Snapshot) MemUsed() uint64 {
	if s.MemTotal == 0 || s.MemAvail > s.MemTotal {
		return 0
	}
	return s.MemTotal - s.MemAvail
}

// Reader produces snapshots. It is safe for concurrent use; the only state
// is the previous CPU counters sample used to compute utilization.
type Reader struct {
	root     string // /proc override for tests ("" = default)
	diskPath string // filesystem to measure (the node data directory)

	mu      sync.Mutex
	prev    cpuTimes
	hasPrev bool
	prevAt  time.Time
	lastCPU *float64 // last computed utilization, reused if samples are too close
}

// New returns a reader measuring the filesystem holding diskPath (typically
// the configured data directory, so the disk metric reflects the database).
func New(diskPath string) *Reader {
	return &Reader{root: procRoot, diskPath: diskPath}
}

// withRoot overrides the /proc source; used by tests to feed fixtures.
func (r *Reader) withRoot(root string) *Reader {
	r.root = root
	return r
}

// Snapshot reads current host metrics. The CPU percentage needs a prior
// sample: the first call returns nil there, later calls report the delta
// since the previous Snapshot. Calls less than 500 ms apart reuse the last
// computed value instead of dividing tiny counter deltas (the dashboard can
// be polled concurrently by several admins).
func (r *Reader) Snapshot(now time.Time) Snapshot {
	s, ct := r.sample(now)
	s.At = now
	s.CPUPercent = r.cpuDelta(now, ct)
	return s
}

func (r *Reader) cpuDelta(now time.Time, ct cpuTimes) *float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hasPrev && now.Sub(r.prevAt) >= 500*time.Millisecond {
		dt := ct.total - r.prev.total
		di := ct.idle - r.prev.idle
		if dt > 0 {
			p := 100 * (1 - float64(di)/float64(dt))
			if p < 0 {
				p = 0
			}
			if p > 100 {
				p = 100
			}
			r.lastCPU = &p
		}
		r.prev, r.prevAt = ct, now
	} else if !r.hasPrev {
		r.prev, r.hasPrev, r.prevAt = ct, true, now
	}
	return r.lastCPU
}

// cpuTimes is the raw /proc/stat counters (idle and total jiffies).
type cpuTimes struct {
	idle  uint64
	total uint64
}

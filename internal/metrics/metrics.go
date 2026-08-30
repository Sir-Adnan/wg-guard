// Package metrics serves the lightweight observability surface: public
// liveness/readiness probes and an optional hand-written /metrics endpoint
// (Prometheus text format, no client library — SPEC §40: "Do not embed a
// heavy metrics stack"). Counters are atomics; the endpoint renders on
// scrape, so idle cost is zero.
package metrics

import (
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Collector holds the process-level counters.
type Collector struct {
	startedAt time.Time

	ready func() bool // nil = always ready

	classes [3]atomic.Int64 // index by statusClass: 2xx, 4xx, 5xx

	mu               sync.Mutex
	lastCycleSet     bool
	lastCycleSeconds float64
	lastCycleAt      time.Time
	lastCycleDeltas  int
}

// New returns a collector anchored at now.
func New() *Collector {
	return &Collector{startedAt: time.Now()}
}

// SetReady installs the readiness probe (DB reachable, bring-up done).
func (c *Collector) SetReady(fn func() bool) { c.ready = fn }

// SetLastCycle records the most recent accounting cycle result.
func (c *Collector) SetLastCycle(d time.Duration, at time.Time, deltas int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastCycleSet = true
	c.lastCycleSeconds = d.Seconds()
	c.lastCycleAt = at
	c.lastCycleDeltas = deltas
}

// IncRequest counts one finished request by status class.
func (c *Collector) IncRequest(status int) {
	switch {
	case status >= 500:
		c.classes[2].Add(1)
	case status >= 400:
		c.classes[1].Add(1)
	default:
		c.classes[0].Add(1)
	}
}

// Healthz is the public liveness probe: the process is up. It carries no
// node information.
func (c *Collector) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

// Readyz is the public readiness probe: 200 when the node can serve, 503
// otherwise (bring-up incomplete or database unreachable).
func (c *Collector) Readyz(w http.ResponseWriter, _ *http.Request) {
	if c.ready != nil && !c.ready() {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"unavailable"}` + "\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ready"}` + "\n"))
}

// Handler serves /metrics in Prometheus text format. Enable it explicitly in
// the boot config (it is off by default: it exposes operational topology).
func (c *Collector) Handler(w http.ResponseWriter, _ *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	c.mu.Lock()
	var lastCycle string
	if c.lastCycleSet {
		lastCycle = fmt.Sprintf(
			"wgguard_accounting_last_cycle_duration_seconds %v\n"+
				"wgguard_accounting_last_cycle_deltas %d\n"+
				"wgguard_accounting_last_cycle_timestamp_seconds %d\n",
			c.lastCycleSeconds, c.lastCycleDeltas, c.lastCycleAt.Unix())
	}
	c.mu.Unlock()

	body := fmt.Sprintf(
		"# HELP wgguard_uptime_seconds Process uptime in seconds.\n"+
			"# TYPE wgguard_uptime_seconds gauge\n"+
			"wgguard_uptime_seconds %d\n"+
			"# HELP wgguard_http_requests_total HTTP requests by status class.\n"+
			"# TYPE wgguard_http_requests_total counter\n"+
			"wgguard_http_requests_total{class=\"2xx\"} %d\n"+
			"wgguard_http_requests_total{class=\"4xx\"} %d\n"+
			"wgguard_http_requests_total{class=\"5xx\"} %d\n"+
			"%s"+
			"# HELP wgguard_goroutines Current goroutine count.\n"+
			"# TYPE wgguard_goroutines gauge\n"+
			"wgguard_goroutines %d\n"+
			"# HELP wgguard_heap_alloc_bytes Heap bytes in use.\n"+
			"# TYPE wgguard_heap_alloc_bytes gauge\n"+
			"wgguard_heap_alloc_bytes %d\n",
		int(time.Since(c.startedAt).Seconds()),
		c.classes[0].Load(), c.classes[1].Load(), c.classes[2].Load(),
		lastCycle,
		runtime.NumGoroutine(),
		ms.HeapAlloc,
	)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

// Uptime returns the process uptime.
func (c *Collector) Uptime() time.Duration { return time.Since(c.startedAt) }

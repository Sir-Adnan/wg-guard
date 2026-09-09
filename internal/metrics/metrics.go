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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
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
	accountingSeen   bool
	accountingFailed bool
	accountingAt     time.Time
	telemetry        *telemetry.Sampler
	telemetryNow     func() time.Time
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
	c.accountingSeen = true
	c.accountingFailed = false
	c.accountingAt = at
}

// SetAccountingError records a failed accounting pass without exposing its
// error text. A later successful SetLastCycle clears the state.
func (c *Collector) SetAccountingError(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accountingSeen = true
	c.accountingFailed = true
	c.accountingAt = at
}

// AccountingStatus reports whether the most recently observed accounting
// state is a failure inside the bounded recent window.
func (c *Collector) AccountingStatus(now time.Time, recent time.Duration) (failed, available bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.accountingSeen {
		return false, false
	}
	return c.accountingFailed && recent > 0 && now.Sub(c.accountingAt) <= recent, true
}

// SetTelemetry connects the shared sampler to the optional Prometheus
// renderer. The function is set during node composition before serving.
func (c *Collector) SetTelemetry(sampler *telemetry.Sampler, now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	c.mu.Lock()
	c.telemetry, c.telemetryNow = sampler, now
	c.mu.Unlock()
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
	sampler, telemetryNow := c.telemetry, c.telemetryNow
	c.mu.Unlock()
	telemetryBody := renderTelemetry(sampler, telemetryNow)

	body := fmt.Sprintf(
		"# HELP wgguard_uptime_seconds Process uptime in seconds.\n"+
			"# TYPE wgguard_uptime_seconds gauge\n"+
			"wgguard_uptime_seconds %d\n"+
			"# HELP wgguard_http_requests_total HTTP requests by status class.\n"+
			"# TYPE wgguard_http_requests_total counter\n"+
			"wgguard_http_requests_total{class=\"2xx\"} %d\n"+
			"wgguard_http_requests_total{class=\"4xx\"} %d\n"+
			"wgguard_http_requests_total{class=\"5xx\"} %d\n"+
			"%s%s"+
			"# HELP wgguard_goroutines Current goroutine count.\n"+
			"# TYPE wgguard_goroutines gauge\n"+
			"wgguard_goroutines %d\n"+
			"# HELP wgguard_heap_alloc_bytes Heap bytes in use.\n"+
			"# TYPE wgguard_heap_alloc_bytes gauge\n"+
			"wgguard_heap_alloc_bytes %d\n",
		int(time.Since(c.startedAt).Seconds()),
		c.classes[0].Load(), c.classes[1].Load(), c.classes[2].Load(),
		lastCycle,
		telemetryBody,
		runtime.NumGoroutine(),
		ms.HeapAlloc,
	)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func renderTelemetry(sampler *telemetry.Sampler, now func() time.Time) string {
	if sampler == nil || now == nil {
		return ""
	}
	history := sampler.Snapshot(now(), 1)
	if !history.Available {
		return ""
	}
	p := history.Latest
	var b strings.Builder
	fmt.Fprintf(&b,
		"# HELP wgguard_telemetry_cadence_seconds Configured live telemetry cadence.\n"+
			"# TYPE wgguard_telemetry_cadence_seconds gauge\n"+
			"wgguard_telemetry_cadence_seconds %g\n"+
			"# HELP wgguard_telemetry_sample_timestamp_seconds Latest sample timestamp.\n"+
			"# TYPE wgguard_telemetry_sample_timestamp_seconds gauge\n"+
			"wgguard_telemetry_sample_timestamp_seconds %d\n"+
			"# HELP wgguard_node_health Current aggregate node health state.\n"+
			"# TYPE wgguard_node_health gauge\n",
		history.Cadence.Seconds(), p.At.Unix())
	for _, state := range []telemetry.Health{telemetry.HealthHealthy, telemetry.HealthDegraded, telemetry.HealthUnavailable} {
		value := 0
		if p.Health == state {
			value = 1
		}
		fmt.Fprintf(&b, "wgguard_node_health{state=%q} %d\n", state, value)
	}
	writeRate := func(name, help string, metric telemetry.Metric) {
		if !metric.Available {
			return
		}
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %g\n", name, help, name, name, metric.Value)
	}
	writeRate("wgguard_host_network_receive_bytes_per_second", "Host default-route receive rate.", p.HostRXRate)
	writeRate("wgguard_host_network_transmit_bytes_per_second", "Host default-route transmit rate.", p.HostTXRate)
	writeRate("wgguard_vpn_receive_bytes_per_second", "Owned VPN interfaces receive rate.", p.VPNRXRate)
	writeRate("wgguard_vpn_transmit_bytes_per_second", "Owned VPN interfaces transmit rate.", p.VPNTXRate)
	return b.String()
}

// Uptime returns the process uptime.
func (c *Collector) Uptime() time.Duration { return time.Since(c.startedAt) }

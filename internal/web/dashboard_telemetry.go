package web

import (
	"fmt"
	"html/template"
	"math"
	"net/http"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
)

// telemetryView is fully formatted presentation data. Empty values represent
// unavailable metrics and never become a misleading zero in the template.
type telemetryView struct {
	Has         bool
	HealthCode  string
	HealthLabel string
	HealthClass string
	Issues      []string
	Updated     string
	Window      string

	CPUValue    string
	CPUChart    template.HTML
	MemoryValue string
	MemoryMeta  string
	MemoryClass string
	MemoryChart template.HTML

	VPNRXRate string
	VPNTXRate string
	VPNChart  template.HTML

	OnlineUsers   string
	ActivePeers   string
	ActivityChart template.HTML

	DiskUsed    string
	DiskFree    string
	DiskClass   string
	Load        string
	Uptime      string
	ProcessRSS  string
	ProcessHeap string
	HostRXRate  string
	HostTXRate  string
	HostChart   template.HTML
	DiskValue   string
	Charts      []liveChartCard
}

type liveChartCard struct {
	TitleKey, Value, Second, Meta string
	SVG                           template.HTML
	Percent                       bool
}

// The detailed snapshot is generated only on demand and stays outside the
// polling region so table focus and reading position survive live updates.
type sampleDataRow struct {
	At     string
	Values []string
}

func (s *Server) loadSampleData(r *http.Request) []sampleDataRow {
	if s.Telemetry == nil {
		return nil
	}
	history := s.Telemetry.Snapshot(time.Now().UTC(), telemetry.HistoryCapacity)
	loc := s.localeFor(r)
	rows := make([]sampleDataRow, 0, len(history.Points))
	percent := func(m telemetry.Metric) string {
		if !validSparkMetric(m) {
			return ""
		}
		return fmt.Sprintf("%.1f%%", m.Value)
	}
	for _, p := range history.Points {
		values := []string{percent(p.CPUPercent), percent(p.MemoryPercent), percent(p.DiskPercent), rateText(loc, p.HostRXRate), rateText(loc, p.HostTXRate), rateText(loc, p.VPNRXRate), rateText(loc, p.VPNTXRate), countText(p.OnlineUsers), countText(p.ActivePeers)}
		for i := range values {
			if values[i] == "" {
				values[i] = i18n.T(loc, "forms.unavailable")
			}
		}
		rows = append(rows, sampleDataRow{At: i18n.FormatDate(loc, p.At, nil) + " " + p.At.UTC().Format("15:04:05") + " UTC", Values: values})
	}
	return rows
}

func newTelemetryView(loc i18n.Locale, history telemetry.History) telemetryView {
	view := telemetryView{
		HealthCode: "unavailable", HealthLabel: i18n.T(loc, "dash.health_unavailable"),
		HealthClass: "badge--danger",
	}
	if !history.Available {
		return view
	}
	point := history.Latest
	view.Has = true
	view.HealthCode = string(point.Health)
	switch point.Health {
	case telemetry.HealthHealthy:
		view.HealthLabel, view.HealthClass = i18n.T(loc, "dash.health_healthy"), "badge--ok"
	case telemetry.HealthDegraded:
		view.HealthLabel, view.HealthClass = i18n.T(loc, "dash.health_degraded"), "badge--warn"
	default:
		view.HealthLabel, view.HealthClass = i18n.T(loc, "dash.health_unavailable"), "badge--danger"
	}
	for _, code := range point.Issues.Codes() {
		view.Issues = append(view.Issues, i18n.T(loc, "dash.issue."+code))
	}
	if !point.At.IsZero() {
		view.Updated = i18n.T(loc, "dash.last_sample", i18n.FormatDateTime(loc, point.At, nil))
	}
	if point.CPUPercent.Available {
		view.CPUValue = fmt.Sprintf("%.0f%%", point.CPUPercent.Value)
	}
	if point.MemoryPercent.Available {
		view.MemoryValue = fmt.Sprintf("%.0f%%", point.MemoryPercent.Value)
	}
	if used, ok := bytesText(loc, point.MemoryUsedBytes); ok {
		if total, totalOK := bytesText(loc, point.MemoryTotalBytes); totalOK {
			view.MemoryMeta = i18n.T(loc, "dash.mem_of", used, total)
			view.MemoryClass = meterClass(int64(point.MemoryUsedBytes.Value), int64(point.MemoryTotalBytes.Value))
		}
	}
	view.VPNRXRate = rateText(loc, point.VPNRXRate)
	view.VPNTXRate = rateText(loc, point.VPNTXRate)
	view.HostRXRate = rateText(loc, point.HostRXRate)
	view.HostTXRate = rateText(loc, point.HostTXRate)
	view.OnlineUsers = countText(point.OnlineUsers)
	view.ActivePeers = countText(point.ActivePeers)
	if used, ok := bytesText(loc, point.DiskUsedBytes); ok {
		view.DiskUsed = used
		if _, totalOK := bytesText(loc, point.DiskTotalBytes); totalOK {
			free := point.DiskTotalBytes.Value - point.DiskUsedBytes.Value
			view.DiskFree = i18n.FormatBytes(loc, int64(free))
			view.DiskClass = meterClass(int64(point.DiskUsedBytes.Value), int64(point.DiskTotalBytes.Value))
		}
	}
	if point.Load1.Available {
		view.Load = fmt.Sprintf("%.2f", point.Load1.Value)
	}
	if point.DiskPercent.Available {
		view.DiskValue = fmt.Sprintf("%.0f%%", point.DiskPercent.Value)
	}
	if point.UptimeSeconds.Available && point.UptimeSeconds.Value <= math.MaxInt64 {
		view.Uptime = i18n.FormatDuration(loc, int64(point.UptimeSeconds.Value))
	}
	view.ProcessRSS, _ = bytesText(loc, point.ProcessRSSBytes)
	view.ProcessHeap, _ = bytesText(loc, point.ProcessHeapBytes)

	cpu, memory := make([]telemetry.Metric, 0, len(history.Points)), make([]telemetry.Metric, 0, len(history.Points))
	vpnRX, vpnTX := make([]telemetry.Metric, 0, len(history.Points)), make([]telemetry.Metric, 0, len(history.Points))
	online, peers := make([]telemetry.Metric, 0, len(history.Points)), make([]telemetry.Metric, 0, len(history.Points))
	hostRX, hostTX := make([]telemetry.Metric, 0, len(history.Points)), make([]telemetry.Metric, 0, len(history.Points))
	times := make([]time.Time, 0, len(history.Points))
	for _, item := range history.Points {
		times = append(times, item.At)
		hostRX, hostTX = append(hostRX, item.HostRXRate), append(hostTX, item.HostTXRate)
		cpu = append(cpu, item.CPUPercent)
		memory = append(memory, item.MemoryPercent)
		vpnRX, vpnTX = append(vpnRX, item.VPNRXRate), append(vpnTX, item.VPNTXRate)
		online, peers = append(online, metricFromUint(item.OnlineUsers)), append(peers, metricFromUint(item.ActivePeers))
	}
	if len(times) > 0 {
		first := 0
		if len(times) > telemetry.HistoryCapacity {
			first = len(times) - telemetry.HistoryCapacity
		}
		view.Window = times[first].UTC().Format("15:04:05") + " — " + times[len(times)-1].UTC().Format("15:04:05") + " UTC"
	}
	view.CPUChart = timedSparklineSVG([]sparkSeries{{Class: "spark-primary", Label: i18n.T(loc, "dash.cpu"), Values: cpu, Display: percentMetricText(cpu)}}, i18n.T(loc, "dash.cpu_history"), 100, times, history.Cadence)
	view.MemoryChart = timedSparklineSVG([]sparkSeries{{Class: "spark-primary", Label: i18n.T(loc, "dash.memory"), Values: memory, Display: percentMetricText(memory)}}, i18n.T(loc, "dash.memory_history"), 100, times, history.Cadence)
	view.VPNChart = timedSparklineSVG([]sparkSeries{
		{Class: "spark-primary", Label: i18n.T(loc, "dash.rx"), Values: vpnRX, Display: rateMetricText(loc, vpnRX)},
		{Class: "spark-secondary", Label: i18n.T(loc, "dash.tx"), Values: vpnTX, Display: rateMetricText(loc, vpnTX)},
	}, i18n.T(loc, "dash.vpn_history"), 0, times, history.Cadence)
	view.HostChart = timedSparklineSVG([]sparkSeries{
		{Class: "spark-primary", Label: i18n.T(loc, "dash.rx"), Values: hostRX, Display: rateMetricText(loc, hostRX)},
		{Class: "spark-secondary", Label: i18n.T(loc, "dash.tx"), Values: hostTX, Display: rateMetricText(loc, hostTX)},
	}, i18n.T(loc, "dash.host_network"), 0, times, history.Cadence)
	view.ActivityChart = timedSparklineSVG([]sparkSeries{
		{Class: "spark-primary", Label: i18n.T(loc, "dash.users_online"), Values: online, Display: countMetricText(online)},
		{Class: "spark-secondary", Label: i18n.T(loc, "dash.active_peers"), Values: peers, Display: countMetricText(peers)},
	}, i18n.T(loc, "dash.activity_history"), 0, times, history.Cadence)
	view.Charts = []liveChartCard{
		{TitleKey: "dash.cpu", Value: view.CPUValue, SVG: view.CPUChart, Percent: true},
		{TitleKey: "dash.memory", Value: view.MemoryValue, Meta: view.MemoryMeta, SVG: view.MemoryChart, Percent: true},
		{TitleKey: "dash.host_network", Value: view.HostRXRate, Second: view.HostTXRate, SVG: view.HostChart},
		{TitleKey: "dash.vpn_live", Value: view.VPNRXRate, Second: view.VPNTXRate, SVG: view.VPNChart},
		{TitleKey: "dash.activity", Value: view.OnlineUsers, Second: view.ActivePeers, SVG: view.ActivityChart},
	}
	return view
}

func metricFromUint(value telemetry.UintMetric) telemetry.Metric {
	if !value.Available {
		return telemetry.Metric{}
	}
	return telemetry.Metric{Value: float64(value.Value), Available: true}
}

func bytesText(loc i18n.Locale, value telemetry.UintMetric) (string, bool) {
	if !value.Available || value.Value > math.MaxInt64 {
		return "", false
	}
	return i18n.FormatBytes(loc, int64(value.Value)), true
}

func rateText(loc i18n.Locale, value telemetry.Metric) string {
	if !value.Available || value.Value < 0 || value.Value > math.MaxInt64 || math.IsNaN(value.Value) || math.IsInf(value.Value, 0) {
		return ""
	}
	return i18n.T(loc, "dash.rate", i18n.FormatBytes(loc, int64(value.Value)))
}

func countText(value telemetry.UintMetric) string {
	if !value.Available || value.Value > math.MaxInt64 {
		return ""
	}
	return i18n.FormatInt(int64(value.Value))
}

func percentMetricText(values []telemetry.Metric) []string {
	out := make([]string, len(values))
	for index, value := range values {
		if validSparkMetric(value) {
			out[index] = fmt.Sprintf("%.1f%%", value.Value)
		}
	}
	return out
}

func rateMetricText(loc i18n.Locale, values []telemetry.Metric) []string {
	out := make([]string, len(values))
	for index, value := range values {
		out[index] = rateText(loc, value)
	}
	return out
}

func countMetricText(values []telemetry.Metric) []string {
	out := make([]string, len(values))
	for index, value := range values {
		if validSparkMetric(value) && value.Value <= math.MaxInt64 {
			out[index] = i18n.FormatInt(int64(value.Value))
		}
	}
	return out
}

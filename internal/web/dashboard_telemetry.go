package web

import (
	"fmt"
	"html/template"
	"math"

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
	if point.UptimeSeconds.Available && point.UptimeSeconds.Value <= math.MaxInt64 {
		view.Uptime = i18n.FormatDuration(loc, int64(point.UptimeSeconds.Value))
	}
	view.ProcessRSS, _ = bytesText(loc, point.ProcessRSSBytes)
	view.ProcessHeap, _ = bytesText(loc, point.ProcessHeapBytes)

	cpu, memory := make([]telemetry.Metric, 0, len(history.Points)), make([]telemetry.Metric, 0, len(history.Points))
	vpnRX, vpnTX := make([]telemetry.Metric, 0, len(history.Points)), make([]telemetry.Metric, 0, len(history.Points))
	online, peers := make([]telemetry.Metric, 0, len(history.Points)), make([]telemetry.Metric, 0, len(history.Points))
	for _, item := range history.Points {
		cpu = append(cpu, item.CPUPercent)
		memory = append(memory, item.MemoryPercent)
		vpnRX, vpnTX = append(vpnRX, item.VPNRXRate), append(vpnTX, item.VPNTXRate)
		online, peers = append(online, metricFromUint(item.OnlineUsers)), append(peers, metricFromUint(item.ActivePeers))
	}
	view.CPUChart = sparklineSVG([]sparkSeries{{Class: "spark-primary", Values: cpu}}, i18n.T(loc, "dash.cpu_history"), 100)
	view.MemoryChart = sparklineSVG([]sparkSeries{{Class: "spark-primary", Values: memory}}, i18n.T(loc, "dash.memory_history"), 100)
	view.VPNChart = sparklineSVG([]sparkSeries{
		{Class: "spark-primary", Values: vpnRX},
		{Class: "spark-secondary", Values: vpnTX},
	}, i18n.T(loc, "dash.vpn_history"), 0)
	view.ActivityChart = sparklineSVG([]sparkSeries{
		{Class: "spark-primary", Values: online},
		{Class: "spark-secondary", Values: peers},
	}, i18n.T(loc, "dash.activity_history"), 0)
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

package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/hoststats"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
)

// dashLiveData is the auto-refreshed region: user counters + host metrics.
type dashLiveData struct {
	Total, Active, Waiting, Online, Expired, Exceeded, Expiring int64
	TrafficTotal                                                int64
	OnlineWindow                                                int64 // seconds, tooltip meta
	HostView                                                    hostView
}

// dashChartData is the traffic chart card (refreshed only on range change).
type dashChartData struct {
	Range    string // "24h" | "7d" | "30d"
	HasChart bool
	SVG      template.HTML
}

// hostView carries pre-formatted host metrics for the template (html/
// template cannot dereference the raw snapshot's pointers, and formatting
// belongs in Go anyway). An empty field means "unavailable".
type hostView struct {
	Has       bool
	CPUPct    string
	MemUsed   string
	MemTotal  string
	MemClass  string // meter width/tone class
	DiskUsed  string
	DiskFree  string
	DiskClass string
	Load      string
	Uptime    string
}

type dashData struct {
	Live         dashLiveData
	Chart        dashChartData
	NodeID       string
	ToolsVersion string
	Endpoint     string
}

// chartRanges are the dashboard time ranges: bucket count, rollup
// granularity and the localized range label (rollup retention: hourly 30 d,
// daily 1 y).
var chartRanges = map[string]struct {
	n     int
	gran  string
	label string
}{
	"24h": {24, "hourly", "dash.range_24h"},
	"7d":  {7, "daily", "dash.range_7d"},
	"30d": {30, "daily", "dash.range_30d"},
}

// handleDashboard renders the operational overview page. One aggregate query
// per table — users/devices are indexed for status and handshake lookups; at
// target scale (thousands of rows) these are sub-ms.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	d := dashData{
		NodeID:       s.NodeID,
		ToolsVersion: s.ToolsVersion,
	}
	d.Live = s.loadLive(r)
	d.Endpoint, _ = s.Settings.GetString(r.Context(), "node.endpoint")
	d.Chart = s.loadChart(r, chartRangeOf(r))

	if err := s.render(w, r, "dashboard", "app", d); err != nil {
		s.logError(r, "dashboard render", err)
	}
}

// handleDashboardLive is the 30 s auto-refresh fragment (htmx swap target).
func (s *Server) handleDashboardLive(w http.ResponseWriter, r *http.Request) {
	if err := s.partial(w, r, "dashboard", "dash_live", dashData{Live: s.loadLive(r)}); err != nil {
		s.logError(r, "dashboard live render", err)
	}
}

// handleDashboardChart swaps the chart card on range change.
func (s *Server) handleDashboardChart(w http.ResponseWriter, r *http.Request) {
	d := dashData{Chart: s.loadChart(r, chartRangeOf(r))}
	if err := s.partial(w, r, "dashboard", "dash_chart", d); err != nil {
		s.logError(r, "dashboard chart render", err)
	}
}

// chartRangeOf resolves the ?range= param (default 24h).
func chartRangeOf(r *http.Request) string {
	switch r.URL.Query().Get("range") {
	case "7d":
		return "7d"
	case "30d":
		return "30d"
	default:
		return "24h"
	}
}

// loadLive gathers the user counters, total traffic and host metrics.
func (s *Server) loadLive(r *http.Request) dashLiveData {
	ctx := r.Context()
	d := dashLiveData{OnlineWindow: 180}
	if v, err := s.Settings.GetInt(ctx, "accounting.online_window_seconds"); err == nil && v > 0 {
		d.OnlineWindow = int64(v)
	}

	cutoff := time.Now().UTC().Add(-time.Duration(d.OnlineWindow) * time.Second).
		Format(time.RFC3339Nano)
	soon := time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339Nano)

	err := s.DB.QueryRowContext(ctx, `SELECT
		COUNT(*),
		COALESCE(SUM(status = 'active'), 0),
		COALESCE(SUM(status = 'waiting_first_connection'), 0),
		COALESCE(SUM(status = 'expired'), 0),
		COALESCE(SUM(status = 'traffic_exceeded'), 0),
		COALESCE(SUM(status = 'active' AND expires_at IS NOT NULL AND expires_at < ?), 0),
		COALESCE(SUM(traffic_used_rx + traffic_used_tx), 0)
		FROM users WHERE deleted_at IS NULL`, soon).
		Scan(&d.Total, &d.Active, &d.Waiting, &d.Expired, &d.Exceeded, &d.Expiring, &d.TrafficTotal)
	if err != nil {
		s.logError(r, "dashboard counters", err)
	}

	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM devices d JOIN users u ON u.id = d.user_id
		WHERE u.deleted_at IS NULL AND d.enabled = 1
		  AND d.last_handshake_at IS NOT NULL AND d.last_handshake_at >= ?`, cutoff).
		Scan(&d.Online)

	// Host metrics: read on demand (the Reader keeps one CPU sample for the
	// utilization delta; no background polling anywhere).
	if s.Host != nil {
		d.HostView = newHostView(s.localeFor(r), s.Host.Snapshot(time.Now()))
	}
	return d
}

// newHostView formats a snapshot for the template; empty fields mean the
// metric is unavailable on this host.
func newHostView(loc i18n.Locale, h hoststats.Snapshot) hostView {
	v := hostView{}
	if h.CPUPercent != nil {
		v.CPUPct = fmt.Sprintf("%.0f%%", *h.CPUPercent)
	}
	if h.MemTotal > 0 {
		used := h.MemUsed()
		v.MemUsed = i18n.FormatBytes(loc, int64(used))
		v.MemTotal = i18n.FormatBytes(loc, int64(h.MemTotal))
		v.MemClass = meterClass(int64(used), int64(h.MemTotal))
	}
	if h.DiskTotal > 0 {
		used := int64(h.DiskTotal) - int64(h.DiskFree)
		v.DiskUsed = i18n.FormatBytes(loc, used)
		v.DiskFree = i18n.FormatBytes(loc, int64(h.DiskFree))
		v.DiskClass = meterClass(used, int64(h.DiskTotal))
	}
	if h.Load1 != nil && h.Load5 != nil && h.Load15 != nil {
		v.Load = fmt.Sprintf("%.2f · %.2f · %.2f", *h.Load1, *h.Load5, *h.Load15)
	}
	if h.Uptime > 0 {
		v.Uptime = i18n.FormatDuration(loc, int64(h.Uptime.Seconds()))
	}
	v.Has = v.CPUPct != "" || v.MemTotal != "" || v.DiskUsed != "" ||
		v.Load != "" || v.Uptime != ""
	return v
}

// meterClass is View.MeterClass without the locale-bound receiver (the host
// card is re-rendered from a handler-side view).
func meterClass(used, total int64) string {
	if total <= 0 {
		return "w0"
	}
	pct := float64(used) / float64(total) * 100
	step := int(pct/5+0.5) * 5
	if step > 100 {
		step = 100
	}
	tone := ""
	switch {
	case pct >= 100:
		tone = " is-danger"
	case pct >= 85:
		tone = " is-warn"
	}
	return "w" + strconv.Itoa(step) + tone
}

// loadChart builds the traffic chart from rollups. Missing buckets are
// zero-filled so the axis is an honest timeline. Errors leave the chart
// empty (the card keeps its empty state) — the dashboard never fails
// because its chart failed.
func (s *Server) loadChart(r *http.Request, rangeKey string) dashChartData {
	out := dashChartData{Range: rangeKey}
	def, ok := chartRanges[rangeKey]
	if !ok {
		return out
	}

	now := time.Now().UTC()
	var unit time.Duration
	if def.gran == "hourly" {
		unit = time.Hour
	} else {
		unit = 24 * time.Hour
	}
	cursor := now.Truncate(unit)
	since := cursor.Add(-time.Duration(def.n-1) * unit)

	rows, err := s.DB.QueryContext(r.Context(), `SELECT r.bucket_start, SUM(r.rx), SUM(r.tx)
		FROM traffic_rollups r
		JOIN devices d ON d.id = r.device_id
		JOIN users u ON u.id = d.user_id
		WHERE u.deleted_at IS NULL AND r.granularity = ? AND r.bucket_start >= ?
		GROUP BY r.bucket_start`, def.gran, since.Format(time.RFC3339Nano))
	if err != nil {
		s.logError(r, "dashboard chart", err)
		return out
	}
	defer rows.Close()

	// Bucket lookup: accounting stores UTC-truncated RFC3339Nano stamps, so
	// epoch-division reproduces the exact keys.
	sums := make(map[int64][2]int64, def.n)
	divisor := int64(unit / time.Second)
	for rows.Next() {
		var bucket string
		var rx, tx int64
		if err := rows.Scan(&bucket, &rx, &tx); err != nil {
			s.logError(r, "dashboard chart scan", err)
			return out
		}
		t, err := time.Parse(time.RFC3339Nano, bucket)
		if err != nil {
			continue
		}
		k := t.UTC().Unix() / divisor
		v := sums[k]
		v[0] += rx
		v[1] += tx
		sums[k] = v
	}
	if err := rows.Err(); err != nil {
		s.logError(r, "dashboard chart rows", err)
		return out
	}

	buckets := make([]chartBucket, def.n)
	for i := 0; i < def.n; i++ {
		t := since.Add(time.Duration(i) * unit)
		v := sums[t.Unix()/divisor]
		buckets[i] = chartBucket{
			Label: bucketLabel(s.localeFor(r), def.gran, t),
			Title: s.bucketTitle(r, def.gran, t, v[0], v[1]),
			RX:    v[0],
			TX:    v[1],
		}
	}
	out.HasChart = true
	out.SVG = trafficChartSVG(buckets, s.t(r, "dash.traffic_chart", s.t(r, def.label)))
	return out
}

// bucketLabel is the compact x-axis label (Latin digits): "14:00" for
// hourly buckets, month/day for daily ones (Jalali for fa).
func bucketLabel(loc i18n.Locale, gran string, t time.Time) string {
	if gran == "hourly" {
		return i18n.FormatTime(t, nil)
	}
	return monthDayLabel(loc, t)
}

// bucketTitle is the native tooltip over each bar group.
func (s *Server) bucketTitle(r *http.Request, gran string, t time.Time, rx, tx int64) string {
	loc := s.localeFor(r)
	var when string
	if gran == "hourly" {
		when = i18n.FormatDateTime(loc, t, nil)
	} else {
		when = i18n.FormatDateLong(loc, t, nil)
	}
	return fmt.Sprintf("%s · %s %s · %s %s", when,
		s.t(r, "dash.rx"), i18n.FormatBytes(loc, rx),
		s.t(r, "dash.tx"), i18n.FormatBytes(loc, tx))
}

func monthDayLabel(loc i18n.Locale, t time.Time) string {
	m, d := int(t.Month()), t.Day()
	if loc == i18n.Fa {
		jy, jm, jd := i18n.ToJalali(t.Year(), int(t.Month()), t.Day())
		_, m, d = jy, jm, jd
	}
	return fmt.Sprintf("%02d/%02d", m, d)
}

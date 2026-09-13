package web

import (
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"math"
	"strconv"
	"strings"
)

// chartBucket is one aggregated traffic period.
type chartBucket struct {
	Label string // short x-axis label (Latin digits, locale-independent)
	Title string // full tooltip text, already localized
	RX    int64  // bytes received
	TX    int64  // bytes sent
}

type chartInspectorPoint struct {
	X     float64    `json:"x"`
	Y     []*float64 `json:"y"`
	Title string     `json:"title"`
}

// Only plot geometry scales. HTML axes outside the SVG retain their CSS font
// size at every viewport, while sharing the same chronological bucket spacing.
const (
	chartW, chartH = 720, 240
)

// trafficChartSVG renders chronological RX/TX lines as an inline SVG. It is
// CSP-safe by construction: CSS classes and presentation attributes only —
// no inline style attributes, no scripts. All text passes through HTML
// escaping; numbers come from strconv. The chart reads LTR in both locales
// (time axes are data, like the rest of the panel's numeric rendering).
func trafficChartSVG(buckets []chartBucket, ariaLabel string) template.HTML {
	if len(buckets) == 0 {
		return ""
	}
	var max int64
	for _, b := range buckets {
		if b.RX > max {
			max = b.RX
		}
		if b.TX > max {
			max = b.TX
		}
	}
	if max == 0 {
		return "" // caller renders the empty state
	}
	scale := niceMax(max)
	plotW := float64(chartW)
	plotH := float64(chartH)
	points := make([]chartInspectorPoint, 0, len(buckets))
	for i, bucket := range buckets {
		x := plotW / 2
		if len(buckets) > 1 {
			x = plotW * float64(i) / float64(len(buckets)-1)
		}
		rxY := plotH * (1 - float64(bucket.RX)/float64(scale))
		txY := plotH * (1 - float64(bucket.TX)/float64(scale))
		points = append(points, chartInspectorPoint{X: x, Y: []*float64{&rxY, &txY}, Title: bucket.Title})
	}
	encodedPoints, err := json.Marshal(points)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(`<div class="traffic-plot interactive-chart"><div class="traffic-y-axis" aria-hidden="true">`)
	for i := 0; i <= 4; i++ {
		val := scale/4*int64(4-i) + scale%4*int64(4-i)/4
		sb.WriteString(`<span>` + compactBytes(val) + `</span>`)
	}
	sb.WriteString(`</div>`)
	sb.WriteString(`<svg class="chart" viewBox="0 0 `)
	sb.WriteString(strconv.Itoa(chartW))
	sb.WriteByte(' ')
	sb.WriteString(strconv.Itoa(chartH))
	sb.WriteString(`" role="img" tabindex="0" data-chart-interactive data-chart-points="`)
	sb.WriteString(html.EscapeString(string(encodedPoints)))
	sb.WriteString(`" aria-label="`)
	sb.WriteString(html.EscapeString(ariaLabel))
	sb.WriteString(`" preserveAspectRatio="none" focusable="true">`)

	// Grid + y-axis labels (4 divisions of the nice-scaled max).
	for i := 0; i <= 4; i++ {
		y := plotH * float64(i) / 4
		sb.WriteString(`<line class="chart-grid" x1="0"` +
			` y1="` + f1(y) + `" x2="` + f1(chartW) +
			`" y2="` + f1(y) + `"/>`)
	}

	// Shared chronological lines with a quiet area under received traffic.
	// Exact per-bucket values are also rendered in the accessible data table.
	var rxPath, txPath strings.Builder
	for i, bucket := range buckets {
		x := plotW / 2
		if len(buckets) > 1 {
			x = plotW * float64(i) / float64(len(buckets)-1)
		}
		command := "L"
		if i == 0 {
			command = "M"
		}
		rxY := plotH * (1 - float64(bucket.RX)/float64(scale))
		txY := plotH * (1 - float64(bucket.TX)/float64(scale))
		rxPath.WriteString(command + f1(x) + " " + f1(rxY))
		txPath.WriteString(command + f1(x) + " " + f1(txY))
		sb.WriteString(`<g><title>` + html.EscapeString(bucket.Title) + `</title></g>`)
		if len(buckets) == 1 {
			sb.WriteString(`<circle class="chart-point-rx" cx="` + f1(x) + `" cy="` + f1(rxY) + `" r="3"/><circle class="chart-point-tx" cx="` + f1(x) + `" cy="` + f1(txY) + `" r="3"/>`)
		}
	}
	if len(buckets) > 1 {
		sb.WriteString(`<path class="chart-area" d="` + rxPath.String() + `L` + f1(chartW) + ` ` + f1(plotH) + `L0 ` + f1(plotH) + `Z"/>`)
	}
	sb.WriteString(`<path class="chart-rx" d="` + rxPath.String() + `"/><path class="chart-tx" d="` + txPath.String() + `"/>`)
	sb.WriteString(`<g class="chart-inspector" aria-hidden="true"><line data-chart-cursor x1="0" y1="0" x2="0" y2="240"/><circle class="chart-marker-rx" data-chart-marker="0" cx="0" cy="0" r="5"/><circle class="chart-marker-tx" data-chart-marker="1" cx="0" cy="0" r="5"/></g>`)
	sb.WriteString(`</svg><div class="traffic-x-axis" aria-hidden="true">`)
	step := (len(buckets) + 5) / 6
	for i, bucket := range buckets {
		sb.WriteString(`<span class="traffic-tick">`)
		if i%step == 0 || i == len(buckets)-1 {
			class := "traffic-tick-label"
			if i != 0 && i != len(buckets)-1 && (i/step)%2 == 1 {
				class += " traffic-tick-minor"
			}
			sb.WriteString(`<span class="` + class + `">` + html.EscapeString(bucket.Label) + `</span>`)
		}
		sb.WriteString(`</span>`)
	}
	sb.WriteString(`</div></div>`)
	return template.HTML(sb.String())
}

// f1 formats a coordinate with one decimal (2 significant decimals are
// pointless at this viewBox size).
func f1(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64)
}

// niceMax rounds a value up to 1/2/5 × 10^n so gridlines land on round
// numbers (traffic 73 GB → 100 GB scale).
func niceMax(v int64) int64 {
	mag := int64(1)
	for mag <= v/10 {
		mag *= 10
	}
	for _, m := range [3]int64{1, 2, 5} {
		if mag > math.MaxInt64/m {
			return math.MaxInt64
		}
		if m*mag >= v {
			return m * mag
		}
	}
	if mag > math.MaxInt64/10 {
		return math.MaxInt64
	}
	return 10 * mag
}

// compactBytes renders an axis tick: language-neutral SI suffix with Latin
// digits (data rendering policy) — "0", "512K", "1.5M", "2G".
func compactBytes(n int64) string {
	switch {
	case n >= 1e12:
		return trimTick(float64(n)/1e12) + "T"
	case n >= 1e9:
		return trimTick(float64(n)/1e9) + "G"
	case n >= 1e6:
		return trimTick(float64(n)/1e6) + "M"
	case n >= 1e3:
		return trimTick(float64(n)/1e3) + "K"
	default:
		return strconv.FormatInt(n, 10)
	}
}

func trimTick(v float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0")
}

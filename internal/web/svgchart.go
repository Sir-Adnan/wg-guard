package web

import (
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

// SVG geometry. The viewBox is fixed; the element scales to its container.
const (
	chartW, chartH = 720, 240
	chartPadL      = 88 // readable y labels even at phone scale
	chartPadR      = 8
	chartPadT      = 12
	chartPadB      = 26 // room for x-axis labels
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
	plotW := float64(chartW - chartPadL - chartPadR)
	plotH := float64(chartH - chartPadT - chartPadB)

	var sb strings.Builder
	sb.WriteString(`<svg class="chart" viewBox="0 0 `)
	sb.WriteString(strconv.Itoa(chartW))
	sb.WriteByte(' ')
	sb.WriteString(strconv.Itoa(chartH))
	sb.WriteString(`" role="img" aria-label="`)
	sb.WriteString(html.EscapeString(ariaLabel))
	sb.WriteString(`" preserveAspectRatio="xMidYMid meet" focusable="false">`)

	// Grid + y-axis labels (4 divisions of the nice-scaled max).
	for i := 0; i <= 4; i++ {
		y := chartPadT + plotH*float64(i)/4
		val := scale/4*int64(4-i) + scale%4*int64(4-i)/4
		sb.WriteString(`<line class="chart-grid" x1="` + f1(chartPadL) +
			`" y1="` + f1(y) + `" x2="` + f1(chartW-chartPadR) +
			`" y2="` + f1(y) + `"/>`)
		if val > 0 || i == 4 {
			sb.WriteString(`<text class="chart-axis" x="` + f1(chartPadL-8) +
				`" y="` + f1(y+4) + `" text-anchor="end">` + compactBytes(val) + `</text>`)
		}
	}

	// Shared chronological lines with a quiet area under received traffic.
	// Exact per-bucket values are also rendered in the accessible data table.
	var rxPath, txPath strings.Builder
	step := (len(buckets) + 5) / 6
	for i, bucket := range buckets {
		x := float64(chartPadL) + plotW/2
		if len(buckets) > 1 {
			x = chartPadL + plotW*float64(i)/float64(len(buckets)-1)
		}
		command := "L"
		if i == 0 {
			command = "M"
		}
		rxY := chartPadT + plotH*(1-float64(bucket.RX)/float64(scale))
		txY := chartPadT + plotH*(1-float64(bucket.TX)/float64(scale))
		rxPath.WriteString(command + f1(x) + " " + f1(rxY))
		txPath.WriteString(command + f1(x) + " " + f1(txY))
		if i%step == 0 || i == len(buckets)-1 {
			class := "chart-axis"
			if i != 0 && i != len(buckets)-1 && (i/step)%2 == 1 {
				class += " chart-axis-minor"
			}
			anchor := "middle"
			if i == 0 {
				anchor = "start"
			} else if i == len(buckets)-1 {
				anchor = "end"
			}
			sb.WriteString(`<text class="` + class + `" x="` + f1(x) + `" y="` + f1(chartH-8) + `" text-anchor="` + anchor + `">` + html.EscapeString(bucket.Label) + `</text>`)
		}
		sb.WriteString(`<g><title>` + html.EscapeString(bucket.Title) + `</title></g>`)
		if len(buckets) == 1 {
			sb.WriteString(`<circle class="chart-point-rx" cx="` + f1(x) + `" cy="` + f1(rxY) + `" r="3"/><circle class="chart-point-tx" cx="` + f1(x) + `" cy="` + f1(txY) + `" r="3"/>`)
		}
	}
	if len(buckets) > 1 {
		sb.WriteString(`<path class="chart-area" d="` + rxPath.String() + `L` + f1(chartW-chartPadR) + ` ` + f1(chartPadT+plotH) + `L` + f1(chartPadL) + ` ` + f1(chartPadT+plotH) + `Z"/>`)
	}
	sb.WriteString(`<path class="chart-rx" d="` + rxPath.String() + `"/><path class="chart-tx" d="` + txPath.String() + `"/>`)
	sb.WriteString(`</svg>`)
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

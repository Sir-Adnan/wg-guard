package web

import (
	"fmt"
	"html"
	"html/template"
	"strconv"
	"strings"
)

// chartBucket is one aggregated traffic period (a bar pair).
type chartBucket struct {
	Label string // short x-axis label (Latin digits, locale-independent)
	Title string // full tooltip text, already localized
	RX    int64  // bytes received
	TX    int64  // bytes sent
}

// SVG geometry. The viewBox is fixed; the element scales to its container.
const (
	chartW, chartH = 720, 240
	chartPadL      = 56 // room for y-axis byte labels
	chartPadR      = 8
	chartPadT      = 12
	chartPadB      = 26 // room for x-axis labels
)

// trafficChartSVG renders a grouped RX/TX bar chart as an inline SVG. It is
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
		val := scale * int64(4-i) / 4
		sb.WriteString(`<line class="chart-grid" x1="` + f1(chartPadL) +
			`" y1="` + f1(y) + `" x2="` + f1(chartW-chartPadR) +
			`" y2="` + f1(y) + `"/>`)
		if val > 0 || i == 4 {
			sb.WriteString(`<text class="chart-axis" x="` + f1(chartPadL-8) +
				`" y="` + f1(y+4) + `" text-anchor="end">` + compactBytes(val) + `</text>`)
		}
	}

	// Bars: two per bucket (RX, TX), grouped and centered in the slot.
	slot := plotW / float64(len(buckets))
	group := slot * 0.62
	barW := group * 0.56
	step := len(buckets) / 7
	if step < 1 {
		step = 1
	}
	for i, b := range buckets {
		x0 := chartPadL + slot*float64(i) + (slot-group)/2
		rxH := plotH * float64(b.RX) / float64(scale)
		txH := plotH * float64(b.TX) / float64(scale)
		sb.WriteString(`<g><title>` + html.EscapeString(b.Title) + `</title>`)
		if b.RX > 0 {
			sb.WriteString(`<rect class="chart-rx" x="` + f1(x0) +
				`" y="` + f1(chartPadT+plotH-rxH) +
				`" width="` + f1(barW) + `" height="` + f1(rxH) + `" rx="2"><title>` +
				html.EscapeString(b.Title) + `</title></rect>`)
		}
		if b.TX > 0 {
			sb.WriteString(`<rect class="chart-tx" x="` + f1(x0+barW+1.5) +
				`" y="` + f1(chartPadT+plotH-txH) +
				`" width="` + f1(barW) + `" height="` + f1(txH) + `" rx="2"/>`)
		}
		if i%step == 0 && b.Label != "" {
			sb.WriteString(`<text class="chart-axis" x="` + f1(x0+group/2) +
				`" y="` + f1(chartH-8) + `" text-anchor="middle">` +
				html.EscapeString(b.Label) + `</text>`)
		}
		sb.WriteString(`</g>`)
	}
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
	for mag*10 <= v {
		mag *= 10
	}
	for _, m := range [3]int64{1, 2, 5} {
		if m*mag >= v {
			return m * mag
		}
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

package web

import (
	"html"
	"html/template"
	"math"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
)

const (
	sparkWidth  = 240
	sparkHeight = 56
	sparkPad    = 2
)

type sparkSeries struct {
	Class  string
	Values []telemetry.Metric
}

var sparkClasses = map[string]bool{
	"spark-primary":   true,
	"spark-secondary": true,
	"spark-muted":     true,
}

// sparklineSVG renders one or more bounded gap-aware line series. Class names
// come from a closed set and all text is escaped, so callers cannot inject
// style, markup, or script into the CSP-safe SVG.
func sparklineSVG(series []sparkSeries, ariaLabel string, fixedMax float64) template.HTML {
	if len(series) == 0 || len(series) > 3 {
		return ""
	}
	maxLen := 0
	for _, item := range series {
		if !sparkClasses[item.Class] {
			return ""
		}
		if len(item.Values) > maxLen {
			maxLen = len(item.Values)
		}
	}
	if maxLen > telemetry.HistoryCapacity {
		maxLen = telemetry.HistoryCapacity
	}
	if maxLen == 0 {
		return ""
	}

	maxValue, minValue := 0.0, math.MaxFloat64
	valid := 0
	for _, item := range series {
		values := newestSparkValues(item.Values, maxLen)
		for _, value := range values {
			if !validSparkMetric(value) {
				continue
			}
			valid++
			if value.Value > maxValue {
				maxValue = value.Value
			}
			if value.Value < minValue {
				minValue = value.Value
			}
		}
	}
	if valid == 0 {
		return ""
	}
	if fixedMax > 0 {
		maxValue = fixedMax
	} else if maxValue == 0 {
		maxValue = 1
	} else if minValue == maxValue {
		maxValue *= 2
	}

	var b strings.Builder
	b.WriteString(`<svg class="sparkline" viewBox="0 0 240 56" role="img" aria-label="`)
	b.WriteString(html.EscapeString(ariaLabel))
	b.WriteString(`" preserveAspectRatio="none" focusable="false">`)
	for _, item := range series {
		values := newestSparkValues(item.Values, maxLen)
		path := sparkPath(values, maxLen, maxValue)
		if path == "" {
			continue
		}
		b.WriteString(`<path class="spark-line `)
		b.WriteString(item.Class)
		b.WriteString(`" d="`)
		b.WriteString(path)
		b.WriteString(`"/>`)
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}

func newestSparkValues(values []telemetry.Metric, limit int) []telemetry.Metric {
	if len(values) <= limit {
		return values
	}
	return values[len(values)-limit:]
}

func validSparkMetric(value telemetry.Metric) bool {
	return value.Available && value.Value >= 0 && !math.IsNaN(value.Value) && !math.IsInf(value.Value, 0)
}

func sparkPath(values []telemetry.Metric, widthPoints int, maxValue float64) string {
	if maxValue <= 0 {
		return ""
	}
	plotW := float64(sparkWidth - 2*sparkPad)
	plotH := float64(sparkHeight - 2*sparkPad)
	offset := widthPoints - len(values)
	var path strings.Builder
	penDown := false
	for i, value := range values {
		if !validSparkMetric(value) {
			penDown = false
			continue
		}
		x := float64(sparkWidth) / 2
		if widthPoints > 1 {
			x = sparkPad + plotW*float64(offset+i)/float64(widthPoints-1)
		}
		clamped := math.Min(value.Value, maxValue)
		y := sparkPad + plotH*(1-clamped/maxValue)
		if penDown {
			path.WriteByte('L')
		} else {
			path.WriteByte('M')
			penDown = true
		}
		path.WriteString(f1(x))
		path.WriteByte(' ')
		path.WriteString(f1(y))
	}
	return path.String()
}

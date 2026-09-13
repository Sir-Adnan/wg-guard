package web

import (
	"encoding/json"
	"html"
	"html/template"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
)

const (
	sparkWidth  = 320
	sparkHeight = 100
	sparkPad    = 6
)

type sparkSeries struct {
	Class   string
	Label   string
	Values  []telemetry.Metric
	Display []string
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
	return timedSparklineSVG(series, ariaLabel, fixedMax, nil, 0)
}

func timedSparklineSVG(series []sparkSeries, ariaLabel string, fixedMax float64, times []time.Time, cadence time.Duration) template.HTML {
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
	points := sparkInspectorPoints(series, maxLen, maxValue, times)
	encodedPoints, err := json.Marshal(points)
	if err != nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(`<svg class="sparkline" viewBox="0 0 320 100" role="img" tabindex="0" data-chart-interactive data-chart-points="`)
	b.WriteString(html.EscapeString(string(encodedPoints)))
	b.WriteString(`" aria-label="`)
	b.WriteString(html.EscapeString(ariaLabel))
	b.WriteString(`" preserveAspectRatio="none" focusable="true">`)
	for _, y := range []int{6, 50, 94} {
		b.WriteString(`<path class="spark-grid" d="M6 ` + f1(float64(y)) + `H314"/>`)
	}
	for _, item := range series {
		values := newestSparkValues(item.Values, maxLen)
		path := timedSparkPath(values, maxLen, maxValue, times, cadence)
		if path == "" {
			continue
		}
		b.WriteString(`<path class="spark-line `)
		b.WriteString(item.Class)
		b.WriteString(`" d="`)
		b.WriteString(path)
		b.WriteString(`"/>`)
		// A move-only segment has no stroke, so show isolated samples explicitly.
		for _, segment := range strings.Split(path, "M")[1:] {
			if strings.Contains(segment, "L") {
				continue
			}
			xy := strings.Fields(segment)
			if len(xy) == 2 {
				b.WriteString(`<circle class="spark-dot ` + item.Class + `" cx="` + xy[0] + `" cy="` + xy[1] + `" r="2.5"/>`)
			}
		}
	}
	b.WriteString(`<g class="chart-inspector" aria-hidden="true"><line data-chart-cursor x1="0" y1="6" x2="0" y2="94"/>`)
	for index := range series {
		b.WriteString(`<circle data-chart-marker="` + strconv.Itoa(index) + `" cx="0" cy="0" r="4"/>`)
	}
	b.WriteString(`</g>`)
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}

func sparkInspectorPoints(series []sparkSeries, maxLen int, maxValue float64, times []time.Time) []chartInspectorPoint {
	points := make([]chartInspectorPoint, 0, maxLen)
	plotW := float64(sparkWidth - 2*sparkPad)
	plotH := float64(sparkHeight - 2*sparkPad)
	if len(times) > maxLen {
		times = times[len(times)-maxLen:]
	}
	timed := len(times) == maxLen && maxLen > 1 && times[maxLen-1].After(times[0])
	for index := 0; index < maxLen; index++ {
		x := float64(sparkWidth) / 2
		if maxLen > 1 {
			x = sparkPad + plotW*float64(index)/float64(maxLen-1)
		}
		if timed {
			x = sparkPad + plotW*float64(times[index].Sub(times[0]))/float64(times[maxLen-1].Sub(times[0]))
		}
		point := chartInspectorPoint{X: x, Y: make([]*float64, len(series))}
		parts := make([]string, 0, len(series)+1)
		if len(times) == maxLen {
			parts = append(parts, times[index].UTC().Format("15:04:05 UTC"))
		}
		for seriesIndex, item := range series {
			values := newestSparkValues(item.Values, maxLen)
			offset := maxLen - len(values)
			valueIndex := index - offset
			if valueIndex < 0 || valueIndex >= len(values) || !validSparkMetric(values[valueIndex]) {
				continue
			}
			y := sparkPad + plotH*(1-math.Min(values[valueIndex].Value, maxValue)/maxValue)
			yCopy := y
			point.Y[seriesIndex] = &yCopy
			display := strconv.FormatFloat(values[valueIndex].Value, 'f', 1, 64)
			if len(item.Display) == len(item.Values) {
				displayIndex := len(item.Display) - len(values) + valueIndex
				if displayIndex >= 0 && displayIndex < len(item.Display) && item.Display[displayIndex] != "" {
					display = item.Display[displayIndex]
				}
			}
			label := item.Label
			if label == "" {
				label = "Value"
			}
			parts = append(parts, label+": "+display)
		}
		if len(parts) > 0 {
			point.Title = strings.Join(parts, " · ")
			points = append(points, point)
		}
	}
	return points
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
	return timedSparkPath(values, widthPoints, maxValue, nil, 0)
}

func timedSparkPath(values []telemetry.Metric, widthPoints int, maxValue float64, times []time.Time, cadence time.Duration) string {
	if maxValue <= 0 {
		return ""
	}
	plotW := float64(sparkWidth - 2*sparkPad)
	plotH := float64(sparkHeight - 2*sparkPad)
	offset := widthPoints - len(values)
	if len(times) > widthPoints {
		times = times[len(times)-widthPoints:]
	}
	timed := len(times) == widthPoints && widthPoints > 1 && times[widthPoints-1].After(times[0])
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
		if timed {
			index := offset + i
			x = sparkPad + plotW*float64(times[index].Sub(times[0]))/float64(times[widthPoints-1].Sub(times[0]))
			if index > 0 && cadence > 0 && times[index].Sub(times[index-1]) > cadence+cadence/2 {
				penDown = false
			}
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

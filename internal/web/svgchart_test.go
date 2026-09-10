package web

import (
	"html/template"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
)

func TestTrafficChartSVG(t *testing.T) {
	buckets := []chartBucket{
		{Label: "00", Title: "hour 00", RX: 600, TX: 100},
		{Label: "01", Title: "hour 01", RX: 300, TX: 200},
		{Label: "02", Title: "hour 02<script>", RX: 500, TX: 0},
	}
	svg := string(trafficChartSVG(buckets, "Traffic, 3 hours"))

	for _, want := range []string{
		`viewBox="0 0 720 240"`,
		`role="img"`, `aria-label="Traffic, 3 hours"`,
		`class="chart-rx"`, `class="chart-tx"`, `class="chart-grid"`, `class="chart-axis"`,
		// niceMax(600) = 1000 → top gridline label 1K
		">1K</text>",
	} {
		if !strings.Contains(svg, want) {
			t.Fatalf("svg missing %q", want)
		}
	}
	// Labels are HTML-escaped (CSP-safe, injection-proof).
	if strings.Contains(svg, "<script>") {
		t.Fatal("label not escaped")
	}
	if !strings.Contains(svg, "hour 02&lt;script&gt;") {
		t.Fatal("expected escaped label")
	}
	// Inline style attributes are forbidden (CSP style-src 'self').
	if strings.Contains(svg, `style="`) {
		t.Fatal("inline style attribute in SVG")
	}
}

func TestTrafficChartSVGTiny(t *testing.T) {
	// One nonzero bucket renders; zero-only buckets render nothing (caller
	// shows the empty state).
	if s := trafficChartSVG([]chartBucket{{RX: 5, TX: 5}}, "x"); !strings.Contains(string(s), "<svg") {
		t.Fatal("nonzero bucket must render")
	}
	if s := trafficChartSVG([]chartBucket{{}, {}}, "x"); s != "" {
		t.Fatalf("all-zero buckets must render nothing, got %q", s)
	}
	if s := trafficChartSVG(nil, "x"); s != "" {
		t.Fatalf("nil buckets must render nothing, got %q", s)
	}
}

func TestNiceMax(t *testing.T) {
	cases := map[int64]int64{
		1:      1,
		73:     100,
		999:    1000,
		1000:   1000,
		1001:   2000,
		5_000:  5000,
		5_001:  10_000,
		73_000: 100_000,
	}
	for in, want := range cases {
		if got := niceMax(in); got != want {
			t.Errorf("niceMax(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestCompactBytes(t *testing.T) {
	cases := map[int64]string{
		0: "0", 512: "512", 1000: "1K", 1537: "1.5K",
		1_500_000: "1.5M", 2_000_000_000: "2G", 3_400_000_000_000: "3.4T",
	}
	for in, want := range cases {
		if got := compactBytes(in); got != want {
			t.Errorf("compactBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestSparklineSVGIsDeterministicEscapedAndGapAware(t *testing.T) {
	available := func(value float64) telemetry.Metric {
		return telemetry.Metric{Value: value, Available: true}
	}
	series := []sparkSeries{
		{Class: "spark-primary", Values: []telemetry.Metric{available(10), available(20), {}, available(30)}},
		{Class: "spark-secondary", Values: []telemetry.Metric{available(5), available(15), available(25), available(35)}},
	}
	first := sparklineSVG(series, `CPU <unsafe> & "quoted"`, 100)
	second := sparklineSVG(series, `CPU <unsafe> & "quoted"`, 100)
	if first == "" || first != second {
		t.Fatalf("sparkline must be non-empty and deterministic: %q / %q", first, second)
	}
	body := string(first)
	for _, want := range []string{`class="sparkline"`, `class="spark-line spark-primary"`, `class="spark-line spark-secondary"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("sparkline missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "<unsafe>") || !strings.Contains(body, "&lt;unsafe&gt;") {
		t.Fatalf("aria label not escaped: %s", body)
	}
	primary := pathForClass(t, template.HTML(body), "spark-primary")
	if strings.Count(primary, "M") != 2 {
		t.Fatalf("gap must start a new path segment: %q", primary)
	}
}

func TestSparklineSVGHandlesFlatInvalidAndBoundedSeries(t *testing.T) {
	flat := make([]telemetry.Metric, telemetry.HistoryCapacity+20)
	for i := range flat {
		flat[i] = telemetry.Metric{Value: 12, Available: true}
	}
	body := string(sparklineSVG([]sparkSeries{{Class: "spark-primary", Values: flat}}, "flat", 0))
	if body == "" || strings.Contains(body, "NaN") || strings.Contains(body, "Inf") {
		t.Fatalf("flat sparkline is invalid: %s", body)
	}
	if strings.Count(pathForClass(t, template.HTML(body), "spark-primary"), "L") != telemetry.HistoryCapacity-1 {
		t.Fatal("sparkline must retain only the bounded newest history")
	}
	if got := sparklineSVG([]sparkSeries{{Class: "untrusted-class", Values: flat}}, "bad", 0); got != "" {
		t.Fatalf("unknown class must be rejected: %s", got)
	}
}

func pathForClass(t *testing.T, svg template.HTML, class string) string {
	t.Helper()
	body := string(svg)
	needle := `class="spark-line ` + class + `" d="`
	start := strings.Index(body, needle)
	if start < 0 {
		t.Fatalf("path %s missing: %s", class, body)
	}
	start += len(needle)
	end := strings.Index(body[start:], `"`)
	if end < 0 {
		t.Fatalf("path %s unterminated", class)
	}
	return body[start : start+end]
}

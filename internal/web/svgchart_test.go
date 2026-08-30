package web

import (
	"strings"
	"testing"
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

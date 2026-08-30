package i18n

import (
	"testing"
	"time"
)

func TestFormatInt(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"}, {7, "7"}, {999, "999"}, {1000, "1,000"},
		{1234567, "1,234,567"}, {-9876543, "-9,876,543"},
	}
	for _, c := range cases {
		if got := FormatInt(c.in); got != c.want {
			t.Errorf("FormatInt(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		n      int64
		en, fa string
	}{
		{0, "0 B", "0 بایت"},
		{523, "523 B", "523 بایت"},
		{999, "999 B", "999 بایت"},
		{1000, "1 KB", "1 کیلوبایت"},
		{1536, "1.5 KB", "1.5 کیلوبایت"},
		{1_500_000_000, "1.5 GB", "1.5 گیگابایت"},
		{-2_000_000, "-2 MB", "-2 مگابایت"},
	}
	for _, c := range cases {
		if got := FormatBytes(En, c.n); got != c.en {
			t.Errorf("FormatBytes(en, %d) = %q, want %q", c.n, got, c.en)
		}
		if got := FormatBytes(Fa, c.n); got != c.fa {
			t.Errorf("FormatBytes(fa, %d) = %q, want %q", c.n, got, c.fa)
		}
	}
}

func TestFormatKbps(t *testing.T) {
	if got := FormatKbps(En, 512); got != "512 Kbps" {
		t.Errorf("en 512 = %q", got)
	}
	if got := FormatKbps(En, 1500); got != "1.5 Mbps" {
		t.Errorf("en 1500 = %q", got)
	}
	if got := FormatKbps(Fa, 1500); got != "1.5 مگابیت/ثانیه" {
		t.Errorf("fa 1500 = %q", got)
	}
}

func TestFormatDate(t *testing.T) {
	// 2026-08-30T10:20:30Z → Jalali 1404/06/09, Gregorian 2026-08-30.
	when := time.Date(2026, 8, 30, 10, 20, 30, 0, time.UTC)
	if got := FormatDate(Fa, when, nil); got != "1405/06/08" {
		t.Errorf("fa date = %q", got)
	}
	if got := FormatDate(En, when, nil); got != "2026-08-30" {
		t.Errorf("en date = %q", got)
	}
	if got := FormatDateLong(Fa, when, nil); got != "8 شهریور 1405" {
		t.Errorf("fa long = %q", got)
	}
	if got := FormatDateLong(En, when, nil); got != "30 Aug 2026" {
		t.Errorf("en long = %q", got)
	}
	if got := FormatTime(when, nil); got != "10:20" {
		t.Errorf("time = %q", got)
	}
	// Timezone shift moves the clock but stays on the same UTC-day date
	// boundary handling: +3:30 keeps 10:20Z within the same day.
	tehran := time.FixedZone("Tehran", 3*3600+1800)
	if got := FormatTime(when, tehran); got != "13:50" {
		t.Errorf("tehran time = %q", got)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		secs   int64
		en, fa string
	}{
		{0, "0s", "0 ثانیه"},
		{45, "45s", "45 ثانیه"},
		{3600, "1h", "1 ساعت"},
		{86400 + 7200, "1d, 2h", "1 روز و 2 ساعت"},
		{400 * 86400, "1y, 1mo", "1 سال و 1 ماه"},
	}
	for _, c := range cases {
		if got := FormatDuration(En, c.secs); got != c.en {
			t.Errorf("FormatDuration(en, %d) = %q, want %q", c.secs, got, c.en)
		}
		if got := FormatDuration(Fa, c.secs); got != c.fa {
			t.Errorf("FormatDuration(fa, %d) = %q, want %q", c.secs, got, c.fa)
		}
	}
}

func TestFormatRelative(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago    time.Duration
		en, fa string
	}{
		{10 * time.Second, "just now", "همین حالا"},
		{3 * time.Minute, "3m ago", "3 دقیقه پیش"},
		{5 * time.Hour, "5h ago", "5 ساعت پیش"},
		{2 * 24 * time.Hour, "2d ago", "2 روز پیش"},
		{-time.Hour, "just now", "همین حالا"}, // future (skew) clamps
	}
	for _, c := range cases {
		at := now.Add(-c.ago)
		if got := FormatRelative(En, now, at); got != c.en {
			t.Errorf("relative(en, %v) = %q, want %q", c.ago, got, c.en)
		}
		if got := FormatRelative(Fa, now, at); got != c.fa {
			t.Errorf("relative(fa, %v) = %q, want %q", c.ago, got, c.fa)
		}
	}
}

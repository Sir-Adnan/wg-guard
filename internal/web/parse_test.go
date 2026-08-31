package web

import (
	"testing"
	"time"
)

func TestParseQuotaBytesExact(t *testing.T) {
	cases := []struct {
		value, unit string
		want        *int64
		wantErr     bool
	}{
		{"", "", nil, false},
		{"   ", "gb", nil, false},
		{"0.2", "gb", i64(200000000), false},  // the bug report: 0.2 GB must not become 0
		{"0.1", "gb", i64(100000000), false},  // unrepresentable in float64 mantissa math
		{"100", "mb", i64(100000000), false},  // 100 MB test account
		{"0.5", "mb", i64(500000), false},     // sub-MB
		{"50", "gb", i64(50000000000), false}, //
		{"1000", "gb", i64(1e12), false},      //
		{"1e9", "gb", nil, true},              // exponent notation rejected
		{"-5", "gb", nil, true},               // sign rejected
		{"abc", "gb", nil, true},              //
		{"0", "gb", nil, true},                // zero is not a limit
		{"0.0000000001", "gb", nil, true},     // finer than one byte
		{"2000000", "gb", nil, true},          // above the 1e6 GB ceiling
		{"10", "kb", nil, true},               // unknown unit
		{"12.34.56", "gb", nil, true},         // two dots
		{"1000000000000000", "mb", nil, true}, // mantissa over ceiling after scale
	}
	for _, c := range cases {
		got, err := parseQuotaBytes(c.value, c.unit)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseQuotaBytes(%q,%q): want error, got %v", c.value, c.unit, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseQuotaBytes(%q,%q): %v", c.value, c.unit, err)
			continue
		}
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("parseQuotaBytes(%q,%q) = %v, want %v", c.value, c.unit, got, c.want)
		}
	}
}

func i64(v int64) *int64 { return &v }

func TestParseDurationSeconds(t *testing.T) {
	cases := []struct {
		value, unit string
		want        *int64
		wantErr     bool
	}{
		{"", "", nil, false},
		{"6", "hours", i64(21600), false},   // the 6-hour test account
		{"0.25", "days", i64(21600), false}, // fractional day
		{"1", "months", i64(30 * 86400), false},
		{"3", "months", i64(90 * 86400), false},
		{"365", "days", i64(365 * 86400), false},
		{"0.5", "hours", i64(1800), false},
		{"1", "", i64(86400), false}, // default unit = days
		{"0", "days", nil, true},
		{"4000", "days", nil, true}, // above 3650-day cap
		{"10", "weeks", nil, true},
		{"1.234", "hours", nil, true}, // finer than a hundredth of the unit
		{"x", "days", nil, true},
	}
	for _, c := range cases {
		got, err := parseDurationSeconds(c.value, c.unit)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseDurationSeconds(%q,%q): want error, got %v", c.value, c.unit, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDurationSeconds(%q,%q): %v", c.value, c.unit, err)
			continue
		}
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("parseDurationSeconds(%q,%q) = %v, want %v", c.value, c.unit, got, c.want)
		}
	}
}

func TestParseDateOnly(t *testing.T) {
	d, err := parseDateOnly("2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC); !d.Equal(want) {
		t.Errorf("parseDateOnly = %v, want noon UTC %v", d, want)
	}
	if d, err := parseDateOnly(""); err != nil || d != nil {
		t.Errorf("empty date: %v %v", d, err)
	}
	if _, err := parseDateOnly("31/12/2026"); err == nil {
		t.Error("want error for non-ISO date")
	}
}

func TestQuotaDisplayRoundTrip(t *testing.T) {
	v := &View{}
	cases := []struct {
		bytes     int64
		value, ui string // unit selection and displayed value
	}{
		{200000000, "200", "mb"},
		{100000000, "100", "mb"},
		{50000000000, "50", "gb"},
		{1500000000, "1.5", "gb"},
		{750000000000, "750", "gb"},
	}
	for _, c := range cases {
		b := c.bytes
		if got := v.QuotaVal(&b); got != c.value {
			t.Errorf("QuotaVal(%d) = %q, want %q", c.bytes, got, c.value)
		}
		if got := v.QuotaUnit(&b); got != c.ui {
			t.Errorf("QuotaUnit(%d) = %q, want %q", c.bytes, got, c.ui)
		}
		// The rendered pair must parse back to the same bytes.
		parsed, err := parseQuotaBytes(c.value, c.ui)
		if err != nil || *parsed != c.bytes {
			t.Errorf("round-trip %d: %v %v", c.bytes, parsed, err)
		}
	}
	if v.QuotaVal(nil) != "" || v.QuotaUnit(nil) != "gb" {
		t.Error("nil limit should render empty/gb")
	}
}

func TestDurationDisplayRoundTrip(t *testing.T) {
	v := &View{}
	cases := []struct {
		secs      int64
		value, ui string
	}{
		{21600, "6", "hours"},
		{86400, "1", "days"},
		{90 * 86400, "3", "months"},
		{365 * 86400, "365", "days"},
		{45 * 86400, "45", "days"},
	}
	for _, c := range cases {
		s := c.secs
		if got := v.DurVal(&s); got != c.value {
			t.Errorf("DurVal(%d) = %q, want %q", c.secs, got, c.value)
		}
		if got := v.DurUnit(&s); got != c.ui {
			t.Errorf("DurUnit(%d) = %q, want %q", c.secs, got, c.ui)
		}
		parsed, err := parseDurationSeconds(c.value, c.ui)
		if err != nil || *parsed != c.secs {
			t.Errorf("round-trip %d: %v %v", c.secs, parsed, err)
		}
	}
	if v.DurVal(nil) != "" || v.DurUnit(nil) != "days" {
		t.Error("nil duration should render empty/days")
	}
}

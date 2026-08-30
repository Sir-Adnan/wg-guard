package i18n

import "testing"

// Anchors are hand-verified historical dates: the Nowruz instants follow
// from the vernal equinox times in Tehran (before solar noon → that day is
// 1 Farvardin). These pin the conversion algorithm to real calendar facts.
var anchors = []struct {
	gy, gm, gd int // Gregorian
	jy, jm, jd int // Jalali
}{
	{2021, 3, 21, 1400, 1, 1},
	{2022, 3, 21, 1401, 1, 1},
	{2023, 3, 21, 1402, 1, 1},
	{2024, 3, 20, 1403, 1, 1}, // 1403 is a leap year
	{2025, 3, 21, 1404, 1, 1},
	{2026, 3, 21, 1405, 1, 1},
	{2026, 8, 30, 1405, 6, 8},
	{2024, 2, 18, 1402, 11, 29}, // last day of Esfand in a non-leap Jalali year
	{2025, 3, 20, 1403, 12, 30}, // 1403 leap → Esfand has 30 days
	{2020, 3, 20, 1399, 1, 1},   // 1399 leap year
	{2020, 3, 19, 1398, 12, 29}, // day before Nowruz 1399: Esfand 1398 has 29 days (1398 not leap)
	{2000, 3, 20, 1379, 1, 1},
	{1979, 3, 22, 1358, 1, 2}, // first full year of the Islamic Republic calendar era
}

func TestJalaliAnchors(t *testing.T) {
	for _, a := range anchors {
		jy, jm, jd := ToJalali(a.gy, a.gm, a.gd)
		if jy != a.jy || jm != a.jm || jd != a.jd {
			t.Errorf("ToJalali(%04d-%02d-%02d) = %04d/%02d/%02d, want %04d/%02d/%02d",
				a.gy, a.gm, a.gd, jy, jm, jd, a.jy, a.jm, a.jd)
			continue
		}
		gy, gm, gd := ToGregorian(a.jy, a.jm, a.jd)
		if gy != a.gy || gm != a.gm || gd != a.gd {
			t.Errorf("ToGregorian(%04d/%02d/%02d) = %04d-%02d-%02d, want %04d-%02d-%02d",
				a.jy, a.jm, a.jd, gy, gm, gd, a.gy, a.gm, a.gd)
		}
	}
}

// TestJalaliRoundTrip sweeps two centuries of days: every Gregorian day must
// survive → Jalali → Gregorian unchanged, and every month length must match
// JalaliMonthLength. An internal inconsistency anywhere in the era fails
// here even if no fixed anchor covers it.
func TestJalaliRoundTrip(t *testing.T) {
	start := g2d(1925, 1, 1)
	end := g2d(2125, 1, 1)
	for jdn := start; jdn <= end; jdn++ {
		gy, gm, gd := d2g(jdn)
		jy, jm, jd := d2j(jdn)
		if jd < 1 || jd > JalaliMonthLength(jy, jm) {
			t.Fatalf("day %d: %04d/%02d/%02d out of month range", jdn, jy, jm, jd)
		}
		if jm < 1 || jm > 12 {
			t.Fatalf("day %d: month %d out of range", jdn, jm)
		}
		back := j2d(jy, jm, jd)
		if back != jdn {
			t.Fatalf("round trip failed for %04d-%02d-%02d → %04d/%02d/%02d → jdn %d",
				gy, gm, gd, jy, jm, jd, back)
		}
	}
}

// TestJalaliLeapYears pins the 33-year cycle arithmetic on well-known leap
// years (Esfand 30).
func TestJalaliLeapYears(t *testing.T) {
	leap := []int{1399, 1403, 1408, 1412, 1416}
	for _, jy := range leap {
		if !jalaliIsLeap(jy) {
			t.Errorf("%d should be a leap year", jy)
		}
		if JalaliMonthLength(jy, 12) != 30 {
			t.Errorf("Esfand %d should have 30 days", jy)
		}
	}
	normal := []int{1400, 1401, 1402, 1404, 1405}
	for _, jy := range normal {
		if jalaliIsLeap(jy) {
			t.Errorf("%d should NOT be a leap year", jy)
		}
		if JalaliMonthLength(jy, 12) != 29 {
			t.Errorf("Esfand %d should have 29 days", jy)
		}
	}
}

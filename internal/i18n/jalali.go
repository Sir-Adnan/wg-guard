package i18n

// Jalali (Solar Hijri) calendar conversion. This is a faithful Go port of
// the widely-used Borkowski/"jalaali" algorithm (jalaali-js, MIT): it is the
// arithmetic the Persian calendar generators converge on and is validated by
// jalali_test.go against fixed anchors (Nowruz dates) plus a round-trip
// sweep. Conversions are pure integer math — no tables, no time.Time
// allocation in the hot path.
//
// The panel only ever FORMATS Gregorian instants as Jalali dates (fa
// locale); the reverse direction exists for tests and future date inputs.

// div is integer division truncated toward zero (Go's native semantics,
// which is what the JS original's `~~(a/b)` produces for these ranges).
func div(a, b int) int { return a / b }

// mod is the truncated remainder matching JS `%` for these ranges.
func mod(a, b int) int { return a - (a/b)*b }

// jalCalBreaks are the 33-year cycle boundaries of the arithmetic Persian
// calendar (Borkowski). The last entry bounds the supported era.
var jalCalBreaks = [21]int{
	-61, 9, 38, 199, 426, 686, 756, 818, 1111, 1181, 1210,
	1635, 2060, 2097, 2192, 2262, 2324, 2394, 2456, 3178,
}

// jalCal returns (leap, gregorianYear, marchDayOfFarvardin1) for a Jalali
// year. withLeap=false skips the leap computation (the j2d path).
func jalCal(jy int, withLeap bool) (leap, gy, march int) {
	bl := len(jalCalBreaks)
	gy = jy + 621
	leapJ := -14
	jp := jalCalBreaks[0]
	var jm, jump, n int

	for i := 1; i < bl; i++ {
		jm = jalCalBreaks[i]
		jump = jm - jp
		if jy < jm {
			break
		}
		leapJ += div(jump, 33)*8 + div(mod(jump, 33), 4)
		jp = jm
	}
	n = jy - jp

	leapJ += div(n, 33)*8 + div(mod(n, 33)+3, 4)
	if mod(jump, 33) == 4 && jump-n == 4 {
		leapJ++
	}

	leapG := div(gy, 4) - div((div(gy, 100)+1)*3, 4) - 150
	march = 20 + leapJ - leapG

	if withLeap {
		if jump-n < 6 {
			n = n - jump + div(jump+4, 33)*33
		}
		leap = mod(mod(n+1, 33)-1, 4)
		if leap == -1 {
			leap = 4
		}
	}
	return leap, gy, march
}

// g2d converts a Gregorian Y/M/D to a Julian Day Number.
func g2d(gy, gm, gd int) int {
	d := div((gy+div(gm-8, 6)+100100)*1461, 4) +
		div(153*mod(gm+9, 12)+2, 5) + gd - 34840408
	return d - div(div(gy+100100+div(gm-8, 6), 100)*3, 4) + 752
}

// d2g converts a Julian Day Number to Gregorian Y/M/D.
func d2g(jdn int) (gy, gm, gd int) {
	j := 4*jdn + 139361631
	j += div(div(4*jdn+183187720, 146097)*3, 4)*4 - 3908
	i := div(mod(j, 1461), 4)*5 + 308
	gd = div(mod(i, 153), 5) + 1
	gm = mod(div(i, 153), 12) + 1
	gy = div(j, 1461) - 100100 + div(8-gm, 6)
	return gy, gm, gd
}

// j2d converts a Jalali Y/M/D to a Julian Day Number.
func j2d(jy, jm, jd int) int {
	_, gy, march := jalCal(jy, false)
	return g2d(gy, 3, march) + (jm-1)*31 - div(jm, 7)*(jm-7) + jd - 1
}

// d2j converts a Julian Day Number to Jalali Y/M/D.
func d2j(jdn int) (jy, jm, jd int) {
	gy, _, _ := d2g(jdn)
	jy = gy - 621
	leap, _, march := jalCal(jy, true)
	jdn1f := g2d(gy, 3, march)

	k := jdn - jdn1f
	if k >= 0 {
		if k <= 185 {
			return jy, 1 + div(k, 31), mod(k, 31) + 1
		}
		k -= 186
	} else {
		jy--
		k += 179
		if leap == 1 {
			k++
		}
	}
	return jy, 7 + div(k, 30), mod(k, 30) + 1
}

// ToJalali converts a Gregorian date to Jalali year/month/day (1-based
// months).
func ToJalali(gy, gm, gd int) (jy, jm, jd int) {
	return d2j(g2d(gy, gm, gd))
}

// ToGregorian converts a Jalali date to Gregorian year/month/day.
func ToGregorian(jy, jm, jd int) (gy, gm, gd int) {
	return d2g(j2d(jy, jm, jd))
}

// jalaliIsLeap reports whether the Jalali year has 366 days.
func jalaliIsLeap(jy int) bool {
	leap, _, _ := jalCal(jy, true)
	return leap == 0
}

// JalaliMonthLength returns the number of days in a Jalali month.
// Months 1–6 have 31 days, 7–11 have 30, and Esfand has 29 or 30.
func JalaliMonthLength(jy, jm int) int {
	if jm <= 6 {
		return 31
	}
	if jm <= 11 {
		return 30
	}
	if jalaliIsLeap(jy) {
		return 30
	}
	return 29
}

// jalaliMonthNames are the twelve Persian month names in the spelling used
// by the fa catalog.
var jalaliMonthNames = [12]string{
	"فروردین", "اردیبهشت", "خرداد", "تیر", "مرداد", "شهریور",
	"مهر", "آبان", "آذر", "دی", "بهمن", "اسفند",
}

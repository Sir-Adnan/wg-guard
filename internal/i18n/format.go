package i18n

import (
	"fmt"
	"strings"
	"time"
)

// Formatting helpers. All output uses Latin digits (package policy) and
// ASCII '.' decimals; locale differences are the unit names, the calendar
// and word order. Templates keep data runs LTR-embedded; these functions
// never inject bidi control characters.

// FormatInt groups thousands with ',' ("1234567" → "1,234,567").
func FormatInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	if len(s) > 3 {
		var b strings.Builder
		head := len(s) % 3
		if head > 0 {
			b.WriteString(s[:head])
		}
		for i := head; i < len(s); i += 3 {
			if b.Len() > 0 {
				b.WriteByte(',')
			}
			b.WriteString(s[i : i+3])
		}
		s = b.String()
	}
	if neg {
		s = "-" + s
	}
	return s
}

// FormatFloat formats v with prec decimals, no grouping (used for rates).
func FormatFloat(v float64, prec int) string {
	return fmt.Sprintf("%.*f", prec, v)
}

var byteUnits = map[Locale][]string{
	// Decimal (SI) units: quotas are marketed in decimal GB and the traffic
	// accounting sums raw bytes, so 1000-based humanization is the honest
	// display (1 GB = 1,000,000,000 bytes). Below one KB values render raw.
	Fa: {"بایت", "کیلوبایت", "مگابایت", "گیگابایت", "ترابایت", "پتابایت"},
	En: {"B", "KB", "MB", "GB", "TB", "PB"},
}

// FormatBytes humanizes a byte count with locale unit names and up to one
// decimal ("0 B", "523 B", "1.5 GB"). Negative values (charged corrections)
// render with a leading minus.
func FormatBytes(locale Locale, n int64) string {
	units, ok := byteUnits[locale]
	if !ok {
		units = byteUnits[Default]
	}
	neg := n < 0
	if neg {
		n = -n
	}
	unit := 0
	val := float64(n)
	for val >= 1000 && unit < len(units)-1 {
		val /= 1000
		unit++
	}
	var s string
	if unit == 0 {
		s = FormatInt(n)
	} else {
		s = strings.TrimSuffix(fmt.Sprintf("%.1f", val), ".0")
		if s == "" || s == "0" {
			s = "0"
		}
	}
	if neg && unit > 0 {
		s = "-" + s
	}
	return s + " " + units[unit]
}

// FormatKbps humanizes a kilobit-per-second limit ("512 Mbps"). Negative or
// zero is meaningless (limits are positive or nil = unlimited upstream).
func FormatKbps(locale Locale, kbps int) string {
	units := [3]string{"Kbps", "Mbps", "Gbps"}
	if locale == Fa {
		units = [3]string{"کیلوبیت/ثانیه", "مگابیت/ثانیه", "گیگابیت/ثانیه"}
	}
	v := float64(kbps)
	unit := 0
	for v >= 1000 && unit < 2 {
		v /= 1000
		unit++
	}
	s := strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0")
	return s + " " + units[unit]
}

// FormatDate renders a UTC instant as a local-calendar date in the locale's
// calendar: fa → Jalali "1404/06/09" (numeric, sortable), en → ISO
// "2026-08-30". tz shifts the instant (callers pass the admin's display
// zone; the panel uses UTC).
func FormatDate(locale Locale, t time.Time, tz *time.Location) string {
	if tz != nil {
		t = t.In(tz)
	} else {
		t = t.UTC()
	}
	if locale == En {
		return fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
	}
	jy, jm, jd := ToJalali(t.Year(), int(t.Month()), t.Day())
	return fmt.Sprintf("%04d/%02d/%02d", jy, jm, jd)
}

// FormatDateLong is the prose form used on detail pages:
// fa "9 شهریور 1404", en "30 Aug 2026".
func FormatDateLong(locale Locale, t time.Time, tz *time.Location) string {
	if tz != nil {
		t = t.In(tz)
	} else {
		t = t.UTC()
	}
	if locale == En {
		return t.Format("2 Jan 2006")
	}
	jy, jm, jd := ToJalali(t.Year(), int(t.Month()), t.Day())
	return fmt.Sprintf("%d %s %d", jd, jalaliMonthNames[jm-1], jy)
}

// FormatTime renders the clock component "14:32" (locale-independent).
func FormatTime(t time.Time, tz *time.Location) string {
	if tz != nil {
		t = t.In(tz)
	} else {
		t = t.UTC()
	}
	return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
}

// FormatDateTime is FormatDate + space + FormatTime.
func FormatDateTime(locale Locale, t time.Time, tz *time.Location) string {
	return FormatDate(locale, t, tz) + " " + FormatTime(t, tz)
}

// FormatDuration renders a second count as a compact span: "45s", "12d 5h",
// "1y 2mo" — fa gets Persian unit words ("12 روز و 5 ساعت").
func FormatDuration(locale Locale, seconds int64) string {
	sec := seconds
	if sec < 0 {
		sec = 0
	}
	const (
		minute = int64(60)
		hour   = 60 * minute
		day    = 24 * hour
		month  = 30 * day
		year   = 365 * day
	)
	type part struct {
		n    int64
		fa   string
		en   string
		next int64
	}
	// Largest two non-zero units; months/years are approximate display units
	// (30/365-day) — the stored value is always exact seconds.
	steps := []part{
		{year, "سال", "y", 0},
		{month, "ماه", "mo", year},
		{day, "روز", "d", month},
		{hour, "ساعت", "h", day},
		{minute, "دقیقه", "m", hour},
		{1, "ثانیه", "s", minute},
	}
	var out []string
	for _, st := range steps {
		if st.next > 0 && sec >= st.next && len(out) < 2 {
			continue
		}
		if sec >= st.n && len(out) < 2 {
			n := sec / st.n
			sec -= n * st.n
			if locale == Fa {
				out = append(out, fmt.Sprintf("%d %s", n, st.fa))
			} else {
				out = append(out, fmt.Sprintf("%d%s", n, st.en))
			}
		}
	}
	if len(out) == 0 {
		if locale == Fa {
			return "0 ثانیه"
		}
		return "0s"
	}
	joiner := ", "
	if locale == Fa {
		joiner = " و "
	}
	return strings.Join(out, joiner)
}

// FormatRelative renders t (vs now) as coarse relative time for activity
// columns: fa "3 دقیقه پیش" / "همین حالا", en "3m ago" / "just now".
// Granularity is minutes below an hour, then hours, days — past only; the
// future (clock skew) renders as "just now".
func FormatRelative(locale Locale, now, t time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		if locale == Fa {
			return "همین حالا"
		}
		return "just now"
	case d < time.Hour:
		n := int64(d.Minutes())
		if locale == Fa {
			return fmt.Sprintf("%d دقیقه پیش", n)
		}
		return fmt.Sprintf("%dm ago", n)
	case d < 24*time.Hour:
		n := int64(d.Hours())
		if locale == Fa {
			return fmt.Sprintf("%d ساعت پیش", n)
		}
		return fmt.Sprintf("%dh ago", n)
	default:
		n := int64(d.Hours()) / 24
		if locale == Fa {
			return fmt.Sprintf("%d روز پیش", n)
		}
		return fmt.Sprintf("%dd ago", n)
	}
}

// FormatRelativeUntil renders "in n …" for future timestamps (expiry hints);
// past timestamps fall back to FormatRelative.
func FormatRelativeUntil(locale Locale, now, t time.Time) string {
	d := t.Sub(now)
	if d <= 0 {
		return FormatRelative(locale, now, t)
	}
	switch {
	case d < time.Hour:
		n := int64(d.Minutes()) + 1
		if locale == Fa {
			return fmt.Sprintf("%d دقیقه دیگر", n)
		}
		return fmt.Sprintf("in %dm", n)
	case d < 24*time.Hour:
		n := int64(d.Hours()) + 1
		if locale == Fa {
			return fmt.Sprintf("%d ساعت دیگر", n)
		}
		return fmt.Sprintf("in %dh", n)
	default:
		n := int64(d.Hours())/24 + 1
		if locale == Fa {
			return fmt.Sprintf("%d روز دیگر", n)
		}
		return fmt.Sprintf("in %dd", n)
	}
}

// MonthNames returns the month names for locale (used by the traffic chart
// axis): fa Jalali names, en Gregorian abbreviations.
func MonthNames(locale Locale) [12]string {
	if locale == En {
		return [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	}
	return jalaliMonthNames
}

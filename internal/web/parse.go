package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// Exact form-value parsing for quotas and durations.
//
// float64 rounds decimal input (0.1 GB is not representable), so quota and
// duration values are parsed digit-by-digit into exact integer bytes/seconds.
// This is what makes tiny test accounts (100 MB, 6 hours, 0.2 GB) round-trip
// without precision loss.

var errInvalid = domain.E(domain.CodeInvalidRequest, "invalid limit")

// SI byte units: quotas use decimal gigabytes (1 GB = 10^9 B), matching the
// historical panel semantics and the byte formatter.
const (
	unitGB = int64(1e9)
	unitMB = int64(1e6)
)

// maxQuotaBytes is the hard quota ceiling: 1,000,000 GB.
const maxQuotaBytes = int64(1e15)

// maxDurationSeconds is the hard duration ceiling: 3650 days.
const maxDurationSeconds = int64(3650) * 86400

// parseMantissa parses a positive decimal number digit-by-digit: the integer
// mantissa and the number of fraction digits. It accepts at most maxDigits
// digits total and nothing but digits and a single '.' — signs, exponent
// notation and stray characters are rejected (the caller trims whitespace).
func parseMantissa(s string, maxDigits int) (mant int64, fracLen int, ok bool) {
	frac := -1 // -1 = integer part; ≥ 0 counts fraction digits
	digits := 0
	for _, r := range s {
		switch {
		case r == '.':
			if frac >= 0 {
				return 0, 0, false
			}
			frac = 0
		case r >= '0' && r <= '9':
			digits++
			if digits > maxDigits {
				return 0, 0, false
			}
			mant = mant*10 + int64(r-'0')
			if frac >= 0 {
				frac++
			}
		default:
			return 0, 0, false
		}
	}
	if digits == 0 {
		return 0, 0, false
	}
	if frac < 0 {
		frac = 0
	}
	return mant, frac, true
}

// pow10 returns 10^n for small n (callers bound n ≥ 0).
func pow10(n int) int64 {
	p := int64(1)
	for i := 0; i < n; i++ {
		p *= 10
	}
	return p
}

// parseQuotaBytes parses a decimal quota with an explicit unit ("gb"/"mb")
// into exact bytes. Empty value = unset (nil, nil). The fraction may not be
// finer than one byte, and the result must land in (0, maxQuotaBytes].
func parseQuotaBytes(value, unit string) (*int64, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil, nil
	}
	unitBytes := unitGB
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "gb":
	case "mb":
		unitBytes = unitMB
	default:
		return nil, errInvalid
	}
	mant, fracLen, ok := parseMantissa(v, 15)
	if !ok || fracLen > 9 {
		return nil, errInvalid
	}
	// bytes = mant × 10^(exp − fracLen); the fraction stops at one byte and
	// the magnitude is bounded before the multiply (no int64 overflow).
	exp := 9
	if unitBytes == unitMB {
		exp = 6
	}
	if fracLen > exp {
		return nil, errInvalid
	}
	scale := pow10(exp - fracLen)
	if mant > maxQuotaBytes/scale {
		return nil, errInvalid
	}
	bytes := mant * scale
	if bytes <= 0 {
		return nil, errInvalid
	}
	return &bytes, nil
}

// parseDurationSeconds parses a decimal duration with unit
// ("hours"|"days"|"months", months = 30 days) into seconds, rounded to the
// nearest second. Empty = unset. Bounds: (0, 3650 days] — the panel's
// historical maximum subscription length.
func parseDurationSeconds(value, unit string) (*int64, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil, nil
	}
	var unitSecs int64
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "days":
		unitSecs = 86400
	case "hours":
		unitSecs = 3600
	case "months":
		unitSecs = 30 * 86400
	default:
		return nil, errInvalid
	}
	mant, fracLen, ok := parseMantissa(v, 6)
	if !ok || fracLen > 2 {
		return nil, errInvalid
	}
	scale := pow10(fracLen)
	secs := (mant*unitSecs + scale/2) / scale // mant ≤ 10^6 × 2.6e6: no overflow
	if secs <= 0 || secs > maxDurationSeconds {
		return nil, errInvalid
	}
	return &secs, nil
}

// parseDateOnly parses an exact YYYY-MM-DD expiry date as noon UTC — the
// same convention the renew flow uses so the date survives display TZ math.
func parseDateOnly(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	d, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, errInvalid
	}
	noon := d.UTC().Add(12 * time.Hour)
	return &noon, nil
}

// formDurationValue returns the submitted duration value under either the
// current (value+unit) or legacy (days) field name — used for error redisplay.
func formDurationValue(r *http.Request) string {
	if v := r.PostFormValue("duration_value"); v != "" {
		return v
	}
	return r.PostFormValue("duration_days")
}

// --- form-display helpers ---------------------------------------------------------

// formatScaled renders n/scale as a decimal string without float math:
// 200000000/1e9 → "0.2", 150e9/1e9 → "150". Trailing zeros are trimmed.
func formatScaled(n, scale int64) string {
	whole := n / scale
	rem := n % scale
	if rem == 0 {
		return strconv.FormatInt(whole, 10)
	}
	frac := make([]byte, 0, 9)
	for rem > 0 && len(frac) < 9 {
		rem *= 10
		frac = append(frac, byte('0'+rem/scale))
		rem %= scale
	}
	for len(frac) > 0 && frac[len(frac)-1] == '0' {
		frac = frac[:len(frac)-1]
	}
	return strconv.FormatInt(whole, 10) + "." + string(frac)
}

// QuotaVal renders a byte limit as a decimal number for a form input, choosing
// the more readable unit: MB below 1 GB, GB otherwise. nil = "".
func (v *View) QuotaVal(b *int64) string {
	if b == nil {
		return ""
	}
	if *b < unitGB {
		return formatScaled(*b, unitMB)
	}
	return formatScaled(*b, unitGB)
}

// QuotaUnit is the matching unit for QuotaVal ("mb" below 1 GB, else "gb").
func (v *View) QuotaUnit(b *int64) string {
	if b != nil && *b < unitGB {
		return "mb"
	}
	return "gb"
}

// DurVal renders a duration for a value+unit form input: whole months when
// the duration is a multiple of 30 days, else whole days, else hours.
func (v *View) DurVal(secs *int64) string {
	if secs == nil || *secs <= 0 {
		return ""
	}
	s := *secs
	const month = int64(30) * 86400
	switch {
	case s >= month && s%month == 0:
		return strconv.FormatInt(s/month, 10)
	case s >= 86400 && s%86400 == 0:
		return strconv.FormatInt(s/86400, 10)
	default:
		return formatScaled(s, 3600)
	}
}

// DurUnit is the matching unit for DurVal.
func (v *View) DurUnit(secs *int64) string {
	if secs == nil || *secs <= 0 {
		return "days"
	}
	s := *secs
	const month = int64(30) * 86400
	switch {
	case s >= month && s%month == 0:
		return "months"
	case s >= 86400 && s%86400 == 0:
		return "days"
	default:
		return "hours"
	}
}

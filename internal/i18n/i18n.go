// Package i18n provides the panel's server-side string tables (fa default,
// en), locale helpers, and locale-aware formatting. Catalogs are embedded Go
// maps so lookups are allocation-free and key typos fail the parity test at
// CI time instead of surfacing as raw keys in the UI.
//
// Number policy (docs/product/ui-ux.md): data — IPs, keys, counters — is
// language-independent and renders with LATIN digits in every locale, so all
// formatters here emit Latin digits. Persian text renders RTL around LTR
// numbers; templates handle the embedding, formatters never inject
// directional marks.
package i18n

import (
	"fmt"
	"strings"
)

// Locale is a supported UI language.
type Locale string

const (
	// Fa is Persian — the product default (requirements.md).
	Fa Locale = "fa"
	// En is English.
	En Locale = "en"
)

// Default is the locale used before an administrator picks one.
const Default = Fa

// Dir returns the base text direction ("rtl" or "ltr") — the value bound to
// <html dir="…"> server-side.
func (l Locale) Dir() string {
	if l == En {
		return "ltr"
	}
	return "rtl"
}

// Valid reports whether the locale is one of the supported catalogs.
func (l Locale) Valid() bool {
	return l == Fa || l == En
}

// Normalize maps a client-supplied language hint (cookie value, query
// parameter) onto a supported locale; unknown or empty values fall back to
// the product default. It accepts the forms browsers emit ("fa", "en-US",
// "fa-IR,fa;q=0.9") — only the first token is considered.
func Normalize(s string) Locale {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return Default
	}
	if i := strings.IndexAny(s, ",;"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	// Exact match first, then language-prefix match ("en-us" → en).
	for _, l := range []Locale{Fa, En} {
		if s == string(l) {
			return l
		}
	}
	if strings.HasPrefix(s, "en") {
		return En
	}
	if strings.HasPrefix(s, "fa") {
		return Fa
	}
	return Default
}

// T translates key for locale, formatting args into the result. Missing
// keys fall back to English (so a partially translated catalog degrades to
// English, never to a raw key) and then to the key itself — which the parity
// test makes unreachable for committed catalogs.
func T(locale Locale, key string, args ...any) string {
	msg, ok := catalogs[locale]
	if !ok {
		msg, ok = catalogs[Default]
	}
	if !ok {
		return key
	}
	s, ok := msg[key]
	if !ok {
		if en, hasEn := catalogs[En]; hasEn {
			if s, ok = en[key]; !ok {
				return key
			}
		} else {
			return key
		}
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

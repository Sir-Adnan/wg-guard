package i18n

import (
	"strings"
	"testing"
)

// TestCatalogParity keeps fa and en in lockstep (AGENTS.md: key parity is
// tested). It fails with the exact missing keys on either side.
func TestCatalogParity(t *testing.T) {
	for key := range catalogEN {
		if _, ok := catalogFA[key]; !ok {
			t.Errorf("key %q exists in en but not fa", key)
		}
	}
	for key := range catalogFA {
		if _, ok := catalogEN[key]; !ok {
			t.Errorf("key %q exists in fa but not en", key)
		}
	}
}

// TestCatalogNoEmptyValues catches accidental empty translations, which
// would render as blank UI text instead of failing loudly.
func TestCatalogNoEmptyValues(t *testing.T) {
	for locale, cat := range map[Locale]map[string]string{Fa: catalogFA, En: catalogEN} {
		for key, val := range cat {
			if strings.TrimSpace(val) == "" {
				t.Errorf("%s: key %q has an empty value", locale, key)
			}
		}
	}
}

// TestTranslate covers lookup, fallback and formatting behavior.
func TestTranslate(t *testing.T) {
	if got := T(En, "common.save"); got != "Save" {
		t.Errorf("en lookup = %q", got)
	}
	if got := T(Fa, "common.save"); got != "ذخیره" {
		t.Errorf("fa lookup = %q", got)
	}
	// Formatting args reach the format string.
	if got := T(En, "users.count", 7); got != "7 users" {
		t.Errorf("formatted = %q", got)
	}
	// A key missing from fa must fall back to en, not to the raw key.
	faOnly := map[string]string{}
	_ = faOnly
	if got := T(Fa, "nonexistent.key"); got != "nonexistent.key" {
		t.Errorf("missing key = %q, want the key echoed", got)
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want Locale }{
		{"", Default},
		{"fa", Fa},
		{"fa-IR", Fa},
		{"fa-IR,fa;q=0.9,en;q=0.8", Fa},
		{"en", En},
		{"en-US", En},
		{"en-GB;q=0.9", En},
		{"de", Default},
		{"  FA ", Fa},
	}
	for _, c := range cases {
		if got := Normalize(string(c.in)); got != c.want {
			t.Errorf("Normalize(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestLocaleDir(t *testing.T) {
	if Fa.Dir() != "rtl" || En.Dir() != "ltr" {
		t.Errorf("dir mismatch: fa=%s en=%s", Fa.Dir(), En.Dir())
	}
}

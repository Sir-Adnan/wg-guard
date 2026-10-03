package i18n

import (
	"strings"
	"testing"
)

func TestNumberPresentationDoesNotRewriteIdentifiersOrProtocols(t *testing.T) {
	if got := LatinDigits.Apply("۱۲۳ · ٤٥"); got != "123 · 45" {
		t.Fatalf("Latin normalization: %s", got)
	}
	if got := PersianDigits.Apply("1,234.5"); got != "۱,۲۳۴.۵" {
		t.Fatal("Persian formatting failed")
	}
	got := TDigits(Fa, PersianDigits, "cleanup.impact", 24, 12)
	if !strings.Contains(got, "۲۴") || !strings.Contains(got, "۱۲") {
		t.Fatal("typed message counts did not follow preference")
	}
	got = TDigits(Fa, PersianDigits, "users.confirm_delete", "customer123")
	if !strings.Contains(got, "customer123") {
		t.Fatal("username formatted as a number")
	}
	got = PersianDigits.Prose("/24 has 253 configs; 10.8.0.0/22 and awg0, v0.1.7, %.1f")
	for _, literal := range []string{"/24", "10.8.0.0/22", "awg0", "0.1.7", "%.1f", "۲۵۳"} {
		if !strings.Contains(got, literal) {
			t.Fatalf("prose broke technical literal %s", literal)
		}
	}
	if strings.ContainsAny(TDigits(Fa, LatinDigits, "ifaces.pools.primary_hint"), "۰۱۲۳۴۵۶۷۸۹") {
		t.Fatal("default help retained hardcoded Persian digits")
	}
}

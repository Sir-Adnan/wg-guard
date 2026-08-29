package version

import (
	"strings"
	"testing"
)

func TestStringReportsVersion(t *testing.T) {
	s := String()
	if !strings.Contains(s, Version) {
		t.Fatalf("String() = %q, want it to contain version %q", s, Version)
	}
	if !strings.Contains(s, "wg-guard") {
		t.Fatalf("String() = %q, want program name prefix", s)
	}
}

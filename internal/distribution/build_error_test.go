package distribution

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

func TestCompilationFailureRetainsFinalCauseAndExitIdentity(t *testing.T) {
	for _, reason := range []string{"no space left on device", "disk quota exceeded", "permission denied", "read-only file system"} {
		original := &subprocess.ExitError{Name: "/private/toolchain/bin/go", ExitCode: 1, Stderr: strings.Repeat("go: downloading fixture/module\n", 80) + "write /cache/modules/fixture/file: " + reason}
		err := sourceCompilationError(original)
		if !strings.Contains(err.Error(), reason) || len(err.Error()) > 1000 {
			t.Fatal("final cause lost or error unbounded")
		}
		var exit *subprocess.ExitError
		if !errors.As(err, &exit) || exit != original {
			t.Fatal("typed failure identity lost")
		}
	}
	if !errors.Is(sourceCompilationError(fmt.Errorf("canceled: %w", errors.ErrUnsupported)), errors.ErrUnsupported) {
		t.Fatal("wrapped cause lost")
	}
}

func TestCompilationFailureRedactsBeforeTakingDiagnosticSuffix(t *testing.T) {
	original := &subprocess.ExitError{Name: "go", ExitCode: 1, Stderr: strings.Repeat("خطا ", 500) + "https://operator:fixture-password@proxy.example password=fixture-secret no space left on device"}
	text := sourceCompilationError(original).Error()
	if strings.Contains(text, "fixture-password") || strings.Contains(text, "fixture-secret") || !strings.Contains(text, "no space left on device") || !utf8.ValidString(text) {
		t.Fatal("unsafe or unusable compiler diagnostic")
	}
}

package subprocess

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfiguredRunUsesExplicitEnvironmentAndDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture.test\n\ngo 1.25.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "GOPROXY=https://example.invalid", "GOTOOLCHAIN=local")
	r, err := NewSystem().RunConfigured(context.Background(), []string{"go", "env", "GOMOD", "GOPROXY"}, dir, env)
	if err != nil {
		t.Fatal(err)
	}
	out := string(r.Stdout)
	if !strings.Contains(out, filepath.Join(dir, "go.mod")) || !strings.Contains(out, "https://example.invalid") {
		t.Fatalf("configured output %q", out)
	}
}

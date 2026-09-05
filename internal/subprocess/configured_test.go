package subprocess

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfiguredRunBoundsBothOutputStreams(t *testing.T) {
	if os.Getenv("WG_GUARD_OUTPUT_FIXTURE") == "1" {
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("o"), 2<<20))
		_, _ = os.Stderr.Write(bytes.Repeat([]byte("e"), 2<<20))
		os.Exit(0)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "WG_GUARD_OUTPUT_FIXTURE=1")
	result, err := NewSystem().RunConfigured(context.Background(),
		[]string{executable, "-test.run=^TestConfiguredRunBoundsBothOutputStreams$"}, t.TempDir(), env)
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []struct {
		name string
		data []byte
		want byte
	}{{"stdout", result.Stdout, 'o'}, {"stderr", result.Stderr, 'e'}} {
		if len(stream.data) != 1<<20 {
			t.Errorf("%s retained %d bytes, want 1048576", stream.name, len(stream.data))
		}
		if !bytes.Equal(stream.data, bytes.Repeat([]byte{stream.want}, 1<<20)) {
			t.Errorf("%s did not retain exactly the allowed output prefix", stream.name)
		}
	}
}

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

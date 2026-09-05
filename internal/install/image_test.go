package install

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
)

type imageHost struct {
	*memHost
	t        *testing.T
	identity string
	fail     bool
	builds   int
}

func (h *imageHost) Run(ctx context.Context, a []string, d time.Duration) error {
	if len(a) > 1 && a[0] == "docker" && a[1] == "build" {
		h.builds++
		if h.fail {
			return fmt.Errorf("build failed")
		}
		dir := a[len(a)-1]
		binary, err := os.ReadFile(filepath.Join(dir, "wg-guard"))
		if err != nil || string(binary) != "candidate" {
			h.t.Fatal("build did not consume candidate binary")
		}
		dockerfile, err := os.ReadFile(filepath.Join(dir, "Dockerfile"))
		if err != nil {
			h.t.Fatal(err)
		}
		if !strings.Contains(string(dockerfile), "procps") {
			h.t.Error("runtime lacks sysctl provider needed by boot")
		}
		if !strings.Contains(string(dockerfile), "amneziawg-tools=1.0.20210914-0~202608130144+ee0f0a9~ubuntu24.04.1") || strings.Contains(string(dockerfile), "go build") {
			h.t.Fatal("runtime build lost compatible tools pin or rebuilt candidate")
		}
		for i, arg := range a {
			if arg == "--iidfile" {
				return os.WriteFile(a[i+1], []byte(h.identity), 0o600)
			}
		}
		h.t.Fatal("missing immutable image output")
	}
	return h.memHost.Run(ctx, a, d)
}

func TestRuntimeImageUsesAcquiredBinaryAndPrivateContext(t *testing.T) {
	parent := t.TempDir()
	binary := filepath.Join(parent, "candidate")
	if err := os.WriteFile(binary, []byte("candidate"), 0o755); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("candidate"))
	build := distribution.Build{Commit: strings.Repeat("a", 40), SHA256: fmt.Sprintf("%x", digest), BinaryPath: binary}
	h := &imageHost{memHost: newMemHost(), t: t, identity: "sha256:" + strings.Repeat("b", 64)}
	b, _ := SelectCore("recommended")
	got, err := BuildRuntimeImage(context.Background(), h, build, b, parent)
	if err != nil || got != h.identity || h.builds != 1 {
		t.Fatalf("image identity %q: %v", got, err)
	}
	files, _ := os.ReadDir(parent)
	if len(files) != 1 || files[0].Name() != "candidate" {
		t.Fatal("private build context not cleaned or caller files damaged")
	}
	if err := os.WriteFile(binary, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildRuntimeImage(context.Background(), h, build, b, parent); err == nil || h.builds != 1 {
		t.Fatal("tampered candidate reached Docker build")
	}
}

func TestRuntimeImageFailureNeverReturnsMutableFallback(t *testing.T) {
	parent := t.TempDir()
	binary := filepath.Join(parent, "candidate")
	_ = os.WriteFile(binary, []byte("candidate"), 0o755)
	sum := sha256.Sum256([]byte("candidate"))
	build := distribution.Build{Commit: strings.Repeat("a", 40), SHA256: fmt.Sprintf("%x", sum), BinaryPath: binary}
	b, _ := SelectCore("recommended")
	for _, identity := range []string{"", "image:latest", "sha256:bad"} {
		h := &imageHost{memHost: newMemHost(), t: t, identity: identity}
		if got, err := BuildRuntimeImage(context.Background(), h, build, b, parent); err == nil || got != "" {
			t.Fatalf("invalid immutable identity accepted: %q %v", got, err)
		}
	}
	h := &imageHost{memHost: newMemHost(), t: t, fail: true}
	if got, err := BuildRuntimeImage(context.Background(), h, build, b, parent); err == nil || got != "" {
		t.Fatal("failed build returned fallback")
	}
}

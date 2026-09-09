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
	t           *testing.T
	identity    string
	fail        bool
	builds      int
	quietBuilds int
	buildArgs   []string
}

func (h *imageHost) RunQuiet(ctx context.Context, a []string, d time.Duration) error {
	h.quietBuilds++
	return h.Run(ctx, a, d)
}

func (h *imageHost) Run(ctx context.Context, a []string, d time.Duration) error {
	if len(a) > 1 && a[0] == "docker" && a[1] == "build" {
		h.builds++
		h.buildArgs = append([]string(nil), a...)
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
		if !strings.Contains(string(dockerfile), "--branch v3.1.20260812") || !strings.Contains(string(dockerfile), "ee0f0a9aa34ff0a0da4b3433b9512781cfe02843") || strings.Contains(string(dockerfile), "ppa:amnezia/ppa") || strings.Contains(string(dockerfile), "amneziawg-tools=") {
			h.t.Fatal("runtime build lost exact GitHub tools provenance")
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
	if err != nil || got != h.identity || h.builds != 1 || h.quietBuilds != 1 {
		t.Fatalf("image identity %q: %v", got, err)
	}
	if !contains(h.buildArgs, "io.wg-guard.awg-tools.commit="+b.ToolsCommit) {
		t.Fatalf("runtime image lacks immutable AWG tools identity: %v", h.buildArgs)
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

func TestLegacyBundleRuntimeUsesTheSamePinnedToolsSource(t *testing.T) {
	b, err := SelectCore("awg-2026-08")
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := runtimeDockerfile(b)
	if !strings.Contains(dockerfile, "https://github.com/amnezia-vpn/amneziawg-tools.git") || !strings.Contains(dockerfile, b.ToolsCommit) {
		t.Fatalf("legacy install update lost its immutable tools source:\n%s", dockerfile)
	}
}

func TestRepositoryDockerfileUsesReviewedSourceTools(t *testing.T) {
	data, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(data)
	b, _ := SelectCore("recommended")
	for _, required := range []string{b.ToolsRepository, b.ToolsVersion, b.ToolsCommit, "/usr/local/bin/awg"} {
		if !strings.Contains(dockerfile, required) {
			t.Fatalf("repository Dockerfile omitted reviewed source identity %q", required)
		}
	}
	if strings.Contains(dockerfile, "ppa:amnezia/ppa") || strings.Contains(dockerfile, "amneziawg-tools=") {
		t.Fatal("repository Dockerfile still depends on mutable PPA package retention")
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

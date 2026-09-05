package distribution

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"go/version"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

var goDirective = regexp.MustCompile(`(?m)^go (1\.[0-9]+(?:\.[0-9]+)?)\s*$`)

func (c *Client) buildSource(ctx context.Context, b Build, stage string) (string, error) {
	archive := filepath.Join(stage, "source.tar.gz")
	if _, err := c.download(ctx, strings.TrimRight(c.options.SourceBase, "/")+"/Sir-Adnan/wg-guard/tar.gz/"+b.Commit, archive, maxSource, 0); err != nil {
		return "", err
	}
	source := filepath.Join(stage, "source")
	if err := extractArchive(ctx, archive, source, "wg-guard-"+b.Commit, 512<<20); err != nil {
		return "", err
	}
	mod, err := os.ReadFile(filepath.Join(source, "go.mod"))
	if err != nil {
		return "", err
	}
	match := goDirective.FindSubmatch(mod)
	if match == nil {
		return "", fmt.Errorf("distribution: source has no valid Go requirement")
	}
	minimum := "go" + string(match[1])
	runner := c.options.Runner
	if runner == nil {
		runner = &subprocess.System{Timeout: 15 * time.Minute}
	}
	env := buildEnvironment(stage, c.options.Arch)
	compiler := "go"
	result, probeErr := runner.RunConfigured(ctx, []string{compiler, "version"}, stage, env)
	fields := strings.Fields(string(result.Stdout))
	if probeErr != nil || len(fields) < 3 || !version.IsValid(fields[2]) || version.Compare(fields[2], minimum) < 0 {
		compiler, err = c.downloadCompiler(ctx, minimum, stage)
		if err != nil {
			return "", err
		}
	}
	output := filepath.Join(stage, "candidate.part")
	flags := "-s -w -X github.com/Sir-Adnan/wg-guard/internal/version.Version=" + b.Version + " -X github.com/Sir-Adnan/wg-guard/internal/version.Commit=" + b.Commit
	_, err = runner.RunConfigured(ctx, []string{compiler, "build", "-trimpath", "-buildvcs=false", "-mod=readonly", "-modcacherw", "-ldflags", flags, "-o", output, "./cmd/wg-guard"}, source, env)
	if err != nil {
		return "", fmt.Errorf("distribution: source compilation failed: %w", err)
	}
	return output, nil
}

func buildEnvironment(stage, arch string) []string {
	// Preserve host executable lookup, OS requirements, and explicit transport
	// proxy/CA configuration. Go configuration, flags, workspaces and caches are
	// private so GOFLAGS/GOWORK/GOENV/GOSUMDB cannot weaken the build boundary.
	var env []string
	for _, key := range []string{"PATH", "SystemRoot", "SYSTEMROOT", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return append(env, "HOME="+stage, "USERPROFILE="+stage, "TMPDIR="+stage, "TEMP="+stage, "TMP="+stage, "GOCACHE="+filepath.Join(stage, "cache"), "GOMODCACHE="+filepath.Join(stage, "modules"), "GOPATH="+filepath.Join(stage, "gopath"), "GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch, "GOPROXY=https://proxy.golang.org,direct", "GOSUMDB=sum.golang.org")
}

// extractArchive never materializes links, devices, special modes, duplicate
// members or paths outside one exact root. Headers and expansion are bounded.
func extractArchive(ctx context.Context, archive, dest, root string, max int64) error {
	if err := os.Mkdir(dest, 0700); err != nil {
		return err
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var total int64
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		count++
		if count > 50000 {
			return fmt.Errorf("distribution: archive has too many entries")
		}
		name := strings.TrimSuffix(h.Name, "/")
		if strings.ContainsAny(name, "\\:\x00") || path.Clean(name) != name || name != root && !strings.HasPrefix(name, root+"/") || seen[name] {
			return fmt.Errorf("distribution: unsafe archive path")
		}
		seen[name] = true
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return fmt.Errorf("distribution: unsupported archive member")
		}
		if h.Size < 0 || h.Size > max-total {
			return fmt.Errorf("distribution: expanded archive exceeds limit")
		}
		total += h.Size
		relative := strings.TrimPrefix(strings.TrimPrefix(name, root), "/")
		if relative == "" {
			if h.Typeflag != tar.TypeDir {
				return fmt.Errorf("distribution: invalid archive root")
			}
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(relative))
		if h.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		mode := os.FileMode(0600)
		if h.Mode&0111 != 0 {
			mode = 0700
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(out, tr, h.Size)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	// Consume the gzip trailer too; tar EOF alone does not verify gzip integrity.
	if n, err := io.Copy(io.Discard, io.LimitReader(gz, (1<<20)+1)); err != nil {
		return err
	} else if n > 1<<20 {
		return fmt.Errorf("distribution: archive trailing data exceeds limit")
	}
	return nil
}

func (c *Client) downloadCompiler(ctx context.Context, minimum, stage string) (string, error) {
	resp, err := c.get(ctx, strings.TrimRight(c.options.GoBase, "/")+"/dl/?mode=json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return "", err
	}
	if len(data) > 1<<20 {
		return "", fmt.Errorf("distribution: Go metadata exceeds limit")
	}
	var releases []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
		Files   []struct {
			Filename, OS, Arch, Kind, SHA256 string
			Size                             int64
		}
	}
	if err = json.Unmarshal(data, &releases); err != nil {
		return "", err
	}
	for _, r := range releases {
		if !r.Stable || !version.IsValid(r.Version) || version.Compare(r.Version, minimum) < 0 {
			continue
		}
		expected := r.Version + ".linux-" + c.options.Arch + ".tar.gz"
		for _, f := range r.Files {
			if f.OS != "linux" || f.Arch != c.options.Arch || f.Kind != "archive" {
				continue
			}
			if f.Filename != expected || !digestSHA.MatchString(f.SHA256) || f.Size <= 0 || f.Size > maxBinary {
				return "", fmt.Errorf("distribution: invalid Go metadata")
			}
			archive := filepath.Join(stage, "go.tar.gz")
			hash, err := c.download(ctx, strings.TrimRight(c.options.GoBase, "/")+"/dl/"+expected, archive, maxBinary, f.Size)
			if err != nil {
				return "", err
			}
			if hash != f.SHA256 {
				return "", fmt.Errorf("distribution: Go toolchain checksum mismatch")
			}
			dest := filepath.Join(stage, "toolchain")
			if err = extractArchive(ctx, archive, dest, "go", 1<<30); err != nil {
				return "", err
			}
			return filepath.Join(dest, "bin", "go"), nil
		}
	}
	return "", fmt.Errorf("distribution: no compatible official Go compiler")
}

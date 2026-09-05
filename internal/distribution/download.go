package distribution

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxBinary int64 = 256 << 20
const maxSource int64 = 128 << 20

// Acquire creates an owned 0700 child of dir. The caller retains/removes that
// child after consuming BinaryPath; on any failure the entire child is removed.
func (c *Client) Acquire(ctx context.Context, s Selection, dir string) (build Build, err error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	if c.options.Arch != "amd64" && c.options.Arch != "arm64" {
		return Build{}, fmt.Errorf("distribution: unsupported architecture")
	}
	if !filepath.IsAbs(dir) {
		return Build{}, fmt.Errorf("distribution: absolute staging directory required")
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return Build{}, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return Build{}, fmt.Errorf("distribution: invalid staging directory")
	}
	stage, err := os.MkdirTemp(dir, "wg-guard-candidate-")
	if err != nil {
		return Build{}, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(stage)
		}
	}()
	if err = os.Chmod(stage, 0700); err != nil {
		return Build{}, err
	}
	var b Build
	var part string
	switch s.Channel {
	case "release":
		var r Release
		r, err = c.release(ctx, s.Ref)
		if err != nil {
			return Build{}, err
		}
		b = Build{Channel: "release", Ref: r.Tag, Version: r.Tag}
		b.Commit, err = c.resolveCommit(ctx, r.Tag)
		if err != nil {
			return Build{}, err
		}
		name := "wg-guard_linux_" + c.options.Arch
		var bin, checks Asset
		bin, err = c.asset(r, name, maxBinary)
		if err != nil {
			return Build{}, err
		}
		checks, err = c.asset(r, "checksums.txt", 64<<10)
		if err != nil {
			return Build{}, err
		}
		sumPath := filepath.Join(stage, "checksums.txt")
		if _, err = c.download(ctx, checks.URL, sumPath, 64<<10, checks.Size); err != nil {
			return Build{}, err
		}
		var content []byte
		content, err = os.ReadFile(sumPath)
		if err != nil {
			return Build{}, err
		}
		var expected string
		expected, err = checksumFor(string(content), name)
		if err != nil {
			return Build{}, err
		}
		part = filepath.Join(stage, "candidate.part")
		b.SHA256, err = c.download(ctx, bin.URL, part, maxBinary, bin.Size)
		if err != nil {
			return Build{}, err
		}
		if b.SHA256 != expected {
			return Build{}, fmt.Errorf("distribution: SHA-256 mismatch")
		}
	case "commit":
		b, err = c.Resolve(ctx, s)
		if err != nil {
			return Build{}, err
		}
		part, err = c.buildSource(ctx, b, stage)
		if err != nil {
			return Build{}, err
		}
		b.SHA256, err = hashFile(part, maxBinary)
		if err != nil {
			return Build{}, err
		}
	default:
		return Build{}, fmt.Errorf("distribution: unknown channel")
	}
	if err = ctx.Err(); err != nil {
		return Build{}, err
	}
	b.BinaryPath = filepath.Join(stage, "wg-guard")
	if err = os.Chmod(part, 0700); err != nil {
		return Build{}, err
	}
	if err = os.Rename(part, b.BinaryPath); err != nil {
		return Build{}, err
	}
	return b, nil
}
func (c *Client) asset(r Release, name string, max int64) (Asset, error) {
	var found Asset
	count := 0
	expected := strings.TrimRight(c.options.DownloadBase, "/") + "/Sir-Adnan/wg-guard/releases/download/" + r.Tag + "/" + name
	for _, a := range r.Assets {
		if a.Name == name {
			found = a
			count++
		}
	}
	if count != 1 || found.URL != expected || found.Size <= 0 || found.Size > max {
		return Asset{}, fmt.Errorf("distribution: missing, ambiguous or unsafe asset %s", name)
	}
	return found, nil
}
func checksumFor(content, name string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	seen := map[string]bool{}
	result := ""
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || !digestSHA.MatchString(fields[0]) {
			return "", fmt.Errorf("distribution: invalid checksum manifest")
		}
		file := strings.TrimPrefix(fields[1], "*")
		if !safeRef.MatchString(file) || seen[file] {
			return "", fmt.Errorf("distribution: ambiguous checksum manifest")
		}
		seen[file] = true
		if file == name {
			result = fields[0]
		}
	}
	if scanner.Err() != nil || result == "" {
		return "", fmt.Errorf("distribution: missing checksum")
	}
	return result, nil
}
func (c *Client) download(ctx context.Context, url, path string, max, expected int64) (string, error) {
	resp, err := c.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.ContentLength > max {
		return "", fmt.Errorf("distribution: download exceeds limit")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, max+1))
	syncErr := f.Sync()
	closeErr := f.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if syncErr != nil {
		return "", syncErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if n == 0 || n > max || expected > 0 && n != expected {
		return "", fmt.Errorf("distribution: download size mismatch")
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
func hashFile(path string, max int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > max {
		return "", fmt.Errorf("distribution: invalid build output")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

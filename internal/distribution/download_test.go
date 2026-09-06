package distribution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireReleaseIntegrity(t *testing.T) {
	binary := []byte("independently hashed executable fixture\n")
	digest := sha256.Sum256(binary)
	checksum := hex.EncodeToString(digest[:]) + "  wg-guard_linux_amd64\n"
	for _, tc := range []struct {
		name, checksums, body, asset string
		size                         int64
		fail                         bool
	}{
		{"verified", checksum, string(binary), "wg-guard_linux_amd64", int64(len(binary)), false},
		{"corrupt", checksum, "corrupt", "wg-guard_linux_amd64", 7, true},
		{"missing checksum", "", "anything", "wg-guard_linux_amd64", 8, true},
		{"duplicate checksum", checksum + checksum, string(binary), "wg-guard_linux_amd64", int64(len(binary)), true},
		{"truncated", checksum, string(binary), "wg-guard_linux_amd64", 200, true},
		{"oversized", checksum, string(binary), "wg-guard_linux_amd64", 300 << 20, true},
		{"unsafe asset", checksum, string(binary), "../wg-guard_linux_amd64", int64(len(binary)), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var c *Client
			c = fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/commits/"):
					fmt.Fprintf(w, `{"sha":"%s"}`, fixtureSHA)
				case strings.Contains(r.URL.Path, "/releases/tags/"):
					base := c.options.DownloadBase + "/Sir-Adnan/wg-guard/releases/download/v1/"
					json.NewEncoder(w).Encode(Release{Tag: "v1", PublishedAt: "2026-01-01", Assets: []Asset{{Name: tc.asset, URL: base + tc.asset, Size: tc.size}, {Name: "checksums.txt", URL: base + "checksums.txt", Size: int64(len(tc.checksums))}}})
				case strings.HasSuffix(r.URL.Path, "checksums.txt"):
					fmt.Fprint(w, tc.checksums)
				default:
					fmt.Fprint(w, tc.body)
				}
			})
			dir := t.TempDir()
			b, err := c.Acquire(context.Background(), Selection{Channel: "release", Ref: "v1"}, dir)
			if (err != nil) != tc.fail {
				t.Fatalf("Acquire error %v, want failure %v", err, tc.fail)
			}
			if tc.fail {
				entries, _ := os.ReadDir(dir)
				if len(entries) != 0 {
					t.Fatal("failed acquisition left staged artifacts")
				}
				return
			}
			got, err := os.ReadFile(b.BinaryPath)
			if err != nil || string(got) != string(binary) {
				t.Fatalf("binary %q error %v", got, err)
			}
			if b.SHA256 != hex.EncodeToString(digest[:]) || b.Commit != fixtureSHA {
				t.Fatalf("incorrect identity %+v", b)
			}
			if filepath.Dir(b.BinaryPath) == dir {
				t.Fatal("candidate not privately staged")
			}
		})
	}
}

func TestStreamingDownloadBoundsAndCancellation(t *testing.T) {
	for _, cancelDownload := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelDownload), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				if cancelDownload {
					cancel()
					<-r.Context().Done()
					return
				}
				fmt.Fprint(w, "more than four bytes")
			})
			_, err := c.download(ctx, c.options.APIBase+"/download", filepath.Join(t.TempDir(), "candidate.part"), 4, 0)
			if err == nil {
				t.Fatal("unbounded/cancelled stream accepted")
			}
			if cancelDownload && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}

func TestDownloadPreservesCancellationAfterCleanEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := &cancelAfterRead{reader: strings.NewReader("x"), cancel: cancel}
	c := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(body),
			ContentLength: -1,
			Header:        make(http.Header),
		}, nil
	})}, Options{})

	_, err := c.download(ctx, "https://example.test/download", filepath.Join(t.TempDir(), "candidate.part"), 4, 2)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation after clean EOF: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type cancelAfterRead struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (r *cancelAfterRead) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 && r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	return n, err
}

func TestUnsafeAssetURLs(t *testing.T) {
	for _, raw := range []string{"http://github.com/Sir-Adnan/wg-guard/releases/download/v1/wg-guard_linux_amd64", "https://evil.test/wg-guard_linux_amd64", "https://github.com/other/repo/releases/download/v1/wg-guard_linux_amd64", "https://github.com/Sir-Adnan/wg-guard/releases/download/v1/wg-guard_linux_amd64?token=x"} {
		c := NewClient(nil, Options{})
		if _, err := c.asset(Release{Tag: "v1", Assets: []Asset{{Name: "wg-guard_linux_amd64", URL: raw, Size: 1}}}, "wg-guard_linux_amd64", 100); err == nil {
			t.Fatalf("unsafe URL accepted %s", raw)
		}
	}
}

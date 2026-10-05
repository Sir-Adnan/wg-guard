package distribution

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestReleaseRuntimeAcquisitionFailsClosedAndCleansPrivateStage(t *testing.T) {
	for _, kind := range []string{"valid", "tampered", "wrong-manager", "wrong-size", "missing-image", "duplicate-metadata", "wrong-sbom", "trailing-json"} {
		t.Run(kind, func(t *testing.T) {
			body := []byte("synthetic compressed image")
			b := Build{Channel: "release", Ref: "v1", Version: "v1", Commit: fixtureSHA, SHA256: strings.Repeat("a", 64)}
			m := RuntimeManifest{Schema: 1, Version: b.Version, Commit: b.Commit, BinarySHA256: b.SHA256, Platform: "linux/amd64", ImageID: "sha256:" + strings.Repeat("b", 64), RecipeSHA256: strings.Repeat("c", 64), Archive: RuntimeArchiveName, ArchiveSize: int64(len(body)), ArchiveSHA256: fmt.Sprintf("%x", sha256.Sum256(body)), ToolsCommit: fixtureSHA, UserspaceCommit: fixtureSHA, Kernels: []KernelIdentity{{"awg-test", "v1", fixtureSHA}}, SBOMSHA256: strings.Repeat("d", 64), NoticesSHA256: strings.Repeat("e", 64)}
			if kind == "wrong-manager" {
				m.BinarySHA256 = strings.Repeat("f", 64)
			}
			if kind == "wrong-size" {
				m.ArchiveSize++
			}
			meta, _ := json.Marshal(m)
			if kind == "trailing-json" {
				meta = append(meta, []byte(" {}")...)
			}
			sums := fmt.Sprintf("%x  runtime-metadata.json\n%s  %s\n%s  sbom.spdx.json\n", sha256.Sum256(meta), m.ArchiveSHA256, RuntimeArchiveName, m.SBOMSHA256)
			if kind == "wrong-sbom" {
				sums = strings.Replace(sums, m.SBOMSHA256, strings.Repeat("f", 64), 1)
			}
			if kind == "tampered" {
				body = []byte("synthetic corrupted image")
			}
			var c *Client
			c = fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/releases/tags/") {
					base := c.options.DownloadBase + "/Sir-Adnan/wg-guard/releases/download/v1/"
					assets := []Asset{{"runtime-metadata.json", base + "runtime-metadata.json", int64(len(meta))}, {"checksums.txt", base + "checksums.txt", int64(len(sums))}}
					if kind != "missing-image" {
						assets = append(assets, Asset{RuntimeArchiveName, base + RuntimeArchiveName, int64(len(body))})
					}
					if kind == "duplicate-metadata" {
						assets = append(assets, assets[0])
					}
					_ = json.NewEncoder(w).Encode(Release{Tag: "v1", PublishedAt: "2026-01-01", Assets: assets})
					return
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "checksums.txt"):
					_, _ = fmt.Fprint(w, sums)
				case strings.HasSuffix(r.URL.Path, "runtime-metadata.json"):
					_, _ = w.Write(meta)
				default:
					_, _ = w.Write(body)
				}
			})
			parent := t.TempDir()
			got, archive, cleanup, err := c.AcquireRuntime(context.Background(), b, parent)
			defer cleanup()
			if kind == "valid" {
				if err != nil || got.ImageID != m.ImageID {
					t.Fatal("verified runtime rejected", err)
				}
				if data, e := os.ReadFile(archive); e != nil || string(data) != string(body) {
					t.Fatal("runtime body not retained", e)
				}
				cleanup()
			} else if err == nil {
				t.Fatal("unsafe release runtime accepted", kind)
			}
			entries, _ := os.ReadDir(parent)
			if len(entries) != 0 {
				t.Fatal("private runtime staging leaked")
			}
		})
	}
}

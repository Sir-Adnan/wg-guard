package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestPreparedReleaseRechecksCommitAndManifest(t *testing.T) {
	for _, change := range []string{"none", "commit", "checksum"} {
		t.Run(change, func(t *testing.T) {
			checksum := strings.Repeat("a", 64)
			var base string
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/releases/tags/"):
					json.NewEncoder(w).Encode(Release{Tag: "v1.2.3", PublishedAt: "2026-10-01T00:00:00Z", Assets: []Asset{{Name: "checksums.txt", URL: base + "/Sir-Adnan/wg-guard/releases/download/v1.2.3/checksums.txt", Size: 88}}})
				case strings.Contains(r.URL.Path, "/commits/"):
					commit := fixtureSHA
					if change == "commit" {
						commit = strings.Repeat("b", 40)
					}
					json.NewEncoder(w).Encode(map[string]string{"sha": commit})
				default:
					value := checksum
					if change == "checksum" {
						value = strings.Repeat("b", 64)
					}
					fmt.Fprintf(w, "%s  wg-guard_linux_amd64\n", value)
				}
			})
			base = client.options.DownloadBase
			err := client.ValidateCachedRelease(context.Background(), Build{Channel: "release", Ref: "v1.2.3", Version: "v1.2.3", Commit: fixtureSHA, SHA256: checksum})
			if (err == nil) != (change == "none") {
				t.Fatal("changed prepared release identity admitted", err)
			}
		})
	}
}

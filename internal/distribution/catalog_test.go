package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fixtureSHA = "0123456789abcdef0123456789abcdef01234567"

func fixtureClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewTLSServer(handler)
	t.Cleanup(s.Close)
	return NewClient(s.Client(), Options{APIBase: s.URL, DownloadBase: s.URL, SourceBase: s.URL, Arch: "amd64"})
}

func TestResolveSelection(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		selection  Selection
		wantRef    string
		fail       bool
	}{
		{"empty release never becomes main", `[]`, Selection{Channel: "release"}, "", true},
		{"only published stable", `[{"tag_name":"v2","prerelease":true},{"tag_name":"v3","draft":true},{"tag_name":"v1","published_at":"2026-01-01T00:00:00Z"}]`, Selection{Channel: "release"}, "v1", false},
		{"exact tag", `{"tag_name":"v1","published_at":"2026-01-01T00:00:00Z"}`, Selection{Channel: "release", Ref: "v1"}, "v1", false},
		{"wrong exact tag", `{"tag_name":"v2","published_at":"2026-01-01T00:00:00Z"}`, Selection{Channel: "release", Ref: "v1"}, "", true},
		{"main immutable", `{"sha":"` + fixtureSHA + `"}`, Selection{Channel: "commit", Ref: "main"}, fixtureSHA, false},
		{"full SHA", `{"sha":"` + fixtureSHA + `"}`, Selection{Channel: "commit", Ref: fixtureSHA}, fixtureSHA, false},
		{"invalid response SHA", `{"sha":"oops"}`, Selection{Channel: "commit"}, "", true},
		{"wrong immutable SHA", `{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, Selection{Channel: "commit", Ref: fixtureSHA}, "", true},
		{"bad ref", `{}`, Selection{Channel: "release", Ref: "../bad"}, "", true},
		{"short SHA", `{}`, Selection{Channel: "commit", Ref: "1234567"}, "", true},
		{"unknown channel", `{}`, Selection{Channel: "nightly"}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/commits/") && tc.selection.Channel == "release" {
					fmt.Fprintf(w, `{"sha":"%s"}`, fixtureSHA)
					return
				}
				fmt.Fprint(w, tc.body)
			})
			b, err := c.Resolve(context.Background(), tc.selection)
			if (err != nil) != tc.fail {
				t.Fatalf("Resolve error = %v, want failure %v", err, tc.fail)
			}
			if err == nil && b.Ref != tc.wantRef {
				t.Fatalf("ref=%q want %q", b.Ref, tc.wantRef)
			}
		})
	}
}

func TestCatalogHTTPBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"status", 429, `{}`}, {"oversized", 200, strings.Repeat(" ", 2<<20)}, {"trailing JSON", 200, `[] []`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) })
			if _, err := c.Releases(context.Background()); err == nil {
				t.Fatal("invalid HTTP response accepted")
			}
		})
	}
}

func TestCatalogBoundAndExactRoute(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commits/") {
			fmt.Fprintf(w, `{"sha":"%s"}`, fixtureSHA)
			return
		}
		if r.URL.Path != "/repos/Sir-Adnan/wg-guard/releases/tags/v1" {
			t.Errorf("unexpected route %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(Release{Tag: "v1", PublishedAt: "2026-01-01T00:00:00Z"})
	})
	if _, err := c.Resolve(context.Background(), Selection{Channel: "release", Ref: "v1"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Releases(ctx); err == nil {
		t.Fatal("cancellation ignored")
	}
}

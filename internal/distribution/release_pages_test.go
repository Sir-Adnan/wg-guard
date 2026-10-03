package distribution

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestReleasePagesContinuationAndConditionalCache(t *testing.T) {
	requests := 0
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "10" {
			t.Error("wrong upstream page")
		}
		if r.Header.Get("If-None-Match") == `"reviewed"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"reviewed"`)
		entries := make([]Release, 10)
		for n := range entries {
			entries[n] = Release{Tag: "v1", PublishedAt: "2026-01-01T00:00:00Z", Prerelease: n != 0}
		}
		json.NewEncoder(w).Encode(entries)
	})
	for n := 0; n < 2; n++ {
		page, err := c.ReleasesPage(context.Background(), 2, 10)
		if err != nil || !page.HasNext || len(page.Releases) != 1 {
			t.Fatal("stable filtering falsely ended the archive", err)
		}
	}
	if requests != 2 {
		t.Fatal("conditional request did not run")
	}
	if _, err := c.ReleasesPage(context.Background(), 101, 10); err == nil {
		t.Fatal("unbounded page admitted")
	}
}

func TestCatalogRateLimitCarriesSafeRetry(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "120"); w.WriteHeader(429) })
	_, err := c.ReleasesPage(context.Background(), 1, 10)
	var status *HTTPStatusError
	if !errors.As(err, &status) || status.Code != 429 || status.RetryAfter.Seconds() != 120 {
		t.Fatal("missing rate-limit classification")
	}
}

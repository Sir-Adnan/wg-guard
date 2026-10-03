package web

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
)

type pagedReleaseCatalog interface {
	ReleasesPage(context.Context, int, int) (distribution.ReleasePage, error)
}
type exactReleaseCatalog interface {
	ReleaseByTag(context.Context, string) (distribution.Release, error)
}
type cachedReleases struct {
	page    distribution.ReleasePage
	at      time.Time
	retryAt time.Time
	err     error
}
type releaseCache struct {
	mu      sync.Mutex
	entries map[string]cachedReleases
	latest  string
}

// One bounded cache belongs to the server. It has no timer/goroutine, serializes
// catalog requests and keeps a stale result usable during a transport outage.
func (c *releaseCache) page(ctx context.Context, source ReleaseCatalog, page, size int, refresh bool) (distribution.ReleasePage, time.Time, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := strconv.Itoa(page) + ":" + strconv.Itoa(size)
	old, ok := c.entries[key]
	if ok && time.Now().Before(old.retryAt) {
		return old.page, old.at, !old.at.IsZero(), old.err
	}
	if ok && time.Since(old.at) < 10*time.Minute && (!refresh || time.Since(old.at) < 30*time.Second) {
		return old.page, old.at, false, nil
	}
	var result distribution.ReleasePage
	var err error
	if pager, ok := source.(pagedReleaseCatalog); ok {
		result, err = pager.ReleasesPage(ctx, page, size)
	} else {
		var all []distribution.Release
		all, err = source.Releases(ctx)
		start := (page - 1) * size
		if start < len(all) {
			end := min(start+size, len(all))
			result.Releases = all[start:end]
			result.HasNext = end < len(all)
		}
	}
	if err != nil {
		delay := time.Minute
		var httpErr *distribution.HTTPStatusError
		if errors.As(err, &httpErr) {
			delay = httpErr.RetryAfter
		}
		if c.entries == nil {
			c.entries = map[string]cachedReleases{}
		}
		old.retryAt = time.Now().Add(delay)
		old.err = err
		if len(c.entries) < 8 || ok {
			c.entries[key] = old
		}
		return old.page, old.at, !old.at.IsZero(), err
	}
	// Upstream text is data, rendered escaped. Bound retained notes separately
	// from the distribution client's HTTP body limit.
	for i := range result.Releases {
		if len(result.Releases[i].Body) > 8192 {
			result.Releases[i].Body = result.Releases[i].Body[:8192]
		}
	}
	if c.entries == nil {
		c.entries = map[string]cachedReleases{}
	}
	if len(c.entries) >= 8 {
		var oldestKey string
		var oldest time.Time
		for k, v := range c.entries {
			if oldestKey == "" || v.at.Before(oldest) {
				oldestKey, oldest = k, v.at
			}
		}
		delete(c.entries, oldestKey)
	}
	at := time.Now().UTC()
	c.entries[key] = cachedReleases{page: result, at: at}
	if page == 1 && len(result.Releases) > 0 {
		c.latest = result.Releases[0].Tag
	}
	return result, at, false, nil
}

func (c *releaseCache) latestTag() string { c.mu.Lock(); defer c.mu.Unlock(); return c.latest }

// Compare only ordinary stable semantic versions. Opaque upstream tags remain
// selectable but cannot be misleadingly labelled as an upgrade/downgrade.
func compareRelease(a, b string) (int, bool) {
	parse := func(value string) ([3]uint64, bool) {
		var out [3]uint64
		p := strings.Split(strings.TrimPrefix(value, "v"), ".")
		if len(p) != 3 {
			return out, false
		}
		for i, s := range p {
			n, e := strconv.ParseUint(s, 10, 32)
			if e != nil {
				return out, false
			}
			out[i] = n
		}
		return out, true
	}
	x, ok := parse(a)
	if !ok {
		return 0, false
	}
	y, ok := parse(b)
	if !ok {
		return 0, false
	}
	for i := range x {
		if x[i] < y[i] {
			return -1, true
		}
		if x[i] > y[i] {
			return 1, true
		}
	}
	return 0, true
}

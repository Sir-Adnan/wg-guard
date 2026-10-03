package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type conditionalPage struct {
	etag     string
	releases []Release
}

// ReleasePage retains upstream continuation before stable filtering. A page
// containing only prereleases is not evidence that the archive has ended.
type ReleasePage struct {
	Releases []Release
	HasNext  bool
}

func (c *Client) ReleasesPage(ctx context.Context, page, size int) (ReleasePage, error) {
	if page < 1 || page > 100 || size != 10 && size != 20 && size != 30 {
		return ReleasePage{}, fmt.Errorf("release catalog: invalid page")
	}
	var raw []Release
	key := fmt.Sprintf("/releases?per_page=%d&page=%d", size, page)
	c.pageMu.Lock()
	defer c.pageMu.Unlock()
	previous := c.pageCache[key]
	response, err := c.getConditional(ctx, strings.TrimRight(c.options.APIBase, "/")+repoPath+key, previous.etag, true)
	if err != nil {
		return ReleasePage{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		if previous.etag == "" {
			return ReleasePage{}, fmt.Errorf("release catalog: unexpected unchanged response")
		}
		raw = previous.releases
	} else {
		body, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		if e != nil {
			return ReleasePage{}, e
		}
		if len(body) > 1<<20 || json.Unmarshal(body, &raw) != nil {
			return ReleasePage{}, fmt.Errorf("release catalog: invalid bounded response")
		}
	}
	if len(raw) > size {
		return ReleasePage{}, fmt.Errorf("release catalog: page exceeds limit")
	}
	for n := range raw {
		if len(raw[n].Body) > 8192 {
			raw[n].Body = strings.ToValidUTF8(raw[n].Body[:8192], "")
		}
	}
	if c.pageCache == nil {
		c.pageCache = map[string]conditionalPage{}
	}
	if len(c.pageCache) >= 4 {
		for key := range c.pageCache {
			delete(c.pageCache, key)
			break
		}
	}
	etag := response.Header.Get("ETag")
	if response.StatusCode == http.StatusNotModified {
		etag = previous.etag
	}
	c.pageCache[key] = conditionalPage{etag: etag, releases: raw}
	result := ReleasePage{HasNext: len(raw) == size}
	for _, r := range raw {
		if !r.Draft && !r.Prerelease && r.PublishedAt != "" && safeRef.MatchString(r.Tag) {
			result.Releases = append(result.Releases, r)
		}
	}
	return result, nil
}

func (c *Client) ReleaseByTag(ctx context.Context, tag string) (Release, error) {
	return c.release(ctx, tag)
}

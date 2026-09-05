// Package distribution acquires candidates without changing active deployments.
package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type Selection struct{ Channel, Ref string }
type Build struct{ Channel, Ref, Commit, Version, SHA256, BinaryPath string }
type Release struct {
	Tag         string  `json:"tag_name"`
	PublishedAt string  `json:"published_at"`
	Draft       bool    `json:"draft"`
	Prerelease  bool    `json:"prerelease"`
	Assets      []Asset `json:"assets"`
}
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}
type BuildRunner interface {
	RunConfigured(context.Context, []string, string, []string) (subprocess.Result, error)
}
type Options struct {
	APIBase, DownloadBase, SourceBase, GoBase, Arch string
	Runner                                          BuildRunner
}
type Client struct {
	http    *http.Client
	options Options
}

var safeRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var digestSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

const repoPath = "/repos/Sir-Adnan/wg-guard"

// NewClient accepts endpoint overrides for isolated tests/private HTTPS mirrors.
// An override is explicit trust configuration; selection never changes repository.
func NewClient(h *http.Client, o Options) *Client {
	if h == nil {
		h = &http.Client{}
	}
	hc := *h
	hc.Timeout = 5 * time.Minute
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil {
			return fmt.Errorf("distribution: unsafe redirect")
		}
		host := req.URL.Hostname()
		if host != via[0].URL.Hostname() && host != "release-assets.githubusercontent.com" && host != "codeload.github.com" && host != "dl.google.com" {
			return fmt.Errorf("distribution: untrusted redirect")
		}
		return nil
	}
	if o.APIBase == "" {
		o.APIBase = "https://api.github.com"
	}
	if o.DownloadBase == "" {
		o.DownloadBase = "https://github.com"
	}
	if o.SourceBase == "" {
		o.SourceBase = "https://codeload.github.com"
	}
	if o.GoBase == "" {
		o.GoBase = "https://go.dev"
	}
	if o.Arch == "" {
		o.Arch = runtime.GOARCH
	}
	return &Client{http: &hc, options: o}
}

func (c *Client) get(ctx context.Context, raw string) (*http.Response, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("distribution: HTTPS URL required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "wg-guard-distribution")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Redirect targets can carry temporary GitHub download credentials.
		return nil, fmt.Errorf("distribution: HTTPS request failed")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("distribution: HTTP status %d", resp.StatusCode)
	}
	return resp, nil
}
func (c *Client) json(ctx context.Context, path string, out any) error {
	resp, err := c.get(ctx, strings.TrimRight(c.options.APIBase, "/")+repoPath+path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 1<<20 {
		return fmt.Errorf("distribution: catalog exceeds limit")
	}
	if err = json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("distribution: invalid JSON: %w", err)
	}
	return nil
}
func (c *Client) Releases(ctx context.Context) ([]Release, error) {
	var all []Release
	if err := c.json(ctx, "/releases?per_page=30&page=1", &all); err != nil {
		return nil, err
	}
	if len(all) > 30 {
		return nil, fmt.Errorf("distribution: release catalog exceeds 30 entries")
	}
	releases := make([]Release, 0, len(all))
	for _, r := range all {
		if !r.Draft && !r.Prerelease && r.PublishedAt != "" && safeRef.MatchString(r.Tag) {
			releases = append(releases, r)
		}
	}
	return releases, nil
}
func (c *Client) release(ctx context.Context, ref string) (Release, error) {
	if ref == "" || ref == "latest" {
		rs, err := c.Releases(ctx)
		if err != nil {
			return Release{}, err
		}
		if len(rs) == 0 {
			return Release{}, fmt.Errorf("distribution: no published stable releases; select commit explicitly to build development source")
		}
		return rs[0], nil
	}
	if !safeRef.MatchString(ref) {
		return Release{}, fmt.Errorf("distribution: invalid tag")
	}
	var r Release
	if err := c.json(ctx, "/releases/tags/"+ref, &r); err != nil {
		return r, err
	}
	if r.Tag != ref || r.Draft || r.Prerelease || r.PublishedAt == "" {
		return Release{}, fmt.Errorf("distribution: release is not published stable")
	}
	return r, nil
}
func (c *Client) resolveCommit(ctx context.Context, ref string) (string, error) {
	var response struct {
		SHA string `json:"sha"`
	}
	if err := c.json(ctx, "/commits/"+ref, &response); err != nil {
		return "", err
	}
	if !commitSHA.MatchString(response.SHA) || commitSHA.MatchString(ref) && ref != response.SHA {
		return "", fmt.Errorf("distribution: invalid immutable commit")
	}
	return response.SHA, nil
}
func (c *Client) Resolve(ctx context.Context, s Selection) (Build, error) {
	b := Build{Channel: s.Channel}
	switch s.Channel {
	case "release":
		r, err := c.release(ctx, s.Ref)
		if err != nil {
			return Build{}, err
		}
		b.Ref = r.Tag
		b.Version = r.Tag
		b.Commit, err = c.resolveCommit(ctx, r.Tag)
		if err != nil {
			return Build{}, err
		}
	case "commit":
		ref := s.Ref
		if ref == "" {
			ref = "main"
		}
		if ref != "main" && !commitSHA.MatchString(ref) {
			return Build{}, fmt.Errorf("distribution: source ref must be main or full lowercase SHA")
		}
		sha, err := c.resolveCommit(ctx, ref)
		if err != nil {
			return Build{}, err
		}
		b.Ref = sha
		b.Commit = sha
		b.Version = "0.0.0-dev." + sha[:12]
	default:
		return Build{}, fmt.Errorf("distribution: channel must be release or commit")
	}
	return b, nil
}

package routing

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Site tests open a real site through one outbound and judge whether the
// service works from that exit (an AI service limited by region, Google's
// abuse CAPTCHA, a site's firewall). The service detectors live in
// sitejudge.go and the region lists in siteregions.go.

// SiteTimeout bounds one site test (redirects, the second page and the
// region lookup included).
const SiteTimeout = 15 * time.Second

// maxSiteBody bounds the bytes read from one page.
const maxSiteBody = 2 << 20

// siteFirstFamilyBudget bounds the IPv6 attempt of an IPv6-first dial, so a
// broken IPv6 path still leaves time to fall back to IPv4 within SiteTimeout.
const siteFirstFamilyBudget = 4 * time.Second

// siteRetryFloor is the time a failed attempt must leave for one retry.
const siteRetryFloor = 5 * time.Second

// siteUserAgent is a current desktop browser: several services answer
// non-browser agents with a different page.
const siteUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

// Site verdicts.
const (
	SiteAvailable = "available" // the service works from this exit
	SiteLimited   = "limited"   // the service works partly (Netflix: originals only)
	SiteBlocked   = "blocked"   // the service refuses this exit (region, VPN)
	SiteCaptcha   = "captcha"   // Google treats the exit as abusive
	SiteChallenge = "challenge" // a bot check answered: the site is up, a browser passes
	SiteRefused   = "refused"   // the site's firewall refused the test client (401/403/429)
	SiteReachable = "reachable" // a custom URL answered (no service detector)
	SiteUnknown   = "unknown"   // the page did not match a known answer
	SiteFailed    = "failed"    // no answer: see Error
)

// ErrSiteURL rejects a custom URL that is not an http(s) URL with a public
// host name or address.
var ErrSiteURL = errors.New("routing: invalid site URL")

// ParseSiteURL accepts a host name or an http(s) URL ("gemini.google.com",
// "https://example.com/path").
func ParseSiteURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return nil, ErrSiteURL
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return nil, ErrSiteURL
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u, nil
}

// SiteResult is one site test.
type SiteResult struct {
	Verdict  string
	Status   int           // final HTTP status (0 without an answer)
	Region   string        // country the service (or Cloudflare, for a custom URL) placed the exit in
	Note     string        // short technical detail from the detector
	Final    string        // final host and path after redirects
	Family   string        // ip4 | ip6: the family the final request used
	Latency  time.Duration // time to the first page's answer
	Attempts int           // 2 when the first attempt failed and was retried
	Error    string        // fixed category when Verdict is failed: timeout | unreachable | tls | dns | unsupported
	TestedAt time.Time
}

// Site test address families: FamilyAuto dials the way client traffic
// leaves (IPv6 first through an outbound whose link carries IPv6, else
// IPv4); FamilyIPv4 and FamilyIPv6 force one family.
const (
	FamilyAuto = "auto"
	FamilyIPv4 = "ip4"
	FamilyIPv6 = "ip6"
)

// ValidFamily reports whether f is a site test family.
func ValidFamily(f string) bool {
	return f == FamilyAuto || f == FamilyIPv4 || f == FamilyIPv6
}

// SiteTester runs site tests through outbound marks.
type SiteTester struct {
	Lookup Lookup
}

// Test fetches u (following up to five redirects) through mark and judges
// the answer with svc's detector (nil: the generic custom-URL judge).
// family is FamilyAuto (IPv6 first when preferIPv6), FamilyIPv4 or
// FamilyIPv6. A failed attempt is retried once while enough time is left: a
// single lost handshake through a tunnel is not a verdict.
func (t SiteTester) Test(ctx context.Context, mark uint32, preferIPv6 bool, family string, u *url.URL, svc *SiteService) SiteResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, SiteTimeout)
	defer cancel()
	var res SiteResult
	for attempt := 1; attempt <= 2; attempt++ {
		res = t.attempt(ctx, mark, preferIPv6, family, u, svc)
		res.Attempts = attempt
		deadline, _ := ctx.Deadline()
		if res.Verdict != SiteFailed || res.Error == "dns" || res.Error == "unsupported" || time.Until(deadline) < siteRetryFloor {
			break
		}
	}
	res.TestedAt = start.UTC()
	return res
}

func (t SiteTester) attempt(ctx context.Context, mark uint32, preferIPv6 bool, family string, u *url.URL, svc *SiteService) SiteResult {
	var res SiteResult
	client, closeIdle := t.client(mark, preferIPv6, family, &res.Family)
	defer closeIdle()
	began := time.Now()
	main, err := fetchSitePage(ctx, client, u.String())
	if err != nil {
		res.Verdict, res.Error = SiteFailed, classifySiteError(err)
		return res
	}
	res.Latency = time.Since(began)
	res.Status, res.Final = main.Status, clipText(main.Host+main.Path, 120)
	var second *SitePage
	if svc != nil && svc.Second != "" {
		if second, err = fetchSitePage(ctx, client, svc.Second); err != nil {
			res.Verdict, res.Error = SiteFailed, classifySiteError(err)
			return res
		}
	}
	var j SiteJudgement
	switch {
	case botCheck(main):
		j.Verdict = SiteChallenge
	case svc != nil && svc.judge != nil:
		j = svc.judge(main, second)
	default:
		j = judgeCustom(main, nil)
	}
	res.Verdict, res.Region, res.Note = j.Verdict, j.Region, j.Note
	// A Cloudflare-served site reports the country it places the exit in on
	// its own trace endpoint: the same view the site's rules see.
	if res.Region == "" && cloudflareServed(main) && main.Path != "/cdn-cgi/trace" {
		trace := &url.URL{Scheme: "https", Host: main.Host, Path: "/cdn-cgi/trace"}
		if tp, err := fetchSitePage(ctx, client, trace.String()); err == nil && tp.Status == http.StatusOK {
			if loc := parseTrace(tp.Body)["loc"]; len(loc) == 2 && isAlpha(loc) {
				res.Region = strings.ToUpper(loc)
			}
		}
	}
	return res
}

// client builds a one-test HTTP client whose connections go through mark;
// family records the address family of the last connection.
func (t SiteTester) client(mark uint32, preferIPv6 bool, only string, family *string) (*http.Client, func()) {
	if only == FamilyAuto {
		only = ""
	}
	dialer := HostDialer{Mark: mark, PreferIPv6: preferIPv6, Only: only, Timeout: ProbeTimeout, FirstFamilyBudget: siteFirstFamilyBudget, Lookup: t.Lookup}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			host, portText, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			port, err := strconv.ParseUint(portText, 10, 16)
			if err != nil {
				return nil, err
			}
			conn, fam, err := dialer.Dial(ctx, host, uint16(port))
			if err == nil {
				*family = fam
			}
			return conn, err
		},
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   ProbeTimeout,
		ResponseHeaderTimeout: SiteTimeout,
		MaxIdleConnsPerHost:   1,
		IdleConnTimeout:       SiteTimeout,
		ForceAttemptHTTP2:     true,
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || (req.URL.Scheme != "https" && req.URL.Scheme != "http") {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	return client, transport.CloseIdleConnections
}

// fetchSitePage GETs one page with a browser's request headers.
func fetchSitePage(ctx context.Context, client *http.Client, target string) (*SitePage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	h := req.Header
	h.Set("User-Agent", siteUserAgent)
	h.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,application/json;q=0.9,*/*;q=0.8")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Sec-Ch-Ua", `"Chromium";v="141", "Not?A_Brand";v="8", "Google Chrome";v="141"`)
	h.Set("Sec-Ch-Ua-Mobile", "?0")
	h.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	h.Set("Sec-Fetch-Dest", "document")
	h.Set("Sec-Fetch-Mode", "navigate")
	h.Set("Sec-Fetch-Site", "none")
	h.Set("Sec-Fetch-User", "?1")
	h.Set("Upgrade-Insecure-Requests", "1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxSiteBody))
	final := resp.Request.URL
	return &SitePage{Status: resp.StatusCode, Host: final.Host, Path: final.Path, Header: resp.Header, Body: body}, nil
}

func classifySiteError(err error) string {
	if errors.Is(err, ErrNoRoutableAddress) {
		return "unreachable"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns"
	}
	return classifyProbeError(err)
}

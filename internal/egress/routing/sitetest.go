package routing

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Site tests open a real site through one outbound and judge whether the
// service works from that exit (an AI service limited by region, Google's
// abuse CAPTCHA). The service detectors follow the markers of the open source
// RegionRestrictionCheck script (source-reviewed at lmc999/RegionRestrictionCheck
// b6d4a6f, 2026-10-19); sites change their pages, so a detector that no longer
// recognizes the page reports "unknown", never a false "available".

// SiteTimeout bounds one site test (redirects included).
const SiteTimeout = 12 * time.Second

// maxSiteBody bounds the bytes read from one page.
const maxSiteBody = 2 << 20

// siteUserAgent is a current desktop browser: several services answer
// non-browser agents with a different page.
const siteUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

// Site verdicts.
const (
	SiteAvailable = "available" // the service works from this exit
	SiteBlocked   = "blocked"   // the service refuses this exit (region, VPN)
	SiteCaptcha   = "captcha"   // Google treats the exit as abusive
	SiteReachable = "reachable" // a custom URL answered (no service detector)
	SiteUnknown   = "unknown"   // the page did not match a known answer
	SiteFailed    = "failed"    // no answer: see Error
)

// SiteService is one known service check.
type SiteService struct {
	ID   string
	Name string
	URL  string
	// judge reads the final response; body is at most maxSiteBody bytes.
	judge func(final *url.URL, status int, body []byte) (verdict, region string)
}

var geminiRegion = regexp.MustCompile(`,2,1,200,"([A-Z]{3})"`)

// SiteServices is the display order.
var SiteServices = []SiteService{
	{ID: "gemini", Name: "Google Gemini", URL: "https://gemini.google.com/", judge: judgeGemini},
	{ID: "google", Name: "Google Search", URL: "https://www.google.com/search?q=wg-guard&hl=en", judge: judgeGoogleSearch},
	{ID: "chatgpt", Name: "ChatGPT", URL: "https://api.openai.com/compliance/cookie_requirements", judge: judgeOpenAI},
	{ID: "claude", Name: "Claude", URL: "https://claude.ai/", judge: judgeClaude},
}

// SiteServiceByID returns a known service.
func SiteServiceByID(id string) (SiteService, bool) {
	for _, s := range SiteServices {
		if s.ID == id {
			return s, true
		}
	}
	return SiteService{}, false
}

func judgeGemini(_ *url.URL, status int, body []byte) (string, string) {
	if status != http.StatusOK {
		return SiteUnknown, ""
	}
	region := ""
	if m := geminiRegion.FindSubmatch(body); m != nil {
		region = string(m[1])
	}
	if bytes.Contains(body, []byte("45631641,null,true")) {
		return SiteAvailable, region
	}
	// The app page without the availability flag is the region refusal
	// (the reviewed script's "No"); a page that is not the app at all is not
	// judged.
	if bytes.Contains(body, []byte("WIZ_global_data")) {
		return SiteBlocked, region
	}
	return SiteUnknown, region
}

var googleAbuse = regexp.MustCompile(`(?i)unusual traffic from|is blocked|unaddressed abuse`)

func judgeGoogleSearch(final *url.URL, status int, body []byte) (string, string) {
	if strings.HasPrefix(final.Path, "/sorry/") || status == http.StatusTooManyRequests || googleAbuse.Match(body) {
		return SiteCaptcha, ""
	}
	if status == http.StatusOK && bytes.Contains(body, []byte("wg-guard")) {
		return SiteAvailable, ""
	}
	return SiteUnknown, ""
}

func judgeOpenAI(_ *url.URL, status int, body []byte) (string, string) {
	if bytes.Contains(bytes.ToLower(body), []byte("unsupported_country")) {
		return SiteBlocked, ""
	}
	if status >= 200 && status < 500 && status != http.StatusForbidden {
		return SiteAvailable, ""
	}
	return SiteUnknown, ""
}

func judgeClaude(final *url.URL, status int, _ []byte) (string, string) {
	switch {
	case strings.Contains(final.Path, "app-unavailable-in-region"):
		return SiteBlocked, ""
	case final.Host == "claude.ai" && status < 400:
		return SiteAvailable, ""
	}
	return SiteUnknown, ""
}

// Phrases of common region and VPN refusals, matched in a custom URL's page.
var blockedPhrases = regexp.MustCompile(`(?i)not available in your (country|region)|isn't (available|supported) in your (country|region)|unsupported_country|app-unavailable-in-region|unusual traffic from|access denied|you have been blocked`)

func judgeCustom(_ *url.URL, status int, body []byte) (string, string) {
	if status == http.StatusForbidden || status == http.StatusUnavailableForLegalReasons || blockedPhrases.Match(body) {
		return SiteBlocked, ""
	}
	if status < 500 {
		return SiteReachable, ""
	}
	return SiteUnknown, ""
}

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
	Status   int    // final HTTP status (0 without an answer)
	Region   string // region the service reported, when it does
	Final    string // final host and path after redirects
	Family   string // ip4 | ip6: the family the final request used
	Latency  time.Duration
	Error    string // fixed category when Verdict is failed: timeout | unreachable | tls | unsupported
	TestedAt time.Time
}

// SiteTester runs site tests through outbound marks.
type SiteTester struct {
	Lookup Lookup
}

// Test fetches u (following up to five redirects) through mark and judges
// the final page with judge (nil: the generic custom-URL judge).
func (t SiteTester) Test(ctx context.Context, mark uint32, preferIPv6 bool, u *url.URL, svc *SiteService) SiteResult {
	start := time.Now()
	res := SiteResult{TestedAt: start.UTC()}
	ctx, cancel := context.WithTimeout(ctx, SiteTimeout)
	defer cancel()
	dialer := HostDialer{Mark: mark, PreferIPv6: preferIPv6, Timeout: ProbeTimeout, Lookup: t.Lookup}
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
			conn, family, err := dialer.Dial(ctx, host, uint16(port))
			if err == nil {
				res.Family = family
			}
			return conn, err
		},
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   ProbeTimeout,
		ResponseHeaderTimeout: SiteTimeout,
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || (req.URL.Scheme != "https" && req.URL.Scheme != "http") {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		res.Verdict, res.Error = SiteFailed, "unsupported"
		return res
	}
	req.Header.Set("User-Agent", siteUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := client.Do(req)
	if err != nil {
		res.Verdict, res.Error = SiteFailed, classifySiteError(err)
		return res
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxSiteBody))
	res.Latency = time.Since(start)
	res.Status = resp.StatusCode
	final := resp.Request.URL
	res.Final = clipFinal(final.Host + final.Path)
	judge := judgeCustom
	if svc != nil && svc.judge != nil {
		judge = svc.judge
	}
	res.Verdict, res.Region = judge(final, resp.StatusCode, body)
	return res
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

func clipFinal(s string) string {
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

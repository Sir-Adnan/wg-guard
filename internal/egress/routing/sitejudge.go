package routing

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// Site test services. Each detector reads the answer of one well-known page
// and decides whether the service works from the exit. The markers follow the
// open source RegionRestrictionCheck script (source-reviewed at
// lmc999/RegionRestrictionCheck b6d4a6f, 2026-10-19) where it still matches
// the live pages; services change their pages, so a detector that no longer
// recognizes the answer reports "unknown", never a false "available".

// SitePage is one fetched answer, as the detectors see it.
type SitePage struct {
	Status int
	Host   string // final host after redirects
	Path   string // final path after redirects
	Header http.Header
	Body   []byte // at most maxSiteBody bytes
}

// SiteJudgement is a detector's reading of the answer.
type SiteJudgement struct {
	Verdict string
	Region  string // country the service placed the exit in, when it says
	Note    string // short technical detail (network owner, page state)
}

// SiteService is one known service check.
type SiteService struct {
	ID    string
	Name  string
	Group string // ai | google | media | network: the picker's option group
	URL   string
	// Second, when set, is fetched as well and handed to judge (Netflix
	// compares a licensed title with an original).
	Second string
	judge  func(main, second *SitePage) SiteJudgement
}

// SiteGroups is the picker's group order.
var SiteGroups = []string{"ai", "google", "media", "network"}

// SiteServices is the display order.
var SiteServices = []SiteService{
	{ID: "gemini", Name: "Google Gemini", Group: "ai", URL: "https://gemini.google.com/", judge: judgeGemini},
	{ID: "chatgpt", Name: "ChatGPT", Group: "ai", URL: "https://api.openai.com/compliance/cookie_requirements", judge: judgeOpenAI},
	// claude.ai answers every non-browser client with a Cloudflare challenge,
	// so the test reads the country Cloudflare places the exit in from the
	// same site's trace endpoint and checks it against Anthropic's list.
	{ID: "claude", Name: "Claude", Group: "ai", URL: "https://claude.ai/cdn-cgi/trace", judge: judgeClaude},
	{ID: "google", Name: "Google Search", Group: "google", URL: "https://www.google.com/search?q=wg-guard&hl=en", judge: judgeGoogleSearch},
	{ID: "netflix", Name: "Netflix", Group: "media", URL: "https://www.netflix.com/title/81280792", Second: "https://www.netflix.com/title/70143836", judge: judgeNetflix},
	{ID: "iplocation", Name: "IP location", Group: "network", URL: "https://ipinfo.io/json", judge: judgeIPInfo},
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

var geminiRegion = regexp.MustCompile(`,2,1,200,"([A-Z]{3})"`)

func judgeGemini(p, _ *SitePage) SiteJudgement {
	if p.Status != http.StatusOK || !bytes.Contains(p.Body, []byte("WIZ_global_data")) {
		return SiteJudgement{Verdict: SiteUnknown}
	}
	region := ""
	if m := geminiRegion.FindSubmatch(p.Body); m != nil {
		region = string(m[1])
	}
	// The app page no longer carries the availability flag the reviewed
	// script reads (45631641,null,true), so the verdict follows the region
	// Google places the exit in. The page is the same in every country.
	switch {
	case region != "" && geminiUnsupported[region]:
		return SiteJudgement{Verdict: SiteBlocked, Region: region}
	case region != "":
		return SiteJudgement{Verdict: SiteAvailable, Region: region}
	case bytes.Contains(p.Body, []byte("45631641,null,true")):
		return SiteJudgement{Verdict: SiteAvailable}
	}
	return SiteJudgement{Verdict: SiteUnknown}
}

var googleAbuse = regexp.MustCompile(`(?i)unusual traffic from|is blocked|unaddressed abuse`)

func judgeGoogleSearch(p, _ *SitePage) SiteJudgement {
	if strings.HasPrefix(p.Path, "/sorry/") || p.Status == http.StatusTooManyRequests || googleAbuse.Match(p.Body) {
		return SiteJudgement{Verdict: SiteCaptcha}
	}
	if p.Status == http.StatusOK && bytes.Contains(p.Body, []byte("wg-guard")) {
		return SiteJudgement{Verdict: SiteAvailable}
	}
	return SiteJudgement{Verdict: SiteUnknown}
}

func judgeOpenAI(p, _ *SitePage) SiteJudgement {
	if bytes.Contains(bytes.ToLower(p.Body), []byte("unsupported_country")) {
		return SiteJudgement{Verdict: SiteBlocked}
	}
	if p.Status >= 200 && p.Status < 500 && p.Status != http.StatusForbidden {
		return SiteJudgement{Verdict: SiteAvailable}
	}
	return SiteJudgement{Verdict: SiteUnknown}
}

func judgeClaude(p, _ *SitePage) SiteJudgement {
	trace := parseTrace(p.Body)
	loc := trace["loc"]
	if p.Status != http.StatusOK || len(loc) != 2 || !isAlpha(loc) {
		return SiteJudgement{Verdict: SiteUnknown}
	}
	if anthropicSupported[loc] {
		return SiteJudgement{Verdict: SiteAvailable, Region: loc}
	}
	return SiteJudgement{Verdict: SiteBlocked, Region: loc}
}

var netflixRegion = regexp.MustCompile(`"id":"([A-Z]{2})","countryName"`)

// judgeNetflix compares a licensed title with a Netflix original: a catalog
// that has neither is "originals only" (Netflix serves the exit, but as an
// unknown or proxy region).
func judgeNetflix(p, second *SitePage) SiteJudgement {
	if p.Status >= 500 || second == nil {
		return SiteJudgement{Verdict: SiteUnknown}
	}
	missing := func(pg *SitePage) bool {
		return bytes.Contains(pg.Body, []byte("Oh no!")) || pg.Status == http.StatusNotFound
	}
	region := ""
	if m := netflixRegion.FindSubmatch(p.Body); m != nil {
		region = string(m[1])
	} else if m := netflixRegion.FindSubmatch(second.Body); m != nil {
		region = string(m[1])
	}
	switch {
	case missing(p) && missing(second):
		return SiteJudgement{Verdict: SiteLimited, Region: region}
	case region != "":
		return SiteJudgement{Verdict: SiteAvailable, Region: region}
	}
	return SiteJudgement{Verdict: SiteUnknown}
}

// judgeIPInfo reports where a common geolocation database places the exit
// and which network owns it: sites that refuse "VPN" addresses usually
// decide on exactly this.
func judgeIPInfo(p, _ *SitePage) SiteJudgement {
	var info struct {
		Country string `json:"country"`
		City    string `json:"city"`
		Org     string `json:"org"`
	}
	if p.Status != http.StatusOK || json.Unmarshal(p.Body, &info) != nil || len(info.Country) != 2 {
		return SiteJudgement{Verdict: SiteUnknown}
	}
	note := strings.TrimSpace(info.Org)
	if info.City != "" {
		if note != "" {
			note += " · "
		}
		note += info.City
	}
	return SiteJudgement{Verdict: SiteAvailable, Region: strings.ToUpper(info.Country), Note: clipText(note, 80)}
}

// Phrases of explicit region refusals, matched in a custom URL's page.
var blockedPhrases = regexp.MustCompile(`(?i)not available in your (country|region)|isn't (available|supported) in your (country|region)|unsupported_country|app-unavailable-in-region|not available in your location`)

// judgeCustom reads any site: an explicit region refusal is Blocked; a
// refusal without a reason (401, 403, 429, a firewall page) is Refused, since
// a site firewall often refuses scripted clients that a browser passes.
func judgeCustom(p, _ *SitePage) SiteJudgement {
	switch {
	case p.Status == http.StatusUnavailableForLegalReasons || blockedPhrases.Match(p.Body):
		return SiteJudgement{Verdict: SiteBlocked}
	case p.Status == http.StatusUnauthorized, p.Status == http.StatusForbidden, p.Status == http.StatusTooManyRequests:
		return SiteJudgement{Verdict: SiteRefused}
	case p.Status < 500:
		return SiteJudgement{Verdict: SiteReachable}
	}
	return SiteJudgement{Verdict: SiteUnknown}
}

// botCheck recognizes a bot challenge page (Cloudflare's "Just a moment",
// the managed challenge, other vendors' interstitials). The site answered and
// a browser passes the check, so it is neither a block nor a failure.
func botCheck(p *SitePage) bool {
	if strings.EqualFold(p.Header.Get("Cf-Mitigated"), "challenge") {
		return true
	}
	if p.Status != http.StatusForbidden && p.Status != http.StatusServiceUnavailable && p.Status != http.StatusTooManyRequests {
		return false
	}
	head := p.Body
	if len(head) > 64<<10 {
		head = head[:64<<10]
	}
	return challengeMarkers.Match(head)
}

var challengeMarkers = regexp.MustCompile(`(?i)<title>just a moment\.\.\.</title>|challenge-platform|_cf_chl_opt|cf-browser-verification|<title>attention required! \| cloudflare</title>|captcha-delivery\.com|px-captcha|/_incapsula_resource`)

// cloudflareServed reports whether Cloudflare answered for the site, so its
// trace endpoint on the same host reports the exit's country.
func cloudflareServed(p *SitePage) bool {
	return strings.EqualFold(p.Header.Get("Server"), "cloudflare") || p.Header.Get("Cf-Ray") != ""
}

// parseTrace reads Cloudflare's /cdn-cgi/trace key=value lines.
func parseTrace(body []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k != "" && len(out) < 32 {
			out[k] = v
		}
	}
	return out
}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

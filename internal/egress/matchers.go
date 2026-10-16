package egress

import (
	"net/netip"
	"regexp"
	"strings"
)

// MatcherType is one matcher syntax. The vocabulary follows the widely known
// v2fly/Xray routing syntax so lists copied from other panels work unchanged.
type MatcherType string

const (
	MatchDomain  MatcherType = "domain"  // domain and every subdomain (default for a bare name)
	MatchFull    MatcherType = "full"    // exactly this name
	MatchKeyword MatcherType = "keyword" // any name containing the text
	MatchRegexp  MatcherType = "regexp"  // RE2 expression over the full name
	MatchGeosite MatcherType = "geosite" // category from the installed geosite.dat
	MatchCIDR    MatcherType = "cidr"    // IPv4 address or prefix
	MatchGeoIP   MatcherType = "geoip"   // country/category from the installed geoip.dat
)

// Matcher is one parsed line.
type Matcher struct {
	Type  MatcherType
	Value string // normalized: lowercase domain, canonical prefix, category
}

// IsDomain reports whether the matcher is evaluated by the resolver (names)
// rather than by nftables (addresses).
func (m Matcher) IsDomain() bool {
	switch m.Type {
	case MatchDomain, MatchFull, MatchKeyword, MatchRegexp, MatchGeosite:
		return true
	}
	return false
}

// Limits keep one rule's compiled state small and bounded.
const (
	MaxMatcherLines  = 2000
	MaxMatcherBytes  = 64 << 10
	maxRegexpLen     = 256
	maxDomainLen     = 253
	maxCategoryLen   = 64
	maxRegexpPerRule = 32
)

var categoryRe = regexp.MustCompile(`^[a-z0-9][a-z0-9!@._-]*$`)

// ParseMatchers parses the rule text: one matcher per line, '#' starts a
// comment, blank lines are ignored. Supported forms:
//
//	example.com            domain and subdomains (same as domain:example.com)
//	domain:example.com     domain and subdomains
//	full:www.example.com   exact name
//	keyword:openai         any name containing "openai"
//	regexp:^.+\.ai$        RE2 over the name
//	geosite:google         geosite.dat category
//	1.2.3.4 / 1.2.3.0/24   IPv4 address or prefix (ip: or cidr: prefixes accepted)
//	geoip:ir               geoip.dat category
//
// Errors name the first invalid line (safe to display).
func ParseMatchers(text string) ([]Matcher, error) {
	if len(text) > MaxMatcherBytes {
		return nil, invalid("matchers", "matchers_too_large")
	}
	var out []Matcher
	seen := map[Matcher]bool{}
	regexps := 0
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		m, err := parseMatcher(line)
		if err != nil {
			return nil, err
		}
		if m.Type == MatchRegexp {
			regexps++
			if regexps > maxRegexpPerRule {
				return nil, invalid("matchers", "matchers_too_many_regexp")
			}
		}
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
		if len(out) > MaxMatcherLines {
			return nil, invalid("matchers", "matchers_too_large")
		}
	}
	return out, nil
}

func parseMatcher(line string) (Matcher, error) {
	if looksLikeAddress(line) {
		return parseAddress(line, line)
	}
	prefix, value, hasPrefix := strings.Cut(line, ":")
	if !hasPrefix || strings.ContainsAny(prefix, "./") {
		// A bare value: an address, a prefix or a domain name.
		return parseBare(line)
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	value = strings.TrimSpace(value)
	switch prefix {
	case "domain":
		d, ok := normalizeDomain(value)
		if !ok {
			return Matcher{}, invalidDetail("matchers", "matcher_invalid", line)
		}
		return Matcher{Type: MatchDomain, Value: d}, nil
	case "full":
		d, ok := normalizeDomain(value)
		if !ok {
			return Matcher{}, invalidDetail("matchers", "matcher_invalid", line)
		}
		return Matcher{Type: MatchFull, Value: d}, nil
	case "keyword":
		v := strings.ToLower(value)
		if v == "" || len(v) > maxDomainLen || strings.ContainsAny(v, " \t") {
			return Matcher{}, invalidDetail("matchers", "matcher_invalid", line)
		}
		return Matcher{Type: MatchKeyword, Value: v}, nil
	case "regexp", "regex":
		if value == "" || len(value) > maxRegexpLen {
			return Matcher{}, invalidDetail("matchers", "matcher_invalid", line)
		}
		if _, err := regexp.Compile(value); err != nil {
			return Matcher{}, invalidDetail("matchers", "matcher_invalid", line)
		}
		return Matcher{Type: MatchRegexp, Value: value}, nil
	case "geosite", "geoip":
		v := strings.ToLower(value)
		if v == "" || len(v) > maxCategoryLen || !categoryRe.MatchString(v) {
			return Matcher{}, invalidDetail("matchers", "matcher_invalid", line)
		}
		if prefix == "geosite" {
			return Matcher{Type: MatchGeosite, Value: v}, nil
		}
		return Matcher{Type: MatchGeoIP, Value: v}, nil
	case "ip", "cidr":
		return parseAddress(value, line)
	}
	return Matcher{}, invalidDetail("matchers", "matcher_invalid", line)
}

// looksLikeAddress reports whether line is meant as an IP address or prefix
// (so its address-specific error is reported rather than a domain error).
func looksLikeAddress(line string) bool {
	if _, err := netip.ParsePrefix(line); err == nil {
		return true
	}
	_, err := netip.ParseAddr(line)
	return err == nil
}

func parseBare(line string) (Matcher, error) {
	if strings.ContainsAny(line, "/:") {
		return Matcher{}, invalidDetail("matchers", "matcher_invalid", line)
	}
	d, ok := normalizeDomain(line)
	if !ok {
		return Matcher{}, invalidDetail("matchers", "matcher_invalid", line)
	}
	return Matcher{Type: MatchDomain, Value: d}, nil
}

func parseAddress(value, line string) (Matcher, error) {
	var prefix netip.Prefix
	if strings.Contains(value, "/") {
		p, err := netip.ParsePrefix(value)
		if err != nil {
			return Matcher{}, invalidDetail("matchers", "matcher_invalid", line)
		}
		prefix = p.Masked()
	} else {
		a, err := netip.ParseAddr(value)
		if err != nil {
			return Matcher{}, invalidDetail("matchers", "matcher_invalid", line)
		}
		prefix = netip.PrefixFrom(a, a.BitLen())
	}
	if !prefix.Addr().Is4() {
		// IPv6 is out of the tunnel scope (client AllowedIPs are IPv4).
		return Matcher{}, invalidDetail("matchers", "matcher_ipv6", line)
	}
	if prefix.Bits() == 0 {
		// "Everything" is the default target, not a rule.
		return Matcher{}, invalidDetail("matchers", "matcher_everything", line)
	}
	return Matcher{Type: MatchCIDR, Value: prefix.String()}, nil
}

// normalizeDomain lowercases, strips a leading "*." or "." and a trailing dot,
// and checks label syntax (letters, digits, hyphen, underscore).
func normalizeDomain(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "*.")
	s = strings.TrimPrefix(s, ".")
	s = strings.TrimSuffix(s, ".")
	if s == "" || len(s) > maxDomainLen {
		return "", false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 {
			return "", false
		}
		for i, r := range label {
			ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' ||
				(r == '-' && i > 0 && i < len(label)-1)
			if !ok {
				return "", false
			}
		}
	}
	return s, true
}

// NormalizeDomain exposes the resolver's name normalization (a trailing dot
// and case are not significant).
func NormalizeDomain(s string) (string, bool) { return normalizeDomain(s) }

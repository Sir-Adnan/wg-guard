// Package domaintls defines approved HTTP origins, hostname roles and certificate
// identity. It performs no host mutation or certificate enrollment from requests.
package domaintls

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const Schema = 1
const MaxPolicyBytes = 32 << 10
const MaxMaterialBytes = 256 << 10

type Role string
type Method string

const (
	Panel        Role   = "panel"
	Subscription Role   = "subscription"
	Automatic    Method = "automatic"
	Manual       Method = "manual"
	External     Method = "external"
	Builtin      Method = "builtin"
)

var ErrPolicy = errors.New("domain policy is invalid or unavailable")
var IDPattern = regexp.MustCompile(`\A[0-9a-f]{32}\z`)

type Origin struct {
	URL  string `json:"url"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

type Site struct {
	Role          Role   `json:"role"`
	Origin        string `json:"origin"`
	Method        Method `json:"method"`
	CertificateID string `json:"certificate_id,omitempty"`
	// LegacyLineage preserves an already owned Certbot renewal subscription.
	// New enrollments always use the deterministic domain-specific lineage.
	LegacyLineage string `json:"legacy_lineage,omitempty"`
	Challenge     string `json:"challenge,omitempty"`
}

// Policy is written only by the trusted host manager and mounted read-only.
// Pending requests/issuance receipts never expand the runtime allowlist.
type Policy struct {
	Schema   int    `json:"schema"`
	Revision string `json:"revision"`
	Sites    []Site `json:"sites"`
}

func (r Role) Valid() bool   { return r == Panel || r == Subscription }
func (m Method) Valid() bool { return m == Automatic || m == Manual || m == External || m == Builtin }

func Hostname(input string) (string, error) {
	s := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(input), "."))
	if ip := net.ParseIP(s); ip != nil {
		return ip.String(), nil
	}
	if len(s) == 0 || len(s) > 253 || strings.ContainsAny(s, " /\\:@?#\t\r\n\x00") {
		return "", ErrPolicy
	}
	if !strings.Contains(s, ".") {
		return "", ErrPolicy
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrPolicy
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", ErrPolicy
			}
		}
	}
	return s, nil
}

func ParseOrigin(input string) (Origin, error) {
	u, err := url.Parse(strings.TrimSpace(input))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || u.RawPath != "" || strings.HasSuffix(u.Host, ":") || u.Path != "" && u.Path != "/" {
		return Origin{}, ErrPolicy
	}
	host, err := Hostname(u.Hostname())
	if err != nil {
		return Origin{}, ErrPolicy
	}
	port := 443
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return Origin{}, ErrPolicy
		}
	}
	authority := host
	if strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	if port != 443 {
		authority = net.JoinHostPort(host, strconv.Itoa(port))
	}
	return Origin{URL: "https://" + authority, Host: host, Port: port}, nil
}

func (p Policy) Validate() error {
	if p.Schema != Schema || !IDPattern.MatchString(p.Revision) || len(p.Sites) < 1 || len(p.Sites) > 2 {
		return ErrPolicy
	}
	seen := map[Role]bool{}
	for _, site := range p.Sites {
		o, err := ParseOrigin(site.Origin)
		if err != nil || o.URL != site.Origin || !site.Role.Valid() || seen[site.Role] || !site.Method.Valid() {
			return ErrPolicy
		}
		if site.Method == Automatic && site.Challenge != "http" && site.Challenge != "cloudflare" || site.Method != Automatic && site.Challenge != "" {
			return ErrPolicy
		}
		if site.LegacyLineage != "" && (site.Method != Automatic || site.LegacyLineage != lineage(o.Host, "wg-guard-")) {
			return ErrPolicy
		}
		if (site.Method == External || site.Method == Builtin) && site.CertificateID != "" || site.Method != External && site.Method != Builtin && !IDPattern.MatchString(site.CertificateID) {
			return ErrPolicy
		}
		seen[site.Role] = true
	}
	if !seen[Panel] {
		return ErrPolicy
	}
	if sub, ok := p.Site(Subscription); ok && sub.Origin == p.PanelOrigin() {
		return ErrPolicy
	}
	return nil
}

func lineage(host, prefix string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(host))))
	return prefix + hex.EncodeToString(sum[:6])
}
func DomainLineage(host string) string { return lineage(host, "wg-guard-domain-") }
func RenewalLineage(site Site) string {
	if site.LegacyLineage != "" {
		return site.LegacyLineage
	}
	o, _ := ParseOrigin(site.Origin)
	return DomainLineage(o.Host)
}

func (p Policy) Site(role Role) (Site, bool) {
	for _, site := range p.Sites {
		if site.Role == role {
			return site, true
		}
	}
	return Site{}, false
}

func (p Policy) PanelOrigin() string { s, _ := p.Site(Panel); return s.Origin }
func (p Policy) SubscriptionOrigin() string {
	if s, ok := p.Site(Subscription); ok {
		return s.Origin
	}
	return p.PanelOrigin()
}

// PublicPath is a closed GET/HEAD surface. Validate before ServeMux can clean or
// redirect a traversal path. Preferences remain GET parameters on /sub itself.
func PublicPath(value string) bool {
	if value == "" || path.Clean(value) != value || strings.ContainsAny(value, "\\%\x00") || strings.Contains(value, "//") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(value, "/"), "/")
	if len(parts) >= 2 && parts[0] == "assets" {
		return true
	}
	if len(parts) == 2 && parts[0] == "sub" && parts[1] != "" {
		return true
	}
	return len(parts) == 5 && parts[0] == "sub" && parts[1] != "" && parts[2] == "devices" && parts[3] != "" && (parts[4] == "config" || parts[4] == "qr")
}

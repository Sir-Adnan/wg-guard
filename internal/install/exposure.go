package install

import (
	"context"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/config"
)

// ExposureMode describes who accepts the panel's public connection. It is
// deliberately separate from the application's runtime TLS mode.
type ExposureMode string

const (
	ExposureAuto          ExposureMode = "auto"
	ExposurePrivate       ExposureMode = "private"
	ExposureDirect        ExposureMode = "direct"
	ExposureNginx         ExposureMode = "nginx"
	ExposureExternalProxy ExposureMode = "external-proxy"
)

func (m ExposureMode) Valid() bool {
	switch m {
	case ExposurePrivate, ExposureDirect, ExposureNginx, ExposureExternalProxy:
		return true
	}
	return false
}

// CertificateSource describes certificate ownership/issuance. The empty
// value is intentional for private access, where no certificate exists.
type CertificateSource string

const (
	CertificateAuto             CertificateSource = "auto"
	CertificateBuiltin          CertificateSource = "builtin"
	CertificateWebroot          CertificateSource = "webroot"
	CertificateCloudflareDNS    CertificateSource = "cloudflare-dns"
	CertificateIP               CertificateSource = "ip"
	CertificateManual           CertificateSource = "manual"
	CertificateCloudflareOrigin CertificateSource = "cloudflare-origin"
	CertificateExternal         CertificateSource = "external"
)

func (s CertificateSource) Valid() bool {
	switch s {
	case CertificateBuiltin, CertificateWebroot, CertificateCloudflareDNS,
		CertificateIP, CertificateManual, CertificateCloudflareOrigin, CertificateExternal:
		return true
	}
	return false
}

const (
	NginxConfigPath       = "/etc/nginx/conf.d/wg-guard.conf"
	ACMEWebrootPath       = "/var/www/wg-guard-acme"
	ManagedTLSDir         = EtcDir + "/tls"
	ManagedCertPath       = ManagedTLSDir + "/fullchain.pem"
	ManagedKeyPath        = ManagedTLSDir + "/privkey.pem"
	CloudflareTokenPath   = ManagedTLSDir + "/cloudflare.ini"
	CertbotDeployHookPath = "/etc/letsencrypt/renewal-hooks/deploy/wg-guard"
)

// ExposureFacts is a bounded, read-only snapshot of the host surface used by
// automatic derivation. Unknown is never treated as free or safe.
type ExposureFacts struct {
	HTTPPortFree        bool
	HTTPSPortFree       bool
	BackendPort         int
	NginxInstalled      bool
	NginxActive         bool
	NginxStandard       bool
	NginxDomainConflict bool
}

var standardNginxInclude = regexp.MustCompile(`(?m)\binclude\s+/etc/nginx/conf\.d/\*\.conf\s*;`)
var cloudflareAPIToken = regexp.MustCompile(`\A[A-Za-z0-9_-]{20,256}\z`)

func validACMEEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Contains(value, "@")
}

// ReadCloudflareToken accepts one bounded root-private API token. It rejects
// symlinks on the real host and returns only the in-memory secret value.
func ReadCloudflareToken(h Host, filename string) (string, error) {
	if !filepath.IsAbs(filename) && !strings.HasPrefix(filename, "/") {
		return "", fmt.Errorf("installer: Cloudflare token file must be an absolute private file")
	}
	_, isRealHost := h.(realHost)
	if isRealHost {
		if err := safeHostPath(filename); err != nil {
			return "", fmt.Errorf("installer: Cloudflare token file is unsafe")
		}
	}
	info, err := h.Stat(filename)
	if err != nil || !info.Mode().IsRegular() || (!isRealHost || runtime.GOOS != "windows") && info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("installer: Cloudflare token file must be a regular root-private file (0600)")
	}
	f, err := h.Open(filename)
	if err != nil {
		return "", fmt.Errorf("installer: Cloudflare token file is unreadable")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(raw) > 4096 {
		return "", fmt.Errorf("installer: Cloudflare token file exceeds the safety limit")
	}
	token := strings.TrimSpace(string(raw))
	clear(raw)
	if !cloudflareAPIToken.MatchString(token) {
		return "", fmt.Errorf("installer: Cloudflare token has an invalid format")
	}
	return token, nil
}

// InspectExposure observes only the standard public ports, a small loopback
// backend range, and the effective Nginx configuration. It performs no writes
// and never stops a listener.
func InspectExposure(ctx context.Context, h Host, domain string) (ExposureFacts, error) {
	facts := ExposureFacts{
		HTTPPortFree:  h.PortFree(":80"),
		HTTPSPortFree: h.PortFree(":443"),
	}
	for port := 8080; port <= 8099; port++ {
		if h.PortFree("127.0.0.1:" + strconv.Itoa(port)) {
			facts.BackendPort = port
			break
		}
	}
	if _, err := h.LookPath("nginx"); err != nil {
		return facts, nil
	}
	facts.NginxInstalled = true
	active, err := h.Output(ctx, []string{"systemctl", "is-active", "nginx.service"}, 10*time.Second)
	if err == nil && strings.TrimSpace(active) == "active" {
		facts.NginxActive = true
	}
	effective, err := h.Output(ctx, []string{"nginx", "-T"}, 15*time.Second)
	if err != nil {
		return facts, nil
	}
	if len(effective) > 1<<20 {
		return facts, fmt.Errorf("installer: effective Nginx configuration exceeds the inspection limit")
	}
	facts.NginxStandard = standardNginxInclude.MatchString(effective)
	if domain != "" {
		facts.NginxDomainConflict = nginxNamesDomain(effective, strings.ToLower(strings.TrimSpace(domain)))
	}
	return facts, nil
}

func nginxNamesDomain(configuration, domain string) bool {
	for _, line := range strings.Split(configuration, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		for {
			at := strings.Index(line, "server_name")
			if at < 0 {
				break
			}
			line = line[at+len("server_name"):]
			end := strings.IndexByte(line, ';')
			if end < 0 {
				break
			}
			for _, name := range strings.Fields(line[:end]) {
				if strings.EqualFold(strings.TrimSpace(name), domain) {
					return true
				}
			}
			line = line[end+1:]
		}
	}
	return false
}

// ResolveExposure turns intent plus observed facts into one concrete and
// secure runtime topology. There is intentionally no public-HTTP result.
func ResolveExposure(p Plan, facts ExposureFacts) (Plan, error) {
	if p.Exposure == "" {
		p.Exposure = ExposureAuto
	}
	if p.Certificate == "" {
		p.Certificate = CertificateAuto
	}

	// Preserve legacy TLS flags as explicit intent.
	if p.Exposure == ExposureAuto {
		switch p.TLSMode {
		case config.TLSModeACME:
			p.Exposure, p.Certificate = ExposureDirect, CertificateBuiltin
		case config.TLSModeManual:
			p.Exposure, p.Certificate = ExposureDirect, CertificateManual
		case config.TLSModeProxy:
			p.Exposure = ExposurePrivate
			if strings.TrimSpace(p.Domain) != "" {
				p.Exposure, p.Certificate = ExposureExternalProxy, CertificateExternal
			}
		}
	}
	if p.Exposure == ExposureAuto {
		switch p.Certificate {
		case CertificateBuiltin, CertificateIP:
			p.Exposure = ExposureDirect
		case CertificateWebroot, CertificateCloudflareOrigin:
			p.Exposure = ExposureNginx
		case CertificateExternal:
			p.Exposure = ExposureExternalProxy
		}
	}

	if p.Exposure == ExposureAuto {
		switch {
		case strings.TrimSpace(p.Domain) == "":
			p.Exposure = ExposurePrivate
		case facts.NginxDomainConflict:
			return p, fmt.Errorf("installer: Nginx already owns %s; choose another hostname or configure the existing virtual host", p.Domain)
		case facts.HTTPPortFree && facts.HTTPSPortFree:
			p.Exposure = ExposureDirect
			if p.Certificate == CertificateAuto {
				p.Certificate = CertificateBuiltin
			}
		case facts.NginxInstalled && facts.NginxActive && facts.NginxStandard:
			p.Exposure = ExposureNginx
			if p.Certificate == CertificateAuto {
				p.Certificate = CertificateWebroot
			}
		default:
			return p, fmt.Errorf("installer: public ports are occupied by an unsupported or unverified service; use private access, DNS-01, or an operator-managed reverse proxy")
		}
	}

	if p.PublicPort == 0 {
		p.PublicPort = 443
	}
	backend := p.PanelPort
	if !p.PanelPortExplicit && p.PanelPort == 8080 && facts.BackendPort != 0 && (p.Exposure == ExposureNginx || p.Exposure == ExposureExternalProxy) {
		backend = facts.BackendPort
	}
	switch p.Exposure {
	case ExposurePrivate:
		if p.Certificate != CertificateAuto && p.Certificate != "" {
			return p, fmt.Errorf("installer: private access does not use a certificate")
		}
		p.Certificate = ""
		p.TLSMode = config.TLSModeProxy
		p.PanelPort = backend
		p.PublicPort = 0
	case ExposureDirect:
		if p.Certificate == CertificateAuto {
			if p.Domain != "" {
				p.Certificate = CertificateBuiltin
			} else {
				p.Certificate = CertificateIP
			}
		}
		if !p.PanelPortExplicit && p.PanelPort == 8080 {
			p.PanelPort = p.PublicPort
		}
		p.PublicPort = p.PanelPort
		switch p.Certificate {
		case CertificateBuiltin:
			if p.Domain == "" || !facts.HTTPPortFree || !facts.HTTPSPortFree && p.PanelPort == 443 {
				return p, fmt.Errorf("installer: built-in HTTPS needs a domain and free challenge/TLS ports")
			}
			p.TLSMode = config.TLSModeACME
		case CertificateIP:
			if p.PublicIP == "" || !facts.HTTPPortFree || !facts.HTTPSPortFree && p.PanelPort == 443 {
				return p, fmt.Errorf("installer: public-IP HTTPS needs the server IP and an available HTTP-01 path")
			}
			p.TLSMode = config.TLSModeManual
			p.CertFile, p.KeyFile = ManagedCertPath, ManagedKeyPath
		case CertificateCloudflareDNS:
			if p.Domain == "" {
				return p, fmt.Errorf("installer: Cloudflare DNS-01 requires a domain")
			}
			p.TLSMode = config.TLSModeManual
			p.CertFile, p.KeyFile = ManagedCertPath, ManagedKeyPath
		case CertificateManual:
			p.TLSMode = config.TLSModeManual
		default:
			return p, fmt.Errorf("installer: the selected certificate source cannot provide direct browser-trusted HTTPS")
		}
	case ExposureNginx:
		if p.Domain == "" || !facts.NginxInstalled || !facts.NginxActive || !facts.NginxStandard || facts.NginxDomainConflict {
			return p, fmt.Errorf("installer: managed Nginx needs the active standard Ubuntu layout and an unused exact hostname")
		}
		if facts.BackendPort == 0 && !p.PanelPortExplicit {
			return p, fmt.Errorf("installer: no free loopback backend port is available in 8080-8099")
		}
		if p.Certificate == CertificateAuto {
			p.Certificate = CertificateWebroot
		}
		switch p.Certificate {
		case CertificateWebroot, CertificateCloudflareDNS, CertificateManual, CertificateCloudflareOrigin:
		default:
			return p, fmt.Errorf("installer: the selected certificate source is incompatible with managed Nginx")
		}
		p.PanelPort = backend
		p.TLSMode = config.TLSModeProxy
	case ExposureExternalProxy:
		if p.Domain == "" {
			return p, fmt.Errorf("installer: an external HTTPS proxy needs a panel hostname")
		}
		if p.Certificate != CertificateAuto && p.Certificate != CertificateExternal {
			return p, fmt.Errorf("installer: the external proxy must own its certificate")
		}
		p.Certificate = CertificateExternal
		p.PanelPort = backend
		p.TLSMode = config.TLSModeProxy
	default:
		return p, fmt.Errorf("installer: unknown panel exposure %q", p.Exposure)
	}
	return p, nil
}

// PublicURL is empty for private-only access and HTTPS for every public mode.
func (p Plan) PublicURL() string {
	if p.Exposure == ExposurePrivate || p.Exposure == "" || p.Exposure == ExposureAuto {
		return ""
	}
	host := p.Domain
	if host == "" {
		host = p.PublicIP
	}
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := p.PublicPort
	if p.Exposure == ExposureDirect {
		port = p.PanelPort
	}
	suffix := ""
	if port != 0 && port != 443 {
		suffix = ":" + strconv.Itoa(port)
	}
	return "https://" + host + suffix
}

func validHTTPSURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}

// ExposureState is the non-secret, deterministic lifecycle ownership record.
type ExposureState struct {
	Mode            ExposureMode      `json:"mode,omitempty"`
	Certificate     CertificateSource `json:"certificate,omitempty"`
	PublicURL       string            `json:"public_url,omitempty"`
	BackendPort     int               `json:"backend_port,omitempty"`
	PublicPort      int               `json:"public_port,omitempty"`
	NginxConfigPath string            `json:"nginx_config_path,omitempty"`
	ACMEWebroot     string            `json:"acme_webroot,omitempty"`
	CertFile        string            `json:"cert_file,omitempty"`
	KeyFile         string            `json:"key_file,omitempty"`
	DeployHook      string            `json:"deploy_hook,omitempty"`
}

func (p Plan) ExposureRecord() ExposureState {
	s := ExposureState{Mode: p.Exposure, Certificate: p.Certificate, PublicURL: p.PublicURL(), BackendPort: p.PanelPort, PublicPort: p.PublicPort}
	if p.Exposure == ExposurePrivate {
		s.PublicPort = 0
	}
	if p.Exposure == ExposureNginx {
		s.NginxConfigPath, s.ACMEWebroot = NginxConfigPath, ACMEWebrootPath
	}
	switch p.Certificate {
	case CertificateWebroot, CertificateCloudflareDNS, CertificateIP:
		s.CertFile, s.KeyFile, s.DeployHook = ManagedCertPath, ManagedKeyPath, CertbotDeployHookPath
	}
	return s
}
